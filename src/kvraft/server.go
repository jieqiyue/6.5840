package kvraft

import (
	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raft"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const Debug = false
const Debugt = false
const ExecuteTimeout = 500 * time.Millisecond

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

func DPrintft(format string, a ...interface{}) (n int, err error) {
	if Debugt {
		log.Printf("kv:"+format, a...)
	}
	return
}

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Op    OperationOp
	Key   string
	Value string

	ClientId int64
	ReqSeq   int64
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	lastApplied int
	// k/v存储核心数据结构
	store map[string]string
	// clientId -->maxReqId,给每一个clientId记录一下已经处理到哪个reqId了。防止重复处理客户端请求。实现exactly once语义。
	clientMaxReq map[int64]int64
	// clientId --> lastReqRest，缓存这个客户端上一次处理的请求的结果，因为客户端是按照顺序请求的，上一个请求没有结束的时候，一定不会开始下一个
	// 请求。所以当发现reqId小于clientMaxReq给这个客户端存的值的时候，就可以认为是过时的请求，直接丢弃。
	clientLastReqRest map[int64]*ClientRequestReply
	// 通知的channel表示该请求处理完成了。由于applier和请求不是在同一个goroutine里面处理的
	notifyChannel map[int]chan *ClientRequestReply
}

// 由于如果read只进一次日志的话，要做很多特殊判断。比如说查看上一次的read的通知channel是否存在。并且继续等待它结束等等。
// 所以使用另外一种处理方式，如果op是read的话，就每次都进日志。非read的话，如果已经进过一次日志了，就不再进日志了。毕竟read操作就算是重复发送
// 只要能够返回这个时间段内的值，都可以认为是线性一致性的。
func (kv *KVServer) HandlerClientRequest(args *ClientRequestArgs, reply *ClientRequestReply) {
	// Your code here.
	kv.mu.Lock()
	//DPrintf("server[%d]begin to HandlerClientRequest,args clientId is:%d, req seq is:%d", kv.me, args.ClientId, args.ReqSeq)
	if maxReq, ok := kv.clientMaxReq[args.ClientId]; ok && maxReq >= args.ReqSeq && args.Op != OpGet {
		reply.Err = Duplicate
		kv.mu.Unlock()
		return
	}
	kv.mu.Unlock()
	// 组装日志，进日志
	opLog := Op{
		Op:       args.Op,
		Key:      args.Key,
		Value:    args.Value,
		ClientId: args.ClientId,
		ReqSeq:   args.ReqSeq,
	}
	// 由于控制不了kv.clientMaxReq的设置时机，所以需要在apply的时候，进行判断，防止执行两次相同的操作。
	index, _, ok := kv.rf.Start(opLog)
	if !ok {
		reply.Err = WrongLeader
		return
	}

	// 获取通知的channel，并且阻塞在这个通知上面获取结果.这个地方不会出现日志同步的太快，导致那边applier的时候，这个channel还没有被创建出来
	// 导致没法发送消息。因为这个GetNotifyChannel函数在applier的时候也会调用。所以也有可能是那边创建的channel。
	DPrintf("server[%d]start a log success,args clientId is:%d, req seq is:%d", kv.me, args.ClientId, args.ReqSeq)
	kv.mu.Lock()
	start := time.Now().UnixMilli()
	DPrintft("in HandlerClientRequest, msg success send to raft, time is:%v", start)
	notifyCh := kv.GetNotifyChannel(index)
	kv.mu.Unlock()

	select {
	case result := <-notifyCh:
		reply.Value, reply.Err = result.Value, result.Err
		start := time.Now().UnixMilli()
		DPrintft("in HandlerClientRequest, notifyCh said opLog apply success, time is:%v", start)
		DPrintf("server[%d]got a notifyCh,reply value:%v,args clientId is:%v, args req seq is:%d", kv.me, reply.Value, args.ClientId, args.ReqSeq)
	case <-time.After(ExecuteTimeout):
		DPrintf("server[%d]got a notifyCh,time out,args clientId is:%v, args req seq is:%d", kv.me, args.ClientId, args.ReqSeq)
		reply.Err = TimeOut
	}

	// 由于select会阻塞，如果运行到这个地方的时候，已经可以把这个channel给删除了
	go func(index int) {
		kv.mu.Lock()
		defer kv.mu.Unlock()
		kv.RemoveNotifyChannel(index)
	}(index)
}

func (kv *KVServer) ApplyTicket() {
	for kv.killed() == false {
		select {
		case applyMsg := <-kv.applyCh:
			DPrintf("server[%d]kv got a applyMsg,applyMsg is:%v", kv.me, applyMsg)
			if applyMsg.CommandValid {
				// 仅仅通过这个kv.lastApplied是不能够完全防止执行两次客户端的相同的opLog的。因为客户端可能发送了两次opLog，这两个opLog是
				// 不同的index。
				kv.mu.Lock()
				if applyMsg.CommandIndex <= kv.lastApplied {
					DPrintf("server[%d]found index:%d have applied,so discard it", kv.me, applyMsg.CommandIndex)
					kv.mu.Unlock()
					continue
				}

				kv.lastApplied = applyMsg.CommandIndex
				// 到这里就可以应用到状态机了
				opLog := applyMsg.Command.(Op)
				reply := &ClientRequestReply{}
				DPrintf("server[%d]ApplyTicket begin to apply a log,log is:%v,opLog reqSeq is:%d,kv clientMaxReq:%v",
					kv.me, opLog, opLog.ReqSeq, kv.clientMaxReq)
				if index, ok := kv.clientMaxReq[opLog.ClientId]; ok && index >= opLog.ReqSeq {
					reply = kv.clientLastReqRest[opLog.ClientId]
				} else {
					reply = kv.ApplyToStateMachine(opLog)
					DPrintf("server[%d]success apply opLog:%v to state machine, reply value is:%v", kv.me, opLog, reply.Value)
					kv.clientLastReqRest[opLog.ClientId] = reply
					kv.clientMaxReq[opLog.ClientId] = opLog.ReqSeq
				}

				if _, isLeader := kv.rf.GetState(); isLeader {
					notifyCh := kv.GetNotifyChannel(applyMsg.CommandIndex)
					notifyCh <- reply
				}

				kv.mu.Unlock()
				DPrintf("server[%d]finish opLog:%v, unlock success", kv.me, opLog)
			} else if applyMsg.SnapshotValid {

			} else {
				DPrintf("server[%d] error, unknow opLog type:%v", kv.me, applyMsg)
			}
		}
	}
}

func (kv *KVServer) ApplyToStateMachine(opLog Op) *ClientRequestReply {
	reply := ClientRequestReply{}
	var err Err
	var value string
	switch opLog.Op {
	case OpGet:
		value, err = kv.StateMachineGet(opLog.Key)
	case OpPut:
		err = kv.StateMachinePut(opLog.Key, opLog.Value)
	case OpAppend:
		err = kv.StateMachineAppend(opLog.Key, opLog.Value)
	}

	reply.Err = err
	reply.Value = value
	return &reply
}

func (kv *KVServer) GetNotifyChannel(index int) chan *ClientRequestReply {
	if _, ok := kv.notifyChannel[index]; !ok {
		kv.notifyChannel[index] = make(chan *ClientRequestReply, 1)
	}

	return kv.notifyChannel[index]
}

func (kv *KVServer) RemoveNotifyChannel(index int) {
	delete(kv.notifyChannel, index)
}

func (kv *KVServer) StateMachineGet(key string) (string, Err) {
	if value, ok := kv.store[key]; ok {
		return value, OK
	}

	return "", NoKey
}

func (kv *KVServer) StateMachinePut(key, value string) Err {
	kv.store[key] = value
	return OK
}

func (kv *KVServer) StateMachineAppend(key, value string) Err {
	kv.store[key] += value
	return OK
}

// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

// StartKVServer servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Op{})

	kv := new(KVServer)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// You may need initialization code here.

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	// You may need initialization code here.
	kv.lastApplied = -1
	kv.notifyChannel = make(map[int]chan *ClientRequestReply)
	kv.store = make(map[string]string)
	kv.clientMaxReq = make(map[int64]int64)
	kv.clientLastReqRest = make(map[int64]*ClientRequestReply)

	go kv.ApplyTicket()

	return kv
}

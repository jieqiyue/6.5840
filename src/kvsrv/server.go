package kvsrv

import (
	"fmt"
	"log"
	"sync"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

type KVServer struct {
	mu sync.Mutex

	// Your definitions here.
	store map[string]string

	// key为clientId，value为这个client的前面多少个请求都处理成功了
	clientAck map[int64]int
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if !kv.CheckReadOpNecessary(args, reply) {
		return
	}

	if value, ok := kv.store[args.Key]; ok {
		reply.Value = value
	} else {
		// 即使map中没有这个key，也要返回一个空字符串
		reply.Value = ""
	}
	DPrintf("after master get function, get a result %s", reply.Value)
	// map操作先不更新clientAck
	kv.MaintainRState(args, reply)
	return
}

func (kv *KVServer) Put(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if !kv.CheckWriteOpNecessary(args, reply) {
		return
	}

	if value, ok := kv.store[args.Key]; ok {
		reply.Value = value
	} else {
		// 即使map中没有这个key，也要返回一个空字符串
		reply.Value = ""
	}

	kv.store[args.Key] = args.Value

	// 在处理成功之后，维护这个请求的client的状态
	kv.MaintainState(args, reply)
}

func (kv *KVServer) Append(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.

	kv.mu.Lock()
	defer kv.mu.Unlock()

	if !kv.CheckWriteOpNecessary(args, reply) {
		return
	}

	if value, ok := kv.store[args.Key]; ok {
		reply.Value = value
		kv.store[args.Key] = value + args.Value
	} else {
		// 即使map中没有这个key，也要返回一个空字符串
		reply.Value = ""
		kv.store[args.Key] = args.Value
	}

	// 在处理成功之后，维护这个请求的client的状态
	kv.MaintainState(args, reply)
}

func (kv *KVServer) MaintainState(args *PutAppendArgs, reply *PutAppendReply) {
	reply.OpResult = true

	// 如果map中没有这个clientId，可以认为是这个client首次发送的请求，此时直接设置ackSeq
	if _, ok := kv.clientAck[args.ClientId]; !ok {
		kv.clientAck[args.ClientId] = args.AckSeq
		fmt.Println("first to modify clientid seq,clientid:", args.ClientId, "after modify:", kv.clientAck[args.ClientId], "args ack is:", args.AckSeq)
		return
	}

	if _, ok := kv.clientAck[args.ClientId]; ok && kv.clientAck[args.ClientId] < args.AckSeq {
		fmt.Println("begin to modify clientid seq,before clientid :", args.ClientId, "seq:", kv.clientAck[args.ClientId], "op")
		kv.clientAck[args.ClientId] = args.AckSeq
		fmt.Println("begin to modify clientid seq,after clientid :", args.ClientId, "seq:", kv.clientAck[args.ClientId])

		return
	}

	fmt.Println("master error, MaintainState unreach place........")
}

func (kv *KVServer) MaintainRState(args *GetArgs, reply *GetReply) {
	reply.OpResult = true

	// 如果map中没有这个clientId，可以认为是这个client首次发送的请求，此时直接设置ackSeq
	if _, ok := kv.clientAck[args.ClientId]; !ok {
		kv.clientAck[args.ClientId] = args.AckSeq
		fmt.Println("first to modify clientid seq,clientid:", args.ClientId, "after modify:", kv.clientAck[args.ClientId], "args ack is:", args.AckSeq)
		return
	}

	if _, ok := kv.clientAck[args.ClientId]; ok && kv.clientAck[args.ClientId] < args.AckSeq {
		fmt.Println("begin to modify clientid seq,before clientid :", args.ClientId, "seq:", kv.clientAck[args.ClientId], "op")
		kv.clientAck[args.ClientId] = args.AckSeq
		fmt.Println("begin to modify clientid seq,after clientid :", args.ClientId, "seq:", kv.clientAck[args.ClientId])

		return
	}

	fmt.Println("master error, MaintainState unreach place........")
}

func (kv *KVServer) CheckWriteOpNecessary(args *PutAppendArgs, reply *PutAppendReply) bool {
	if args.AckSeq < 0 {
		fmt.Println("clientid: ", args.ClientId, "return false,because ackseq less 0")
		return false
	}

	// 如果map中没有这个clientId，可以认为是这个client首次发送的请求，此时有必要进行OP操作
	if _, ok := kv.clientAck[args.ClientId]; !ok {
		if args.AckSeq == 0 {
			return true
		}

		reply.OpResult = false
		fmt.Println("clientid: ", args.ClientId, "seq ack:", args.AckSeq, "return false,because the first package is not 0")
		return false
	}

	// 如果map中存放的这个clientId最后处理的ackSeq大于请求过来的ackS，那么可以认为这个请求是重复请求，直接丢弃
	if value, ok := kv.clientAck[args.ClientId]; ok {
		if value+1 == args.AckSeq {
			return true
			// todo 如果请求的ack seq小于master维护的ack seq，那么可以认为是之前的请求重传，但是此时已经将这个op进行了，所以此处要return，并且
			// reply.OpResult要设置为true，防止一直重试
		} else if args.AckSeq <= value {
			// 此时要认为这个op已经被master做完了，因为client请求的ack seq小于等于当前保存的已经做完的ack seq
			// 此时需要返回reply.OpResult = true,让客户端不要重复发送请求。并且这里要返回false。不再重复这个op。
			reply.OpResult = true
			return false
		} else {
			reply.OpResult = false
			fmt.Println("clientid: ", args.ClientId, "seq ack:", args.AckSeq, "master maintain seq is", value, "return false,because the first package seq ack is not plus 1")
			return false
		}
	}

	DPrintf("error,CheckWriteOpNecessary unreached place...")
	return true
}

func (kv *KVServer) CheckReadOpNecessary(args *GetArgs, reply *GetReply) bool {
	if args.AckSeq < 0 {
		fmt.Println("clientid: ", args.ClientId, "return false,because ackseq less 0")
		return false
	}

	// 如果map中没有这个clientId，可以认为是这个client首次发送的请求，此时有必要进行OP操作
	if _, ok := kv.clientAck[args.ClientId]; !ok {
		if args.AckSeq == 0 {
			return true
		}

		//reply.OpResult = false
		fmt.Println("CheckReadOpNecessary clientid: ", args.ClientId, "seq ack:", args.AckSeq, "return false,because the first package is not 0")
		return false
	}

	// 如果map中存放的这个clientId最后处理的ackSeq大于请求过来的ackS，那么可以认为这个请求是重复请求，直接丢弃
	if value, ok := kv.clientAck[args.ClientId]; ok {
		if value+1 == args.AckSeq {
			return true
			// todo 如果请求的ack seq小于master维护的ack seq，那么可以认为是之前的请求重传，但是此时已经将这个op进行了，所以此处要return，并且
			// reply.OpResult要设置为true，防止一直重试
		} else if args.AckSeq <= value {
			// 可能是重传之前的请求
			fmt.Println("client CheckReadOpNecessary unreach place..... ")
			return true
		} else {
			// 说明这个是超前请求，要回复让client重发
			reply.OpResult = false
			fmt.Println("clientid: ", args.ClientId, "seq ack:", args.AckSeq, "master maintain seq is", value, "return false,because the first package seq ack is not plus 1")
			return false
		}
	}

	fmt.Println("error,CheckReadOpNecessary unreached place...")
	DPrintf("error,CheckReadOpNecessary unreached place...")
	return true
}

func StartKVServer() *KVServer {
	kv := new(KVServer)

	// You may need initialization code here.
	kv.clientAck = make(map[int64]int)
	kv.store = make(map[string]string)

	return kv
}

package kvraft

import (
	"6.5840/labrpc"
	"sync"
	"time"
)
import "crypto/rand"
import "math/big"

type Clerk struct {
	servers []*labrpc.ClientEnd
	// You will have to modify this struct.

	clientId int64
	// 这个标识了当前Clerk下一次要发送的请求的ID，是递增的
	reqSeq   int64
	leaderId int
	mu       sync.Mutex
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// You'll have to add code here.
	// 使用随机数生成clientId，避免不同的client生成了同样的id
	ck.clientId = nrand()
	// 序列号从0开始
	ck.reqSeq = 0
	ck.leaderId = 0

	return ck
}

// Get fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer."+op, &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
func (ck *Clerk) Get(key string) string {
	// You will have to modify this function.
	return ck.SendClientRequest(key, "", OpGet)
}

// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
func (ck *Clerk) PutAppend(key string, value string, op string) {
	// You will have to modify this function.

	if op == "Put" {
		ck.SendClientRequest(key, value, OpPut)
	}

	if op == "Append" {
		ck.SendClientRequest(key, value, OpAppend)
	}
}

func (ck *Clerk) Put(key string, value string) {
	DPrintf("client Clerk begin to put key:%v and value:%v to server", key, value)
	ck.PutAppend(key, value, "Put")
}

func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}

func (ck *Clerk) SendClientRequest(key string, value string, op OperationOp) string {
	args := ClientRequestArgs{
		Op:       op,
		Key:      key,
		Value:    value,
		ClientId: ck.clientId,
		ReqSeq:   ck.reqSeq,
	}

	DPrintf("client begin to send a request:%v,op is:%v,key is:%v,value is:%v,clientId is:%v,reqSeq is:%v",
		args, args.Op, args.Key, args.Value, args.ClientId, args.ReqSeq)
	start := time.Now().UnixMilli()
	DPrintft("in SendClientRequest, before send msg, time is:%v,op:%v,key:%v,value:%v", start, args.Op, args.Key, args.Value)
	for true {
		// 一直请求，直到这个请求成功为止
		reply := ClientRequestReply{}
		ok := ck.servers[ck.leaderId].Call("KVServer.HandlerClientRequest", &args, &reply)
		if !ok || reply.Err == WrongLeader || reply.Err == TimeOut {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
			continue
		}

		// 能到这里代表这个请求处理成功了，此时需要增加reqSeq为下一次请求做准备
		ck.reqSeq++

		return reply.Value
	}

	return ""
}

package kvsrv

import (
	"6.5840/labrpc"
	"fmt"
	"sync"
)
import "crypto/rand"
import "math/big"

type Clerk struct {
	server *labrpc.ClientEnd
	// You will have to modify this struct.

	clientId int64
	// 这个标识了当前Clerk下一次要发送的请求的ID，是递增的
	ackSeq int

	lock sync.Mutex
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(server *labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.server = server
	// You'll have to add code here.
	ck.ackSeq = 0
	ck.clientId = nrand()

	return ck
}

// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.server.Call("KVServer.Get", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
func (ck *Clerk) Get(key string) string {
	DPrintf("client begin do Get function")
	// You will have to modify this function.
	ck.lock.Lock()
	args := GetArgs{
		ClientId: ck.clientId,
		AckSeq:   ck.ackSeq,
		Key:      key,
	}

	ck.ackSeq++
	ck.lock.Unlock()
	reply := GetReply{}

	ok := false
	for !ok {
		ok = ck.server.Call("KVServer.Get", &args, &reply)
		fmt.Println("[client log]client id:", args.ClientId, "request for seq:", args.AckSeq)

		if ok && !reply.OpResult {
			ok = false
		}
	}

	//ck.ackSeq++

	//fmt.Println("client id:", ck.clientId, "success finish get job")
	DPrintf("%s\n", "client get reply is "+reply.Value)

	return reply.Value
}

// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.server.Call("KVServer."+op, &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
func (ck *Clerk) PutAppend(key string, value string, op string) string {
	// You will have to modify this function.
	ck.lock.Lock()
	args := PutAppendArgs{
		Key:      key,
		Value:    value,
		ClientId: ck.clientId,
		// 在这里初始化一次，然后下面重试的话，每次都使用这个AckSeq
		AckSeq: ck.ackSeq,
	}
	ck.ackSeq++
	ck.lock.Unlock()

	reply := PutAppendReply{}
	ok := false

	fmt.Println("[client log]client begin to reqeust op:", op, "clientId:", args.ClientId, "seq:", args.AckSeq)
	for !ok {
		//DPrintf("client.PutAppend receiver a fail reply, but not retry")
		ok = ck.server.Call("KVServer."+op, &args, &reply)

		// 如果ok返回true，并且OpResult返回的是true，那证明这个op已经在master上面被操作了，此时不要重试
		if ok && !reply.OpResult {
			fmt.Println("[client log]package not loss, master return false for client id:", ck.clientId, "request id:", ck.ackSeq)
			ok = false
		}
	}

	fmt.Println("[client log] client id:", ck.clientId, "success finish op:", op, " and ack seq is:", ck.ackSeq)
	// 当成功之后，进行递增
	//ck.ackSeq++

	return reply.Value
}

func (ck *Clerk) Put(key string, value string) {
	DPrintf("%s\n", "client begin do Put function")
	ck.PutAppend(key, value, "Put")
}

// Append value to key's value and return that value
func (ck *Clerk) Append(key string, value string) string {
	DPrintf("%s\n", "client begin do Append function")
	return ck.PutAppend(key, value, "Append")
}

package kvraft

type Err string

const (
	OK          = "OK"
	NoKey       = "ErrNoKey"
	WrongLeader = "ErrWrongLeader"
	TimeOut     = "TimeOut"
	Duplicate   = "Duplicate"
)

type OperationOp uint8

const (
	OpPut OperationOp = iota
	OpAppend
	OpGet
)

// Put or Append
type PutAppendArgs struct {
	Key   string
	Value string
	// You'll have to add definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
}

type PutAppendReply struct {
	Err Err
}

type GetArgs struct {
	Key string
	// You'll have to add definitions here.
}

type GetReply struct {
	Err   Err
	Value string
}

type ClientRequestArgs struct {
	Op    OperationOp
	Key   string
	Value string

	ClientId int64
	ReqSeq   int64
}

type ClientRequestReply struct {
	Err   Err
	Value string
}

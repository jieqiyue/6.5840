package raft

type HeartBeatArgs struct {
	Term        int
	CandidateId int
}

type HeartBeatReply struct {
	Term int
}

type MsgType int

const (
	UnKnowMgsType MsgType = iota
	HeartBeatMsgType
	LogMsgType
)

// {40 0 230 38 [{40 231 8368273227940715514} {40 232 6261130192811087240}] 232 2 false}
type SendLogArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int

	// 不在和follower进行log复制的时候产生实际作用
	MsgType            MsgType
	ShouldSendSnapShot bool // 标识这次是否需要发送snapshot
}

type SendLogReply struct {
	Term    int
	Success bool

	// 是否需要一个logIndex返回给leader，让leader更加方便的更新自己的nextIndex
	SavedLogIndex int
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

type RequestSnapShotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Offset            int
	Data              []byte
	Done              bool
}

type RequestSnapShotReply struct {
	Term int
}

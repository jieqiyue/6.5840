package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (Index, Term, isleader)
//   start agreement on a new log entry
// rf.GetState() (Term, isLeader)
//   ask a Raft for its current Term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"fmt"
	"math"

	"6.5840/labgob"

	//	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.5840/labgob"
	"6.5840/labrpc"
)

// ApplyMsg as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 3D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 3D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

// LogEntry 用来表示每条日志
type LogEntry struct {
	Term  int         //  这个日志发出的leader的term是什么
	Index int         // 这个日志在整体日志中的位置
	Data  interface{} // 存储的LogEntry的数据
}

type ServerState int

const (
	UnknownState ServerState = iota // 未知状态
	Leader                          // 领导者
	Follower                        // 跟随者
	Candidate                       // 候选者
)

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's Index into peers[]
	dead      int32               // set by Kill()

	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	applyCh      chan ApplyMsg
	applyCond    *sync.Cond
	applySeqMu   sync.Mutex
	applySeqCond *sync.Cond

	state      ServerState
	receiveNew bool

	startElectionTime time.Time
	electionInterval  int

	// persistent state
	currentTerm int        // 当前任期
	voteFor     int        // 当前任期的选票投给了哪个server
	log         []LogEntry // 当前这个server中的所有日志
	snapshot    []byte     // 快照

	// volatile state
	commitIndex int // 当前可以应用到状态机的日志为止
	lastApplied int // 当前server已经应用到状态机的位置

	// volatile state on leader
	nextIndex  []int // 下一条要发送到各个client的日志的位置，指示的是log数组的下标
	matchIndex []int // 目前每个client已经匹配到的位置
}

// SetElectionTime 不使用锁保护，需要调用者保证安全
// 1. 在投了赞成票的时候
// 2. 接收到leader的log（心跳）之后
// 3. 接收到一个reply大于自己的term的时候，转变为follower
// 4. 开始一次选举之后
func (rf *Raft) SetElectionTime(interval int) {
	rf.startElectionTime = time.Now()
	rf.electionInterval = rand.Intn(200) + 150 + interval

	return
}

// GetState return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	var term int
	var isleader bool

	isleader = false

	rf.mu.Lock()
	term = rf.currentTerm
	if rf.state == Leader {
		isleader = true
	}
	rf.mu.Unlock()

	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	if e.Encode(rf.currentTerm) != nil || e.Encode(rf.voteFor) != nil || e.Encode(rf.log) != nil {
		fmt.Printf("when persist server[%d] state, error, the raft is:%v", rf.me, rf)
	}

	raftstate := w.Bytes()
	rf.persister.Save(raftstate, rf.snapshot)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}

	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var voteFor int
	var log []LogEntry
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&voteFor) != nil ||
		d.Decode(&log) != nil {
		fmt.Printf("server [%d] when decode state from persist fail", rf.me)
	} else {
		rf.currentTerm = currentTerm
		rf.voteFor = voteFor
		rf.log = log

		rf.lastApplied = rf.GetFirstLog().Index
		rf.commitIndex = rf.GetFirstLog().Index
		DPrintf("server[%d]restart,the lastapplied is:%d, the commitIndex is:%d,the log is:%v", rf.me, rf.lastApplied, rf.commitIndex, rf.log)
	}
}

// Snapshot the service says it has created a snapshot that has
// all info up to and including Index. this means the
// service no longer needs the log through (and including)
// that Index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	DPrintf("server[%d]reciver a snapshot from top service,index is:%d", rf.me, index)
	firstLog := rf.GetFirstLog()
	lastLog := rf.GetLastLog()
	if index <= firstLog.Index {
		DPrintf("server[%d]try to snapShot,but args index:%d is less then local first log index:%d", rf.me, index, firstLog.Index)
		return
	}

	if index > lastLog.Index {
		DPrintf("server[%d]try to snapShot,but args index:%d is bigger then local last log index:%d", rf.me, index, lastLog.Index)
		return
	}

	// 需要截断从log的index为传入的这个index这个log之后的日志，需要计算出这个index日志在本地的位置
	dummyLog := LogEntry{
		Term:  rf.log[index-firstLog.Index].Term,
		Index: rf.log[index-firstLog.Index].Index,
		Data:  nil,
	}

	tempLogEntry := make([]LogEntry, 0)
	tempLogEntry = append(tempLogEntry, dummyLog)
	tempLogEntry = append(tempLogEntry, rf.log[index-firstLog.Index+1:]...)
	DPrintf("server[%d] is snapshot in index:%d, now dummy log is:%v, "+
		"and new log is:%v", rf.me, index, dummyLog, tempLogEntry)
	rf.log = tempLogEntry
	rf.snapshot = snapshot

	rf.persist()
	DPrintf("server[%d]finish a snapshot,now first log index is:%d, term is:%d, now i have:%v, the snapshot is:%v", rf.me, rf.GetFirstLog().Index, rf.GetFirstLog().Term, rf.log, rf.snapshot)
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// 该函数是否需要先计算一下请求的args里面的日志和本地日志，哪个更新？先得出这个结论来，
	// 因为如果仅仅靠着大term，是不能够获取到选票的。获取选票的关键还是要看日志哪个更新。因为有可能一个网络分区的节点，一直在投票，导致自己的term
	// 非常大，然后忽然不分区了，重新加入集群之后，就会出现这种情况。
	rf.mu.Lock()
	DPrintf("server[%d] recive %d server vote request, args Term is:%d, get lock success", rf.me, args.CandidateId, args.Term)
	defer func() {
		if reply.VoteGranted == true {
			//rf.receiveNew = true
			rf.SetElectionTime(300)
			rf.state = Follower
			rf.persist()
		}
		DPrintf("server[%d] has finish request vote, request server is:%d, args Term is:%d, result granted is:%v", rf.me, args.CandidateId, args.Term, reply.VoteGranted)
		rf.mu.Unlock()
	}()

	DPrintf("server[%d] begin to handler vote request, local Term is:%d, request server is:%d, request Term is:%d, request log last term is:%d"+
		", request log last index is:%d", rf.me, rf.currentTerm, args.CandidateId, args.Term, args.LastLogTerm, args.LastLogIndex)
	reply.Term = rf.currentTerm
	// 先判断两种特殊情况
	// 1. 如果请求的term小于当前server的term，则返回false
	if args.Term < rf.currentTerm {
		reply.VoteGranted = false
		DPrintf("server[%d]not granted to server %d, because args Term less than me", rf.me, args.CandidateId)
		return
	}

	// 这种情况应该要先处理，就是当请求的term大于当前的term的时候
	if args.Term > rf.currentTerm {
		rf.AddCurrentTerm(args.Term)
		DPrintf("server[%d]handler %d vote request, args term is:%d bigger than local term:%d, so make self to follower,and votefor is:%d,status is:%d",
			rf.me, args.CandidateId, args.Term, rf.currentTerm, rf.voteFor, rf.state)
	}

	// 2. 如果请求的term等于当前的term，并且自己当前的voteFor字段等于args里面的candidateId
	if args.Term == rf.currentTerm && rf.voteFor == args.CandidateId {
		rf.voteFor = args.CandidateId
		//rf.receiveNew = true
		reply.VoteGranted = true
		return
	}

	// 3. 如果当前term已经投票过了的话，那么就返回false,因为一个任期最多只能投票一次
	if rf.voteFor != -1 {
		reply.VoteGranted = false
		DPrintf("server[%d]not granted to server %d, because server has vote to %d", rf.me, args.CandidateId, rf.voteFor)
		return
	}

	// 接下来判断日志是否至少跟自己一样新
	localLastIndex := -1
	localLastTerm := -1

	if len(rf.log) != 0 {
		localLastIndex = rf.GetLastLog().Index
		localLastTerm = rf.GetLastLog().Term
	}

	// 这里的判断不能使用args.Term >= localLastTerm && args.LastLogIndex >= localLastIndex，而是要使用
	if args.LastLogTerm > localLastTerm || (args.LastLogTerm == localLastTerm && args.LastLogIndex >= localLastIndex) {
		rf.voteFor = args.CandidateId
		reply.VoteGranted = true
	} else {
		DPrintf("server[%d]not granted to server %d, because local Index is new than args", rf.me, args.CandidateId)
		reply.VoteGranted = false
	}
}

func (rf *Raft) RequestHeartBeat(args *HeartBeatArgs, reply *HeartBeatReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		DPrintf("server[%d] got a heart beat, but args Term less than local Term, request Term is:%d, request server id is:%d", rf.me, args.Term, args.CandidateId)
		return
	}

	if args.Term == rf.currentTerm && rf.state == Candidate {
		DPrintf("server[%d] got a heart beat, local status is Candidate, but args server:%d send a heart beat,so change local state to follower", rf.me, args.CandidateId)
		rf.state = Follower
	}

	// 当请求的term大于本地的term的时候，就证明自己收到了更高的term的心跳，此时应该将自身转为follower，并且修改自身的term。
	if args.Term > rf.currentTerm {
		DPrintf("server[%d] got a heart beat, and args Term is bigger than local Term, args Term:%d, args server id:%d, local Term:%d", rf.me, args.Term, args.CandidateId, rf.currentTerm)
		rf.AddCurrentTerm(args.Term)
		return
	}

	rf.SetElectionTime(200)
	DPrintf("server[%d]got heart beat, reset timeout, local Term is:%d, local status is:%d, args server id is:%d, args Term is:%d", rf.me, rf.currentTerm, rf.state, args.CandidateId, args.Term)
}

func (rf *Raft) RequestInstallSnapShot(args *RequestSnapShotArgs, reply *RequestSnapShotReply) {
	rf.mu.Lock()
	defer func() {
		rf.persist()
		rf.mu.Unlock()
	}()

	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		DPrintf("server[%d] reciver a snapshot but found term:%d is less then local term:%d", rf.me, args.Term, rf.currentTerm)
		return
	}

	if args.Term > rf.currentTerm {
		rf.AddCurrentTerm(args.Term)
	}

	if rf.state == Candidate {
		DPrintf("server[%d]got a append entries rpc, but current status is candidate,so change state to follower", rf.me)
		rf.state = Follower
	}

	// 如果LastIncludedIndex小于等于rf.log[0].index的话，那么保留所有的日志并返回
	if args.LastIncludedIndex <= rf.GetFirstLog().Index ||
		(args.LastIncludedIndex <= rf.GetLastLog().Index && rf.log[args.LastIncludedIndex-rf.GetFirstLog().Index].Term == args.LastIncludedTerm) {
		DPrintf("server[%d]discoad a snapshot,args lastIndex is:%d,args last term is:%d, local first log index is:%d, local last log index is:%d",
			rf.me, args.LastIncludedIndex, args.LastIncludedTerm, rf.GetFirstLog().Index, rf.GetLastLog().Index)
		return
	}

	// 需要丢弃所有日志的情况：1. 日志对不上term 2. args.LastIncludedIndex大于最后一条日志的index
	if args.LastIncludedIndex > rf.GetLastLog().Index ||
		(args.LastIncludedIndex <= rf.GetLastLog().Index && rf.log[args.LastIncludedIndex-rf.GetFirstLog().Index].Term != args.LastIncludedTerm) {
		rf.log = make([]LogEntry, 0)
		rf.log = append(rf.log, LogEntry{
			Data:  nil,
			Index: args.LastIncludedIndex,
			Term:  args.LastIncludedTerm,
		})
		rf.snapshot = args.Data

		// 在apply的时候，需要使用锁保护，使得一个时刻只能有一个goroutine去apply消息。因为可能出现这种情况：老的普通消息，比如说50-100这
		// 一段的，一直在apply。然后某一个时刻，如果150这个位置被leader发送了一个snapshot。那么是不需要去再次apply150之前的任何消息的。
		// 但是这个goroutine已经在运行了。那么不能停止他。所以还是要用一个锁的。
		// 如果是普通消息一直apply，然后上层忽然来了个snapshot的请求。
		// 那么这种情况不会出现问题的。因为在这种情况下，不需要发送applyMsg到上层。
		go func(snapShotIndex int, snapShotTerm int) {
			rf.applySeqMu.Lock()
			applyMsg := ApplyMsg{
				SnapshotValid: true,
				Snapshot:      rf.snapshot,
				SnapshotIndex: snapShotIndex,
				SnapshotTerm:  snapShotTerm,
			}

			rf.applyCh <- applyMsg

			rf.mu.Lock()
			if args.LastIncludedIndex > rf.lastApplied {
				rf.lastApplied = args.LastIncludedIndex
			}

			if args.LastIncludedIndex > rf.commitIndex {
				rf.commitIndex = args.LastIncludedIndex
				rf.applyCond.Signal()
			}
			DPrintf("server[%d]send snapshot to applyCh success,now lastapplied is:%d,commitIndex is:%d", rf.me, rf.lastApplied, rf.commitIndex)
			rf.mu.Unlock()

			rf.applySeqCond.Signal()
			rf.applySeqMu.Unlock()
		}(args.LastIncludedIndex, args.LastIncludedTerm)

		rf.SetElectionTime(200)
		//DPrintf("server[%d]after install snapshot,and lastapplied is:%d, commitIndex is:%d,have %v log", rf.me, rf.lastApplied, rf.commitIndex, rf.log)
		return
	}

	DPrintf("server[%d] error,RequestInstallSnapShot should never reach place....", rf.me)
}

func (rf *Raft) GetLastLog() LogEntry {
	return rf.log[len(rf.log)-1]
}

func (rf *Raft) GetFirstLog() LogEntry {
	return rf.log[0]
}

func (rf *Raft) RequestSendLog(args *SendLogArgs, reply *SendLogReply) {
	DPrintf("server[%d]got a append entries rpc, args server is:%d, args term is:%d,not get lock", rf.me, args.LeaderId, args.Term)
	rf.mu.Lock()
	DPrintf("server[%d]got a append entries rpc,local status is:%d, args server is:%d, args term is:%d,get lock success", rf.me, rf.state, args.LeaderId, args.Term)
	defer func() {
		reply.Term = rf.currentTerm
		// 将重置定时器的代码放在defer里面，防止某些特殊情况提前return的时候，没有执行到这个重置定时器的代码
		if args.Term >= rf.currentTerm {
			rf.SetElectionTime(200)
		}

		DPrintf("server[%d]got a append entries rpc, reset timeout, local Term is:%d, local status is:%d, args server id is:%d, args Term is:%d,now i have:%v log, commit index is:%d",
			rf.me, rf.currentTerm, rf.state, args.LeaderId, args.Term, rf.log, rf.commitIndex)
		rf.mu.Unlock()
	}()

	// 1. 首先判断args的term，如果term小于自己的term，则直接返回
	if args.Term < rf.currentTerm {
		DPrintf("server[%d] got a append entries rpc, but args Term less than local Term, request Term is:%d, current term is:%d, request server id is:%d", rf.me, args.Term, rf.currentTerm, args.LeaderId)
		return
	}

	// 2. 如果请求的term大于等于本地的term，则需要将自己转为follower，并且更新自己的term
	if args.Term > rf.currentTerm {
		rf.AddCurrentTerm(args.Term)
	}

	// 如果一个follower落后的太多的话，直接返回false
	if args.PrevLogIndex > rf.GetLastLog().Index {
		reply.Success = false
		DPrintf("server[%d]discard %d server send heart log beat, because log is too new, local log dot not contain pre log, pre log term:%d, pre log index:%d",
			rf.me, args.LeaderId, args.PrevLogTerm, args.PrevLogIndex)
		return
	}

	if args.PrevLogIndex < rf.GetFirstLog().Index && len(args.Entries) > 0 {
		DPrintf("server[%d]got a append entries rpc,but preLogIndex:%d is less than local first log index:%d,so terncate", rf.me, args.PrevLogIndex, rf.GetFirstLog().Index)
		// 获取到这个请求的最后一条日志，查看是否小于snapshot的index，或者是term能够和已有的对上了
		argsLastLogEntry := args.Entries[len(args.Entries)-1]
		if argsLastLogEntry.Index <= rf.GetFirstLog().Index {
			reply.Success = true
			return
		}

		// 寻找到请求的Entries里面的第一条大于rf.log[0].index这个位置的日志,这个截取是必要的。
		for firstBig := 0; firstBig < len(args.Entries); firstBig++ {
			if args.Entries[firstBig].Index > rf.GetFirstLog().Index {
				args.Entries = args.Entries[firstBig:]
				break
			}
		}

		args.PrevLogIndex = rf.GetFirstLog().Index
		args.PrevLogTerm = rf.GetFirstLog().Term
	}

	// 当preLog是dummy log的时候，一定是能够符合的吧
	if args.PrevLogIndex > rf.GetFirstLog().Index && rf.log[args.PrevLogIndex-rf.GetFirstLog().Index].Term != args.PrevLogTerm {
		DPrintf("server[%d]got %d server log request,and args preIndex is:%d, args preTerm is:%d, local index in %d ",
			rf.me, args.LeaderId, args.PrevLogIndex, args.PrevLogTerm, args.PrevLogIndex)
		reply.Success = false
		return
	}

	// 3. 这个代码得放在处理当本地term大于args term之后，因为当本地term大于args term的时候，可能是在选举，那么低term就不能影响高term的选举。
	if rf.state == Candidate {
		DPrintf("server[%d]got a append entries rpc, but current status is candidate,so change state to follower", rf.me)
		rf.state = Follower
	}

	// 由于切片截取不包括最后的那个下标，所以这里要+1
	if len(args.Entries) > 0 {
		argsLastLog := args.Entries[len(args.Entries)-1]
		if !(rf.GetLastLog().Index >= argsLastLog.Index && rf.log[argsLastLog.Index-rf.GetFirstLog().Index].Term == argsLastLog.Term) {
			rf.log = append(rf.log[:(args.PrevLogIndex-rf.GetFirstLog().Index)+1], args.Entries...)
		}
	}

	rf.persist()

	// 更新commitIndex,这个分支虽然可能leader并没有发送消息过来，但是还是需要进。因为可能要可以更新自己的commitIndex了。
	if args.LeaderCommit > rf.commitIndex {
		newCommitIndex := int(math.Min(float64(args.LeaderCommit), float64(rf.GetLastLog().Index)))
		if newCommitIndex > rf.commitIndex {
			rf.commitIndex = newCommitIndex
			rf.applyCond.Signal()
		}
	}
	DPrintft("follower[%d]append a msg to local,time is:%v", rf.me, time.Now().UnixMilli())
	reply.Success = true
}

// 此方法不使用锁保护，防止锁重入出现panic，需要调用方保证线程安全性
func (rf *Raft) AddCurrentTerm(term int) {
	// 验证代码，一般不会出现这种情况
	if term <= rf.currentTerm {
		DPrintf("error, AddCurrentTerm function get a %d Term, current Term is %d, this is not happen", term, rf.currentTerm)
		return
	}

	// 新增任期，然后将term和state进行调整
	rf.currentTerm = term
	rf.state = Follower
	// 虽然这个voteFor没有明确在论文中指出需要设置为什么值，这里我考虑是需要设置为-1的
	rf.voteFor = -1
	// 当一个分区的follower，它一直在自我选举，导致自身的term很大。在重新加入集群之后，新的leader给它发送心跳之后，或者是接收到它的投票请求之后，会更新
	// 自身的term为这个follower的term。并且此时会来执行这个SetElectionTime，又由于这个SetElectionTime设置的超时时间是800ms+的，但是那边分区出去的
	// follower是每隔150ms就要重新选举一次，导致每次都是这个分区的follower先发起选举。但是又由于它没有最新的日志，导致成为不了leader。但是其他有最新log的
	// 又由于定时器没有超时所以不进行选举。造成集群出问题了。
	rf.SetElectionTime(0)
	rf.persist()
}

// example code to send a RequestVote RPC to a server.
// server is the Index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendHeartBeat(server int, args *HeartBeatArgs, reply *HeartBeatReply) bool {
	ok := rf.peers[server].Call("Raft.RequestHeartBeat", args, reply)
	return ok
}

func (rf *Raft) sendLog(server int, args *SendLogArgs, reply *SendLogReply) bool {
	ok := rf.peers[server].Call("Raft.RequestSendLog", args, reply)
	return ok
}

func (rf *Raft) sendInstallSnapShot(server int, args *RequestSnapShotArgs, reply *RequestSnapShotReply) bool {
	ok := rf.peers[server].Call("Raft.RequestInstallSnapShot", args, reply)
	return ok
}

// 获取到最后一条日志，因为有dummy log，所以不用担心下标的问题
//func (rf *Raft) getLastLog() LogEntry {
//	return rf.log[len(rf.log)-1]
//}

// Start the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the Index that the command will appear at
// if it's ever committed. the second return value is the current
// Term. the third return value is true if this server believes it is
// the leader.
// Start() should return immediately, without waiting for the log appends to complete.
// 思路是在接收到上层的command之后，转化为一个Log，然后保存到本地。这里先直接返回，然后在另外一个专门分发Log的goroutine中
// 将这个日志fan out出去。另外那个专门分发Log的goroutine可以固定sleep 100ms，然后每次醒了之后，就判断自己是否是leader，
// 如果不是leader，则继续睡觉，如果是leader，则遍历rf里面的所有peer，给每一个follower和candidate发送日志。发送日志的时候，
// 给每一个不同的server启动一个新的goroutine来发送，这样就等于睡醒了之后，给每一个其他server发送完消息就不管了，继续去睡觉。如果其他server
// 有回应的话，就是在新建的那个goroutine中来处理的。比如说更新nextIndex，加锁去更新。这样就算某一个节点特别慢，也不会拖累整体的流程。因为每
// 一个server都是在独立的goroutine中进行处理的。并且就算某一个节点特别慢，导致第二次唤醒之后，又给他发了相同的日志，那么就需要他自己来保证这个
// 幂等性了。
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	// Your code here (3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != Leader {
		return 0, rf.currentTerm, false
	}

	// 将command转化为一个LogEntry结构体
	log := LogEntry{
		Data: command,
		Term: rf.currentTerm,
	}

	// 如果本地的Log中已经有日志了，则新日志的index需要比原有的最后一条日志的index加一
	log.Index = rf.GetLastLog().Index + 1

	// 此时LogEntry已经构建好了，将它添加到Raft中的log数组中
	rf.log = append(rf.log, log)
	rf.matchIndex[rf.me] = log.Index
	rf.persist()

	go rf.sendLogTicketCore()
	DPrintf("server[%d]get a log, current term is:%d, current state is:%v, all log is:%v", rf.me, rf.currentTerm, rf.state, rf.log)

	return log.Index, log.Term, true
}

// Kill the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// heartBeat 废弃的方法，已经将心跳和发送日志合二为一了。
func (rf *Raft) heartBeat() {
	// 周期性的发送心跳消息，暂时不对返回值做处理
	shouldSendHeartBeat := false
	for rf.killed() == false {
		heartBeatArgs := HeartBeatArgs{}

		rf.mu.Lock()
		shouldSendHeartBeat = false
		if rf.state == Leader {
			shouldSendHeartBeat = true
			heartBeatArgs.CandidateId = rf.me
			heartBeatArgs.Term = rf.currentTerm
		}
		// for log
		serverId := rf.me
		serverState := rf.state
		// end
		rf.mu.Unlock()

		if shouldSendHeartBeat {
			for i, _ := range rf.peers {
				if i == rf.me {
					continue
				}

				go func(i int, args HeartBeatArgs) {
					reply := HeartBeatReply{}
					ok := rf.sendHeartBeat(i, &args, &reply)
					if !ok {
						// 针对已经不是leader的server还是会发心跳这个问题，可以把这个请求的args里面的term给打印出来，这样就能知道了
						DPrintf("server[%d] send heart beat to %d, bug get fail,i am a leader? status:%v, args Term is:%d", serverId, i, serverState, args.Term)
					}
					// todo 此处先不根据返回值来修改当前server的term值，有可能出现这样的情况：一个leader，独自发生了分区，然后他一直给
					// todo 其它的server发送心跳。此时其它server已经进行了新的选举了，所以其它server的term可能比当前server的大，但是
					// todo 此处并不根据这个心跳返回的term来更新本地server，而是希望通过新领导者的appendEntries或者是心跳请求来更新本地
					// todo 的term。
				}(i, heartBeatArgs)
			}
		}

		ms := 150
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

func (rf *Raft) sendSnapShot(peer int) {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}

	sendLeaderTerm := rf.currentTerm
	sendNextIndex := rf.nextIndex[peer]
	sendMatchIndex := rf.matchIndex[peer]
	sendLastLogIndex := rf.GetFirstLog().Index

	args := RequestSnapShotArgs{
		Term:              rf.currentTerm,
		LeaderId:          rf.me,
		LastIncludedIndex: rf.GetFirstLog().Index,
		LastIncludedTerm:  rf.GetFirstLog().Term,
		Offset:            0,
		Data:              rf.snapshot,
		Done:              true,
	}
	DPrintf("server[%d], server term:%d, begin to send snapshot to:%d, args is:%v", rf.me, rf.currentTerm, peer, args)
	rf.mu.Unlock()
	reply := RequestSnapShotReply{}
	ok := rf.sendInstallSnapShot(peer, &args, &reply)
	if !ok {
		return
	}

	// 如果返回true，就需要增加nextIndex
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if reply.Term > rf.currentTerm {
		DPrintf("server[%d]reciver %d server snapshot reply, and found local term is less, so change local term from:%d to:%d", rf.me, peer, rf.currentTerm, reply.Term)
		rf.AddCurrentTerm(reply.Term)
		return
	}

	if rf.state == Leader && rf.currentTerm == sendLeaderTerm {
		// 如果nextIndex还是0的话，那么就把nextIndex更新为1.让它下次不再发送snapshot了。
		if rf.nextIndex[peer] == sendNextIndex && sendLastLogIndex+1 > rf.nextIndex[peer] {
			DPrintf("server[%d], term:%d, got peer:%d send snapshot reply,and update it nextIndex from:%d, to %d",
				rf.me, rf.currentTerm, peer, rf.nextIndex[peer], sendLastLogIndex+1)
			rf.nextIndex[peer] = sendLastLogIndex + 1
		}

		if rf.matchIndex[peer] == sendMatchIndex && sendLastLogIndex > rf.matchIndex[peer] {
			rf.matchIndex[peer] = sendLastLogIndex
		}

		DPrintf("server[%d], term:%d, got peer:%d send snapshot reply,it nextIndex is:%d, matchIndex is:%d",
			rf.me, rf.currentTerm, peer, rf.nextIndex[peer], rf.matchIndex[peer])
		//  apply到状态机
		// 开始修改commitIndex,这里不能拿最后一条日志来比较matchIndex
		for beginIndex := rf.commitIndex + 1; beginIndex <= rf.GetLastLog().Index; beginIndex++ {
			matchCount := 0
			DPrintf("server[%d]begin to cacu commit index, begin index is:%d, len of log is:%d, this is:%d server reply,"+
				"and now match index is:%v,commit index is:%d", rf.me, beginIndex, len(rf.log), peer, rf.matchIndex, rf.commitIndex)
			for _, matchIndex := range rf.matchIndex {
				if matchIndex >= beginIndex {
					matchCount++
				}
			}

			if matchCount <= len(rf.peers)/2 {
				break
			}

			// 更新commitIndex，注意这里不能提交之前term的日志，只能提交本term的日志
			if beginIndex > rf.commitIndex && rf.currentTerm == rf.log[beginIndex-rf.GetFirstLog().Index].Term {
				// 由于leader不能提交之前term的日志，只能间接提交之前term的日志，就导致有可能leader在某一个之后的时刻更新自身的
				// commitIndex是跳过了前面的一些日志的。但是往applyCh里面发送applyMsg还是必须是连续的，所以这个地方需要遍历从
				// rf.commitIndex + 1到beginIndex之间这一段的日志，并提交到applyCh。
				//for toApplyIndex := rf.commitIndex + 1; toApplyIndex <= beginIndex; toApplyIndex++ {
				//	go func() {
				//		applyMsg := ApplyMsg{
				//			CommandValid: true,
				//			Command:      rf.log[toApplyIndex-rf.GetFirstLog().Index].Data,
				//			CommandIndex: toApplyIndex,
				//		}
				//
				//		rf.applyCh <- applyMsg
				//	}()
				//
				//	DPrintf("leader server[%d] apply a log entry to applyCh, log term is:%d, log index is:%d",
				//		rf.me, rf.log[toApplyIndex-rf.GetFirstLog().Index].Term, rf.log[toApplyIndex-rf.GetFirstLog().Index].Index)
				//}

				rf.commitIndex = beginIndex
				rf.applyCond.Signal()
			}
		}
	}
}

// todo 将应用到状态机的代码抽取出来一个函数
func (rf *Raft) applyToRaftStateMachine() {

}

func (rf *Raft) applyTicket() {
	for rf.killed() == false {
		rf.mu.Lock()
		for rf.lastApplied >= rf.commitIndex || rf.lastApplied < rf.GetFirstLog().Index {
			rf.applyCond.Wait()
		}

		if rf.lastApplied < rf.GetFirstLog().Index {
			DPrintf("server[%d]error, apply ticket reset lastapplied form %d to %d", rf.me, rf.lastApplied, rf.GetFirstLog().Index)
			//rf.lastApplied = rf.GetFirstLog().Index
		}

		toApplyLogEntries := make([]LogEntry, 0)
		DPrintf("server[%d]begin to apply ticket,lastApply is:%d, commitIndex is:%d,now have:%v log", rf.me, rf.lastApplied, rf.commitIndex, rf.log)

		if rf.lastApplied < rf.commitIndex {
			for toApplyIndex := rf.lastApplied + 1; toApplyIndex <= rf.commitIndex; toApplyIndex++ {
				toApplyLogEntries = append(toApplyLogEntries, rf.log[toApplyIndex-rf.GetFirstLog().Index])
			}
		}
		DPrintf("server[%d]begin to apply ticket,lastApply is:%d, commit index is:%d, len of toAppLogEntries is:%d, array is:%v", rf.me, rf.lastApplied, rf.commitIndex, len(toApplyLogEntries), toApplyLogEntries)
		rf.mu.Unlock()

		if len(toApplyLogEntries) == 0 {
			continue
		}

		rf.applySeqMu.Lock()
		firstLog := toApplyLogEntries[0]
		for firstLog.Index != rf.lastApplied+1 && firstLog.Index > rf.lastApplied {
			rf.applySeqCond.Wait()
		}

		if firstLog.Index <= rf.lastApplied {
			continue
		}

		for i := 0; i < len(toApplyLogEntries); i++ {
			DPrintf("server[%d]want to apply %d logEntry, the logEntry is:%v", rf.me, i, toApplyLogEntries[i])
			applyMsg := ApplyMsg{
				CommandValid: true,
				Command:      toApplyLogEntries[i].Data,
				CommandIndex: toApplyLogEntries[i].Index,
			}

			//start := time.Now().UnixMilli()
			//DPrintft("in applyTicket,begin send msg to applyCh, time is:%v", start)
			rf.applyCh <- applyMsg
		}

		if len(toApplyLogEntries) > 0 && toApplyLogEntries[len(toApplyLogEntries)-1].Index > rf.lastApplied {
			rf.lastApplied = toApplyLogEntries[len(toApplyLogEntries)-1].Index
			DPrintf("server[%d]apply all log,now lastapply is:%d", rf.me, rf.lastApplied)
		}

		rf.applySeqMu.Unlock()
	}
}

// 将发送日志的逻辑抽象出来，供start()函数调用，防止leader的非必要的延迟同步到follower
func (rf *Raft) sendLogTicketCore() {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}

	recodeNextIndex := make([]int, len(rf.peers))
	allSendLogArgs := make([]SendLogArgs, len(rf.peers))

	for i, _ := range rf.peers {
		sendLogArgs := &allSendLogArgs[i]
		// 此处不能使用append，要使用下标的方式每次都覆盖上次这个位置的值

		// 如果这个follower的nextIndex是leader的最后一条日志的index + 1的位置，那么就认为没有新的日志要发送
		// 所以这个nextIndex一开始需要初始化为len(rf.log) - 1的位置，表示将要发送的Log的下标。如果一开始的时候，leader一条日志都
		// 没有，那么会初始化为0，否则初始化为len(rf.log) - 1.当初始化为0的时候，这个条件也会成立，所以也不会再发送RPC了。而当leader
		// 后续添加了一条log之后，len(rf.log) 就变成1了，此时的nextIndex还是0，那么不成立也就会继续往下去发送日志了

		// 将rf.nextIndex[i] >= len(rf.log)这个条件去掉。因为现在已经把发送心跳和发送日志的合二为一了。那么也就不需要用那个比较follower和
		// leader是否跟上了，因为无论怎样都要发送RPC的，只是如果是跟上了的话，发送的数据是空的，只用来重置定时器。
		if i == rf.me {
			continue
		}

		// 如果发现nextIndex为0的话，那么就需要发送snapshot了
		sendLogArgs.ShouldSendSnapShot = false
		if rf.GetFirstLog().Term != -1 && rf.nextIndex[i] <= rf.GetFirstLog().Index {
			sendLogArgs.ShouldSendSnapShot = true
			DPrintf("server[%d]found follower:%d nextIndex is:%d less than first log index:%d,so begin to send snapshot",
				rf.me, i, rf.nextIndex[i], rf.GetFirstLog().Index)
			continue
		}

		if rf.GetFirstLog().Term == -1 && rf.nextIndex[i] == rf.GetFirstLog().Index {
			rf.nextIndex[i] = rf.GetFirstLog().Index + 1
		}

		if rf.nextIndex[i] < 0 {
			DPrintf("error,server[%d] found %d nextIndex less than 0", rf.me, i)
			fmt.Printf("error,server[%d] found %d nextIndex less than 0", rf.me, i)
			rf.nextIndex[i] = 0
		}

		// todo 此时是否需要判断nextIndex是否是大于0的，有可能某一个follower的nextIndex一直减少，一直到0以下了
		sendLogArgs.LeaderId = rf.me
		sendLogArgs.Term = rf.currentTerm

		// 因为nextIndex一定是大于等于1的，所以这里一定可以拿到preLog。
		if rf.nextIndex[i]-rf.GetFirstLog().Index == 0 {
			fmt.Printf("error,leader[%d] have:%v log, and nextIndex is:%v", rf.me, rf.log, rf.nextIndex)
		}
		DPrintf("server[%d]to cacl server:%d prelog,nextIndex is:%d, first log index is:%d", rf.me, i, rf.nextIndex[i], rf.GetFirstLog().Index)
		preLog := rf.log[(rf.nextIndex[i]-rf.GetFirstLog().Index)-1]
		sendLogArgs.PrevLogIndex = preLog.Index
		sendLogArgs.PrevLogTerm = preLog.Term

		sendLogArgs.Entries = rf.log[(rf.nextIndex[i] - rf.GetFirstLog().Index):]
		sendLogArgs.LeaderCommit = rf.commitIndex

		// for update nextIndex
		recodeNextIndex[i] = rf.nextIndex[i]
		// === for log
		sendLogArgs.MsgType = LogMsgType
		if len(sendLogArgs.Entries) == 0 {
			sendLogArgs.MsgType = HeartBeatMsgType
		}
		DPrintf("server[%d] prepare send log to %d,leader have %v log, "+
			"the nextIndex is:%d, len of log is:%d, status is:%v, and term is:%d, send args term is:%d, msg type is:%v, papare to send log is:%v, args commitIndex is:%d",
			rf.me, i, rf.log, rf.nextIndex[i], len(rf.log), rf.state, rf.currentTerm, allSendLogArgs[i].Term, sendLogArgs.MsgType, sendLogArgs.Entries, sendLogArgs.LeaderCommit)
	}

	rf.mu.Unlock()

	DPrintf("server[%d], before send log to others, ths allSendLogArgs is:%v,", rf.me, allSendLogArgs)
	DPrintft("leader[%d] in sendLogTicket,begin to send log to others,time is:%v", rf.me, time.Now().UnixMilli())
	for i, _ := range rf.peers {
		if i == rf.me {
			continue
		}

		if allSendLogArgs[i].ShouldSendSnapShot {
			go rf.sendSnapShot(i)
			continue
		}

		// todo 不能直接使用外部的allSendLogArgs，而是要将这个作为参数传入
		go func(i int, args SendLogArgs, recodeNextIndex int) {
			reply := SendLogReply{}
			//  for update commitIndex,这里不能用args的term，而是应该用当前leader的term，因为日志的term有可能是之前的term的，
			// 而这里要拿到的是当前发出这个请求的leader的term。
			leaderTerm := args.Term
			// lastSendLogIndex := args.Entries[len(allSendLogArgs[i].Entries)-1].Index
			// lastSendLogTerm := args.Entries[len(allSendLogArgs[i].Entries)-1].Term
			// 注意这个如果toSendCommond是空的话，那么在接收到这个请求的success返回的时候不应该去更新nextIndex。如果不为空的话，
			// 则表示刚刚发出的这些日志都已经被这个follower保存了。那么就得更新为最后一个LogEntry的index+1的位置
			toSendEntries := args.Entries
			// for log
			//preLogTerm := args.PrevLogTerm
			//preLogIndex := args.PrevLogIndex

			ok := rf.sendLog(i, &args, &reply)
			if !ok {
				//DPrintf("server[%d] send log to %d, bug get fail, args Term is:%d, args Index is:%d", rf.me, i, preLogTerm, preLogIndex)
				return
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()
			if reply.Term > rf.currentTerm {
				DPrintf("server[%d]reciver %d server reply, and found local term is less, so change local term from:%d to:%d", rf.me, i, rf.currentTerm, reply.Term)
				rf.AddCurrentTerm(reply.Term)
				return
			}

			// 在更新之前，还需要进行两个判断，当前是否是leader，并且当前的term是否还是之前发出这个请求的时候的term，因为可能出现如下情形：
			// 1. 如果当前已经不是leader了，那么此时再去更新commitIndex，并且把已经提交的日志发送到上层的话，会造成误解，因为这个位置的日志可能
			// 已经被新的term的leader重写了。此时可能会提交一个错误的index的日志。
			// 2. 如果当前是leader，但是当前的term和发出请求的term不相同。那么可能中间没有当leader的那个term可能已经把这个位置的日志重写了。
			// 那么此时如果更新nextIndex的话，就更新不是自己term的了，会影响最新的term的nextIndex了。
			if rf.state == Leader && rf.currentTerm == leaderTerm {
				// 更新这个follower的nextIndex，和leader的commitIndex
				if reply.Success {
					// 更新nextIndex,matchIndex也更新为这次请求设置的值，这个地方还需要判断是否之前的大，如果大的话，才去更新，
					// 因为有可能这次请求执行的很慢，导致leader下一次重发了这个请求，并且重发了后面的请求，然后后面的请求都已经执行成功了。
					// 所以matchIndex已经被更新到更大的值了，此时如果不判断直接设置的话，会导致matchIndex回退。
					if len(toSendEntries) > 0 {
						lastEntry := toSendEntries[len(toSendEntries)-1]
						if lastEntry.Index > rf.matchIndex[i] {
							rf.matchIndex[i] = lastEntry.Index
						}
						if lastEntry.Index+1 > rf.nextIndex[i] {
							rf.nextIndex[i] = lastEntry.Index + 1
							DPrintf("server[%d]update follower server:%d nextIndex to:%d", rf.me, i, rf.nextIndex[i])
						}

						// 开始修改commitIndex,这里不能拿最后一条日志来比较matchIndex
						for beginIndex := rf.commitIndex + 1; beginIndex <= rf.GetLastLog().Index; beginIndex++ {
							matchCount := 0
							DPrintf("server[%d]begin to cacu match count, begin index is:%d, len of log is:%d, this is:%d server reply,"+
								"and now match index is:%v,commit index is:%d", rf.me, beginIndex, len(rf.log), i, rf.matchIndex, rf.commitIndex)
							for _, matchIndex := range rf.matchIndex {
								if matchIndex >= beginIndex {
									matchCount++
								}
							}

							if matchCount <= len(rf.peers)/2 {
								break
							}

							// 更新commitIndex，注意这里不能提交之前term的日志，只能提交本term的日志
							if beginIndex > rf.commitIndex && rf.currentTerm == rf.log[beginIndex-rf.GetFirstLog().Index].Term {
								// 由于leader不能提交之前term的日志，只能间接提交之前term的日志，就导致有可能leader在某一个之后的时刻更新自身的
								// commitIndex是跳过了前面的一些日志的。但是往applyCh里面发送applyMsg还是必须是连续的，所以这个地方需要遍历从
								// rf.commitIndex + 1到beginIndex之间这一段的日志，并提交到applyCh。
								rf.commitIndex = beginIndex
								rf.applyCond.Signal()
							}
						}

						DPrintf("server[%d]after append entries to %d server request, now it commit index is:%d,it nextIndex is:%d", rf.me, i, rf.commitIndex, rf.nextIndex[i])
					}
				} else {
					if rf.nextIndex[i] == recodeNextIndex {
						backNextIndex := rf.nextIndex[i] - 1
						// leader有可能在发出这个append entries rpc之后发生了一次snapshot，当这次snapshot的lastIncludedIndex大于等于
						// backNextIndex的时候，直接设置nextIndex为这个lastIncludedIndex。
						if backNextIndex <= rf.GetFirstLog().Index {
							// 能进入这个分支，一定是发生了snapshot的。
							rf.nextIndex[i] = rf.GetFirstLog().Index
							DPrintf("server[%d]after send append entry to:%d, but need reduce nextIndex to:%d", rf.me, i, rf.nextIndex[i])
							return
						}

						// 这个地方不能直接作为下标了，有两种情况，发生了snapshot，或者是没有发生snapshot。
						// 1. 发生了snapshot：rf.log中的第一个位置保存的是snapshot最后一条日志的index。计算应该是 backNextIndex - rf.log[0].index
						// 2. 没有发生snapshot：backNextIndex就可以当做下标。此时的计算应该变成 backNextIndex - rf.log[0].index
						//nowNextTerm := rf.GetLastLog().Term
						//if backNextIndex <= rf.GetLastLog().Index {
						//	nowNextTerm = rf.log[backNextIndex-rf.log[0].Index].Term
						//}
						if backNextIndex > rf.GetLastLog().Index {
							backNextIndex = rf.GetLastLog().Index
						}

						nowNextTerm := rf.log[backNextIndex-rf.GetFirstLog().Index].Term
						// 最多让nextIndex回退到rf.log第一个log的index位置
						for backNextIndex >= rf.GetFirstLog().Index && rf.log[backNextIndex-rf.log[0].Index].Term == nowNextTerm {
							backNextIndex--
						}

						rf.nextIndex[i] = backNextIndex + 1
						DPrintf("server[%d]after send append entry to:%d, but need reduce nextIndex to:%d", rf.me, i, backNextIndex)
					}
				}
			}
		}(i, allSendLogArgs[i], recodeNextIndex[i])
	}
}

func (rf *Raft) sendLogTicket() {
	// 周期性的发送日志
	for rf.killed() == false {
		// 将心跳和发送日志合二为一。所以这里不要判断rf.log的len是否大于0了，而只需要判断是否是leader就行了。
		rf.sendLogTicketCore()

		ms := 110
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

func (rf *Raft) ticker() {
	shouldElection := false
	for rf.killed() == false {
		// Check if a leader election should be started.
		// 此处只需要args，args可以共用，但是reply得是每一个goroutine新建立的
		var args RequestVoteArgs

		rf.mu.Lock()
		shouldElection = false

		DPrintf("server[%d]begin a new ticker, local state is:%v, local Term is:%d, and time is bigger than interval?:%v",
			rf.me, rf.state, rf.currentTerm, time.Since(rf.startElectionTime) > time.Duration(rf.electionInterval)*time.Millisecond)
		if time.Since(rf.startElectionTime) > time.Duration(rf.electionInterval)*time.Millisecond && (rf.state == Follower || rf.state == Candidate) {
			DPrintf("server[%d]should begin a election", rf.me)

			shouldElection = true
			rf.SetElectionTime(600)
			rf.currentTerm++
			rf.state = Candidate
			rf.voteFor = rf.me
			rf.persist()
			DPrintf("server[%d]has vote for himself, Term is:%d", rf.me, rf.currentTerm)

			args = RequestVoteArgs{
				Term:        rf.currentTerm,
				CandidateId: rf.me,
			}

			// 如果当前是空日志的话
			if len(rf.log) == 0 {
				args.LastLogIndex = -1
				args.LastLogTerm = -1
			} else {
				// 由于rf.log里面至少都会有一条dummy log，所以此处可以直接这样获取最后一条日志
				tempLog := rf.log[len(rf.log)-1]
				args.LastLogTerm = tempLog.Term
				args.LastLogIndex = tempLog.Index
			}
		}
		DPrintf("server[%d]begin a new ticker, local state is:%v, local Term is:%d, release a lock success", rf.me, rf.state, rf.currentTerm)
		rf.mu.Unlock()

		// 在发送RPC之前释放锁，防止死锁
		go func(shouldElection bool) {
			if shouldElection {
				cond := sync.NewCond(&rf.mu)
				finishRequest := 1
				voteCount := 1
				rpcFailCount := 0

				for i, _ := range rf.peers {
					if i == rf.me {
						continue
					}

					go func(i int, args RequestVoteArgs) {
						// 此处的reply必须每个goroutine都使用不同的
						reply := RequestVoteReply{}
						// 注意这里的i要使用外部传入的参数，不能直接使用闭包
						ok := rf.sendRequestVote(i, &args, &reply)

						rf.mu.Lock()
						defer rf.mu.Unlock()
						finishRequest++
						DPrintf("server[%d] got server %d vote respone, args Term is:%d,reply grante:%v, is ok? %v, get lock success", rf.me, i, args.Term, reply.VoteGranted, ok)
						if ok {
							if reply.VoteGranted {
								voteCount++
							}

							if reply.Term > rf.currentTerm {
								DPrintf("server[%d]request for leader, but server:%d, "+
									"response Term:%d is bigger than local Term:%d, so change local Term to new", rf.me, i, reply.Term, rf.currentTerm)
								rf.AddCurrentTerm(reply.Term)
							}
						} else {
							rpcFailCount++
						}
						cond.Broadcast()
					}(i, args)
				}

				rf.mu.Lock()
				// 退出循环的条件
				// 1. 过半节点同意   2. 所有请求都已经回复了
				// 考虑一个3server组成的一个集群的情况：分别是 0，1，2三个机器。0机器是离线的，分区了。然后集群中只有1，2两台机器能互相通信。此时1开始选举，选举的term为5，2也开始选举，term也为5，
				// 原来的判断条件中，需要所有的节点都回复才能跳出循环。
				for voteCount <= (len(rf.peers)/2) && finishRequest < len(rf.peers) && voteCount+(len(rf.peers)-finishRequest) > (len(rf.peers)/2) {
					cond.Wait()
					DPrintf("server[%d] wait walk up, voteCount is:%d, finishRequest:%d, len/2 is:%d, vote plus finish is:%d", rf.me, voteCount, finishRequest, len(rf.peers)/2, voteCount+(len(rf.peers)-finishRequest))
				}
				//DPrintf("server[%d]is going to sleep.....555", rf.me)
				DPrintf("server[%d], vote for leader, got %d tickets, got %d failRpc, total %d request, args Term is:%d", rf.me, voteCount, rpcFailCount, finishRequest, args.Term)
				// 接下来要根据投票的结果来修改本地的状态了
				if rf.currentTerm == args.Term && rf.state == Candidate {
					if voteCount > len(rf.peers)/2 {
						rf.state = Leader
						rf.resetRaftIndex()
						DPrintf("server[%d], become a leader, got %d tickets", rf.me, voteCount)
					}
				}
				rf.mu.Unlock()
			}
		}(shouldElection)

		ms := 200 + (rand.Int63() % 300)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

// 该函数不加锁，需要调用者加锁
// 对于启动的时候初始化nextIndex的一层含义是不能跳过某一些已有的Log不去同步。对于初始化之后，还是只有一条dummy log的情况，
// 设置nextIndex为1.
func (rf *Raft) resetRaftIndex() {
	for i, _ := range rf.nextIndex {
		// 这里直接设置为log的长度，因为有个dummy节点，所以实际上可以直接用这个数字当做log数组的下标来用了，go语言中切片截取array[begin:]，这个begin
		// 可以等于len(array)。并不会报错。
		// 在snapshot之后，即使这个master什么log都没有，但是会有一个dummy log，此处将nextIndex初始化为1，
		// 每个server的snapshot的生成都是自己控制的。所以leader不用关心是否需要把snapshot发送给follower。而是只需要正常的同步日志，当发现
		// nextIndex在本地已经成为了snapshot之后，就发送snapshot就行了。之前是dummy log一定会符合，现在是dummy log也不一定符合了，所以当
		// 发送nextIndex为1的位置的日志都不符合了的话，那么就需要继续减少nextIndex。此时nextIndex会变成0，所以在发送日志的时候，就要判断，如果
		// nextIndex是0的话，就去发送snapshot，而不要再去截取log数组里面的日志发送了。
		// 在加入snapshot之后，这个地方不能设置为log的下标了。而是要设置为log的index。
		if len(rf.log) == 1 {
			rf.nextIndex[i] = rf.log[0].Index + 1
		} else {
			// todo 这里为什么要设置为len（rf.log） - 1 ？ 如果len是2的话，那么会设置为1.
			rf.nextIndex[i] = rf.log[len(rf.log)-1].Index + 1
		}

		rf.matchIndex[i] = 0
		if i == rf.me {
			rf.matchIndex[i] = rf.GetLastLog().Index
		}

	}

	// 由于这里设置了commitIndex为0，所以不会将index为0的日志提交掉。
	//rf.commitIndex = 0
}

// Make the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applyCh = applyCh
	rf.applyCond = sync.NewCond(&rf.mu)
	rf.applySeqCond = sync.NewCond(&rf.applySeqMu)
	// Your initialization code here (3A, 3B, 3C).

	// 启动的时候，服务器应该是跟随者的状态
	rf.state = Follower
	rf.currentTerm = 0
	// todo 此处不能设置为true，不然第一个测试会过不了
	//rf.receiveNew = true
	//rf.SetElectionTime()
	rf.startElectionTime = time.Now()
	rf.electionInterval = 0
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.voteFor = -1

	// 初始化日志分发相关的切片
	rf.log = make([]LogEntry, 0)
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))

	// 设置一个dummy log
	rf.log = append(rf.log, LogEntry{
		Term:  -1,
		Index: 0,
		Data:  nil,
	})

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	rf.snapshot = persister.ReadSnapshot()
	// 在读取完持久化配置之后,初始化nextIndex和matchIndex
	rf.resetRaftIndex()

	// start ticker goroutine to start elections
	go rf.ticker()

	// go rf.heartBeat()

	go rf.sendLogTicket()

	go rf.applyTicket()

	return rf
}

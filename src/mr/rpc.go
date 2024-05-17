package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import "os"
import "strconv"

// example to show how to declare the arguments
// and reply for an RPC.

// 定义任务类型的枚举值
type MRJobType int

const (
	UnknownJob  MRJobType = iota // 未知任务
	MapJob                       // map任务
	ReduceJob                    // reduce任务
	TerminalJob                  // worker停止任务
	WaitJob
)

type MRJobState int

const (
	UnknownState MRJobState = iota // 未知任务
	UnFinish                       // map任务
	Doing                          // reduce任务
	Finished                       // worker停止任务
)

type Job struct {
	JobType  MRJobType
	JobState MRJobState

	JobId           int
	OperateFileName string
}

type ExampleArgs struct {
	X int
}

type ExampleReply struct {
	JobType MRJobType // 任务类型，map任务，或者是reduce任务，或者是让worker终结的任务
	// 共同使用同一个字段表示任务id
	JobId int

	// ====== map 任务
	MapJobFileName string // map任务的文件名
	MapJobId       int    // map任务的id， 可以使用第几个文件来作为id
	NReduce        int

	// ====== reduce 任务
	ReduceJobId int // reduce任务的id，一共有nReduce个reduce任务
}

type JobFinishArgs struct {
	JobType MRJobType // 任务类型，map任务，或者是reduce任务，或者是让worker终结的任务
	Success bool

	// 共同使用同一个字段表示任务id
	JobId int
}

type JobFinishReply struct {
}

// Add your RPC definitions here.

// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the coordinator.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func coordinatorSock() string {
	s := "/var/tmp/5840-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}

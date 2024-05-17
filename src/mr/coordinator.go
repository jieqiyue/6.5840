package mr

import (
	"fmt"
	"log"
	"sync"
	"time"
)
import "net"
import "os"
import "net/rpc"
import "net/http"

type Coordinator struct {
	// Your definitions here.
	files   []string
	nReduce int

	allJob map[MRJobType][]Job

	// map job
	mapJobFinish bool
	mapJob       []Job

	// reduce job
	reduceJobFinish bool
	reduceJob       []Job

	// lock
	lock sync.Mutex
}

// Your code here -- RPC handlers for the worker to call.

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	//reply.Y = args.X + 1
	return nil
}

// todo  1. 先把GetJob中的锁都注释了，使用一个大锁    2. 在worker上报结果的时候，再去更新整体map任务状态
func (c *Coordinator) GetJob(args *ExampleArgs, reply *ExampleReply) error {
	// 这里先获取map job，如果map job全部完成之后，就顺着在同一个RPC里面返回reduce job
	//fmt.Println("master GetJob function get start .....")
	fmt.Println("master get job, the default job type is:", reply.JobType)
	c.lock.Lock()
	defer c.lock.Unlock()
	if !c.mapJobFinish {
		for i, job := range c.mapJob {
			//c.lock.Lock()
			// 此处仅仅是寻找状态为UnFinish的任务，如果没有找到，就直接认为map阶段结束了，这是有问题的。有可能有一个任务还在Doing。
			if job.JobState != UnFinish {
				//c.lock.Unlock()
				continue
			}

			reply.MapJobFileName = job.OperateFileName
			reply.JobType = job.JobType
			reply.JobId = i
			reply.NReduce = c.nReduce
			// job.JobState = Doing  此处易错，这里得到的job是一个复制，而不是原map中的引用
			c.mapJob[i].JobState = Doing

			//fmt.Println("master getJob function, replay a map task, filename is "+reply.MapJobFileName, "job state after:", c.mapJob[i].JobState)
			//c.lock.Unlock()

			// 创建一个定时器，在10秒钟之后触发，以此判断该任务是否完成
			timer := time.NewTimer(10 * time.Second)
			go func(t *time.Timer, jobId int, jobType MRJobType) {
				for {
					<-t.C

					c.lock.Lock()
					// 寻找到这个job，查看任务状态是否是完成状态
					if c.allJob[jobType][jobId].JobState == Doing {
						// 此处直接设置为unfinish，后续如果worker上报任务完成，则要先判断任务是否处于Doing状态，如果出于Doing状态才将任务
						// 设置为finish状态。
						c.allJob[jobType][jobId].JobState = UnFinish
						//fmt.Println("master GetJob,map timer find job:", jobId, " is time out, put it state to unFinish")
					} else {
						//fmt.Println("master time find job id:", jobId, " is done")
					}
					c.lock.Unlock()

					return
				}
			}(timer, i, job.JobType)

			// 这里要直接返回，防止执行到下面的reduce任务
			return nil
		}

		// 如果走到这里，证明没有找到map job， 不应该在这个地方
		// c.mapJobFinish = true
		//fmt.Println("master GetJob, all map job finish.")
	}

	//fmt.Println("not reach place ...................")
	if c.mapJobFinish {
		for i, job := range c.reduceJob {
			//c.lock.Lock()
			if job.JobState != UnFinish {
				//c.lock.Unlock()
				continue
			}

			reply.JobType = job.JobType
			reply.JobId = i
			//job.JobState = Doing
			c.reduceJob[i].JobState = Doing
			//c.lock.Unlock()

			//fmt.Print("master getJob function, get a reduce task, task id is:", reply.JobId)
			// 创建一个定时器，在10秒钟之后触发，以此判断该任务是否完成
			timer := time.NewTimer(10 * time.Second)
			go func(t *time.Timer, jobId int, jobType MRJobType) {
				for {
					<-t.C

					c.lock.Lock()
					// todo use defer
					// defer c.lock.Unlock()
					// 寻找到这个job，查看任务状态是否是完成状态
					if c.allJob[jobType][jobId].JobState == Doing {
						// 此处直接设置为unfinish，后续如果worker上报任务完成，则要先判断任务是否处于Doing状态，如果出于Doing状态才将任务
						// 设置为finish状态。
						c.allJob[jobType][jobId].JobState = UnFinish
						//fmt.Println("master GetJob,reduce timer find job:", jobId, " is time out, put it state to unFinish")
					}

					c.lock.Unlock()
					return
				}
			}(timer, i, job.JobType)

			// 这里要直接返回，去下面发送terminal任务
			return nil
		}
	}

	if c.reduceJobFinish {
		reply.JobType = TerminalJob
		return nil
	}

	//  todo  不能在这里设置reduce job的finish
	// c.reduceJobFinish = true

	// 如果上面都没有设置job type，则让worker暂时等待
	reply.JobType = WaitJob

	return nil
}

func (c *Coordinator) ReportJobResult(args *JobFinishArgs, reply *JobFinishReply) error {
	// 要操作共享数据，要用锁保护
	c.lock.Lock()
	defer c.lock.Unlock()

	if args.JobType == TerminalJob || args.JobType == WaitJob {
		return nil
	}

	if args.Success == false {
		if c.allJob[args.JobType][args.JobId].JobState == Doing {
			c.allJob[args.JobType][args.JobId].JobState = UnFinish
			//fmt.Println("master ReportJobResult,fail job id:", args.JobId, "job type: ", args.JobType, "worker miss error")
		} else {
			//fmt.Println("master ReportJobResult,fail job id:", args.JobId, "job type: ", args.JobType, "are time out")
		}
	} else if args.Success == true {
		if c.allJob[args.JobType][args.JobId].JobState == Doing {
			c.allJob[args.JobType][args.JobId].JobState = Finished

			// check所有任务，看是否全部完成,这里不需要担心worker挂掉，因为分发任务的时候启动了定时器，会定时把任务状态重置为unfinish，所以会在下一个worker的
			// 状态上报的时候进行所有任务状态的更新
			c.CheckAllState(args, reply)
			//fmt.Println("master ReportJobResult,success job id:", args.JobId, "job type: ", args.JobType, "are finished ")
		} else {
			//fmt.Println("master ReportJobResult,success job id:", args.JobId, "job type: ", args.JobType, "job State:", c.allJob[args.JobType][args.JobId].JobState, " are time out")
		}
	}

	return nil
}

// 调用该方法需要保证外部已经上锁了,并发不安全
func (c *Coordinator) CheckAllState(args *JobFinishArgs, reply *JobFinishReply) {
	// 要操作共享数据，要用锁保护
	mapJobAllFinish, reduceJobAllFinish := true, true
	if args.JobType == MapJob {
		// 遍历所有map job，如果所有job都是finished状态，则可以认为map job已经结束
		for _, job := range c.allJob[MapJob] {
			if job.JobState != Finished {
				mapJobAllFinish = false
				break
			}
		}
	}

	if args.JobType == ReduceJob {
		// 遍历所有reduce job，如果所有job都是finished状态，则可以认为reduce job已经结束
		for _, job := range c.allJob[ReduceJob] {
			if job.JobState != Finished {
				reduceJobAllFinish = false
				break
			}
		}
	}

	if args.JobType == MapJob && mapJobAllFinish {
		fmt.Println("master all map job are finish.......")
		c.mapJobFinish = true
	}

	if args.JobType == ReduceJob && reduceJobAllFinish {
		fmt.Println("master all reduce job are finish.......")
		c.reduceJobFinish = true
	}

	return
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	// Your code here.
	return c.reduceJobFinish
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// Your code here.

	//fmt.Println("begin master, get files:", files)
	// 初始化Coordinator
	c.nReduce = nReduce
	c.files = files

	c.mapJobFinish = false
	c.reduceJobFinish = false
	c.mapJob = make([]Job, 0)
	c.reduceJob = make([]Job, 0)

	for i, fileName := range files {
		tempJob := Job{
			JobType:         MapJob,
			OperateFileName: fileName,
			JobId:           i,
			JobState:        UnFinish,
		}

		c.mapJob = append(c.mapJob, tempJob)
	}

	for i := 0; i < nReduce; i++ {
		tempJob := Job{
			JobType:  ReduceJob,
			JobId:    i,
			JobState: UnFinish,
		}

		c.reduceJob = append(c.reduceJob, tempJob)
	}

	c.allJob = make(map[MRJobType][]Job)
	c.allJob[MapJob] = c.mapJob
	c.allJob[ReduceJob] = c.reduceJob

	c.server()
	return &c
}

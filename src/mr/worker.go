package mr

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)
import "log"
import "net/rpc"
import "hash/fnv"

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

var canTerminal bool
var MapInterMediateTemplate = "mr-%s-%s" // mr-0-1    mr-0-2    mr-1-1   mr-1-2
var ReduceFileTemplate = "mr-out-%s"

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.
	canTerminal = false

	for !canTerminal {
		//fmt.Println("worker do loop .......")
		// 1. 发送RPC请求到master，请求一个任务
		args := ExampleArgs{}
		args.X = 99
		reply := ExampleReply{}
		CallExample(&args, &reply)
		//fmt.Println("worker call get job return , get a job ,job id :", reply.JobId)
		//if reply.JobType == TerminalJob {
		//	fmt.Println("worker get terminal job, shut down")
		//	return
		//}

		// 2. 获取到了任务，开始执行任务
		success := processJob(&reply, mapf, reducef)

		// 3. 将任务结果返回到master
		jobResultArgs := JobFinishArgs{
			JobType: reply.JobType,
			JobId:   reply.JobId,
			Success: false,
		}
		jobResultResp := JobFinishReply{}

		if success {
			//fmt.Println("worker do job type", reply.JobType, " job id:", reply.JobId, " are success")
			jobResultArgs.Success = true
		} else {
			//fmt.Println("worker do job type", reply.JobType, " job id:", reply.JobId, " are fail")
		}

		RpcCallMaster("Coordinator.ReportJobResult", &jobResultArgs, &jobResultResp)

		//fmt.Println("worker begin to sleep")
		time.Sleep(3 * time.Second)
		//fmt.Println("worker sleep over")
	}

}

func processJob(reply *ExampleReply, mapf func(string, string) []KeyValue, reducef func(string, []string) string) bool {
	if reply.JobType == TerminalJob {
		//fmt.Println("get terminal job for debug to exit")
		canTerminal = true
		return true
	}

	if reply.JobType == MapJob {
		intermediate := []KeyValue{}
		// 进行map任务
		file, err := os.Open(reply.MapJobFileName)
		if err != nil {
			fmt.Printf("cannot open %v", reply.MapJobFileName)
			return false
		}
		content, err := ioutil.ReadAll(file)
		if err != nil {
			fmt.Printf("cannot read %v", reply.MapJobFileName)
			return false
		}
		file.Close()
		kva := mapf(reply.MapJobFileName, string(content))
		intermediate = append(intermediate, kva...)

		//fmt.Println("map task after mapf, the len is:", len(intermediate))
		// 将这个key value进行分组到map中
		var imap = make(map[int][]KeyValue)
		for _, pair := range intermediate {
			index := ihash(pair.Key) % reply.NReduce
			imap[index] = append(imap[index], pair)
		}
		//fmt.Println("map task,imap len is:", len(imap))

		// 遍历map将每一个key，value写入到中间文件
		for i, pairs := range imap {
			fianlInterMediateFileName := fmt.Sprintf(MapInterMediateTemplate, strconv.Itoa(reply.JobId), strconv.Itoa(i))
			tmpFile, err := os.CreateTemp(".", fianlInterMediateFileName)
			if err != nil {
				fmt.Println("Error creating temp file:", err)
				return false
			}
			// 创建 JSON 编码器
			encoder := json.NewEncoder(tmpFile)
			for _, pair := range pairs {
				err = encoder.Encode(&pair)
				if err != nil {
					fmt.Println("Error encoding JSON:", err)
					return false
				}
			}

			// 当所有key，value都写入到文件之后，进行rename操作
			//fmt.Println("map task begin to rename intermediate file, the finally file name is:", fianlInterMediateFileName, "the origin name is:", tmpFile.Name())
			err = os.Rename(tmpFile.Name(), fianlInterMediateFileName)
			if err != nil {
				fmt.Println("map task rename tmep file fail, error:", err)
				return false
			}

			tmpFile.Close()
		}

		//fmt.Println("map task, all key/value are success store to file")
	} else if reply.JobType == ReduceJob {
		// 读取所有的中间文件
		files, err := filepath.Glob(fmt.Sprintf(MapInterMediateTemplate, "*", strconv.Itoa(reply.JobId)))
		if err != nil {
			fmt.Println("reduce task, get all inter file fail:", err)
			return false
		}
		if len(files) == 0 {
			//fmt.Println("reduce task, find " + strconv.Itoa(reply.JobId) + " file len is 0")
			return true
		}
		//fmt.Println("reduce job, get all file, len:", len(files), "print all file:", files)

		tempPairs := []KeyValue{}
		for _, file := range files {
			file, err := os.Open(file)
			if err != nil {
				fmt.Printf("reduce task,cannot open %v", reply.MapJobFileName)
				return false
			}

			dec := json.NewDecoder(file)

			for {
				var kv KeyValue
				if err := dec.Decode(&kv); err != nil {
					break
				}
				tempPairs = append(tempPairs, kv)
			}
			file.Close()
		}

		sort.Sort(ByKey(tempPairs))
		//fmt.Println("after sort:", tempPairs)

		fianlReduceFileName := fmt.Sprintf(ReduceFileTemplate, strconv.Itoa(reply.JobId))
		tmpFile, err := os.CreateTemp(".", fianlReduceFileName)
		if err != nil {
			fmt.Println("Error creating temp file:", err)
			return false
		}

		i := 0
		for i < len(tempPairs) {
			j := i + 1
			for j < len(tempPairs) && tempPairs[j].Key == tempPairs[i].Key {
				j++
			}
			values := []string{}
			for k := i; k < j; k++ {
				values = append(values, tempPairs[k].Value)
			}
			output := reducef(tempPairs[i].Key, values)

			// this is the correct format for each line of Reduce output.
			fmt.Fprintf(tmpFile, "%v %v\n", tempPairs[i].Key, output)

			i = j
		}

		err = os.Rename(tmpFile.Name(), fianlReduceFileName)
		if err != nil {
			fmt.Println("reduce task rename tmep file fail, error:", err)
			return false
		}
		tmpFile.Close()
	} else if reply.JobType == WaitJob {
		return true
	} else {
		fmt.Printf("%s %d", "error...worker get unknow job type, type is:", reply.JobType)
		return true
	}

	return true
}

func RpcCallMaster(funcName string, args *JobFinishArgs, reply *JobFinishReply) {
	ok := call(funcName, args, reply)
	if ok {
		// reply.Y should be 100.
		//fmt.Printf("reply.Y %v\n", reply.Y)
	} else {
		fmt.Printf("call failed!\n")
	}
}

// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
func CallExample(args *ExampleArgs, reply *ExampleReply) {
	ok := call("Coordinator.GetJob", args, reply)
	if ok {
	} else {
		fmt.Printf("call failed!\n")
	}
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}

package raft

//
// Raft tests.
//
// we will use the original test_test.go to test your code for grading.
// so, while you can modify this code to help you debug, please
// test with the original before submitting.
//

import "testing"
import "fmt"
import "time"
import "math/rand"
import "sync/atomic"
import "sync"

// The tester generously allows solutions to complete elections in one second
// (much more than the paper's range of timeouts).
const RaftElectionTimeout = 1000 * time.Millisecond

func TestInitialElection3A(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3A): initial election")

	// is a leader elected?
	cfg.checkOneLeader()

	// sleep a bit to avoid racing with followers learning of the
	// election, then check that all peers agree on the Term.
	time.Sleep(50 * time.Millisecond)
	term1 := cfg.checkTerms()
	if term1 < 1 {
		t.Fatalf("Term is %v, but should be at least 1", term1)
	}

	// does the leader+Term stay the same if there is no network failure?
	time.Sleep(2 * RaftElectionTimeout)
	term2 := cfg.checkTerms()
	if term1 != term2 {
		fmt.Printf("warning: Term changed even though there were no failures")
	}

	// there should still be a leader.
	cfg.checkOneLeader()

	cfg.end()
}

func TestReElection3A(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3A): election after network failure")
	// 1. 一开始是在有网络问题的情况下启动的，需要测试在网络有问题的情况下，能不能够选举出一个leader
	leader1 := cfg.checkOneLeader()
	fmt.Println("TestReElection3A 0 success, leader is:", leader1)
	// if the leader disconnects, a new one should be elected.
	fmt.Println("test script, make leader1 disconnects ...... leader1 is:", leader1)
	cfg.disconnect(leader1)
	// 2. 将第一次选举出来的领导人下线，看是否能够继续成功选举出一个leader
	cfg.checkOneLeader()
	fmt.Println("TestReElection3A 1 success ")
	// if the old leader rejoins, that shouldn't
	// disturb the new leader. and the old leader
	// should switch to follower.
	fmt.Println("test script, make leader1 connect ...... leader1 is:", leader1)
	cfg.connect(leader1)
	// 3. 将
	leader2 := cfg.checkOneLeader()
	fmt.Println("TestReElection3A 2 success ")
	// if there's no quorum, no new leader should
	// be elected.

	fmt.Println("test script, make leader2 disconnects ...... leader2 is:", leader2)
	cfg.disconnect(leader2)
	fmt.Println("test script, make leader2 and leader 2+1 disconnects ...... leader2+1 is:", (leader2+1)%servers)
	cfg.disconnect((leader2 + 1) % servers)
	time.Sleep(2 * RaftElectionTimeout)

	// check that the one connected server
	// does not think it is the leader.
	fmt.Println("begin to test no leader for test3A 4 is success?")
	cfg.checkNoLeader()
	fmt.Println("TestReElection3A 4 success ")
	// if a quorum arises, it should elect a leader.
	fmt.Println("test script, make leader2+1 back to cluster, it should elect a leader ......")
	cfg.connect((leader2 + 1) % servers)
	fmt.Println("begin to test one leader only for test3A 4 is success?")
	cfg.checkOneLeader()

	// re-join of last node shouldn't prevent leader from existing.
	fmt.Println("leader2 rejoin to cluster")
	cfg.connect(leader2)
	fmt.Println("begin to test only one leader")
	cfg.checkOneLeader()

	fmt.Println("TestReElection3A all finish ......")
	cfg.end()
}

func TestManyElections3A(t *testing.T) {
	servers := 7
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3A): multiple elections")

	cfg.checkOneLeader()

	iters := 10
	for ii := 1; ii < iters; ii++ {
		// disconnect three nodes
		i1 := rand.Int() % servers
		i2 := rand.Int() % servers
		i3 := rand.Int() % servers
		cfg.disconnect(i1)
		cfg.disconnect(i2)
		cfg.disconnect(i3)

		// either the current leader should still be alive,
		// or the remaining four should elect a new one.
		cfg.checkOneLeader()

		cfg.connect(i1)
		cfg.connect(i2)
		cfg.connect(i3)
	}

	cfg.checkOneLeader()

	cfg.end()
}

func TestBasicAgree3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): basic agreement")

	iters := 3
	for index := 1; index < iters+1; index++ {
		nd, _ := cfg.nCommitted(index)
		if nd > 0 {
			t.Fatalf("some have committed before Start()")
		}

		xindex := cfg.one(index*100, servers, false)
		if xindex != index {
			t.Fatalf("got Index %v but expected %v", xindex, index)
		}
	}

	cfg.end()
}

// check, based on counting bytes of RPCs, that
// each command is sent to each peer just once.
func TestRPCBytes3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): RPC byte count")

	cfg.one(99, servers, false)
	bytes0 := cfg.bytesTotal()

	iters := 10
	var sent int64 = 0
	for index := 2; index < iters+2; index++ {
		cmd := randstring(5000)
		xindex := cfg.one(cmd, servers, false)
		if xindex != index {
			t.Fatalf("got Index %v but expected %v", xindex, index)
		}
		sent += int64(len(cmd))
	}

	bytes1 := cfg.bytesTotal()
	got := bytes1 - bytes0
	expected := int64(servers) * sent
	if got > expected+50000 {
		t.Fatalf("too many RPC bytes; got %v, expected %v", got, expected)
	}

	cfg.end()
}

// test just failure of followers.
func TestFollowerFailure3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): test progressive failure of followers")

	cfg.one(101, servers, false)

	// disconnect one follower from the network.
	leader1 := cfg.checkOneLeader()
	DPrintf("TestFollowerFailure3B: now leader is:%d, make %d disconnect ... ", leader1, (leader1+1)%servers)
	cfg.disconnect((leader1 + 1) % servers)

	// the leader and remaining follower should be
	// able to agree despite the disconnected follower.
	DPrintf("TestFollowerFailure3B:the leader and remaining follower should beable to agree despite the disconnected follower.")
	cfg.one(102, servers-1, false)
	time.Sleep(RaftElectionTimeout)
	cfg.one(103, servers-1, false)

	// disconnect the remaining follower
	leader2 := cfg.checkOneLeader()
	DPrintf("TestFollowerFailure3B: now leader is:%d, make two disconnect, one is:%d, another one is:%d ... ", leader2, (leader2+1)%servers, (leader2+2)%servers)
	cfg.disconnect((leader2 + 1) % servers)
	cfg.disconnect((leader2 + 2) % servers)

	// submit a command.
	DPrintf("TestFollowerFailure3B:i am going to start a 104 message to leader:%d, it is still in the cluster,so it can be submit success.", leader2)
	index, _, ok := cfg.rafts[leader2].Start(104)
	if ok != true {
		t.Fatalf("leader rejected Start()")
	}
	if index != 4 {
		t.Fatalf("expected Index 4, got %v", index)
	}

	time.Sleep(2 * RaftElectionTimeout)

	// check that command 104 did not commit.
	n, _ := cfg.nCommitted(index)
	DPrintf("begin to test how many server commit index:%d is commited? it is %d commited.and now leader:%d matchindex is:%v", index, n, leader2, cfg.rafts[leader2].matchIndex)
	if n > 0 {
		t.Fatalf("%v committed but no majority", n)
	}

	cfg.end()
}

// test just failure of leaders.
func TestLeaderFailure3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): test failure of leaders")

	cfg.one(101, servers, false)

	// disconnect the first leader.
	leader1 := cfg.checkOneLeader()
	cfg.disconnect(leader1)

	// the remaining followers should elect
	// a new leader.
	cfg.one(102, servers-1, false)
	time.Sleep(RaftElectionTimeout)
	cfg.one(103, servers-1, false)

	// disconnect the new leader.
	leader2 := cfg.checkOneLeader()
	cfg.disconnect(leader2)

	// submit a command to each server.
	for i := 0; i < servers; i++ {
		cfg.rafts[i].Start(104)
	}

	time.Sleep(2 * RaftElectionTimeout)

	// check that command 104 did not commit.
	n, _ := cfg.nCommitted(4)
	if n > 0 {
		t.Fatalf("%v committed but no majority", n)
	}

	cfg.end()
}

// test that a follower participates after
// disconnect and re-connect.
func TestFailAgree3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): agreement after follower reconnects")

	DPrintf("TestFailAgree3B:begin to test cmd:101 is commited?excep commit is 3")
	cfg.one(101, servers, false)

	// disconnect one follower from the network.
	// 将leader+1这个机器断连，此时这个机器其实是有101这条日志的。所以nextIndex应该是2.
	// 而且这个断连的机器是一个follower，所以他会一直超时选举，在后面重新加入集群之后，需要重新发起一次选举。
	leader := cfg.checkOneLeader()
	DPrintf("TestFailAgree3B:begin to disconnect server:%d", (leader+1)%servers)
	cfg.disconnect((leader + 1) % servers)

	// the leader and remaining follower should be
	// able to agree despite the disconnected follower.
	DPrintf("TestFailAgree3B:the leader and remaining follower should beable to agree despite the disconnected follower.")
	DPrintf("TestFailAgree3B:begin to test cmd 102 is 2 server commited?")

	cfg.one(102, servers-1, false)
	DPrintf("TestFailAgree3B:begin to test cmd 103 is 2 server commited?")
	cfg.one(103, servers-1, false)
	time.Sleep(RaftElectionTimeout)
	DPrintf("TestFailAgree3B:begin to test cmd 104 is 2 server commited?")
	cfg.one(104, servers-1, false)
	DPrintf("TestFailAgree3B:begin to test cmd 105 is 2 server commited?")
	cfg.one(105, servers-1, false)

	// re-connect
	DPrintf("TestFailAgree3B:make server:%d reconnect to cluster", (leader+1)%servers)
	cfg.connect((leader + 1) % servers)

	// the full set of servers should preserve
	// previous agreements, and be able to agree
	// on new commands.
	DPrintf("TestFailAgree3B:begin to test cmd 106 is 3 server commited?")
	cfg.one(106, servers, true)
	time.Sleep(RaftElectionTimeout)
	DPrintf("TestFailAgree3B:begin to test cmd 107 is 3 server commited?")
	cfg.one(107, servers, true)

	cfg.end()
}

func TestFailNoAgree3B(t *testing.T) {
	servers := 5
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): no agreement if too many followers disconnect")

	cfg.one(10, servers, false)

	// 3 of 5 followers disconnect
	leader := cfg.checkOneLeader()
	cfg.disconnect((leader + 1) % servers)
	cfg.disconnect((leader + 2) % servers)
	cfg.disconnect((leader + 3) % servers)

	index, _, ok := cfg.rafts[leader].Start(20)
	if ok != true {
		t.Fatalf("leader rejected Start()")
	}
	if index != 2 {
		t.Fatalf("expected Index 2, got %v", index)
	}

	time.Sleep(2 * RaftElectionTimeout)

	n, _ := cfg.nCommitted(index)
	if n > 0 {
		t.Fatalf("%v committed but no majority", n)
	}

	// repair
	cfg.connect((leader + 1) % servers)
	cfg.connect((leader + 2) % servers)
	cfg.connect((leader + 3) % servers)

	// the disconnected majority may have chosen a leader from
	// among their own ranks, forgetting Index 2.
	leader2 := cfg.checkOneLeader()
	index2, _, ok2 := cfg.rafts[leader2].Start(30)
	if ok2 == false {
		t.Fatalf("leader2 rejected Start()")
	}
	if index2 < 2 || index2 > 3 {
		t.Fatalf("unexpected Index %v", index2)
	}

	cfg.one(1000, servers, true)

	cfg.end()
}

func TestConcurrentStarts3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): concurrent Start()s")

	var success bool
loop:
	for try := 0; try < 5; try++ {
		if try > 0 {
			// give solution some time to settle
			time.Sleep(3 * time.Second)
		}

		leader := cfg.checkOneLeader()
		_, term, ok := cfg.rafts[leader].Start(1)
		if !ok {
			// leader moved on really quickly
			continue
		}

		iters := 5
		var wg sync.WaitGroup
		is := make(chan int, iters)
		for ii := 0; ii < iters; ii++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				i, term1, ok := cfg.rafts[leader].Start(100 + i)
				if term1 != term {
					return
				}
				if ok != true {
					return
				}
				is <- i
			}(ii)
		}

		wg.Wait()
		close(is)

		for j := 0; j < servers; j++ {
			if t, _ := cfg.rafts[j].GetState(); t != term {
				// Term changed -- can't expect low RPC counts
				continue loop
			}
		}

		failed := false
		cmds := []int{}
		for index := range is {
			cmd := cfg.wait(index, servers, term)
			if ix, ok := cmd.(int); ok {
				if ix == -1 {
					// peers have moved on to later terms
					// so we can't expect all Start()s to
					// have succeeded
					failed = true
					break
				}
				cmds = append(cmds, ix)
			} else {
				t.Fatalf("value %v is not an int", cmd)
			}
		}

		if failed {
			// avoid leaking goroutines
			go func() {
				for range is {
				}
			}()
			continue
		}

		for ii := 0; ii < iters; ii++ {
			x := 100 + ii
			ok := false
			for j := 0; j < len(cmds); j++ {
				if cmds[j] == x {
					ok = true
				}
			}
			if ok == false {
				t.Fatalf("cmd %v missing in %v", x, cmds)
			}
		}

		success = true
		break
	}

	if !success {
		t.Fatalf("Term changed too often")
	}

	cfg.end()
}

func TestRejoin3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): rejoin of partitioned leader")

	cfg.one(101, servers, true)

	// leader network failure
	leader1 := cfg.checkOneLeader()
	cfg.disconnect(leader1)

	// make old leader try to agree on some entries
	cfg.rafts[leader1].Start(102)
	cfg.rafts[leader1].Start(103)
	cfg.rafts[leader1].Start(104)

	// new leader commits, also for Index=2
	cfg.one(103, 2, true)

	// new leader network failure
	leader2 := cfg.checkOneLeader()
	cfg.disconnect(leader2)

	// old leader connected again
	cfg.connect(leader1)

	cfg.one(104, 2, true)

	// all together now
	cfg.connect(leader2)

	cfg.one(105, servers, true)

	cfg.end()
}

func TestBackup3B(t *testing.T) {
	servers := 5
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): leader backs up quickly over incorrect follower logs")

	//ran := rand.Int()
	DPrintf("TestBackup3B:make a rand num is:%d", 0)
	cfg.one(0, servers, true)

	// put leader and one follower in a partition
	leader1 := cfg.checkOneLeader()
	DPrintf("TestBackup3B:now leader is:%d, and make %d, %d ,%d to disconnect", leader1, (leader1+2)%servers, (leader1+3)%servers, (leader1+4)%servers)
	cfg.disconnect((leader1 + 2) % servers)
	cfg.disconnect((leader1 + 3) % servers)
	cfg.disconnect((leader1 + 4) % servers)
	DPrintf("TestBackup3B:%d, %d ,%d is all disconnect success", (leader1+2)%servers, (leader1+3)%servers, (leader1+4)%servers)

	// submit lots of commands that won't commit
	// 1 - 50 是不会提交的，并且只会出现在leader1和leader1 + 1这两个机器上面。
	for i := 0; i < 50; i++ {
		cfg.rafts[leader1].Start(i + 1)
	}

	DPrintf("TestBackup3B:send 50 log entry to leader1:%d, but now it is only %d, %d in cluster, so it commitIndex should equals"+
		" to:1", leader1, leader1, (leader1+1)%servers)
	time.Sleep(RaftElectionTimeout / 2)
	DPrintf("TestBackup3B:sleep over")
	DPrintf("TestBackup3B:begin to disconnect %d and %d, this two server should make the 50 entry log to same", (leader1+0)%servers, (leader1+1)%servers)
	// leader1和leader1+1这两个服务器还是会将那50个给弄成一致的
	cfg.disconnect((leader1 + 0) % servers)
	cfg.disconnect((leader1 + 1) % servers)
	DPrintf("=============first to print every server log=============")
	DPrintf("leader1 is:%d, server %d have:%v", leader1, leader1, cfg.rafts[leader1].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+1)%servers, cfg.rafts[(leader1+1)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+2)%servers, cfg.rafts[(leader1+2)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+3)%servers, cfg.rafts[(leader1+3)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+4)%servers, cfg.rafts[(leader1+4)%servers].log)
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	// allow other partition to recover
	// 这三个服务器放到集群里面了，所以他们会选举出新的leader
	DPrintf("TestBackup3B:make %d , %d, %d back to cluster", (leader1+2)%servers, (leader1+3)%servers, (leader1+4)%servers)
	cfg.connect((leader1 + 2) % servers)
	cfg.connect((leader1 + 3) % servers)
	cfg.connect((leader1 + 4) % servers)
	DPrintf("TestBackup3B:make %d , %d, %d already back to cluster", (leader1+2)%servers, (leader1+3)%servers, (leader1+4)%servers)

	// lots of successful commands to new group.
	DPrintf("TestBackup3B:begin to make 50 log entry to commited")
	for i := 0; i < 50; i++ {
		cfg.one(51+i, 3, true)
	}

	// now another partitioned leader and one follower
	leader2 := cfg.checkOneLeader()
	DPrintf("TestBackup3B:now new leader is:%d, and commitIndex should 51", leader2)
	DPrintf("=============second to print every server log=============")
	DPrintf("leader1 is:%d, server %d have:%v", leader1, leader1, cfg.rafts[leader1].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+1)%servers, cfg.rafts[(leader1+1)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+2)%servers, cfg.rafts[(leader1+2)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+3)%servers, cfg.rafts[(leader1+3)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+4)%servers, cfg.rafts[(leader1+4)%servers].log)
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	// leader1  			0 and 1-50     un commit
	// leader1 + 1  		0 and 1-50     un commit
	// leader1 + 2			0 and 51-100   commit
	// leader1 + 3          0 and 51-100   commit
	// leader1 + 4          0 and 51-100   commit

	// 从2，3，4中选举一个剥离出去
	other := (leader1 + 2) % servers
	if leader2 == other {
		other = (leader2 + 1) % servers
	}
	// 从新的集群中寻找一个非leader的剥离出去
	DPrintf("TestBackup3B:now make %d disconnect", other)
	cfg.disconnect(other)

	// lots more commands that won't commit
	for i := 0; i < 50; i++ {
		cfg.rafts[leader2].Start(101 + i)
	}

	// leader1  			[0 commit] , [1-50   un commit]
	// leader1 + 1  		[0 commit] , [1-50   un commit]
	// leader1 + 2			[0 commit] , [51-100  commit]    							这是other		leader1 + 2 并且非 leader2 剥离
	// leader1 + 3          [0 commit] , [51-100  commit] ,  [101 - 150 un commit] 						leader2
	// leader1 + 4          [0 commit] , [51-100  commit] , [101 - 150 un commit]

	// 此时继续给50条日志，并且不会被提交的，但是剩下的两个会达成共识
	DPrintf("TestBackup3B:the 50 entry should not make agreement")
	time.Sleep(RaftElectionTimeout / 2)

	DPrintf("=============forth to print every server log=============")
	DPrintf("leader1 is:%d, server %d have:%v", leader1, leader1, cfg.rafts[leader1].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+1)%servers, cfg.rafts[(leader1+1)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+2)%servers, cfg.rafts[(leader1+2)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+3)%servers, cfg.rafts[(leader1+3)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+4)%servers, cfg.rafts[(leader1+4)%servers].log)
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	DPrintf("===============================================================")

	// bring original leader back to life,
	DPrintf("make every one disconnect to cluster")
	for i := 0; i < servers; i++ {
		cfg.disconnect(i)
	}

	// make every one disconnect to cluster
	DPrintf("make %d, %d, %d back to cluster", (leader1+0)%servers, (leader1+1)%servers, other)
	cfg.connect((leader1 + 0) % servers)
	cfg.connect((leader1 + 1) % servers)
	cfg.connect(other)

	DPrintf("make last 50 log entry to be same.")
	// lots of successful commands to new group.
	for i := 0; i < 50; i++ {
		cfg.one(151+i, 3, true)
	}
	DPrintf("=============fivth to print every server log=============")
	DPrintf("leader1 is:%d, server %d have:%v", leader1, leader1, cfg.rafts[leader1].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+1)%servers, cfg.rafts[(leader1+1)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+2)%servers, cfg.rafts[(leader1+2)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+3)%servers, cfg.rafts[(leader1+3)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+4)%servers, cfg.rafts[(leader1+4)%servers].log)
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	// leader1  			[0 commit] , [51-100  commit]  , [151-200  commit] 						---------
	// leader1 + 1  		[0 commit] , [51-100  commit]  , [151-200  commit] 						---------》》》》to same cluster， 并且提交了150 - 200 的日志，所以，此处应该是
	// leader1 + 2			[0 commit] , [51-100  commit]  , [151-200  commit]    		这是other	---------

	// leader1 + 3          [0 commit] , [51-100  commit] ,  [101 - 150 un commit] 		disconnect
	// leader1 + 4          [0 commit] , [51-100  commit] , [101 - 150 un commit]		disconnect

	// now everyone
	DPrintf("now connect every one to cluster")
	for i := 0; i < servers; i++ {
		cfg.connect(i)
	}

	//lastLog := rand.Int()
	DPrintf("now make last log entry:%d to be same", 234)
	cfg.one(234, servers, true)

	DPrintf("=============six to print every server log=============")
	DPrintf("leader1 is:%d, server %d have:%v", leader1, leader1, cfg.rafts[leader1].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+1)%servers, cfg.rafts[(leader1+1)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+2)%servers, cfg.rafts[(leader1+2)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+3)%servers, cfg.rafts[(leader1+3)%servers].log)
	DPrintf("leader1 is:%d, server %d have:%v", leader1, (leader1+4)%servers, cfg.rafts[(leader1+4)%servers].log)
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	DPrintf("===============================================================")
	// leader1  			[0 commit] , [51-100  commit]  , [151-200  commit] 	, [234, commit]
	// leader1 + 1  		[0 commit] , [51-100  commit]  , [151-200  commit] 	, [234, commit]
	// leader1 + 2			[0 commit] , [51-100  commit]  , [151-200  commit]  , [234, commit]
	// leader1 + 3          [0 commit] , [51-100  commit]  , [151-200  commit] 	, [234, commit]
	// leader1 + 4          [0 commit] , [51-100  commit]  , [151-200  commit]	, [234, commit]

	cfg.end()
}

func TestCount3B(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3B): RPC counts aren't too high")

	rpcs := func() (n int) {
		for j := 0; j < servers; j++ {
			n += cfg.rpcCount(j)
		}
		return
	}

	leader := cfg.checkOneLeader()

	total1 := rpcs()

	if total1 > 30 || total1 < 1 {
		t.Fatalf("too many or few RPCs (%v) to elect initial leader\n", total1)
	}

	var total2 int
	var success bool
loop:
	for try := 0; try < 5; try++ {
		if try > 0 {
			// give solution some time to settle
			time.Sleep(3 * time.Second)
		}

		leader = cfg.checkOneLeader()
		total1 = rpcs()

		iters := 10
		starti, term, ok := cfg.rafts[leader].Start(1)
		if !ok {
			// leader moved on really quickly
			continue
		}
		cmds := []int{}
		for i := 1; i < iters+2; i++ {
			x := int(rand.Int31())
			cmds = append(cmds, x)
			index1, term1, ok := cfg.rafts[leader].Start(x)
			if term1 != term {
				// Term changed while starting
				continue loop
			}
			if !ok {
				// No longer the leader, so Term has changed
				continue loop
			}
			if starti+i != index1 {
				t.Fatalf("Start() failed")
			}
		}

		for i := 1; i < iters+1; i++ {
			cmd := cfg.wait(starti+i, servers, term)
			if ix, ok := cmd.(int); ok == false || ix != cmds[i-1] {
				if ix == -1 {
					// Term changed -- try again
					continue loop
				}
				t.Fatalf("wrong value %v committed for Index %v; expected %v\n", cmd, starti+i, cmds)
			}
		}

		failed := false
		total2 = 0
		for j := 0; j < servers; j++ {
			if t, _ := cfg.rafts[j].GetState(); t != term {
				// Term changed -- can't expect low RPC counts
				// need to keep going to update total2
				failed = true
			}
			total2 += cfg.rpcCount(j)
		}

		if failed {
			continue loop
		}

		if total2-total1 > (iters+1+3)*3 {
			t.Fatalf("too many RPCs (%v) for %v entries\n", total2-total1, iters)
		}

		success = true
		break
	}

	if !success {
		t.Fatalf("Term changed too often")
	}

	time.Sleep(RaftElectionTimeout)

	total3 := 0
	for j := 0; j < servers; j++ {
		total3 += cfg.rpcCount(j)
	}

	if total3-total2 > 3*20 {
		t.Fatalf("too many RPCs (%v) for 1 second of idleness\n", total3-total2)
	}

	cfg.end()
}

func TestPersist13C(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3C): basic persistence")

	cfg.one(11, servers, true)

	// crash and re-start all
	for i := 0; i < servers; i++ {
		cfg.start1(i, cfg.applier)
	}
	for i := 0; i < servers; i++ {
		cfg.disconnect(i)
		cfg.connect(i)
	}

	cfg.one(12, servers, true)

	leader1 := cfg.checkOneLeader()
	cfg.disconnect(leader1)
	cfg.start1(leader1, cfg.applier)
	cfg.connect(leader1)

	cfg.one(13, servers, true)

	leader2 := cfg.checkOneLeader()
	cfg.disconnect(leader2)
	cfg.one(14, servers-1, true)
	cfg.start1(leader2, cfg.applier)
	cfg.connect(leader2)

	cfg.wait(4, servers, -1) // wait for leader2 to join before killing i3

	i3 := (cfg.checkOneLeader() + 1) % servers
	cfg.disconnect(i3)
	cfg.one(15, servers-1, true)
	cfg.start1(i3, cfg.applier)
	cfg.connect(i3)

	cfg.one(16, servers, true)

	cfg.end()
}

func TestPersist23C(t *testing.T) {
	servers := 5
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3C): more persistence")

	index := 1
	for iters := 0; iters < 5; iters++ {
		cfg.one(10+index, servers, true)
		index++

		leader1 := cfg.checkOneLeader()

		cfg.disconnect((leader1 + 1) % servers)
		cfg.disconnect((leader1 + 2) % servers)

		cfg.one(10+index, servers-2, true)
		index++

		cfg.disconnect((leader1 + 0) % servers)
		cfg.disconnect((leader1 + 3) % servers)
		cfg.disconnect((leader1 + 4) % servers)

		cfg.start1((leader1+1)%servers, cfg.applier)
		cfg.start1((leader1+2)%servers, cfg.applier)
		cfg.connect((leader1 + 1) % servers)
		cfg.connect((leader1 + 2) % servers)

		time.Sleep(RaftElectionTimeout)

		cfg.start1((leader1+3)%servers, cfg.applier)
		cfg.connect((leader1 + 3) % servers)

		cfg.one(10+index, servers-2, true)
		index++

		cfg.connect((leader1 + 4) % servers)
		cfg.connect((leader1 + 0) % servers)
	}

	cfg.one(1000, servers, true)

	cfg.end()
}

func TestPersist33C(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3C): partitioned leader and one follower crash, leader restarts")

	cfg.one(101, 3, true)
	DPrintf("log 101 should make same")
	leader := cfg.checkOneLeader()
	DPrintf("leader is:%d, and disconnect %d server", leader, (leader+2)%servers)
	cfg.disconnect((leader + 2) % servers)

	DPrintf("now send 102 to leader:%d", leader)
	cfg.one(102, 2, true)

	DPrintf("now leader:%d, and leader + 1:%d should have 102 log, now crash this two server", leader, (leader+1)%servers)
	cfg.crash1((leader + 0) % servers)
	cfg.crash1((leader + 1) % servers)
	DPrintf("now make %d server back to cluster", (leader+2)%servers)
	cfg.connect((leader + 2) % servers)
	DPrintf("now make leader:%d back to cluster", (leader+0)%servers)
	cfg.start1((leader+0)%servers, cfg.applier)
	cfg.connect((leader + 0) % servers)

	DPrintf("now send log 103 to cluster, two server should make same, leader:%d and leader + 2:%d", leader, (leader+2)%servers)
	cfg.one(103, 2, true)

	DPrintf("now make leader + 1 %d back to cluster", (leader+1)%servers)
	cfg.start1((leader+1)%servers, cfg.applier)
	cfg.connect((leader + 1) % servers)

	DPrintf("now send 104 to leader, 3 server should make same ")
	cfg.one(104, servers, true)

	cfg.end()
}

// Test the scenarios described in Figure 8 of the extended Raft paper. Each
// iteration asks a leader, if there is one, to insert a command in the Raft
// log.  If there is a leader, that leader will fail quickly with a high
// probability (perhaps without committing the command), or crash after a while
// with low probability (most likey committing the command).  If the number of
// alive servers isn't enough to form a majority, perhaps start a new server.
// The leader in a new Term may try to finish replicating log entries that
// haven't been committed yet.
func TestFigure83C(t *testing.T) {
	servers := 5
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3C): Figure 8")

	cfg.one(rand.Int(), 1, true)

	nup := servers
	for iters := 0; iters < 1000; iters++ {
		leader := -1
		for i := 0; i < servers; i++ {
			if cfg.rafts[i] != nil {
				_, _, ok := cfg.rafts[i].Start(rand.Int())
				if ok {
					leader = i
				}
			}
		}

		// 小概率能够复制成功
		if (rand.Int() % 1000) < 100 {
			ms := rand.Int63() % (int64(RaftElectionTimeout/time.Millisecond) / 2)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		} else {
			// 大概率不能复制成功，这个leader只会将这个日志发送给其他服务器，但是不能提交它
			ms := (rand.Int63() % 13)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}

		if leader != -1 {
			cfg.crash1(leader)
			nup -= 1
		}

		if nup < 3 {
			s := rand.Int() % servers
			if cfg.rafts[s] == nil {
				cfg.start1(s, cfg.applier)
				cfg.connect(s)
				nup += 1
			}
		}
	}

	for i := 0; i < servers; i++ {
		if cfg.rafts[i] == nil {
			cfg.start1(i, cfg.applier)
			cfg.connect(i)
		}
	}

	cfg.one(rand.Int(), servers, true)

	cfg.end()
}

func TestUnreliableAgree3C(t *testing.T) {
	servers := 5
	cfg := make_config(t, servers, true, false)
	defer cfg.cleanup()

	cfg.begin("Test (3C): unreliable agreement")

	var wg sync.WaitGroup

	for iters := 1; iters < 50; iters++ {
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func(iters, j int) {
				defer wg.Done()
				cfg.one((100*iters)+j, 1, true)
			}(iters, j)
		}
		cfg.one(iters, 1, true)
	}

	cfg.setunreliable(false)

	wg.Wait()

	cfg.one(100, servers, true)

	cfg.end()
}

func TestFigure8Unreliable3C(t *testing.T) {
	servers := 5
	cfg := make_config(t, servers, true, false)
	defer cfg.cleanup()

	cfg.begin("Test (3C): Figure 8 (unreliable)")

	cfg.one(0, 1, true)

	nup := servers
	for iters := 0; iters < 1000; iters++ {
		if iters == 200 {
			cfg.setlongreordering(true)
		}
		leader := -1
		for i := 0; i < servers; i++ {
			_, _, ok := cfg.rafts[i].Start(1 + iters)
			if ok && cfg.connected[i] {
				DPrintf("server[%d]is leader, make %d save to log", i, iters+1)
				leader = i
			}
		}

		if (rand.Int() % 1000) < 100 {
			ms := rand.Int63() % (int64(RaftElectionTimeout/time.Millisecond) / 2)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		} else {
			ms := (rand.Int63() % 13)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}

		if leader != -1 && (rand.Int()%1000) < int(RaftElectionTimeout/time.Millisecond)/2 {
			cfg.disconnect(leader)
			nup -= 1
		}

		if nup < 3 {
			s := rand.Int() % servers
			if cfg.connected[s] == false {
				cfg.connect(s)
				nup += 1
			}
		}
	}

	DPrintf("TestFigure8Unreliable3C: make every server back to cluster")
	for i := 0; i < servers; i++ {
		if cfg.connected[i] == false {
			cfg.connect(i)
		}
	}

	DPrintf("TestFigure8Unreliable3C: test last value is same")
	cfg.one(9000, servers, true)

	cfg.end()
}

func internalChurn(t *testing.T, unreliable bool) {

	servers := 5
	cfg := make_config(t, servers, unreliable, false)
	defer cfg.cleanup()

	if unreliable {
		cfg.begin("Test (3C): unreliable churn")
	} else {
		cfg.begin("Test (3C): churn")
	}

	stop := int32(0)

	// create concurrent clients
	cfn := func(me int, ch chan []int) {
		var ret []int
		ret = nil
		defer func() { ch <- ret }()
		values := []int{}
		for atomic.LoadInt32(&stop) == 0 {
			x := rand.Int()
			index := -1
			ok := false
			for i := 0; i < servers; i++ {
				// try them all, maybe one of them is a leader
				cfg.mu.Lock()
				rf := cfg.rafts[i]
				cfg.mu.Unlock()
				if rf != nil {
					index1, _, ok1 := rf.Start(x)
					if ok1 {
						ok = ok1
						index = index1
					}
				}
			}
			if ok {
				// maybe leader will commit our value, maybe not.
				// but don't wait forever.
				for _, to := range []int{10, 20, 50, 100, 200} {
					nd, cmd := cfg.nCommitted(index)
					if nd > 0 {
						if xx, ok := cmd.(int); ok {
							if xx == x {
								values = append(values, x)
							}
						} else {
							cfg.t.Fatalf("wrong command type")
						}
						break
					}
					time.Sleep(time.Duration(to) * time.Millisecond)
				}
			} else {
				time.Sleep(time.Duration(79+me*17) * time.Millisecond)
			}
		}
		ret = values
	}

	ncli := 3
	cha := []chan []int{}
	for i := 0; i < ncli; i++ {
		cha = append(cha, make(chan []int))
		go cfn(i, cha[i])
	}

	for iters := 0; iters < 20; iters++ {
		if (rand.Int() % 1000) < 200 {
			i := rand.Int() % servers
			cfg.disconnect(i)
		}

		if (rand.Int() % 1000) < 500 {
			i := rand.Int() % servers
			if cfg.rafts[i] == nil {
				cfg.start1(i, cfg.applier)
			}
			cfg.connect(i)
		}

		if (rand.Int() % 1000) < 200 {
			i := rand.Int() % servers
			if cfg.rafts[i] != nil {
				cfg.crash1(i)
			}
		}

		// Make crash/restart infrequent enough that the peers can often
		// keep up, but not so infrequent that everything has settled
		// down from one change to the next. Pick a value smaller than
		// the election timeout, but not hugely smaller.
		time.Sleep((RaftElectionTimeout * 7) / 10)
	}

	time.Sleep(RaftElectionTimeout)
	cfg.setunreliable(false)
	for i := 0; i < servers; i++ {
		if cfg.rafts[i] == nil {
			cfg.start1(i, cfg.applier)
		}
		cfg.connect(i)
	}

	atomic.StoreInt32(&stop, 1)

	values := []int{}
	for i := 0; i < ncli; i++ {
		vv := <-cha[i]
		if vv == nil {
			t.Fatal("client failed")
		}
		values = append(values, vv...)
	}

	time.Sleep(RaftElectionTimeout)

	lastIndex := cfg.one(rand.Int(), servers, true)

	really := make([]int, lastIndex+1)
	for index := 1; index <= lastIndex; index++ {
		v := cfg.wait(index, servers, -1)
		if vi, ok := v.(int); ok {
			really = append(really, vi)
		} else {
			t.Fatalf("not an int")
		}
	}

	for _, v1 := range values {
		ok := false
		for _, v2 := range really {
			if v1 == v2 {
				ok = true
			}
		}
		if ok == false {
			cfg.t.Fatalf("didn't find a value")
		}
	}

	cfg.end()
}

func TestReliableChurn3C(t *testing.T) {
	internalChurn(t, false)
}

func TestUnreliableChurn3C(t *testing.T) {
	internalChurn(t, true)
}

const MAXLOGSIZE = 2000

func snapcommon(t *testing.T, name string, disconnect bool, reliable bool, crash bool) {
	iters := 30
	servers := 3
	cfg := make_config(t, servers, !reliable, true)
	defer cfg.cleanup()

	cfg.begin(name)

	cfg.one(0, servers, true)
	DPrintf("snapcommon: now command 0 should make same at three server")
	leader1 := cfg.checkOneLeader()
	DPrintf("snapcommon: leader1 is:%d", leader1)

	for i := 0; i < iters; i++ {
		victim := (leader1 + 1) % servers
		sender := leader1
		if i%3 == 1 {
			sender = (leader1 + 1) % servers
			victim = leader1
			DPrintf("snapcommon:this victim is leader:%d, and sender not affect,sender:%d", victim, sender)
		}

		if disconnect {
			DPrintf("snapcommon:make server[%d] disconnect", victim)
			cfg.disconnect(victim)
			ran := rand.Int()
			DPrintf("snapcommon:make %d to be same", ran)
			cfg.one(ran, servers-1, true)
		}
		if crash {
			DPrintf("snapcommon:make server[%d] crash", victim)
			cfg.crash1(victim)
			ran := rand.Int()
			DPrintf("snapcommon:make %d to be same", ran)
			cfg.one(ran, servers-1, true)
		}

		// perhaps send enough to get a snapshot
		outi := i
		nn := (SnapShotInterval / 2) + (rand.Int() % SnapShotInterval)
		for i := 0; i < nn; i++ {
			DPrintf("snapcommon: send %d to %d server", outi, sender)
			cfg.rafts[sender].Start(outi)
		}

		// let applier threads catch up with the Start()'s
		if disconnect == false && crash == false {
			// make sure all followers have caught up, so that
			// an InstallSnapshot RPC isn't required for
			// TestSnapshotBasic3D().
			cfg.one(888, servers, true)
			DPrintf("snapcommon: should make 888 to same")
		} else {
			ran := rand.Int()
			DPrintf("snapcommon:make %d to be same", ran)
			cfg.one(ran, servers-1, true)
		}

		if cfg.LogSize() >= MAXLOGSIZE {
			cfg.t.Fatalf("Log size too large")
		}
		if disconnect {
			// reconnect a follower, who maybe behind and
			// needs to rceive a snapshot to catch up.
			DPrintf("snapcommon:make disconnect server[%d]back", victim)
			cfg.connect(victim)
			ran := rand.Int()
			DPrintf("snapcommon:make %d to be same", ran)
			cfg.one(ran, servers, true)
			leader1 = cfg.checkOneLeader()
		}
		if crash {
			DPrintf("snapcommon:make crash server[%d] back", victim)
			cfg.start1(victim, cfg.applierSnap)
			cfg.connect(victim)
			cfg.one(rand.Int(), servers, true)
			leader1 = cfg.checkOneLeader()
		}
	}
	cfg.end()
}

func TestSnapshotBasic3D(t *testing.T) {
	snapcommon(t, "Test (3D): snapshots basic", false, true, false)
}

func TestSnapshotInstall3D(t *testing.T) {
	snapcommon(t, "Test (3D): install snapshots (disconnect)", true, true, false)
}

func TestSnapshotInstallUnreliable3D(t *testing.T) {
	snapcommon(t, "Test (3D): install snapshots (disconnect+unreliable)",
		true, false, false)
}

func TestSnapshotInstallCrash3D(t *testing.T) {
	snapcommon(t, "Test (3D): install snapshots (crash)", false, true, true)
}

func TestSnapshotInstallUnCrash3D(t *testing.T) {
	snapcommon(t, "Test (3D): install snapshots (unreliable+crash)", false, false, true)
}

// do the servers persist the snapshots, and
// restart using snapshot along with the
// tail of the log?
func TestSnapshotAllCrash3D(t *testing.T) {
	servers := 3
	iters := 5
	cfg := make_config(t, servers, false, true)
	defer cfg.cleanup()

	cfg.begin("Test (3D): crash and restart all servers")

	cfg.one(rand.Int(), servers, true)

	for i := 0; i < iters; i++ {
		// perhaps enough to get a snapshot
		nn := (SnapShotInterval / 2) + (rand.Int() % SnapShotInterval)
		for i := 0; i < nn; i++ {
			cfg.one(rand.Int(), servers, true)
		}

		index1 := cfg.one(rand.Int(), servers, true)

		// crash all
		for i := 0; i < servers; i++ {
			cfg.crash1(i)
		}

		// revive all
		for i := 0; i < servers; i++ {
			cfg.start1(i, cfg.applierSnap)
			cfg.connect(i)
		}

		index2 := cfg.one(rand.Int(), servers, true)
		if index2 < index1+1 {
			t.Fatalf("Index decreased from %v to %v", index1, index2)
		}
	}
	cfg.end()
}

// do servers correctly initialize their in-memory copy of the snapshot, making
// sure that future writes to persistent state don't lose state?
func TestSnapshotInit3D(t *testing.T) {
	servers := 3
	cfg := make_config(t, servers, false, true)
	defer cfg.cleanup()

	cfg.begin("Test (3D): snapshot initialization after crash")
	cfg.one(rand.Int(), servers, true)

	// enough ops to make a snapshot
	nn := SnapShotInterval + 1
	for i := 0; i < nn; i++ {
		cfg.one(rand.Int(), servers, true)
	}

	// crash all
	for i := 0; i < servers; i++ {
		cfg.crash1(i)
	}

	// revive all
	for i := 0; i < servers; i++ {
		cfg.start1(i, cfg.applierSnap)
		cfg.connect(i)
	}

	// a single op, to get something to be written back to persistent storage.
	cfg.one(rand.Int(), servers, true)

	// crash all
	for i := 0; i < servers; i++ {
		cfg.crash1(i)
	}

	// revive all
	for i := 0; i < servers; i++ {
		cfg.start1(i, cfg.applierSnap)
		cfg.connect(i)
	}

	// do another op to trigger potential bug
	cfg.one(rand.Int(), servers, true)
	cfg.end()
}

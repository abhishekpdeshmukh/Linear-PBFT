package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Node struct {
	pb.UnimplementedPBFTServiceServer
	pb.UnimplementedClientServiceServer
	nodeID               int32
	nodeBalances         map[string]int32
	isActive             bool
	isLeader             bool
	isbyzantine          bool
	isViewChangeProcess  bool
	lowestNextView       int32
	view                 int32
	publickeys           map[int32][]byte
	privateKey           ed25519.PrivateKey
	globalSequence       int
	lastStableCheckpoint int
	transactionQueue     chan *pb.TransactionRequest
	prepareChan          chan struct{}
	commitChan           chan struct{}
	notifyCh             chan struct{}
	processNotify        chan struct{}
	lock                 sync.Mutex
	processPool          map[int]bool
	logs                 map[int]Log
	ServerMapping        map[int32]int32
	viewChangeLog        map[int][]*pb.ViewChangeRequest
	prepareTrackers      map[int]*PrepareTracker // Trackers for each sequence number
	commitTrackers       map[int]*CommitTracker
	checkpointTracker    map[int]*CheckPointTracker
	viewChangeTracker    ViewChangeTracker
	timer                *time.Timer
}
type Log struct {
	sequenceNumber   int
	transactionID    int
	prePrepareMsgLog []*pb.PrePrepareRequest
	prepareMsgLog    []*pb.PrepareMessageRequest
	commitMsgLog     []*pb.CommitMessage
	checkpointMsgLog []*pb.CheckpointMsg
	currBalances     map[string]int32
	transaction      *pb.TransactionRequest
	digest           []byte
	viewNumber       int
	isCommitted      bool
	status           string
}

func main() {
	id, _ := strconv.Atoi(os.Args[1])

	currNode := &Node{
		nodeID:      int32(id),
		isActive:    true,
		isbyzantine: false,
		nodeBalances: map[string]int32{
			"A": 10,
			"B": 10,
			"C": 10,
			"D": 10,
			"E": 10,
			"F": 10,
			"G": 10,
			"H": 10,
			"I": 10,
			"J": 10,
		},
		view:                 1,
		publickeys:           make(map[int32][]byte),
		logs:                 make(map[int]Log),
		processPool:          make(map[int]bool),
		prepareTrackers:      make(map[int]*PrepareTracker),
		commitTrackers:       make(map[int]*CommitTracker),
		checkpointTracker:    make(map[int]*CheckPointTracker),
		viewChangeLog:        make(map[int][]*pb.ViewChangeRequest),
		transactionQueue:     make(chan *pb.TransactionRequest, 100),
		globalSequence:       0,
		lastStableCheckpoint: 0,
		prepareChan:          make(chan struct{}, 1),
		commitChan:           make(chan struct{}, 1),
		notifyCh:             make(chan struct{}, 1),
		processNotify:        make(chan struct{}, 100),
		lock:                 sync.Mutex{},
		ServerMapping: map[int32]int32{
			0: 7,
			1: 1,
			2: 2,
			3: 3,
			4: 4,
			5: 5,
			6: 6,
		},
	}
	currNode.isLeader = currNode.ServerMapping[currNode.view%7] == currNode.nodeID
	go timerThread(currNode)
	go setupReplicaReceiver(id, currNode)
	// go setupReplicaSender(id)
	go setupClientReceiver(id, currNode)
	// go setupClientSender(id)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	currNode.privateKey = privateKey
	if err != nil {
		fmt.Printf("Failed to generate key pair %d: %v\n", 1, err)

	}
	fmt.Printf("Entity %d:\nPublic Key: %x\nPrivate Key: %x\n\n", 1, publicKey, privateKey)
	sendKey(currNode, publicKey)
	currNode.StartTransactionProcessor()

	go executionThread(currNode)

	println("Node ", currNode.nodeID, " Working")

	println(currNode.publickeys)
	for {

	}

}

func (node *Node) PrePrepare(ctx context.Context, req *pb.PrePrepareMessageWrapper) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()
	if !node.isActive {
		return &emptypb.Empty{}, nil
	}
	prepreparemsg := req.PrePrepareRequest.PrePrepareMessage
	signature1 := req.PrePrepareRequest.Signature
	signature2 := req.TransactionRequest.Signature
	// Retrieve the Log entry for this SequenceNumber, or create a new one if it doesn't exist
	logEntry, exists := node.logs[int(prepreparemsg.SequenceNumber)]
	if !exists {
		logEntry = Log{} // Initialize a new Log entry if it doesn't exist
	}
	// Verify signatures
	isValid1 := ed25519.Verify(node.publickeys[prepreparemsg.LeaderId], prepreparemsg.TransactionDigest, signature1)
	isValid2 := ed25519.Verify(node.publickeys[0], prepreparemsg.TransactionDigest, signature2)
	fmt.Println("Signature Verification done")
	// If either signature is invalid, return early
	if !isValid1 || !isValid2 {
		return &emptypb.Empty{}, nil
	}

	// Check if the view number matches the node's view
	if prepreparemsg.ViewNumber != node.view {
		return &emptypb.Empty{}, nil
	}
	fmt.Println("View Verification done")
	// Check for an existing digest and ensure it matches the incoming TransactionDigest
	if logEntry.digest != nil && !bytes.Equal(logEntry.digest, prepreparemsg.TransactionDigest) {
		return &emptypb.Empty{}, nil
	}
	// Update the log entry's fields as necessary
	logEntry.prePrepareMsgLog = append(logEntry.prePrepareMsgLog, req.PrePrepareRequest)
	logEntry.digest = prepreparemsg.TransactionDigest
	logEntry.transaction = req.TransactionRequest
	logEntry.transactionID = int(req.TransactionRequest.TransactionId)
	logEntry.status = "PP"
	// Save the updated log entry back into the map
	// fmt.Println("Log entry Before ")
	// fmt.Println(node.logs[int(prepreparemsg.SequenceNumber)])
	node.logs[int(prepreparemsg.SequenceNumber)] = logEntry
	fmt.Println("Log entry After appending inside PREPREPARE")
	fmt.Println(node.logs[int(prepreparemsg.SequenceNumber)])
	// Prepare the PrepareMessage to be sent

	fmt.Println(node.logs[int(prepreparemsg.SequenceNumber)])
	preparemsg := &pb.PrepareMessage{
		ReplicaId:         node.nodeID,
		TransactionId:     prepreparemsg.TransactionId,
		ViewNumber:        prepreparemsg.ViewNumber,
		SequenceNumber:    prepreparemsg.SequenceNumber,
		TransactionDigest: prepreparemsg.TransactionDigest,
	}
	// Call sendCollectorPrepare asynchronously
	node.processPool[int(preparemsg.SequenceNumber)] = true
	node.processNotify <- struct{}{}
	if !node.isbyzantine {
		fmt.Println("Sending Collector Prepare in reply")
		go sendCollectorPrepare(node, preparemsg)
	}
	return &emptypb.Empty{}, nil
}

func (node *Node) Prepare(ctx context.Context, req *pb.PrepareMessageRequest) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()
	fmt.Println("*****************INSIDE PREPARE****************")
	sequenceNum := int(req.PrepareMessage.SequenceNumber)
	fmt.Println("Got Prepare Request from ", req.PrepareMessage.ReplicaId, "for SequenceNum:", sequenceNum)

	tracker, exists := node.prepareTrackers[sequenceNum]
	if !exists {
		tracker = NewPrepareTracker(time.Second * 2) // Example with f=2 and 10s timeout
		node.prepareTrackers[sequenceNum] = tracker
		fmt.Println("Initializing new PrepareTracker for SequenceNum:", sequenceNum)

		// Start timeout monitoring in a goroutine
		if !node.isbyzantine {
			go node.monitorPrepareTimeout(sequenceNum)
		}
	} else {
		fmt.Println("Tracker already exists for SequenceNum:", sequenceNum)
	}

	fmt.Printf("Current Prepare Trackers: %+v\n", node.prepareTrackers) // Print entire tracker map for debugging

	// Append PrepareMessage to the log
	logEntry, logExists := node.logs[sequenceNum]
	if !logExists {
		fmt.Println("Adding the first Prepare because it did not exist before")
		logEntry = Log{
			sequenceNumber: sequenceNum,
			viewNumber:     int(req.PrepareMessage.ViewNumber),
			transactionID:  int(req.PrepareMessage.TransactionId),
		}
	}
	logEntry.prepareMsgLog = append(logEntry.prepareMsgLog, req)
	node.logs[sequenceNum] = logEntry

	// Increment the prepare count and check thresholds
	// fmt.Println("Previous Count", tracker.prepareCount)
	tracker.prepareCount++
	fmt.Println("Current Count for ", sequenceNum, " ", tracker.prepareCount)
	if !node.isbyzantine {
		node.checkPrepareThresholds(tracker)
	}
	fmt.Println("*****************INSIDE PREPARE****************")
	return &emptypb.Empty{}, nil
}

func (node *Node) CollectedPrepare(ctx context.Context, req *pb.CollectPrepareRequest) (*emptypb.Empty, error) {
	if !node.isActive || node.isbyzantine {
		return &emptypb.Empty{}, nil
	}
	fmt.Println("Outside replicae collected  PREPARE")
	fmt.Println("AM i a leader ", node.isLeader)
	fmt.Println("My current View is ", node.view)
	seq := req.CollectPrepare.PrepareMessageRequest[0].PrepareMessage.SequenceNumber
	if len(req.CollectPrepare.PrepareMessageRequest) >= 3*F {

		fmt.Println("Inside Replica Accepting Collected Prepare and doing direct commit because I have 3f Collected Prepare")

		logEntry := node.logs[int(seq)]
		// Modify the field
		logEntry.prepareMsgLog = append(logEntry.prepareMsgLog, req.CollectPrepare.PrepareMessageRequest...)
		// Assign the modified struct back to the map
		logEntry.isCommitted = true
		logEntry.status = "C"
		node.logs[int(seq)] = logEntry
		node.notifyCh <- struct{}{}

	} else if len(req.CollectPrepare.PrepareMessageRequest) >= 2*F {
		temp := req.CollectPrepare.PrepareMessageRequest[0].PrepareMessage
		commitMsg := &pb.CommitMessage{
			SequenceNumber:    temp.SequenceNumber,
			TransactionId:     temp.TransactionId,
			ReplicaId:         temp.ReplicaId,
			ViewNumber:        temp.ViewNumber,
			TransactionDigest: temp.TransactionDigest,
		}
		logEntry := node.logs[int(seq)]
		logEntry.status = "P"
		node.logs[int(seq)] = logEntry
		fmt.Println("Inside Replica Accepting Collected Prepare and sending commit msgs because I have 2f Collected Prepare")
		for i := 1; i < 8; i++ {

			if i != int(node.nodeID) {
				go func(i int) {

					fmt.Println("Sending All collected Prepare to node ", i)
					c, ctx, conn := setupReplicaSender(i)

					// fmt.Println(seq)
					_, _ = c.Commit(ctx, commitMsg)
					conn.Close()
				}(i)
			}
		}
		fmt.Println(commitMsg)
	}
	return &emptypb.Empty{}, nil

}
func (node *Node) RequestViewChange(ctx context.Context, req *pb.ViewChangeRequest) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()

	fmt.Println("Got View Change Request from ", req.ReplicaId)
	fmt.Println("Last Stable Checkpoint ", req.LastStableCheckpoint)
	fmt.Println("Checkpoint Proof \n", req.CheckpointMsg)
	// fmt.Println("View Change load \n", req.ViewChangeLoad)
	fmt.Println("This server is forcing me to go to view ", req.NextView)
	if _, exists := node.viewChangeLog[int(req.NextView)]; !exists {
		// Initialize the slice if it does not exist
		node.viewChangeLog[int(req.NextView)] = []*pb.ViewChangeRequest{}
		node.viewChangeTracker.viewChangeCount = 0
	}
	node.viewChangeLog[int(req.NextView)] = append(node.viewChangeLog[int(req.NextView)], req)
	node.viewChangeTracker.viewChangeCount++
	node.lowestNextView = min(node.lowestNextView, req.NextView)
	node.checkViewChangeThresholds(&node.viewChangeTracker, int(req.NextView))

	return &emptypb.Empty{}, nil
}
func (node *Node) NewView(ctx context.Context, req *pb.NewViewMessage) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()
	// Check if ViewChangeReq is non-empty before accessing its elements
	if len(req.ViewChangeReq) > 0 && req.ViewChangeReq[0] != nil {
		if node.lastStableCheckpoint < int(req.MaxCheckpoint) {
			go synchCheckpoint(int(req.ViewChangeReq[0].LastStableCheckpoint))
		} else {
			node.lastStableCheckpoint = int(req.MaxCheckpoint)
		}
	}

	fmt.Println("Inside New View sent by", req.NewLeaderId)
	node.isViewChangeProcess = false
	// Iterate over PrePrepareReq and check for nil values
	for _, preprepare := range req.PrePrepareReq {
		// fmt.Println("Inside LOOP")
		fmt.Println()
		if preprepare != nil && preprepare.PrePrepareMessage != nil {
			tempSeq := preprepare.PrePrepareMessage.SequenceNumber
			fmt.Println("Temp SEQ ", tempSeq, " LastStable Check point ", node.lastStableCheckpoint)
			if tempSeq > int32(node.lastStableCheckpoint) {
				node.processPool[int(tempSeq)] = true
				if node.timer != nil {
					node.startOrResetTimer(time.Second * 10)
				}
				go node.AcceptNewViewPrePrepare(preprepare)
			}
		}
	}

	return &emptypb.Empty{}, nil
}

func (node *Node) Commit(ctx context.Context, req *pb.CommitMessage) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()
	sequenceNum := int(req.SequenceNumber)
	// Retrieve or initialize the CommitTracker
	tracker, exists := node.commitTrackers[sequenceNum]
	if !exists {
		tracker = NewCommitTracker(time.Second * 10) // Example with f=2
		node.commitTrackers[sequenceNum] = tracker
		if !node.isbyzantine {
			go node.monitorCommitTimeout(sequenceNum)
		}
	}
	tracker.commitCount++
	fmt.Println("Commit Count for", sequenceNum, ":", tracker.commitCount)
	if !node.isbyzantine {
		node.checkCommitThresholds(sequenceNum, tracker)
	}
	return &emptypb.Empty{}, nil
}
func (node *Node) RecieveCheckpoint(ctx context.Context, req *pb.CheckpointMsg) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()
	sequenceNum := int(req.SequenceNumber)
	// Retrieve or initialize the CommitTracker
	tracker, exists := node.checkpointTracker[sequenceNum]
	if !exists {
		tracker = NewCheckPointTracker(time.Second * 10) // Example with f=2
		node.checkpointTracker[sequenceNum] = tracker
		if !node.isbyzantine {
			go node.monitorCheckpointTimeout(sequenceNum)
		}
	}
	tracker.checkPointCount++
	log := node.logs[sequenceNum]
	log.checkpointMsgLog = append(log.checkpointMsgLog, req)
	node.logs[sequenceNum] = log
	fmt.Println("Checkpoint Count for", sequenceNum, ":", tracker.checkPointCount)
	if !node.isbyzantine {
		node.checkPointThresholds(tracker)
	}
	return &emptypb.Empty{}, nil
}

func (s *Node) Kill(ctx context.Context, req *pb.AdminRequest) (*pb.NodeResponse, error) {
	println(req.Command)
	if s.isActive {
		s.isActive = false
		return &pb.NodeResponse{Ack: "I wont respond consider me dead"}, nil
	} else {
		return &pb.NodeResponse{Ack: "I am already Dead why are you trying to kill  me again"}, nil
	}
}

func (node *Node) Revive(ctx context.Context, req *pb.ReviveRequest) (*pb.ReviveResponse, error) {
	node.lock.Lock()
	defer node.lock.Unlock()
	node.isActive = true
	node.isbyzantine = false
	return &pb.ReviveResponse{
		Success: true,
	}, nil
}

func (node *Node) BecomeMalicious(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()
	fmt.Println("Careful I am malicious now")
	node.isbyzantine = true
	return &emptypb.Empty{}, nil
}

func (node *Node) GetBalance(ctx context.Context, req *emptypb.Empty) (*pb.BalanceResponse, error) {
	node.lock.Lock()
	defer node.lock.Unlock()

	return &pb.BalanceResponse{
		Balance: node.nodeBalances,
	}, nil
}

func (node *Node) GetStatus(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	node.lock.Lock()
	defer node.lock.Unlock()

	return &pb.StatusResponse{
		State: node.logs[int(req.SequenceNumber)].status,
	}, nil
}

func (node *Node) Flush(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()

	close(node.transactionQueue)
	close(node.notifyCh)
	close(node.processNotify)

	node.isActive = true
	if node.nodeID == 1 {
		node.isLeader = true
	} else {
		node.isLeader = false
	}
	node.isbyzantine = false
	node.isViewChangeProcess = false
	node.lowestNextView = 0
	node.view = 1
	node.globalSequence = 0
	node.lastStableCheckpoint = 0

	// Clear all collections and data structures
	node.nodeBalances = map[string]int32{
		"A": 10,
		"B": 10,
		"C": 10,
		"D": 10,
		"E": 10,
		"F": 10,
		"G": 10,
		"H": 10,
		"I": 10,
		"J": 10,
	} // Reset balances to default

	// Clear logs and trackers
	node.logs = make(map[int]Log)
	node.processPool = make(map[int]bool)
	node.viewChangeLog = make(map[int][]*pb.ViewChangeRequest)
	node.prepareTrackers = make(map[int]*PrepareTracker)
	node.commitTrackers = make(map[int]*CommitTracker)
	node.checkpointTracker = make(map[int]*CheckPointTracker)
	node.viewChangeTracker = ViewChangeTracker{
		viewChangeCount:       0,
		viewChangeStartChan:   make(chan struct{}, 1),
		doneChan:              make(chan struct{}, 1),
		viewChangeSuccessChan: make(chan struct{}, 1),
	}

	// Clear channels
	node.transactionQueue = make(chan *pb.TransactionRequest, 100)
	node.prepareChan = make(chan struct{}, 1)
	node.commitChan = make(chan struct{}, 1)
	node.notifyCh = make(chan struct{}, 1)
	node.processNotify = make(chan struct{}, 100)

	// Stop and reset the timer
	if node.timer != nil {
		node.timer.Stop()
		node.timer = nil
	}
	node.StartTransactionProcessor()
	go executionThread(node)
	go timerThread(node)
	return &emptypb.Empty{}, nil
}

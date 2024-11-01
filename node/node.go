package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type PrepareTracker struct {
	prepareCount int
	prepareChan  chan struct{} // Channel to signal 2f prepares
	doneChan     chan struct{} // Channel to indicate timeout
	quorumChan   chan struct{} // Channel to signal 3f+1 prepares
	timeout      time.Duration // Timeout duration
	f            int           // Fault tolerance level
}
type Node struct {
	pb.UnimplementedPBFTServiceServer
	pb.UnimplementedClientServiceServer
	nodeID           int32
	isActive         bool
	isLeader         bool
	view             int32
	publickeys       map[int32][]byte
	privateKey       ed25519.PrivateKey
	globalSequence   int
	prepareCount     int
	transactionQueue chan *pb.TransactionRequest
	prepareChan      chan struct{}
	lock             sync.Mutex
	logs             map[int]Log
	prepareTrackers  map[int]*PrepareTracker // Trackers for each sequence number
}
type Log struct {
	sequenceNumber   int
	transactionID    int
	prePrepareMsgLog []*pb.PrePrepareRequest
	prepareMsgLog    []*pb.PrepareMessageRequest
	commitMsgLog     []*pb.CommitMessage
	transaction      *pb.Transaction
	digest           []byte
	viewNumber       int
}

func main() {
	id, _ := strconv.Atoi(os.Args[1])
	currNode := &Node{
		nodeID:           int32(id),
		isActive:         true,
		view:             1,
		publickeys:       make(map[int32][]byte),
		logs:             make(map[int]Log),
		prepareTrackers:  make(map[int]*PrepareTracker),
		transactionQueue: make(chan *pb.TransactionRequest, 100),
		globalSequence:   1,
		prepareChan:      make(chan struct{}, 1),
		lock:             sync.Mutex{},
	}
	currNode.isLeader = currNode.view%7 == currNode.nodeID

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
	println("Node ", currNode.nodeID, " Working")

	println(currNode.publickeys)
	for {

	}

}

func NewPrepareTracker(f int, timeout time.Duration) *PrepareTracker {
	return &PrepareTracker{
		prepareCount: 0,
		prepareChan:  make(chan struct{}, 1),
		doneChan:     make(chan struct{}, 1),
		quorumChan:   make(chan struct{}, 1),
		timeout:      timeout,
		f:            f,
	}
}
func transactionDigest(tx *pb.Transaction) ([]byte, error) {
	// Serialize the transaction
	data, err := proto.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %w", err)
	}
	// Compute the SHA-256 hash of the transaction data
	hash := sha256.New()
	hash.Write(data)
	return hash.Sum(nil), nil
}

func (node *Node) SendTransaction(ctx context.Context, req *pb.TransactionRequest) (*emptypb.Empty, error) {
	// fmt.Println("Received Transaction:", req.Transaction)

	// Enqueue the transaction
	if node.isActive && node.isLeader {
		node.transactionQueue <- req
		fmt.Println("Transaction enqueued")
	}

	return &emptypb.Empty{}, nil
}

// func (node *Node) SendTransaction(ctx context.Context, req *pb.TransactionRequest) (*emptypb.Empty, error) {
// 	// node.lock.Lock()
// 	// defer node.lock.Unlock()
// 	fmt.Println(req.Transaction)
// 	fmt.Println("Inside ")
// 	if node.isActive && node.isLeader {
// 		sendPreprepare(node, req)
// 	} else if node.isActive {

//		}
//		return &emptypb.Empty{}, nil
//	}

func (node *Node) StartTransactionProcessor() {
	fmt.Println("INSIDE  TRANSACTION PROCESSOR")
	go func() {
		for req := range node.transactionQueue {
			fmt.Println("INSIDE DEQUEUE")
			node.processTransaction(req)
		}
	}()
}

func (node *Node) processTransaction(req *pb.TransactionRequest) {
	node.lock.Lock()
	defer node.lock.Unlock()

	// Process the transaction
	fmt.Println("Processing Transaction:", req.Transaction)
	if node.isActive && node.isLeader {
		fmt.Println("Inside Actual Send Prepare")
		go sendPreprepare(node, req)
	} else if node.isActive {

	}
}

func (node *Node) PrePrepare(ctx context.Context, req *pb.PrePrepareMessageWrapper) (*emptypb.Empty, error) {

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

	// Save the updated log entry back into the map
	// fmt.Println("Log entry Before ")
	fmt.Println(node.logs[int(prepreparemsg.SequenceNumber)])
	node.logs[int(prepreparemsg.SequenceNumber)] = logEntry
	// fmt.Println("Log entry After ")
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
	fmt.Println("Sending Collector Prepare in reply")

	go sendCollectorPrepare(node, preparemsg)

	return &emptypb.Empty{}, nil
}
func (node *Node) Prepare(ctx context.Context, req *pb.PrepareMessageRequest) (*emptypb.Empty, error) {
	node.lock.Lock()
	defer node.lock.Unlock()

	sequenceNum := int(req.PrepareMessage.SequenceNumber)
	fmt.Println("Got Prepare Request from ", req.PrepareMessage.ReplicaId, "for SequenceNum:", sequenceNum)

	tracker, exists := node.prepareTrackers[sequenceNum]
	if !exists {
		tracker = NewPrepareTracker(2, time.Second*20) // Example with f=2 and 10s timeout
		node.prepareTrackers[sequenceNum] = tracker
		fmt.Println("Initializing new PrepareTracker for SequenceNum:", sequenceNum)

		// Start timeout monitoring in a goroutine
		go node.monitorPrepareTimeout(sequenceNum)
	} else {
		fmt.Println("Tracker already exists for SequenceNum:", sequenceNum)
	}

	fmt.Printf("Current Prepare Trackers: %+v\n", node.prepareTrackers) // Print entire tracker map for debugging

	// Append PrepareMessage to the log
	logEntry, logExists := node.logs[sequenceNum]
	if !logExists {
		logEntry = Log{
			sequenceNumber: sequenceNum,
			viewNumber:     int(req.PrepareMessage.ViewNumber),
		}
	}
	logEntry.prepareMsgLog = append(logEntry.prepareMsgLog, req)
	node.logs[sequenceNum] = logEntry

	// Increment the prepare count and check thresholds
	fmt.Println("Previous Count", tracker.prepareCount)
	tracker.prepareCount++
	fmt.Println("Current Count", tracker.prepareCount)
	node.checkPrepareThresholds(tracker)

	return &emptypb.Empty{}, nil
}

// Check prepare thresholds for 2f and 3f+1
func (node *Node) checkPrepareThresholds(tracker *PrepareTracker) {
	// Signal 2f prepares
	// Signal 3f+1 prepares
	if tracker.prepareCount >= 3*tracker.f {
		select {
		case tracker.quorumChan <- struct{}{}:
			fmt.Println("Received 3f Prepare messages in checkPRepare Thresholds")
		default:
		}
	}
	if tracker.prepareCount >= 2*tracker.f {
		select {
		case tracker.prepareChan <- struct{}{}:
			fmt.Println("Received 2f Prepare messages in check prepare threshholds")
		default:
		}
	}

}

// Monitor prepare timeout and take actions based on received Prepare counts
func (node *Node) monitorPrepareTimeout(sequenceNum int) {
	tracker := node.prepareTrackers[sequenceNum]
	done := make(chan struct{}) // Channel to cancel timeout on action

	select {
	case <-time.After(tracker.timeout):
		// Timeout case will only execute if `done` channel isn't closed
		select {
		case <-done:
			// Timeout was canceled, do nothing
			return
		default:
			// Timeout reached
			fmt.Println("Timeout reached for Prepare messages in monitor timeout")

			node.lock.Lock()
			defer node.lock.Unlock()

			if tracker.prepareCount >= 3*tracker.f {
				fmt.Println("3f+1 Prepare messages received, committing transaction in monitor timeout")

				// sendCollectedPrepare(node, sequenceNum)
				// Action for 3f+1 case
			} else if tracker.prepareCount >= 2*tracker.f {
				fmt.Println("Only 2f Prepare messages received, partial action")
				// Action for 2f case
			} else {
				fmt.Println("Insufficient Prepare messages received, aborting")
				fmt.Println(tracker.prepareCount)
				// Action for insufficient prepares
			}

			tracker.doneChan <- struct{}{} // Signal the timeout has completed
		}

	case <-tracker.quorumChan:
		// 3f+1 Prepare messages received before timeout, committing immediately
		close(done) // Cancel timeout by closing `done`
		fmt.Println("3f Prepare messages received before timeout, committing immediately in second half of monitor prepare timout")
		// sendCollectedPrepare(node, sequenceNum)
		// Action for immediate commit

	case <-tracker.prepareChan:
		// 2f Prepare messages received, processing accordingly
		close(done) // Cancel timeout by closing `done`
		fmt.Println("2f Prepare messages received, processing accordingly in 2nd half of monitor prepare timeout")
		sendCollectedPrepare(node, sequenceNum)
		// Action for 2f prepares
	}

	// delete(node.prepareTrackers, sequenceNum) // Clean up tracker after processing
}

func sendCollectedPrepare(node *Node, seq int) {
	fmt.Println("INSIDE sencollected prepare")
	prepare := &pb.CollectorPrepare{
		PrepareMessageRequest: node.logs[seq].prepareMsgLog,
	}
	digest, _ := transactionDigest(node.logs[seq].transaction)
	signature := ed25519.Sign(node.privateKey, digest)
	payLoad := &pb.CollectPrepareRequest{
		CollectPrepare: prepare,
		Signature:      signature,
	}
	for i := 1; i < 8; i++ {
		fmt.Println("Loop")
		if i != int(node.nodeID) {
			go func(i int) {

				fmt.Println("Sending All Prepare to nodes ", i)
				c, ctx, conn := setupReplicaSender(i)

				fmt.Println(seq)
				_, _ = c.CollectedPrepare(ctx, payLoad)
				conn.Close()
			}(i)
		}
	}

}
func (node *Node) CollectedPrepare(ctx context.Context, req *pb.CollectPrepareRequest) (*emptypb.Empty, error) {
	fmt.Println("Outside replicae collected  PREPARE")
	if len(req.CollectPrepare.PrepareMessageRequest) >= 3*2 {
		// req.CollectPrepare.PrepareMessageRequest[0].PrepareMessage.TransactionDigest
		fmt.Println("INSIDE replica collected PREPARE")
		c, ctx, conn := setupClientSender(0)
		c.ClientResponse(ctx, &pb.ClientRequest{
			ClientId:      node.nodeID,
			TransactionId: req.CollectPrepare.PrepareMessageRequest[0].PrepareMessage.TransactionId,
		})
		conn.Close()
	}
	return &emptypb.Empty{}, nil
}

// func (node *Node) ClientResponse(ctx context.Context, req *pb.ClientRequest) (*emptypb.Empty, error) {

//		return &emptypb.Empty{}, nil
//	}
func sendCollectorPrepare(node *Node, preparemsg *pb.PrepareMessage) {

	signature := ed25519.Sign(node.privateKey, preparemsg.TransactionDigest)
	prepareReq := &pb.PrepareMessageRequest{
		PrepareMessage: preparemsg,
		Signature:      signature,
	}
	go func(i int, prepareReq *pb.PrepareMessageRequest) {
		c, ctx, conn := setupReplicaSender(i)
		c.Prepare(ctx, prepareReq)
		conn.Close()
	}(int(node.view%7), prepareReq)
}
func sendPreprepare(node *Node, req *pb.TransactionRequest) {
	fmt.Println("Inside Send PRepare")
	node.lock.Lock()
	defer node.lock.Unlock()
	node.globalSequence++

	fmt.Println("Grabbed the lock")
	for i := 1; i < 8; i++ {
		fmt.Println("Loop")
		if i != int(node.nodeID) {
			go func(id int, transacid int, view int, privateKey ed25519.PrivateKey, seq int, i int, tran *pb.Transaction) {

				fmt.Println("Trying to send PrePrepare to ", i)
				c, ctx, conn := setupReplicaSender(i)
				digest, err := transactionDigest(tran)
				if err != nil {
					fmt.Println("Error in making digest ", err)
				}
				signature := ed25519.Sign(privateKey, digest)
				prePrepare := &pb.PrePrepareMessage{
					LeaderId:          int32(id),
					TransactionId:     int32(transacid),
					ViewNumber:        int32(view),
					TransactionDigest: digest,
					SequenceNumber:    int32(seq),
				}

				fmt.Println(seq)
				_, err = c.PrePrepare(ctx, &pb.PrePrepareMessageWrapper{
					PrePrepareRequest: &pb.PrePrepareRequest{
						PrePrepareMessage: prePrepare,
						Signature:         signature,
					},
					TransactionRequest: req,
				})

				conn.Close()
			}(int(node.nodeID), int(req.TransactionId), int(node.view), node.privateKey, node.globalSequence, i, req.Transaction)
		}
	}

}
func sendKey(node *Node, publicKey ed25519.PublicKey) {
	fmt.Println("Sending my Public key to everyone")
	for i := 1; i < 8; i++ {
		if node.nodeID != int32(i) {
			go func(nodeid int32, i int) {
				for {
					fmt.Println("Trying")
					c, ctx, conn := setupReplicaSender(i)
					publicKeyReq := &pb.PublicKeyRequest{
						Key: publicKey,
						Id:  nodeid,
					}
					r, err := c.ReceiveServerPublicKey(ctx, publicKeyReq)
					if err != nil {
						fmt.Println("Not able to send Public key")
					} else {
						if r.Success {
							fmt.Println("Successfully sent")
							break
						}
					}
					conn.Close()
				}
			}(node.nodeID, i)
		}
	}
	fmt.Println("Sending Key to Client")
	c, ctx, conn := setupClientSender(0)
	publicKeyReq := &pb.PublicKeyRequest{
		Key: publicKey,
	}
	c.ReceivePublicKey(ctx, publicKeyReq)
	conn.Close()
}

func (node *Node) ReceivePublicKey(ctx context.Context, req *pb.PublicKeyRequest) (*emptypb.Empty, error) {
	// fmt.Println(hex.EncodeToString(req.Key))
	fmt.Println("YEss! I got the ultimate client Key ", req.Id, " ", hex.EncodeToString(req.Key))
	// node.publickeys[req.Id] = hex.EncodeToString(req.Key)
	node.publickeys[req.Id] = req.Key
	return &emptypb.Empty{}, nil
}
func (node *Node) ReceiveServerPublicKey(ctx context.Context, req *pb.PublicKeyRequest) (*pb.Acknowledgment, error) {
	fmt.Println("YEss! I got a key from ", req.Id, " ", hex.EncodeToString(req.Key))
	// node.publickeys[req.Id] = hex.EncodeToString(req.Key)
	node.publickeys[req.Id] = req.Key
	return &pb.Acknowledgment{Success: true}, nil
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

func (s *Node) Revive(ctx context.Context, req *pb.ReviveRequest) (*pb.ReviveResponse, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.isActive = true
	return &pb.ReviveResponse{
		Success: true,
	}, nil
}
func setupReplicaReceiver(id int, node *Node) {
	lis, err := net.Listen("tcp", ":"+strconv.Itoa((5005+id+5)))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterPBFTServiceServer(s, node)
	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func setupReplicaSender(id int) (pb.PBFTServiceClient, context.Context, *grpc.ClientConn) {
	// fmt.Println("Setting Up RPC Reciever")
	conn, err := grpc.Dial("localhost:"+strconv.Itoa((5005+id+5)), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}
	// defer conn.Close()
	c := pb.NewPBFTServiceClient(conn)

	ctx, _ := context.WithTimeout(context.Background(), time.Second*10)
	// cancel()
	return c, ctx, conn
}

func setupClientSender(id int) (pb.ClientServiceClient, context.Context, *grpc.ClientConn) {
	conn, err := grpc.Dial("localhost:"+strconv.Itoa((4000+id)), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}
	c := pb.NewClientServiceClient(conn)
	ctx, _ := context.WithTimeout(context.Background(), time.Second*60)
	return c, ctx, conn
}

func setupClientReceiver(id int, node *Node) {
	lis, err := net.Listen("tcp", ":"+strconv.Itoa((4000+id)))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterClientServiceServer(s, node)
	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func (log Log) String() string {
	return fmt.Sprintf(
		"Log{sequenceNumber: %d, transactionID: %d, prePrepareMsgLog: %v, prepareMsgLog: %v, commitMsgLog: %v, transaction: %v, viewNumber: %d}",
		log.sequenceNumber,
		log.transactionID,
		log.prePrepareMsgLog,
		log.prepareMsgLog,
		log.commitMsgLog,
		log.transaction,
		// log.digest,
		log.viewNumber,
	)
}

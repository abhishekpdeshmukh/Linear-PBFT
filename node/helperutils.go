package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func sendCollectedPrepare(node *Node, seq int) {
	fmt.Println("INSIDE sencollected prepare")
	prepare := &pb.CollectorPrepare{
		PrepareMessageRequest: node.logs[seq].prepareMsgLog,
	}
	fmt.Println("Sequence number is ", seq)
	digest, _ := transactionDigest(node.logs[seq].transaction.Transaction)
	signature := ed25519.Sign(node.privateKey, digest)
	payLoad := &pb.CollectPrepareRequest{
		CollectPrepare: prepare,
		Signature:      signature,
	}
	for i := 1; i < 8; i++ {

		// if i != int(node.nodeID) {
		go func(i int) {

			fmt.Println("Sending All collected Prepare to node ", i)
			c, ctx, conn := setupReplicaSender(i)

			fmt.Println(seq)
			_, _ = c.CollectedPrepare(ctx, payLoad)
			conn.Close()
		}(i)
		// }
	}
	// time.Sleep(500*time.Millisecond)

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
func BalanceDigest(tx *pb.BalanceResponse) ([]byte, error) {
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
	} else if node.isActive {
		fmt.Println("DOING THE LEADER A FAVOUR BY SENDING THIS TO HIM")
		fmt.Println("SENDING IT TO ", node.ServerMapping[node.view%N])
		fmt.Println("My current View is ", node.view)
		fmt.Println("Am I a leader ", node.isLeader)
		// go func(i int, req *pb.TransactionRequest) {
		// 	c, ctx, conn := setupReplicaSender(i)
		// 	c.SendToNewLeader(ctx, req)
		// 	conn.Close()
		// }(int(node.ServerMapping[node.view%F]), req)
	}

	return &emptypb.Empty{}, nil
}

func (node *Node) StartTransactionProcessor() {
	fmt.Println("INSIDE  TRANSACTION PROCESSOR")
	go func() {
		for req := range node.transactionQueue {
			fmt.Println("INSIDE DEQUEUE started processing ", req.TransactionId, " from client ", req.ClientId)
			node.processTransaction(req)
		}
	}()
}

func (node *Node) processTransaction(req *pb.TransactionRequest) {
	fmt.Println("Inside PRocess Transaction about to grab locks")
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
	}(int(node.ServerMapping[node.view%F]), prepareReq)
}

func sendPreprepare(node *Node, req *pb.TransactionRequest) {
	fmt.Println("Inside Send PReprepare")
	node.lock.Lock()
	defer node.lock.Unlock()
	node.globalSequence++
	// logEntry, exists := node.logs[int(preparemsg.SequenceNumber)]
	// if !exists {
	// 	logEntry = Log{} // Initialize a new Log entry if it doesn't exist
	// }
	digest, err := transactionDigest(req.Transaction)
	if err != nil {
		fmt.Println("Error in making digest ", err)
	}
	signature := ed25519.Sign(node.privateKey, digest)
	prePrepare := &pb.PrePrepareMessage{
		LeaderId:          int32(node.nodeID),
		TransactionId:     int32(req.TransactionId),
		ViewNumber:        int32(node.view),
		TransactionDigest: digest,
		SequenceNumber:    int32(node.globalSequence),
	}
	logEntry := Log{
		prePrepareMsgLog: []*pb.PrePrepareRequest{
			{
				PrePrepareMessage: prePrepare,
				Signature:         signature,
			},
		},
		transaction:    req,
		transactionID:  int(req.TransactionId),
		sequenceNumber: node.globalSequence,
		status:         "PP",
		digest:         digest,
		viewNumber:     int(node.view),
	}
	node.logs[node.globalSequence] = logEntry
	node.processPool[int(req.TransactionId)] = true

	for i := 1; i < 8; i++ {
		fmt.Println("Loop")
		if i != int(node.nodeID) {
			go func(prePrepare *pb.PrePrepareMessage, id int) {

				fmt.Println("Trying to send PrePrepare to ", i)
				c, ctx, conn := setupReplicaSender(i)

				_, err = c.PrePrepare(ctx, &pb.PrePrepareMessageWrapper{
					PrePrepareRequest: &pb.PrePrepareRequest{
						PrePrepareMessage: prePrepare,
						Signature:         signature,
					},
					TransactionRequest: req,
				})

				conn.Close()
			}(prePrepare, i)
		}
	}

}
func timerThread(node *Node) {
	for {
		fmt.Println("I AM INSIDE THE TIMER THREAD")
		<-node.processNotify // Wait for notification to start the timer

		var currentSeq int

		// Start the inner loop for processing
		for {
			if node.isViewChangeProcess {
				time.Sleep(time.Second * 1)
				break
			}
			// Check if the process pool is empty
			if len(node.processPool) == 0 {
				fmt.Println("Hello All my processing is done")
				node.stopTimer() // Stop the timer if there's nothing to process
				break
			}

			// Get any sequence number from the pool to monitor
			for seq := range node.processPool {
				currentSeq = seq
				break
			}

			// Start or reset the timer for the current sequence number
			node.startOrResetTimer(5 * time.Second)

			<-node.timer.C // Wait for the timer to expire

			// Check if the sequence number is still in the process pool
			fmt.Println(node.processPool)
			if _, exists := node.processPool[currentSeq]; exists {
				fmt.Printf("Timeout reached for sequence %d: Initiating view change\n", currentSeq)
				node.processPool = make(map[int]bool) // NEw addition check later
				if !node.isViewChangeProcess {
					node.isViewChangeProcess = true
					node.stopTimer()
					node.initiateViewChange()
				}
				// Stop the timer as view change is initiated
				break // Exit the inner loop
			} else {
				fmt.Printf("Sequence %d processed before timeout\n", currentSeq)
				// Check if the pool is empty
				if len(node.processPool) == 0 {
					fmt.Println("All processing done, breaking out of the loop")
					node.stopTimer() // Stop the timer
					break            // Exit the inner loop
				}
				// Otherwise, restart the timer for the next sequence number
			}
		}
	}
}

func (node *Node) startOrResetTimer(duration time.Duration) {
	// If a timer already exists, stop it before creating a new one
	if node.timer != nil {
		node.timer.Stop()
		fmt.Println("Timer starts tik----tok----tik---tok")
	}
	// Create and start a new timer
	node.timer = time.NewTimer(duration)
}

func (node *Node) stopTimer() {
	if node.timer != nil {
		node.timer.Stop()
		fmt.Println("Stopped the Bloody Timer!!!")
		node.timer = nil // Clear the timer reference
	}
}
func executionThread(node *Node) {
	seqCounter := 1

	for {
		// Wait for a notification
		<-node.notifyCh

		// Process committed log entries
		for {
			// node.lock.Lock()
			entry, exists := node.logs[seqCounter]
			if !exists || !entry.isCommitted {
				// node.lock.Unlock()
				break // Exit the loop if the log entry does not exist or is not committed
			}

			// Execute the transaction
			if !node.isbyzantine && node.isActive {
				fmt.Println("INSIDE EXECUTOR")

				// Extract transaction details
				sender := entry.transaction.Transaction.Sender
				receiver := entry.transaction.Transaction.Receiver
				amount := entry.transaction.Transaction.Amount
				// if !node.transactionPool[entry.transactionID] && !(node.nodeBalances[sender]-int32(amount) >= 0) {

				fmt.Println("Sender:", sender, "Receiver:", receiver, "Amount:", amount)
				fmt.Println("Current Balances - Sender:", node.nodeBalances[sender], "Receiver:", node.nodeBalances[receiver])

				// Update balances
				node.lock.Lock()
				node.nodeBalances[sender] -= int32(amount)
				node.nodeBalances[receiver] += int32(amount)

				entry.status = "E"
				node.logs[seqCounter] = entry
				node.transactionPool[entry.transactionID] = true
				node.lock.Unlock()
				// }
				delete(node.processPool, seqCounter)
				// Unlock before making external calls
				// node.lock.Unlock()

				// Send a response to the client
				c, ctx, conn := setupClientSender(0)
				go func() {
					c.ServerResponse(ctx, &pb.ServerResponseMsg{
						ClientId:      sender,
						TransactionId: int32(entry.transactionID),
						View:          node.view,
					})
					conn.Close()
				}()

				// Checkpoint logic
				if seqCounter%CheckPoint == 0 {
					digest, _ := BalanceDigest(&pb.BalanceResponse{
						Balance: node.nodeBalances,
					})
					for i := 1; i < 8; i++ {
						go func(i int, digest []byte, seq int) {
							c, ctx, conn := setupReplicaSender(i)
							c.RecieveCheckpoint(ctx, &pb.CheckpointMsg{
								ReplicaId:      node.nodeID,
								SequenceNumber: int32(seq),
								Digest:         digest,
							})
							conn.Close()
						}(i, digest, seqCounter)
					}
				}
			} else {
				node.lock.Unlock() // Unlock if the node is Byzantine
			}

			// Increment the seqCounter to process the next entry
			seqCounter++
		}
	}
}

// func executionThread(node *Node) {
// 	seqCounter := 1

// 	for {
// 		// Wait for a notification
// 		<-node.notifyCh

// 		// Check if the current seqCounter log entry is committed
// 		for {
// 			entry, exists := node.logs[seqCounter]
// 			if !exists || !entry.isCommitted {
// 				break // Exit the loop if the log entry does not exist or is not committed
// 			}

// 			// Execute the transaction and send a reply to the client
// 			// updateState(seqCounter)
// 			// sendReplyToClient(seqCounter)
// 			if !node.isbyzantine {
// 				fmt.Println("INSIDE EXECUTOR")
// 				node.lock.Lock()
// 				sender := entry.transaction.Transaction.Sender
// 				fmt.Println("Sender ", sender)
// 				receiver := entry.transaction.Transaction.Receiver
// 				fmt.Println("Reciever ", receiver)
// 				amount := entry.transaction.Transaction.Amount
// 				fmt.Println("Amoun ", amount)
// 				fmt.Println("Current Balance of Accounts involved")
// 				fmt.Println("Sender Reciever ", node.nodeBalances[sender], " ", node.nodeBalances[receiver])
// 				node.nodeBalances[sender] -= int32(amount)
// 				node.nodeBalances[receiver] += int32(amount)
// 				delete(node.processPool, seqCounter)
// 				entry.status = "E"
// 				node.logs[seqCounter] = entry

// 				c, ctx, conn := setupClientSender(0)
// 				go c.ServerResponse(ctx, &pb.ServerResponseMsg{
// 					ClientId:      sender,
// 					TransactionId: int32(node.logs[seqCounter].transactionID),
// 					View:          node.view,
// 				})
// 				conn.Close()

// 				if seqCounter%CheckPoint == 0 {
// 					digest, _ := BalanceDigest(&pb.BalanceResponse{
// 						Balance: entry.currBalances,
// 					})
// 					for i := 1; i < 8; i++ {
// 						go func(i int, digest []byte, seq int) {
// 							c, ctx, conn := setupReplicaSender(i)
// 							c.RecieveCheckpoint(ctx, &pb.CheckpointMsg{
// 								ReplicaId:      node.nodeID,
// 								SequenceNumber: int32(seq),
// 								Digest:         digest,
// 							})
// 							conn.Close()
// 						}(i, digest, seqCounter)
// 					}
// 				}

// 			}
// 			// Increment the seqCounter to process the next entry
// 			node.lock.Unlock()
// 			seqCounter++
// 		}
// 	}
// }

func (node *Node) initiateViewChange() {
	var viewChangeLoadReq []*pb.ViewChangeLoad
	for i := node.lastStableCheckpoint + 1; i <= len(node.logs)-1; i++ {
		if !node.isbyzantine && len(node.logs[i].prepareMsgLog) >= 2*F {
			viewload := &pb.ViewChangeLoad{
				PrePrepareReq: node.logs[i].prePrepareMsgLog[0],
				PrepareReq:    node.logs[i].prepareMsgLog,
			}
			viewChangeLoadReq = append(viewChangeLoadReq, viewload)
		}
	}

	viewChangeReq := &pb.ViewChangeRequest{
		ReplicaId:            node.nodeID,
		NextView:             node.view + 1,
		LastStableCheckpoint: int32(node.lastStableCheckpoint),
		CheckpointMsg:        node.logs[node.lastStableCheckpoint].checkpointMsgLog,
		ViewChangeLoad:       viewChangeLoadReq,
	}

	for i := 1; i < 8; i++ {
		go func(i int, viewChangeReq *pb.ViewChangeRequest) {
			c, ctx, conn := setupReplicaSender(i)
			c.RequestViewChange(ctx, viewChangeReq)
			conn.Close()
		}(i, viewChangeReq)
	}
}

func (node *Node) sendNewView(newView int) {
	fmt.Println("Hi I am the leader sending NEw View MSGS to everyone")
	node.lock.Lock()
	defer node.lock.Unlock()
	viewchangeReq := node.viewChangeLog[newView]
	PrePrepare := make([]*pb.PrePrepareRequest, 100)
	node.isLeader = true
	maxCheckpoint := 0
	for _, viewChange := range viewchangeReq {
		maxCheckpoint = max(maxCheckpoint, int(viewChange.LastStableCheckpoint))
		if len(viewChange.ViewChangeLoad) > 0 {
			oldPreprepare := viewChange.ViewChangeLoad[0].PrePrepareReq
			newPreprepare := &pb.PrePrepareRequest{
				PrePrepareMessage: &pb.PrePrepareMessage{
					LeaderId:          node.nodeID,
					TransactionId:     oldPreprepare.PrePrepareMessage.TransactionId,
					ViewNumber:        int32(newView),
					SequenceNumber:    oldPreprepare.PrePrepareMessage.SequenceNumber,
					TransactionDigest: oldPreprepare.PrePrepareMessage.TransactionDigest,
				},
				Signature: oldPreprepare.Signature,
			}
			PrePrepare = append(PrePrepare, newPreprepare)
		}
	}
	newViewMsg := &pb.NewViewMessage{
		NextView:      int32(newView),
		NewLeaderId:   node.ServerMapping[int32(newView)%N],
		ViewChangeReq: node.viewChangeLog[newView],
		PrePrepareReq: PrePrepare,
		MaxCheckpoint: int32(maxCheckpoint),
	}
	for i := 0; i < 8; i++ {
		if i != int(node.nodeID) {
			go func(i int, viewChangeReq *pb.NewViewMessage) {
				c, ctx, conn := setupReplicaSender(i)
				c.NewView(ctx, newViewMsg)
				conn.Close()
			}(i, newViewMsg)
		}
	}
}

func (node *Node) AcceptNewViewPrePrepare(req *pb.PrePrepareRequest) {

	fmt.Println("Hi, I accept the new Preprepare sent by ", req.PrePrepareMessage.LeaderId)
	node.lock.Lock()
	defer node.lock.Unlock()
	node.viewChangeTracker.viewChangeCount = 0
	if !node.isActive {
		// return &emptypb.Empty{}, nil
		return
	}
	prepreparemsg := req.PrePrepareMessage
	signature1 := req.Signature

	// Retrieve the Log entry for this SequenceNumber, or create a new one if it doesn't exist
	logEntry, exists := node.logs[int(prepreparemsg.SequenceNumber)]
	if !exists {
		logEntry = Log{} // Initialize a new Log entry if it doesn't exist
	} else {
		signature2 := logEntry.transaction.Signature
		isValid2 := ed25519.Verify(node.publickeys[0], prepreparemsg.TransactionDigest, signature2)
		if !isValid2 {
			return
		}
	}
	// Verify signatures
	isValid1 := ed25519.Verify(node.publickeys[prepreparemsg.LeaderId], prepreparemsg.TransactionDigest, signature1)

	fmt.Println("Signature Verification done")
	// If either signature is invalid, return early
	if !isValid1 {
		return
	}

	// Check if the view number matches the node's view
	if prepreparemsg.ViewNumber != node.view {
		fmt.Println("Sorry Views dont Match")
		return
	}
	fmt.Println("View Verification done")
	// Check for an existing digest and ensure it matches the incoming TransactionDigest
	if logEntry.digest != nil && !bytes.Equal(logEntry.digest, prepreparemsg.TransactionDigest) {
		fmt.Println("Sorry Digest dont match")
		return
	}
	// Update the log entry's fields as necessary
	logEntry.prePrepareMsgLog = append(logEntry.prePrepareMsgLog, req)
	logEntry.digest = prepreparemsg.TransactionDigest
	// logEntry.transaction = req.TransactionRequest
	logEntry.transactionID = int(req.PrePrepareMessage.TransactionId)
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
}
func synchCheckpoint(checkpointSeq int) {
	fmt.Println("Helloo In the Synch checkpoint till  ", checkpointSeq)
}

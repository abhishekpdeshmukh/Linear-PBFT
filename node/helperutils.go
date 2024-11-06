package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"

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
	}(int(node.view%7), prepareReq)
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

func executionThread(node *Node) {
	seqCounter := 1

	for {
		// Wait for a notification
		<-node.notifyCh

		// Check if the current seqCounter log entry is committed
		for {
			entry, exists := node.logs[seqCounter]
			if !exists || !entry.isCommitted {
				break // Exit the loop if the log entry does not exist or is not committed
			}

			// Execute the transaction and send a reply to the client
			// updateState(seqCounter)
			// sendReplyToClient(seqCounter)
			if !node.isbyzantine {
				fmt.Println("INSIDE EXECUTOR")
				sender := entry.transaction.Transaction.Sender
				receiver := entry.transaction.Transaction.Receiver
				amount := entry.transaction.Transaction.Amount
				node.nodeBalances[sender] -= int32(amount)
				node.nodeBalances[receiver] += int32(amount)

				entry.status = "E"
				node.logs[seqCounter] = entry
				c, ctx, conn := setupClientSender(0)
				c.ServerResponse(ctx, &pb.ServerResponseMsg{
					ClientId:      sender,
					TransactionId: int32(node.logs[seqCounter].transactionID),
				})
				conn.Close()
				if seqCounter%CheckPoint == 0 {
					digest, _ := BalanceDigest(&pb.BalanceResponse{
						Balance: entry.currBalances,
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
			}
			// Increment the seqCounter to process the next entry
			seqCounter++
		}
	}
}

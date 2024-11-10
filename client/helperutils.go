package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/protobuf/proto"
)

func getClientHandler(id string) *ClientHandler {
	// temp, _ := strconv.Atoi(id)
	// clientID := temp - 96
	clientHandlersMu.Lock()
	defer clientHandlersMu.Unlock()

	// Check if a handler already exists for the client
	handler, exists := clientHandlers[id]
	if !exists {
		// If not, create a new handler with the simplified structure
		handler = &ClientHandler{
			replyCh:     make(chan bool, F+1),
			timer:       time.NewTimer(10 * time.Second),
			clientID:    id,
			processing:  false,
			processDone: make(chan struct{}, 1),
		}
		clientHandlers[id] = handler

		// Start the goroutine to monitor replies for this client
		go handler.monitorReplies()
	}

	return handler
}

func (ch *ClientHandler) monitorReplies() {
	for {
		ch.mu.Lock()
		if ch.processing {
			select {
			case <-ch.timer.C: // Timeout occurred
				fmt.Printf("Client %s: Timeout occurred for transaction %d, broadcasting to all nodes\n",
					ch.clientID, ch.currentTx.TransactionId)

				// Broadcast the transaction to all nodes
				go broadcastTransaction(ch.currentTx)

				// Reset the timer and continue waiting for replies
				ch.timer.Reset(10 * time.Second)
				ch.mu.Unlock()

			case <-ch.processDone: // Processing completed
				ch.processing = false
				ch.mu.Unlock()

			default:
				ch.mu.Unlock()
				time.Sleep(100 * time.Millisecond) // Prevent busy waiting
			}
		} else {
			ch.mu.Unlock()
			time.Sleep(100 * time.Millisecond) // Prevent busy waiting
		}
	}
}

// ReadTransactions function to process the updated CSV input file
func ReadTransactions(currClient *Client, filename string) (map[int][]*pb.TransactionRequest, map[int][]int, map[int][]int, error) {
	// Initialize maps to store transactions, live servers, and Byzantine servers for each set
	transactionSets := make(map[int][]*pb.TransactionRequest)
	liveServersMap := make(map[int][]int)
	byzantineServersMap := make(map[int][]int)

	// resp, err := http.Get(filename)
	// if err != nil {
	// 	return nil, nil, nil, fmt.Errorf("failed to download file: %v", err)
	// }
	// defer resp.Body.Close()

	// // Check if the response status is OK
	// if resp.StatusCode != http.StatusOK {
	// 	return nil, nil, nil, fmt.Errorf("failed to download file: status code %d", resp.StatusCode)
	// }
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, nil, err
	}
	defer file.Close()

	// reader := csv.NewReader(resp.Body)
	reader := csv.NewReader(file)
	var currentSetNumber int
	// fmt.Println("Reached here")
	// fmt.Println(resp.Body)
	for {
		record, err := reader.Read()
		// fmt.Println("Record is")
		// fmt.Println(record)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("Returning from here")
			return nil, nil, nil, err
		}

		// Check if this is a set header row (has set number)
		if len(record) > 0 && record[0] != "" {
			// Parse set number
			setNumber, err := strconv.Atoi(record[0])
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid set number: %s", record[0])
			}
			currentSetNumber = setNumber

			// Parse live servers
			if len(record) > 2 && record[2] != "" {
				liveServersStr := strings.Trim(record[2], "[]")
				currentAliveServers := []int{}
				for _, serverStr := range strings.Split(liveServersStr, ",") {
					server, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(serverStr), "S"))
					if err != nil {
						return nil, nil, nil, fmt.Errorf("invalid live server: %s", serverStr)
					}
					currentAliveServers = append(currentAliveServers, server)
				}
				liveServersMap[currentSetNumber] = currentAliveServers
			}

			// Parse Byzantine servers
			if len(record) > 3 && record[3] != "" {
				byzantineServersStr := strings.Trim(record[3], "[]")
				if byzantineServersStr != "" {
					currentByzantineServers := []int{}
					for _, serverStr := range strings.Split(byzantineServersStr, ",") {
						server, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(serverStr), "S"))
						if err != nil {
							return nil, nil, nil, fmt.Errorf("invalid byzantine server: %s", serverStr)
						}
						currentByzantineServers = append(currentByzantineServers, server)
					}
					byzantineServersMap[currentSetNumber] = currentByzantineServers
				}
			}
		}

		// Process transaction (present in the record)
		if len(record) > 1 && record[1] != "" {
			// Parse transaction details
			transactionStr := strings.Trim(record[1], "()")
			parts := strings.Split(transactionStr, ",")
			if len(parts) != 3 {
				return nil, nil, nil, fmt.Errorf("invalid transaction format: %s", record[1])
			}

			// Parse sender, receiver, and amount
			from := strings.TrimSpace(parts[0])
			to := strings.TrimSpace(parts[1])
			amount, err := strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid amount: %s", parts[2])
			}

			// Create transaction
			transaction := &pb.Transaction{
				Sender:   from,
				Receiver: to,
				Amount:   float32(amount),
			}

			// Sign transaction
			digest, err := transactionDigest(transaction)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("error creating transaction digest: %v", err)
			}
			signature := ed25519.Sign(currClient.privateKey, digest)

			// Create transaction request
			transRequest := &pb.TransactionRequest{
				SetNumber:     int32(currentSetNumber),
				ClientId:      from,
				TransactionId: int32(globalTransactionID),
				Transaction:   transaction,
				Signature:     signature,
			}

			// Add to transaction set
			if transactionSets[currentSetNumber] == nil {
				transactionSets[currentSetNumber] = make([]*pb.TransactionRequest, 0)
			}
			transactionSets[currentSetNumber] = append(transactionSets[currentSetNumber], transRequest)
			globalTransactionID++

		}
	}

	return transactionSets, liveServersMap, byzantineServersMap, nil
}

func transactionDigest(tx *pb.Transaction) ([]byte, error) {
	data, err := proto.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %w", err)
	}
	hash := sha256.New()
	hash.Write(data)
	return hash.Sum(nil), nil
}

func PrintLogs(logs []*pb.LogEntry) {
	// Initialize tab writer for formatted output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Print basic log info
	fmt.Println("========== Log Entries ==========")
	fmt.Fprintln(w, "SeqNum\tTxnID\tStatus\tViewNum\tTransaction")
	for _, logEntry := range logs {
		txn := logEntry.Transaction.Transaction
		fmt.Fprintf(w, "%d\t%d\t%s\t%d\tFrom %s to %s Amount %.2f\n",
			logEntry.SequenceNumber,
			logEntry.TransactionId,
			logEntry.Status,
			logEntry.ViewNumber,
			txn.Sender,
			txn.Receiver,
			txn.Amount)
	}
	w.Flush()

	// Print PrePrepare Messages
	fmt.Println("\n======== PrePrepare Messages ========")
	fmt.Fprintln(w, "SeqNum\tLeaderID\tViewNum\tTxnDigest")
	for _, logEntry := range logs {
		for _, pp := range logEntry.PrePrepareMessages {
			fmt.Fprintf(w, "%d\t%d\t%d\t%x\n",
				pp.PrePrepareMessage.SequenceNumber,
				pp.PrePrepareMessage.LeaderId,
				pp.PrePrepareMessage.ViewNumber,
				pp.PrePrepareMessage.TransactionDigest)
		}
	}
	w.Flush()

	// Print Prepare Messages
	fmt.Println("\n======== Prepare Messages ========")
	fmt.Fprintln(w, "SeqNum\tReplicaID\tViewNum\tTxnDigest")
	for _, logEntry := range logs {
		for _, p := range logEntry.PrepareMessages {
			fmt.Fprintf(w, "%d\t%d\t%d\t%x\n",
				p.PrepareMessage.SequenceNumber,
				p.PrepareMessage.ReplicaId,
				p.PrepareMessage.ViewNumber,
				p.PrepareMessage.TransactionDigest)
		}
	}
	w.Flush()

	// Print Commit Messages
	fmt.Println("\n======== Commit Messages ========")
	fmt.Fprintln(w, "SeqNum\tReplicaID\tViewNum\tTxnDigest")
	for _, logEntry := range logs {
		for _, c := range logEntry.CommitMessages {
			fmt.Fprintf(w, "%d\t%d\t%d\t%x\n",
				c.SequenceNumber,
				c.ReplicaId,
				c.ViewNumber,
				c.TransactionDigest)
		}
	}
	w.Flush()
}

func PrintNewViewMessages(newViewMessages []*pb.NewViewLogEntry) {
	if len(newViewMessages) == 0 {
		fmt.Println("No view changes have occurred.")
		return
	}

	for idx, newView := range newViewMessages {
		if newView == nil {
			continue // Skip nil entries
		}
		fmt.Printf("\n===== New View Message %d =====\n", idx+1)
		fmt.Printf("View Number: %d\n", newView.ViewNumber)
		fmt.Printf("New Leader ID: %d\n", newView.NewLeaderId)
		fmt.Printf("Max Checkpoint: %d\n", newView.MaxCheckpoint)

		// Print View Change Requests
		fmt.Println("\n-- View Change Requests --")
		for _, vcr := range newView.ViewChangeRequests {
			if vcr == nil {
				continue
			}
			fmt.Printf("\nReplica ID: %d\n", vcr.ReplicaId)
			// ... rest of the printing logic
		}

		// Print PrePrepare Requests
		fmt.Println("\n-- PrePrepare Requests --")
		for _, pp := range newView.PrePrepareRequests {
			if pp == nil || pp.PrePrepareMessage == nil {
				continue
			}
			ppm := pp.PrePrepareMessage
			fmt.Printf("Sequence Number: %d, Transaction ID: %d, View Number: %d, Leader ID: %d\n",
				ppm.SequenceNumber, ppm.TransactionId, ppm.ViewNumber, ppm.LeaderId)
		}
	}
}

func PrintPerformance() {
	var totalTransactions int
	var totalTime time.Duration
	var totalLatency time.Duration
	var totalThroughput float64

	clientHandlersMu.Lock()
	defer clientHandlersMu.Unlock()

	fmt.Println("\n========== Performance Metrics ==========")
	for clientID, handler := range clientHandlers {
		handler.mu.Lock()

		numTransactions := handler.transactionsNum
		if numTransactions == 0 {
			handler.mu.Unlock()
			continue
		}

		// Calculate total time for this client
		clientTotalTime := handler.totalEndTime.Sub(handler.totalStartTime)

		// Sum up latencies
		clientTotalLatency := time.Duration(0)
		for _, latency := range handler.latencies {
			clientTotalLatency += latency
		}

		// Calculate average latency and throughput
		avgLatency := clientTotalLatency / time.Duration(numTransactions)
		throughput := float64(numTransactions) / clientTotalTime.Seconds()

		fmt.Printf("Client ID: %s\n", clientID)
		fmt.Printf("  Total Transactions: %d\n", numTransactions)
		fmt.Printf("  Total Time: %.2f seconds\n", clientTotalTime.Seconds())
		fmt.Printf("  Average Latency: %.2f ms\n", avgLatency.Seconds()*1000)
		fmt.Printf("  Throughput: %.2f transactions/sec\n\n", throughput)

		// Aggregate totals
		totalTransactions += numTransactions
		totalTime += clientTotalTime
		totalLatency += clientTotalLatency
		totalThroughput += throughput

		handler.mu.Unlock()
	}

	// Calculate overall performance
	if totalTransactions > 0 && totalTime.Seconds() > 0 {
		overallAvgLatency := totalLatency / time.Duration(totalTransactions)
		overallThroughput := float64(totalTransactions) / totalTime.Seconds()

		fmt.Println("----- Overall Performance -----")
		fmt.Printf("Total Transactions: %d\n", totalTransactions)
		fmt.Printf("Total Time: %.2f seconds\n", totalTime.Seconds())
		fmt.Printf("Average Latency per Transaction: %.2f ms\n", overallAvgLatency.Seconds()*1000)
		fmt.Printf("Throughput: %.2f transactions/sec\n", overallThroughput)
	} else {
		fmt.Println("No transactions were processed.")
	}
}

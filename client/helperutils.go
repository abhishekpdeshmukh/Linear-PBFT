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
			ch.mu.Unlock()
			select {
			case <-ch.timer.C: // Timeout occurred
				ch.mu.Lock()
				if ch.processing {
					fmt.Printf("Client %s: Timeout occurred for transaction %d, broadcasting to all nodes\n",
						ch.clientID, ch.currentTx.TransactionId)
					// Broadcast to all nodes because we did not receive f+1 responses
					// go broadcastTransaction(ch.currentTx)
					// ch.replyCount = 0
					// ch.processing = false
					// ch.processDone <- struct{}{} // Signal processing complete
				}
				ch.mu.Unlock()
			default:
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

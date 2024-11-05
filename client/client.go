package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"

	"fmt"
	"log"

	"sync"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Client struct {
	pb.UnimplementedClientServiceServer
	id         int32
	privateKey ed25519.PrivateKey
	view       int32
	// transactionMap map[int]TransactionHandler
}

type ClientHandler struct {
	replyCh     chan bool
	timer       *time.Timer
	clientID    string
	currentTx   *pb.TransactionRequest
	mu          sync.Mutex
	replyCount  int
	processing  bool
	processDone chan struct{} // Channel to signal processing completion
}

const N = 7
const F int = (N - 1) / 3

var clientHandlers = make(map[string]*ClientHandler)
var clientHandlersMu sync.Mutex
var transactionSets = make(map[int][]*pb.TransactionRequest)
var liveServersMap = make(map[int][]int)
var byzantineServersMap = make(map[int][]int)
var killedServers = make(map[int]bool)
var previouslyKilledServers = make(map[int]bool) // Track previously killed servers
var globalTransactionID = 1

func main() {
	// Initialize key pair for the client
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	currClient := &Client{id: 0, privateKey: privateKey, view: 1}
	if err != nil {
		fmt.Printf("Failed to generate key pair %d: %v\n", 1, err)
	}

	fmt.Printf("Entity %d:\nPublic Key: %x\nPrivate Key: %x\n\n", 1, publicKey, privateKey)
	var currentSetNumber = 1
	var allSetsRead = false
	go setupClientReceiver(int(currClient.id), currClient)

	// Menu loop
	// transactionMap := make(map[int]*TransactionHandler)
	for {
		// Display menu options
		fmt.Println("\nSelect an option:")
		fmt.Println("1. Read Transaction SET on Default Path")
		fmt.Println("2. Send Transactions (Next Set)")
		fmt.Println("3. Send Client Public Key")
		fmt.Println("4. Print Balance on Specific Server")
		fmt.Println("5. Print Log for Specific Server")               // New option
		fmt.Println("6. Print DB State")                              // New option
		fmt.Println("7. Print Transaction Status by Sequence Number") // New option
		fmt.Println("8. Print View Changes")                          // New option
		fmt.Println("9. Spawn All Nodes")
		fmt.Println("10. Exit")

		// Read user's choice
		var option int
		fmt.Print("Enter your option: ")
		_, err := fmt.Scanln(&option)
		if err != nil {
			fmt.Println("Invalid input, please enter a number.")
			continue
		}

		// Handle user input
		switch option {
		case 1:
			fmt.Println("Executing: Read Transaction SET on Default Path")
			transactionSets, liveServersMap, byzantineServersMap, err = ReadTransactions(currClient, "../test.csv")
			if err != nil {
				fmt.Printf("Error reading transactions: %v\n", err)
				continue
			}
			allSetsRead = true
			currentSetNumber = 1
			fmt.Println(transactionSets)
			fmt.Println("Transactions successfully read.")

		case 2:
			if !allSetsRead {
				fmt.Println("No transactions have been read. Please choose option 1 first.")
				continue
			}

			// Check if the current set has already been processed
			if currentSetNumber > len(transactionSets) {
				fmt.Println("No more sets to send.")
				continue
			}

			if transactions, ok := transactionSets[currentSetNumber]; ok {
				fmt.Printf("Processing Set %d\n", currentSetNumber)

				// Group transactions by client ID
				clientTransactions := make(map[string][]*pb.TransactionRequest)
				for _, tx := range transactions {
					clientTransactions[tx.ClientId] = append(clientTransactions[tx.ClientId], tx)
				}

				aliveServers := liveServersMap[currentSetNumber]
				aliveServerSet := make(map[int]bool)
				for _, server := range aliveServers {
					aliveServerSet[server] = true

					// Only revive if the server was previously killed
					if killedServers[server] {
						fmt.Printf("Reviving Server %d for this set\n", server)
						c, ctx, conn := setupClientSender(server)
						_, err := c.Revive(ctx, &pb.ReviveRequest{NodeID: int32(server)})
						if err != nil {
							log.Printf("Failed to revive Server %d: %v", server, err)
						}
						conn.Close()

						// Mark the server as alive
						killedServers[server] = false
					}
				}

				// Kill servers that are not supposed to be active for this set
				for i := 1; i <= 7; i++ { // Assuming there are 7 servers
					if !aliveServerSet[i] && !killedServers[i] {
						fmt.Printf("Killing Server %d for this set\n", i)
						c, ctx, conn := setupClientSender(i)
						_, err := c.Kill(ctx, &pb.AdminRequest{Command: "Die and Perish"})
						if err != nil {
							log.Printf("Failed to kill Server %d: %v", i, err)
						}
						conn.Close()

						// Mark the server as killed
						killedServers[i] = true
					}
				}

				// Process transactions for each client
				var wg sync.WaitGroup
				for clientID, txs := range clientTransactions {
					wg.Add(1)
					go func(cID string, transactions []*pb.TransactionRequest) {
						defer wg.Done()
						handler := getClientHandler(cID)

						// Process transactions sequentially for this client
						for _, tx := range transactions {
							// Wait for the previous transaction to complete
							handler.mu.Lock()
							for handler.processing {
								handler.mu.Unlock()
								time.Sleep(10 * time.Second)
								handler.mu.Lock()
							}

							// Start processing the new transaction
							handler.currentTx = tx
							handler.replyCount = 0
							handler.processing = true
							handler.timer.Reset(8 * time.Second) // Restart the timer for the new transaction
							handler.mu.Unlock()

							// Send transaction to primary
							go func() {
								c, ctx, conn := setupClientSender(int(currClient.view) % 7)
								defer conn.Close()
								_, err := c.SendTransaction(ctx, tx)
								if err != nil {
									log.Printf("Failed to send transaction to primary: %v", err)
								}
							}()
						}
					}(clientID, txs)
				}

				wg.Wait()

				// Increment the set number only after processing all transactions in the set
				currentSetNumber++
			} else {
				fmt.Println("No more sets to send.")
			}

		case 3:
			sendKey(publicKey)

		case 4:
			fmt.Println("Executing: Print Balance on Specific Server")
			// Placeholder for balance logic

		case 5:
			fmt.Println("Executing: Print Log for Specific Server")
			// Placeholder for printing the log of a specific server

		case 6:
			fmt.Println("Executing: Print DB State")
			// Placeholder for printing the DB state

		case 7:
			fmt.Println("Executing: Print Transaction Status by Sequence Number")
			// Placeholder for printing transaction status

		case 8:
			fmt.Println("Executing: Print View Changes")
			// Placeholder for printing view change messages

		case 9:
			fmt.Println("Executing: Spawn All Nodes")
			// Placeholder for spawning all nodes logic

		case 10:
			fmt.Println("Exiting...")
			return

		default:
			fmt.Println("Invalid option, please try again.")
		}
	}
}

func (c *Client) ServerResponse(ctx context.Context, req *pb.ServerResponseMsg) (*emptypb.Empty, error) {
	fmt.Printf("Received ClientResponse for Client %s, Transaction %d\n", req.ClientId, req.TransactionId)

	// Find the appropriate client handler
	clientHandlersMu.Lock()
	handler, exists := clientHandlers[req.ClientId]
	clientHandlersMu.Unlock()

	if exists {
		handler.mu.Lock()
		defer handler.mu.Unlock()

		// Check if the transaction ID matches the current transaction being processed
		// req.TransactionId == handler.currentTx.TransactionId
		if handler.processing {
			// Increment the reply count
			handler.replyCount++
			fmt.Printf("Client %s: Current reply count for transaction %d is %d\n", req.ClientId, req.TransactionId, handler.replyCount)

			// If we've received f+1 replies, stop the timer and prepare for the next transaction
			if handler.replyCount >= F+1 {
				if !handler.timer.Stop() {
					<-handler.timer.C // Drain the timer if it already fired
				}
				fmt.Printf("Client %s: Received f+1 replies for transaction %d\n", req.ClientId, req.TransactionId)
				handler.replyCount = 0
				handler.processing = false
				handler.processDone <- struct{}{} // Signal processing completion
			}
		}
	}

	return &emptypb.Empty{}, nil
}

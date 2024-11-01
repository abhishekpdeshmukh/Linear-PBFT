package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
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
	commitCh    chan bool
	responseCh  chan *pb.ClientRequest // New channel for RPC responses
	timer       *time.Timer
	clientID    int32
	currentTx   *pb.TransactionRequest
	mu          sync.Mutex
	replyCount  int
	processing  bool
	processDone chan struct{} // Channel to signal processing completion
}
type TransactionHandler struct {
	replyCh chan bool   // Channel to track replies for each transaction
	timer   *time.Timer // Timer to track timeout
}

const N = 7
const F int = (N - 1) / 2

var clientHandlers = make(map[int32]*ClientHandler)
var clientHandlersMu sync.Mutex
var transactionSets = make(map[int][]*pb.TransactionRequest)
var liveServersMap = make(map[int][]int)
var previouslyKilledServers = make(map[int]bool) // Track previously killed servers
var globalTransactionID = 1

func getClientHandler(clientID int32) *ClientHandler {
	clientHandlersMu.Lock()
	defer clientHandlersMu.Unlock()

	if handler, exists := clientHandlers[clientID]; !exists {
		handler = &ClientHandler{
			replyCh:     make(chan bool, F+1),
			commitCh:    make(chan bool, 1),
			responseCh:  make(chan *pb.ClientRequest, F+1),
			timer:       time.NewTimer(5 * time.Second),
			clientID:    clientID,
			processing:  false,
			processDone: make(chan struct{}, 1),
		}
		clientHandlers[clientID] = handler
		// Start goroutine to monitor replies for this client
		go handler.monitorReplies()
		go handler.processResponses()
	}
	return clientHandlers[clientID]

}

func (ch *ClientHandler) processResponses() {
	for {
		select {
		case req := <-ch.responseCh:
			ch.mu.Lock()
			if ch.processing && req.TransactionId == ch.currentTx.TransactionId {
				fmt.Printf("Received response for Client %d, Transaction %d\n",
					ch.clientID, req.TransactionId)
				ch.replyCh <- true
			}
			ch.mu.Unlock()
		}
	}
}

func (ch *ClientHandler) monitorReplies() {
	for {
		ch.mu.Lock()
		if ch.processing {
			ch.mu.Unlock()
			select {
			case <-ch.replyCh:
				ch.mu.Lock()
				ch.replyCount++
				if ch.replyCount >= F+1 {
					if !ch.timer.Stop() {
						<-ch.timer.C
					}
					fmt.Printf("Client %d: Received f+1 replies for transaction %d\n",
						ch.clientID, ch.currentTx.TransactionId)
					ch.replyCount = 0
					ch.processing = false
					ch.processDone <- struct{}{} // Signal processing complete
				}
				ch.mu.Unlock()

			case <-ch.timer.C:
				ch.mu.Lock()
				fmt.Printf("Client %d: Timeout occurred for transaction %d, broadcasting to all nodes\n",
					ch.clientID, ch.currentTx.TransactionId)
				// Broadcast to all nodes on timeout
				go broadcastTransaction(ch.currentTx)
				ch.replyCount = 0
				ch.processing = false
				ch.processDone <- struct{}{} // Signal processing complete
				ch.mu.Unlock()
			}
		} else {
			ch.mu.Unlock()
			time.Sleep(100 * time.Millisecond) // Prevent busy waiting
		}
	}
}

func broadcastTransaction(tx *pb.TransactionRequest) {
	// Implement broadcasting to all available nodes
	for i := 1; i <= 7; i++ {
		go func(serverID int) {
			c, ctx, conn := setupClientSender(serverID)
			defer conn.Close()
			_, err := c.SendTransaction(ctx, tx)
			if err != nil {
				log.Printf("Failed to broadcast to server %d: %v", serverID, err)
			}
		}(i)
	}
}

func (c *Client) ClientResponse(ctx context.Context, req *pb.ClientRequest) (*emptypb.Empty, error) {
	fmt.Printf("Received ClientResponse for Client %d, Transaction %d\n", req.ClientId, req.TransactionId)

	// Find the appropriate client handler
	clientHandlersMu.Lock()
	handler, exists := clientHandlers[req.ClientId]
	clientHandlersMu.Unlock()

	if exists {
		// Send the response to the handler's response channel
		handler.responseCh <- req
	}

	return &emptypb.Empty{}, nil
}
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
			transactionSets, liveServersMap, err = ReadTransactions(currClient, "../sample.csv")
			if err != nil {
				fmt.Printf("Error reading transactions: %v\n", err)
				continue
			}
			allSetsRead = true
			currentSetNumber = 1
			fmt.Println("Transactions successfully read.")

		case 2:
			if !allSetsRead {
				fmt.Println("No transactions have been read. Please choose option 1 first.")
				continue
			}
			if transactions, ok := transactionSets[currentSetNumber]; ok {
				fmt.Printf("Processing Set %d\n", currentSetNumber)
				aliveServers := liveServersMap[currentSetNumber]
				aliveServerSet := make(map[int]bool)
				for _, server := range aliveServers {
					aliveServerSet[server] = true
				}

				// Revive previously killed servers
				for serverID := range previouslyKilledServers {
					if previouslyKilledServers[serverID] && aliveServerSet[serverID] {
						fmt.Printf("Reviving Server %d for this set\n", serverID)
						c, ctx, conn := setupClientSender(serverID)
						ack, err := c.Revive(ctx, &pb.ReviveRequest{NodeID: int32(serverID)})
						if err != nil {
							log.Fatalf("Could not revive: %v", err)
						}
						log.Printf("Revive Command Sent: %s for Server %d", ack, serverID)
						conn.Close()
						previouslyKilledServers[serverID] = false
					}
				}

				// Kill servers that are not alive for the current set
				for i := 1; i <= 5; i++ {
					if _, isAlive := aliveServerSet[i]; !isAlive {
						fmt.Printf("Killing Server %d for this set\n", i)
						c, ctx, conn := setupClientSender(i)
						r, err := c.Kill(ctx, &pb.AdminRequest{Command: "Die and Perish"})
						if err != nil {
							log.Fatalf("Could not kill: %v", err)
						}
						log.Printf("Kill Command Sent: %s for Server %d", r.Ack, i)
						conn.Close()
						previouslyKilledServers[i] = true
					}
				}

				clientTransactions := make(map[int32][]*pb.TransactionRequest)
				for _, tx := range transactions {
					clientTransactions[tx.ClientId] = append(clientTransactions[tx.ClientId], tx)
				}

				// Process transactions for each client
				var wg sync.WaitGroup
				for clientID, txs := range clientTransactions {
					wg.Add(1)
					go func(cID int32, transactions []*pb.TransactionRequest) {
						defer wg.Done()
						handler := getClientHandler(cID)

						// Process transactions sequentially for this client
						for _, tx := range transactions {
							// Wait for previous transaction to complete
							handler.mu.Lock()
							for handler.processing {
								handler.mu.Unlock()
								time.Sleep(100 * time.Millisecond)
								handler.mu.Lock()
							}

							// Start processing new transaction
							handler.currentTx = tx
							handler.replyCount = 0
							handler.processing = true
							handler.timer.Reset(5 * time.Second)
							handler.mu.Unlock()

							// Send transaction to primary
							
							go func() {
								c, ctx, conn := setupClientSender(int(currClient.view) % 7)
								defer conn.Close()
								fmt.Println(tx)
								_, err := c.SendTransaction(ctx, tx)
								if err != nil {
									log.Printf("Failed to send transaction to primary: %v", err)
								}
							}()
						}
					}(clientID, txs)
				}

				wg.Wait()
				// // Send transactions to all servers
				// for _, tran := range transactions {
				// 	replyCh := make(chan bool, 1)
				// 	handler := &TransactionHandler{
				// 		replyCh: replyCh,
				// 		timer:   time.NewTimer(5 * time.Second),
				// 	}
				// 	transactionMap[int(tran.TransactionId)] = handler

				// 	// Goroutine for handling replies and timeout
				// 	go func(id int, handler *TransactionHandler) {
				// 		replyCount := 0
				// 		select {
				// 		case <-handler.replyCh:
				// 			replyCount++
				// 			if replyCount >= F+1 {
				// 				if !handler.timer.Stop() {
				// 					<-handler.timer.C
				// 				}
				// 				fmt.Printf("Transaction %d: Received f+1 replies, timer stopped\n", id)
				// 			}
				// 		case <-handler.timer.C:
				// 			fmt.Printf("Transaction %d: Timeout occurred, broadcasting transaction to all nodes\n", id)
				// 		}
				// 	}(int(tran.TransactionId), handler)

				// 	// Send transaction
				// 	go func() {
				// 		c, ctx, conn := setupClientSender(int(currClient.view) % 7)
				// 		_, err := c.SendTransaction(ctx, tran)
				// 		if err != nil {
				// 			log.Fatalf("Could not send transaction: %v", err)
				// 		}
				// 		conn.Close()
				// 	}()
				// }
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

func transactionDigest(tx *pb.Transaction) ([]byte, error) {
	data, err := proto.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %w", err)
	}
	hash := sha256.New()
	hash.Write(data)
	return hash.Sum(nil), nil
}

func ReadTransactions(currClient *Client, filename string) (map[int][]*pb.TransactionRequest, map[int][]int, error) {
	// Initialize maps to store transactions and live servers for each set
	transactionSets := make(map[int][]*pb.TransactionRequest)
	liveServersMap := make(map[int][]int)
	byzantineServersMap := make(map[int][]int)

	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	var currentSetNumber int
	globalTransactionID := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		// if err != nil {
		// 	return nil, nil, err
		// }

		// Check if this is a set header row (has set number)
		if len(record) > 0 && record[0] != "" {
			// Parse set number
			setNumber, err := strconv.Atoi(record[0])
			if err != nil {
				return nil, nil, fmt.Errorf("invalid set number: %s", record[0])
			}
			currentSetNumber = setNumber

			// Parse live servers
			if len(record) > 2 && record[2] != "" {
				liveServersStr := strings.Trim(record[2], "[]")
				currentAliveServers := []int{}
				for _, serverStr := range strings.Split(liveServersStr, ", ") {
					server, err := strconv.Atoi(strings.TrimPrefix(serverStr, "S"))
					if err != nil {
						return nil, nil, fmt.Errorf("invalid live server: %s", serverStr)
					}
					currentAliveServers = append(currentAliveServers, server)
				}
				liveServersMap[currentSetNumber] = currentAliveServers
			}

			// Parse Byzantine servers
			if len(record) > 3 && record[3] != "" {
				byzantineServersStr := strings.Trim(record[3], "[]")
				currentByzantineServers := []int{}
				for _, serverStr := range strings.Split(byzantineServersStr, ", ") {
					server, err := strconv.Atoi(strings.TrimPrefix(serverStr, "S"))
					if err != nil {
						return nil, nil, fmt.Errorf("invalid byzantine server: %s", serverStr)
					}
					currentByzantineServers = append(currentByzantineServers, server)
				}
				byzantineServersMap[currentSetNumber] = currentByzantineServers
			}
		}

		// Process transaction (present in both header and regular rows)
		if len(record) > 1 && record[1] != "" {
			// Parse transaction details
			transactionStr := strings.Trim(record[1], "()")
			parts := strings.Split(transactionStr, ", ")
			if len(parts) != 3 {
				return nil, nil, fmt.Errorf("invalid transaction format: %s", record[1])
			}

			// Parse sender, receiver, and amount
			from, err := strconv.Atoi(strings.TrimPrefix(parts[0], "S"))
			if err != nil {
				return nil, nil, fmt.Errorf("invalid sender: %s", parts[0])
			}

			to, err := strconv.Atoi(strings.TrimPrefix(parts[1], "S"))
			if err != nil {
				return nil, nil, fmt.Errorf("invalid receiver: %s", parts[1])
			}

			amount, err := strconv.Atoi(parts[2])
			if err != nil {
				return nil, nil, fmt.Errorf("invalid amount: %s", parts[2])
			}

			// Create transaction
			transaction := &pb.Transaction{
				Sender:   int32(from),
				Receiver: int32(to),
				Amount:   float32(amount),
			}

			// Sign transaction
			digest, err := transactionDigest(transaction)
			if err != nil {
				return nil, nil, fmt.Errorf("error creating transaction digest: %v", err)
			}
			signature := ed25519.Sign(currClient.privateKey, digest)

			// Create transaction request
			transRequest := &pb.TransactionRequest{
				SetNumber:     int32(currentSetNumber),
				ClientId:      int32(from),
				TransactionId: int32(globalTransactionID),
				Transaction:   transaction,
				Signature:     signature,
			}
			fmt.Println(transRequest)
			// Add to transaction set
			if transactionSets[currentSetNumber] == nil {
				transactionSets[currentSetNumber] = make([]*pb.TransactionRequest, 0)
			}
			transactionSets[currentSetNumber] = append(transactionSets[currentSetNumber], transRequest)
			globalTransactionID++
		}
	}

	return transactionSets, liveServersMap, nil
}

// func (c *Client) ClientResponse(ctx context.Context, req *pb.ClientRequest) (*emptypb.Empty, error) {
// 	fmt.Println("I AM OFFICIALLY FREE!!!!!!!!!!!!!!!!!!!!")
// 	// Placeholder for handling client responses
// 	return &emptypb.Empty{}, nil
// }

func setupClientSender(id int) (pb.ClientServiceClient, context.Context, *grpc.ClientConn) {
	conn, err := grpc.Dial("localhost:"+strconv.Itoa(4000+id), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}
	c := pb.NewClientServiceClient(conn)
	ctx, _ := context.WithTimeout(context.Background(), time.Second*100)
	return c, ctx, conn
}

func setupClientReceiver(id int, client *Client) {
	lis, err := net.Listen("tcp", ":"+strconv.Itoa(4000+id))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterClientServiceServer(s, client)
	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func sendKey(publicKey ed25519.PublicKey) {
	fmt.Println("Sending my Public key to everyone")
	for i := 1; i < 8; i++ {
		c, ctx, conn := setupClientSender(i)
		publicKeyReq := &pb.PublicKeyRequest{
			Key: publicKey,
		}
		c.ReceivePublicKey(ctx, publicKeyReq)
		conn.Close()
	}
}

func (client *Client) ReceivePublicKey(ctx context.Context, req *pb.PublicKeyRequest) (*emptypb.Empty, error) {
	fmt.Println("Inside Receiver key")
	fmt.Println(hex.EncodeToString(req.Key))
	return &emptypb.Empty{}, nil
}

func processTransactionSet(currentSetNumber int, transactionSets map[int][]*pb.TransactionRequest,
	liveServersMap map[int][]int, byzantineServersMap map[int][]int,
	previouslyKilledServers map[int]bool, currClient *Client) {

	if transactions, ok := transactionSets[currentSetNumber]; ok {
		fmt.Printf("Processing Set %d\n", currentSetNumber)

		// Handle server states (alive/killed/byzantine)
		// setupServers(currentSetNumber, liveServersMap, byzantineServersMap, previouslyKilledServers)

		// Group transactions by client ID
		clientTransactions := make(map[int32][]*pb.TransactionRequest)
		for _, tx := range transactions {
			clientTransactions[tx.ClientId] = append(clientTransactions[tx.ClientId], tx)
		}

		// Process transactions for each client
		var wg sync.WaitGroup
		for clientID, txs := range clientTransactions {
			wg.Add(1)
			go func(cID int32, transactions []*pb.TransactionRequest) {
				defer wg.Done()
				handler := getClientHandler(cID)

				// Process transactions sequentially for this client
				for _, tx := range transactions {
					// Wait for previous transaction to complete
					handler.mu.Lock()
					for handler.processing {
						handler.mu.Unlock()
						time.Sleep(100 * time.Millisecond)
						handler.mu.Lock()
					}

					// Start processing new transaction
					handler.currentTx = tx
					handler.replyCount = 0
					handler.processing = true
					handler.timer.Reset(5 * time.Second)
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
	}
}

// func reviveServer(serverID int) error {

//  }
// func killServer(serverID int) error   { }
// func setByzantine(serverID int) error { }

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
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Client struct {
	pb.UnimplementedClientServiceServer
	id             int32
	privateKey     ed25519.PrivateKey
	view           int32
	transactionMap map[int]TransactionHandler
}
type TransactionHandler struct {
	replyCh chan bool   // Channel to track replies for each transaction
	timer   *time.Timer // Timer to track timeout
}

const N = 7
const F int = (N - 1) / 2

var transactionSets = make(map[int][]*pb.TransactionRequest)
var liveServersMap = make(map[int][]int)
var previouslyKilledServers = make(map[int]bool) // Track previously killed servers
var golbalTransactionID = 1

func main() {
	// Initialize variables to track the current set being sent and whether all sets have been read

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	currClient := &Client{id: 0, privateKey: privateKey, view: 1}
	if err != nil {
		fmt.Printf("Failed to generate key pair %d: %v\n", 1, err)

	}
	fmt.Printf("Entity %d:\nPublic Key: %x\nPrivate Key: %x\n\n", 1, publicKey, privateKey)
	var currentSetNumber = 1
	var allSetsRead = false
	go setupClientReceiver(int(currClient.id), currClient)
	// Infinite loop for continuously taking user input

	transactionMap := make(map[int]*TransactionHandler)
	for {
		// Display menu options
		fmt.Println("\nSelect an option:")
		fmt.Println("1. Read Transaction SET on Default Path")
		fmt.Println("2. Send Transactions (Next Set)")
		fmt.Println("3. Send Client Public Key")
		fmt.Println("4. Print Balance on Specific Server")
		fmt.Println("5. Spawn All Nodes")
		fmt.Println("6. Exit")

		// Read user's choice
		var option int
		fmt.Print("Enter your option: ")
		_, err := fmt.Scanln(&option)
		if err != nil {
			fmt.Println("Invalid input, please enter a number.")
			continue
		}

		// Handle user input with a switch-case
		switch option {
		case 1:
			// Read the transactions from the default CSV file path
			fmt.Println("Executing: Read Transaction SET on Default Path")
			transactionSets, liveServersMap, err = ReadTransactions(currClient, "../sample.csv")
			if err != nil {
				fmt.Printf("Error reading transactions: %v\n", err)
				continue
			}
			allSetsRead = true
			currentSetNumber = 1 // Reset to the first set after reading
			fmt.Println("Transactions successfully read.")

		case 2:
			// Send transactions from the current set number

			if !allSetsRead {
				fmt.Println("No transactions have been read. Please choose option 1 first.")
				continue
			}
			if transactions, ok := transactionSets[currentSetNumber]; ok {
				fmt.Printf("Processing Set %d\n", currentSetNumber)
				// fmt.Println(transactions)
				// Get the alive servers for the current set
				aliveServers := liveServersMap[currentSetNumber]
				aliveServerSet := make(map[int]bool)
				for _, server := range aliveServers {
					aliveServerSet[server] = true
				}

				// Revive servers that were previously killed but are now alive
				for serverID := range previouslyKilledServers {
					if previouslyKilledServers[serverID] && aliveServerSet[serverID] {
						fmt.Printf("Reviving Server %d for this set\n", serverID)
						// Placeholder for Revive logic, you will implement it later
						c, ctx, conn := setupClientSender(serverID)
						ack, err := c.Revive(ctx, &pb.ReviveRequest{NodeID: int32(serverID)})
						if err != nil {
							log.Fatalf("Could not revive: %v", err)
						}
						log.Printf("Revive Command Sent: %s for Server %d", ack, serverID)
						conn.Close()
						// Mark as no longer killed
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
						// Track that this server is now killed
						previouslyKilledServers[i] = true
					}
				}

				// Now send transactions to all servers as usual
				for _, tran := range transactions {
					// Set up RPC connection and send the transaction to all servers (including killed ones)
					replyCh := make(chan bool, 1) // Buffered channel for non-blocking
					handler := &TransactionHandler{
						replyCh: replyCh,
						timer:   time.NewTimer(5 * time.Second), // Adjust the timeout as needed
					}
					transactionMap[int(tran.TransactionId)] = handler

					// Start a goroutine to handle the timer and replies
					go func(id int, handler *TransactionHandler) {
						replyCount := 0
						select {
						case <-handler.replyCh:
							// f+1 replies received before timeout, stop the timer
							replyCount++
							fmt.Println("Yess got response from a server")
							if replyCount >= F+1 {
								if !handler.timer.Stop() {
									<-handler.timer.C // Drain the timer channel if already expired
								}
								fmt.Printf("Transaction %d: Received f+1 replies, timer stopped\n", id)
							}

						case <-handler.timer.C:
							// Timeout occurred before receiving f+1 replies
							fmt.Printf("Transaction %d: Timeout occurred, broadcasting transaction to all nodes\n", id)
							// Logic to handle broadcasting can be placed here
						}
					}(int(tran.TransactionId), handler)
					go func() {
						c, ctx, conn := setupClientSender(int(currClient.view) % 7)
						_, err := c.SendTransaction(ctx, tran)
						if err != nil {
							log.Fatalf("Could not send transaction: %v", err)
						}

						conn.Close()
					}()
				}

				currentSetNumber++ // Move to the next set
			} else {
				fmt.Println("No more sets to send.")
			}
		case 3:
			sendKey(publicKey)
		}

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
func ReadTransactions(currClient *Client, filename string) (map[int][]*pb.TransactionRequest, map[int][]int, error) {

	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	var currentSetNumber int
	var currentAliveServers []int

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		// If the first column contains the set number, we are starting a new set
		if record[0] != "" {
			// Parse the new set number
			setNumber, err := strconv.Atoi(record[0])
			if err != nil {
				fmt.Println("Skipping invalid set number:", record[0])
				continue
			}
			currentSetNumber = setNumber

			// Parse the live servers for this set from the last column
			aliveServersStr := strings.Trim(record[2], "[]")
			serverParts := strings.Split(aliveServersStr, ", ")
			currentAliveServers = []int{}
			for _, serverStr := range serverParts {
				server, err := strconv.Atoi(serverStr[1:])
				if err != nil {
					fmt.Println("Skipping invalid server:", serverStr)
					continue
				}
				currentAliveServers = append(currentAliveServers, server)
			}
			// Store the live servers for this set
			liveServersMap[currentSetNumber] = currentAliveServers
			// continue // Go to the next line for transactions
		}

		// If no set number is provided, we are still in the current set
		// Parse the transaction details (from, to, amount)
		transactionDetails := strings.Trim(record[1], "()")
		transactionParts := strings.Split(transactionDetails, ", ")
		if len(transactionParts) != 3 {
			fmt.Println("Skipping invalid transaction details:", record[1])
			continue
		}
		from, err := strconv.Atoi(transactionParts[0][1:])
		if err != nil {
			fmt.Println("Skipping invalid 'from' field:", transactionParts[0])
			continue
		}
		to, err := strconv.Atoi(transactionParts[1][1:])
		if err != nil {
			fmt.Println("Skipping invalid 'to' field:", transactionParts[1])
			continue
		}
		amount, err := strconv.Atoi(transactionParts[2])
		if err != nil {
			fmt.Println("Skipping invalid amount:", transactionParts[2])
			continue
		}

		// Add the transaction to the current set
		transaction := &pb.Transaction{
			Sender:   int32(from),
			Receiver: int32(to),
			Amount:   float32(amount),
		}
		digest, err := transactionDigest(transaction)
		if err != nil {
			fmt.Println("Error in making digest ", err)
		}
		signature := ed25519.Sign(currClient.privateKey, digest)
		trans := pb.TransactionRequest{
			SetNumber:     int32(currentSetNumber),
			ClientId:      int32(from),
			TransactionId: int32(golbalTransactionID),
			Transaction:   transaction,
			Signature:     signature,
		}
		fmt.Println(&trans)
		transactionSets[currentSetNumber] = append(transactionSets[currentSetNumber], &trans)
		golbalTransactionID++
	}
	return transactionSets, liveServersMap, nil
}

func (c *Client) ClientResponse(ctx context.Context, req *pb.ClientRequest) (*emptypb.Empty, error) {
	id := req.TransactionId
	c.transactionMap[int(id)].replyCh <- true
	return &emptypb.Empty{}, nil
}

func setupClientSender(id int) (pb.ClientServiceClient, context.Context, *grpc.ClientConn) {
	conn, err := grpc.Dial("localhost:"+strconv.Itoa((4000+id)), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}
	c := pb.NewClientServiceClient(conn)
	ctx, _ := context.WithTimeout(context.Background(), time.Millisecond*100)
	return c, ctx, conn
}

func setupClientReceiver(id int, client *Client) {
	lis, err := net.Listen("tcp", ":"+strconv.Itoa((4000+id)))
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
	fmt.Println("Inside Reciever key")
	fmt.Println(hex.EncodeToString(req.Key))
	return &emptypb.Empty{}, nil
}

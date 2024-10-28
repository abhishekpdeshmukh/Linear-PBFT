package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
	"google.golang.org/protobuf/types/known/emptypb"
)

type Node struct {
	pb.UnimplementedPBFTServiceServer
	pb.UnimplementedClientServiceServer
	nodeID     int32
	isActive   bool
	isLeader   bool
	view       int32
	publickeys map[int32]string
	lock       sync.Mutex
}

func main() {
	id, _ := strconv.Atoi(os.Args[1])
	currNode := &Node{
		nodeID:     int32(id),
		isActive:   true,
		view:       1,
		publickeys: make(map[int32]string),
		lock:       sync.Mutex{},
	}
	go setupReplicaReceiver(id, currNode)
	// go setupReplicaSender(id)
	go setupClientReceiver(id, currNode)
	// go setupClientSender(id)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Printf("Failed to generate key pair %d: %v\n", 1, err)

	}
	fmt.Printf("Entity %d:\nPublic Key: %x\nPrivate Key: %x\n\n", 1, publicKey, privateKey)
	sendKey(currNode, publicKey)
	println("Node ", currNode.nodeID, " Working")

	println(currNode.publickeys)
	for {

	}

}

func (node *Node) SendTransaction(ctx context.Context, req *pb.TransactionRequest) (*emptypb.Empty, error) {
	fmt.Println(req)
	if node.isActive && node.isLeader {
		startPBFT(node)
	} else if node.isActive {

	}
	return &emptypb.Empty{}, nil
}

func startPBFT(node *Node) {

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
	return &emptypb.Empty{}, nil
}
func (node *Node) ReceiveServerPublicKey(ctx context.Context, req *pb.PublicKeyRequest) (*pb.Acknowledgment, error) {
	fmt.Println("YEss! I got a key from ", req.Id, " ", hex.EncodeToString(req.Key))
	node.publickeys[req.Id] = hex.EncodeToString(req.Key)
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

	ctx, _ := context.WithTimeout(context.Background(), time.Millisecond*10)
	// cancel()
	return c, ctx, conn
}

func setupClientSender(id int) (pb.ClientServiceClient, context.Context, *grpc.ClientConn) {
	conn, err := grpc.Dial("localhost:"+strconv.Itoa((4000+id)), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}
	c := pb.NewClientServiceClient(conn)
	ctx, _ := context.WithTimeout(context.Background(), time.Millisecond*60)
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

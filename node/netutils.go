package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

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

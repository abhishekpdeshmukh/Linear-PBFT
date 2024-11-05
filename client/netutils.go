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

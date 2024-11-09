package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	pb "github.com/abhishekpdeshmukh/LINEAR-PBFT/proto"
	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/share"
	"go.dedis.ch/kyber/v3/util/random"
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
	fmt.Println("Broadcasting transaction from client ", tx.ClientId)

	// fmt.Println(tx)

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

func generateMasterKeyPairAndShares() (kyber.Scalar, kyber.Point, []*share.PriShare, *share.PubPoly) {
	suite := bn256.NewSuite()
	// Generate a master private and public key
	masterPrivateKey := suite.G2().Scalar().Pick(random.New())
	masterPublicKey := suite.G2().Point().Mul(masterPrivateKey, nil)

	fmt.Println("Master Public Key:", masterPublicKey)

	// Create a private polynomial for secret sharing
	priPoly := share.NewPriPoly(suite.G2(), 2*F+1, masterPrivateKey, random.New())
	priShares := priPoly.Shares(N)

	// Create a public polynomial for verification
	pubPoly := priPoly.Commit(suite.G2().Point().Base())

	return masterPrivateKey, masterPublicKey, priShares, pubPoly
}

func initiateTSSHandshake(masterPublicKey kyber.Point, priShares []*share.PriShare, pubPoly *share.PubPoly) {

	for i := 1; i <= 7; i++ {
		go func(serverID int) {
			var masterPublicKeyBytes []byte
			var privateShareBytes []byte
			var publicPolyCommitments []byte
			masterPublicKeyBytes, _ = masterPublicKey.MarshalBinary()

			privateShareBytes, err := serializePriShare(priShares[serverID])
			if err != nil {
				// Handle error
			}

			publicPolyCommitments, _ = pubPoly.Commit().MarshalBinary()

			tssKeyExchange := &pb.TSSKeyExchange{
				MasterPublicKey:       masterPublicKeyBytes,
				PrivateShares:         privateShareBytes,
				PublicPolyCommitments: publicPolyCommitments,
			}
			c, ctx, conn := setupClientSender(serverID)
			defer conn.Close()
			_, err = c.SendTSSKeys(ctx, tssKeyExchange)
			if err != nil {
				log.Printf("Failed to sendd TSS KEy exchange to server %d: %v", serverID, err)
			}
		}(i)
	}
}

func serializePriShare(share *share.PriShare) ([]byte, error) {
	buf := new(bytes.Buffer)

	// Serialize the index (I)
	err := binary.Write(buf, binary.BigEndian, int32(share.I))
	if err != nil {
		return nil, err
	}

	// Serialize the scalar value (V)
	vBytes, err := share.V.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// Write the scalar bytes to the buffer
	buf.Write(vBytes)

	return buf.Bytes(), nil
}

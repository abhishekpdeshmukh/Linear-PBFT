package main

// func main() {
// 	suite := bn256.NewSuite()
// 	// Generate a master private and public key
// 	masterPrivateKey := suite.G2().Scalar().Pick(random.New())
// 	masterPublicKey := suite.G2().Point().Mul(masterPrivateKey, nil)

// 	fmt.Println("Master Public Key:", masterPublicKey)

// 	// Create a private polynomial for secret sharing
// 	priPoly := share.NewPriPoly(suite.G2(), 5, masterPrivateKey, random.New())
// 	priShares := priPoly.Shares(7)

// 	// Create a public polynomial for verification
// 	pubPoly := priPoly.Commit(suite.G2().Point().Base())
// 	// for i := range pubPoly.Shares()
// 	// x := pubPoly.Commit().MarshalBinary()
// 	PublicKey, _ := masterPublicKey.MarshalBinary()
// 	pri,_ := serializePriShare()
// 	// privateShareBytes0, err := serializePriShare(priShares[0])
// 	// privateShareBytes1, err := serializePriShare(priShares[1])
// 	// privateShareBytes2, err := serializePriShare(priShares[2])
// 	// privateShareBytes3, err := serializePriShare(priShares[3])
// 	// privateShareBytes4, err := serializePriShare(priShares[4])

// 	publicPolyCommitments, _ := marshalPubPoly(pubPoly)

// 	message := []byte("Hello Threshold Signature")

// 	// Each participant generates a signature share
// 	sigShares := make([][]byte, 5)
// 	for i := 0; i < 5; i++ {
// 		share := priShares[i]
// 		sigShare, err := tbls.Sign(suite, share, message)
// 		if err != nil {
// 			panic(err)
// 		}
// 		sigShares[i] = sigShare
// 	}

// 	// Combine signature shares to form a full signature
// 	signature, err := tbls.Recover(suite, publicPolyCommitments, message, sigShares, 5, 7)
// 	if err != nil {
// 		panic(err)
// 	}

// 	// Verify the combined signature using the master public key
// 	err = bls.Verify(suite, masterPublicKey, message, signature)
// 	if err != nil {
// 		fmt.Println("Signature verification failed:", err)
// 	} else {
// 		fmt.Println("Signature verified successfully")
// 	}
// }

// func serializePriShare(share *share.PriShare) ([]byte, error) {
// 	buf := new(bytes.Buffer)

// 	// Serialize the index (I)
// 	err := binary.Write(buf, binary.BigEndian, int32(share.I))
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Serialize the scalar value (V)
// 	vBytes, err := share.V.MarshalBinary()
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Write the scalar bytes to the buffer
// 	buf.Write(vBytes)

// 	return buf.Bytes(), nil
// }

// // Helper function to marshal *share.PubPoly
// // Helper function to marshal *share.PubPoly
// func marshalPubPoly(pubPoly *share.PubPoly) ([]byte, error) {
// 	var buf bytes.Buffer
// 	encoder := gob.NewEncoder(&buf)

// 	degree := pubPoly.Threshold() - 1 // Degree of the polynomial
// 	if err := encoder.Encode(degree); err != nil {
// 		return nil, err
// 	}

// 	// Serialize each point from the polynomial
// 	suite := edwards25519.NewBlakeSHA256Ed25519()
// 	for i := 0; i <= degree; i++ {
// 		point := pubPoly.Eval(i).V
// 		data, err := point.MarshalBinary()
// 		if err != nil {
// 			return nil, err
// 		}
// 		if err := encoder.Encode(data); err != nil {
// 			return nil, err
// 		}
// 	}

// 	return buf.Bytes(), nil
// }

// func unmarshalPubPoly(data []byte) (*share.PubPoly, error) {
// 	buf := bytes.NewBuffer(data)
// 	decoder := gob.NewDecoder(buf)

// 	var degree int
// 	if err := decoder.Decode(&degree); err != nil {
// 		return nil, err
// 	}

// 	suite := edwards25519.NewBlakeSHA256Ed25519()
// 	coefficients := make([]kyber.Point, degree+1)
// 	for i := 0; i <= degree; i++ {
// 		coefficients[i] = suite.Point()
// 		var pointData []byte
// 		if err := decoder.Decode(&pointData); err != nil {
// 			return nil, err
// 		}
// 		if err := coefficients[i].UnmarshalBinary(pointData); err != nil {
// 			return nil, err
// 		}
// 	}

// 	return share.NewPubPoly(suite, coefficients), nil
// }

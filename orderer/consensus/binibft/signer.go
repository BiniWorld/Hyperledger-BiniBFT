/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"encoding/asn1"
	"math/big"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/common/crypto"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/internal/pkg/identity"
	"github.com/hyperledger/fabric/protoutil"
)

// Signer signs messages for BiniBFT consensus
type Signer struct {
	ID                 uint64
	Logger             *flogging.FabricLogger
	SignerSerializer   identity.SignerSerializer
	LastConfigBlockNum func(*cb.Block) uint64
}

// Signature represents a BiniBFT signature - identical to SmartBFT for compatibility
type Signature struct {
	IdentifierHeader     []byte
	BlockHeader          []byte
	OrdererBlockMetadata []byte
}

// Unmarshal the signature using ASN.1 (same as SmartBFT)
func (sig *Signature) Unmarshal(bytes []byte) error {
	_, err := asn1.Unmarshal(bytes, sig)
	return err
}

// Marshal the signature using ASN.1 (same as SmartBFT)
func (sig *Signature) Marshal() []byte {
	bytes, err := asn1.Marshal(*sig)
	if err != nil {
		panic(err)
	}
	return bytes
}

// AsBytes returns the message to sign (same as SmartBFT)
func (sig *Signature) AsBytes() []byte {
	msg2Sign := util.ConcatenateBytes(sig.OrdererBlockMetadata, sig.IdentifierHeader, sig.BlockHeader)
	return msg2Sign
}

// Sign signs a message and returns the signature
func (s *Signer) Sign(message []byte) ([]byte, error) {
	s.Logger.Debugf("=== BINIBFT SIGN DEBUG ===")
	s.Logger.Debugf("Signer ID: %d", s.ID)
	s.Logger.Debugf("Message length: %d bytes", len(message))
	if len(message) > 0 {
		s.Logger.Debugf("Message (first %d bytes): %x", min(100, len(message)), message[:min(100, len(message))])
	}

	// Get the signer's identity to verify we're using the right one
	identity, err := s.SignerSerializer.Serialize()
	if err != nil {
		s.Logger.Errorf("Failed to serialize signer identity: %v", err)
	} else {
		s.Logger.Debugf("Signer identity length: %d bytes", len(identity))
		if len(identity) > 0 {
			s.Logger.Debugf("Signer identity (first %d bytes): %x", min(100, len(identity)), identity[:min(100, len(identity))])
		}
	}

	signature, err := s.SignerSerializer.Sign(message)
	if err != nil {
		s.Logger.Errorf("Signing failed: %v", err)
		return nil, err
	}

	s.Logger.Debugf("Signature created (length %d): %x", len(signature), signature)
	s.Logger.Debugf("=== END BINIBFT SIGN DEBUG ===")
	return signature, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SignProposal signs the proposal (same approach as SmartBFT)
func (s *Signer) SignProposal(proposal BiniBFTProposal, _ []byte) *BiniBFTSignature {
	s.Logger.Infof("*** BINIBFT SIGNER CALLED *** - SignProposal for signer ID %d", s.ID)

	block, err := ProposalToBlock(proposal)
	if err != nil {
		s.Logger.Panicf("Tried to sign bad proposal: %v", err)
	}

	nonce := randomNonceOrPanic()

	lastConfigBlockNum := s.LastConfigBlockNum(block)
	s.Logger.Infof("=== BINIBFT SIGNATURE CREATION DEBUG ===")
	s.Logger.Infof("Block Number: %d", block.Header.Number)
	s.Logger.Infof("Last Config Block Num: %d", lastConfigBlockNum)
	s.Logger.Infof("Proposal Metadata Length: %d", len(proposal.Metadata))
	s.Logger.Infof("Block Header: %x", protoutil.BlockHeaderBytes(block.Header))
	s.Logger.Infof("Nonce: %x", nonce)

	ordererBlockMetadata := &cb.OrdererBlockMetadata{
		LastConfig:        &cb.LastConfig{Index: lastConfigBlockNum},
		ConsenterMetadata: proposal.Metadata,
	}

	sig := &Signature{
		BlockHeader:          protoutil.BlockHeaderBytes(block.Header),
		IdentifierHeader:     protoutil.MarshalOrPanic(s.newIdentifierHeaderOrPanic(nonce)),
		OrdererBlockMetadata: protoutil.MarshalOrPanic(ordererBlockMetadata),
	}

	s.Logger.Infof("OrdererBlockMetadata: %x", sig.OrdererBlockMetadata)
	s.Logger.Infof("IdentifierHeader: %x", sig.IdentifierHeader)
	s.Logger.Infof("Message to sign: %x", sig.AsBytes())

	// Get signer identity for debugging
	identity, err := s.SignerSerializer.Serialize()
	if err != nil {
		s.Logger.Errorf("Failed to get signer identity: %v", err)
	} else {
		s.Logger.Infof("Signer identity (first 100 bytes): %x", identity[:min(100, len(identity))])
	}

	s.Logger.Infof("=== END BINIBFT SIGNATURE CREATION DEBUG ===")

	// Use the same signing approach as SmartBFT
	signature := protoutil.SignOrPanic(s.SignerSerializer, sig.AsBytes())

	s.Logger.Infof("=== SIGNATURE RESULT ===")
	s.Logger.Infof("Signature length: %d", len(signature))
	s.Logger.Infof("Signature: %x", signature)
	s.Logger.Infof("Marshaled signature message length: %d", len(sig.Marshal()))
	s.Logger.Infof("=== END SIGNATURE RESULT ===")

	return &BiniBFTSignature{
		ID:    s.ID,
		Value: signature,
		Msg:   sig.Marshal(),
	}
}

// newIdentifierHeaderOrPanic creates an IdentifierHeader with the signer's identifier and a valid nonce
func (s *Signer) newIdentifierHeaderOrPanic(nonce []byte) *cb.IdentifierHeader {
	return &cb.IdentifierHeader{
		Identifier: uint32(s.ID),
		Nonce:      nonce,
	}
}

func randomNonceOrPanic() []byte {
	nonce, err := crypto.GetRandomNonce()
	if err != nil {
		panic(err)
	}
	return nonce
}

// LastConfigBlockNum returns the last config block number (same as SmartBFT)

type asn1Header struct {
	Number       *big.Int
	PreviousHash []byte
	DataHash     []byte
}

package main

import (
	"binibft-poc/consensus"
	"crypto/ecdsa"
	"crypto/sha256"
	"fmt"
	"strconv"
)

// ECDSASigner implements consensus.Signer using an ECDSA private key
type ECDSASigner struct {
	nodeID    consensus.NodeID
	privKey   *ecdsa.PrivateKey
	channelID string
}

// NewECDSASigner creates a new ECDSASigner
func NewECDSASigner(nodeID consensus.NodeID, privKey *ecdsa.PrivateKey, channelID string) *ECDSASigner {
	return &ECDSASigner{
		nodeID:    nodeID,
		privKey:   privKey,
		channelID: channelID,
	}
}

// Sign signs arbitrary raw message bytes after hashing with SHA-256
func (s *ECDSASigner) Sign(msg []byte) []byte {
	if s.privKey == nil {
		return nil
	}
	h := sha256.Sum256(msg)
	sig, err := SignDigest(s.privKey, h[:])
	if err != nil {
		return nil
	}
	return sig
}

// SignDigest signs a 32-byte digest directly
func (s *ECDSASigner) SignDigest(digest []byte) ([]byte, error) {
	if s.privKey == nil {
		return nil, fmt.Errorf("private key not configured for node %s", s.nodeID)
	}
	return SignDigest(s.privKey, digest)
}

// SignProposal signs a proposal digest
func (s *ECDSASigner) SignProposal(proposal consensus.Proposal, data []byte) *consensus.Signature {
	nodeIdUint, _ := strconv.ParseUint(string(s.nodeID), 10, 64)
	digest := proposal.Digest()
	sigBytes, err := s.SignDigest([]byte(digest))
	if err != nil {
		return &consensus.Signature{
			ID:  nodeIdUint,
			Msg: []byte(digest),
		}
	}
	return &consensus.Signature{
		ID:    nodeIdUint,
		Value: sigBytes,
		Msg:   []byte(digest),
	}
}

// Node signing methods delegating to ECDSASigner or internal private key
func (n *Node) Sign(msg []byte) []byte {
	if n.signer != nil {
		return n.signer.Sign(msg)
	}
	return nil
}

func (n *Node) SignDigest(digest []byte) ([]byte, error) {
	if n.signer != nil {
		return n.signer.SignDigest(digest)
	}
	return nil, fmt.Errorf("signer not initialized for node %s", n.id)
}

func (n *Node) SignProposal(proposal consensus.Proposal, data []byte) *consensus.Signature {
	if n.signer != nil {
		return n.signer.SignProposal(proposal, data)
	}
	nodeId, _ := strconv.ParseUint(string(n.id), 10, 64)
	return &consensus.Signature{
		ID: nodeId,
	}
}

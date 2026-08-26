package main

import (
	"binibft-poc/consensus"
	"crypto/ecdsa"
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"
)

// ECDSAVerifier implements consensus.Verifier using ECDSA P-256 public keys
type ECDSAVerifier struct {
	mu        sync.RWMutex
	pubKeys   map[consensus.NodeID]*ecdsa.PublicKey
	channelID string
}

// NewECDSAVerifier creates a new ECDSAVerifier instance
func NewECDSAVerifier(channelID string) *ECDSAVerifier {
	return &ECDSAVerifier{
		pubKeys:   make(map[consensus.NodeID]*ecdsa.PublicKey),
		channelID: channelID,
	}
}

// RegisterPublicKey registers an ECDSA public key for a node ID
func (v *ECDSAVerifier) RegisterPublicKey(nodeID consensus.NodeID, pubKey *ecdsa.PublicKey) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pubKeys[nodeID] = pubKey
}

// GetPublicKey retrieves the registered public key for a node ID
func (v *ECDSAVerifier) GetPublicKey(nodeID consensus.NodeID) (*ecdsa.PublicKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	pk, exists := v.pubKeys[nodeID]
	return pk, exists
}

// VerifySignature verifies a signature over arbitrary raw bytes
func (v *ECDSAVerifier) VerifySignature(nodeID consensus.NodeID, data []byte, signature []byte) error {
	v.mu.RLock()
	pubKey, exists := v.pubKeys[nodeID]
	v.mu.RUnlock()

	if !exists {
		return fmt.Errorf("unknown consenter node ID: %s", nodeID)
	}
	if pubKey == nil {
		return fmt.Errorf("public key is nil for node ID: %s", nodeID)
	}
	if len(signature) == 0 {
		return fmt.Errorf("signature is empty for node ID: %s", nodeID)
	}

	h := sha256.Sum256(data)
	if !VerifyDigest(pubKey, h[:], signature) {
		return fmt.Errorf("invalid signature for node ID %s", nodeID)
	}
	return nil
}

// VerifyDigestSignature verifies an ECDSA signature directly over a 32-byte digest
func (v *ECDSAVerifier) VerifyDigestSignature(nodeID consensus.NodeID, digest []byte, signature []byte) error {
	v.mu.RLock()
	pubKey, exists := v.pubKeys[nodeID]
	v.mu.RUnlock()

	if !exists {
		return fmt.Errorf("unknown consenter node ID: %s", nodeID)
	}
	if pubKey == nil {
		return fmt.Errorf("public key is nil for node ID: %s", nodeID)
	}
	if len(signature) == 0 {
		return fmt.Errorf("signature is empty for node ID: %s", nodeID)
	}

	if !VerifyDigest(pubKey, digest, signature) {
		return fmt.Errorf("invalid digest signature for node ID %s", nodeID)
	}
	return nil
}

// VerifyProposalSignature verifies the signature on a proposal
func (v *ECDSAVerifier) VerifyProposalSignature(nodeID consensus.NodeID, proposal consensus.Proposal, sig consensus.Signature) error {
	expectedNodeIDStr := consensus.NodeID(strconv.FormatUint(sig.ID, 10))
	if expectedNodeIDStr != nodeID {
		return fmt.Errorf("signature node ID %d does not match claimed node ID %s", sig.ID, nodeID)
	}

	digest := proposal.Digest()
	return v.VerifyDigestSignature(nodeID, []byte(digest), sig.Value)
}

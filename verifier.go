package main

import (
	"binibft-poc/consensus"
	"crypto/ecdsa"
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"
)

// ECDSAVerifier implements consensus.Verifier using ECDSA P-256 public keys and validates requests and proposals
type ECDSAVerifier struct {
	mu            sync.RWMutex
	pubKeys       map[consensus.NodeID]*ecdsa.PublicKey
	channelID     string
	batchMaxCount uint64
	batchMaxBytes uint64
}

// NewECDSAVerifier creates a new ECDSAVerifier instance
func NewECDSAVerifier(channelID string) *ECDSAVerifier {
	return &ECDSAVerifier{
		pubKeys:       make(map[consensus.NodeID]*ecdsa.PublicKey),
		channelID:     channelID,
		batchMaxCount: 10000,
		batchMaxBytes: 10 * 1024 * 1024, // 10MB
	}
}

// SetBatchLimits sets the maximum transaction count and byte size for proposals
func (v *ECDSAVerifier) SetBatchLimits(maxCount uint64, maxBytes uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if maxCount > 0 {
		v.batchMaxCount = maxCount
	}
	if maxBytes > 0 {
		v.batchMaxBytes = maxBytes
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

// VerifyRequest validates the structure, nonces, and digest of a transaction
func (v *ECDSAVerifier) VerifyRequest(request []byte) (consensus.RequestInfo, error) {
	if len(request) == 0 {
		return consensus.RequestInfo{}, fmt.Errorf("empty request bytes")
	}

	txn, err := TransactionFromBytes(request)
	if err != nil {
		return consensus.RequestInfo{}, fmt.Errorf("malformed transaction encoding: %w", err)
	}

	if txn.ClientID == "" {
		return consensus.RequestInfo{}, fmt.Errorf("transaction missing clientID")
	}

	if txn.Data == "" {
		return consensus.RequestInfo{}, fmt.Errorf("transaction missing data payload")
	}

	expectedID := consensus.ComputeTransactionDigest(v.channelID, txn.ClientID, txn.TS, txn.Data)
	if txn.ID != "" && txn.ID != expectedID {
		return consensus.RequestInfo{}, fmt.Errorf("transaction ID mismatch: expected %s, got %s", expectedID, txn.ID)
	}

	return consensus.RequestInfo{
		ClientID: txn.ClientID,
		ID:       expectedID,
	}, nil
}

// VerifyProposal validates the complete structure, sequence, data hash, and batch constraints of a proposal
func (v *ECDSAVerifier) VerifyProposal(proposal consensus.Proposal) ([]consensus.RequestInfo, error) {
	if len(proposal.Header) == 0 {
		return nil, fmt.Errorf("proposal missing header")
	}
	if len(proposal.Payload) == 0 {
		return nil, fmt.Errorf("proposal missing payload")
	}

	header, err := BlockHeaderFromBytes(proposal.Header)
	if err != nil {
		return nil, fmt.Errorf("invalid proposal header: %w", err)
	}

	blockData, err := BlockDataFromBytes(proposal.Payload)
	if err != nil {
		return nil, fmt.Errorf("invalid proposal payload: %w", err)
	}

	if len(blockData.Transactions) == 0 {
		return nil, fmt.Errorf("proposal contains empty transaction batch")
	}

	v.mu.RLock()
	maxCount := v.batchMaxCount
	maxBytes := v.batchMaxBytes
	v.mu.RUnlock()

	if uint64(len(blockData.Transactions)) > maxCount {
		return nil, fmt.Errorf("transaction count %d exceeds batch maximum %d", len(blockData.Transactions), maxCount)
	}

	if uint64(len(proposal.Payload)) > maxBytes {
		return nil, fmt.Errorf("payload size %d bytes exceeds batch maximum %d", len(proposal.Payload), maxBytes)
	}

	// Verify DataHash in header matches SHA-256 of payload
	computedDataHash := computeDigest(proposal.Payload)
	if header.DataHash != computedDataHash {
		return nil, fmt.Errorf("proposal data hash mismatch: header=%s, computed=%s", header.DataHash, computedDataHash)
	}

	// Validate individual transactions and check for duplicates within batch
	reqInfos := make([]consensus.RequestInfo, 0, len(blockData.Transactions))
	seenIDs := make(map[string]struct{}, len(blockData.Transactions))

	for idx, rawTxn := range blockData.Transactions {
		reqInfo, err := v.VerifyRequest(rawTxn)
		if err != nil {
			return nil, fmt.Errorf("invalid transaction at index %d in proposal: %w", idx, err)
		}
		if _, duplicate := seenIDs[reqInfo.ID]; duplicate {
			return nil, fmt.Errorf("duplicate transaction %s at index %d in proposal batch", reqInfo.ID, idx)
		}
		seenIDs[reqInfo.ID] = struct{}{}
		reqInfos = append(reqInfos, reqInfo)
	}

	return reqInfos, nil
}

// RequestsFromProposal safely extracts RequestInfo list from a proposal payload
func (v *ECDSAVerifier) RequestsFromProposal(proposal consensus.Proposal) []consensus.RequestInfo {
	if len(proposal.Payload) == 0 {
		return nil
	}
	blockData, err := BlockDataFromBytes(proposal.Payload)
	if err != nil || blockData == nil {
		return nil
	}

	reqs := make([]consensus.RequestInfo, 0, len(blockData.Transactions))
	for _, rawTxn := range blockData.Transactions {
		txn, err := TransactionFromBytes(rawTxn)
		if err != nil || txn == nil {
			continue
		}
		reqID := txn.ID
		if reqID == "" {
			reqID = consensus.ComputeTransactionDigest(v.channelID, txn.ClientID, txn.TS, txn.Data)
		}
		reqs = append(reqs, consensus.RequestInfo{
			ClientID: txn.ClientID,
			ID:       reqID,
		})
	}
	return reqs
}

// Node helper methods implementing consensus.Verifier delegation
func (n *Node) VerifyRequest(request []byte) (consensus.RequestInfo, error) {
	if n.verifier != nil {
		return n.verifier.VerifyRequest(request)
	}
	return consensus.RequestInfo{}, nil
}

func (n *Node) VerifyProposal(proposal consensus.Proposal) ([]consensus.RequestInfo, error) {
	if n.verifier != nil {
		return n.verifier.VerifyProposal(proposal)
	}
	return nil, nil
}

func (n *Node) RequestsFromProposal(proposal consensus.Proposal) []consensus.RequestInfo {
	if n.verifier != nil {
		return n.verifier.RequestsFromProposal(proposal)
	}
	return nil
}

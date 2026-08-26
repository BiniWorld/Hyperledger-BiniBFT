package main

import (
	"binibft-poc/consensus"
	"testing"
	"time"
)

func TestVerifyRequest(t *testing.T) {
	verifier := NewECDSAVerifier("test-channel")

	ts := int(time.Now().UnixNano() / 1000000)
	txID := consensus.ComputeTransactionDigest("test-channel", "client-1", ts, "transfer-100")

	validTx := Transaction{
		ClientID: "client-1",
		TS:       ts,
		ID:       txID,
		Data:     "transfer-100",
	}

	// 1. Valid transaction
	reqInfo, err := verifier.VerifyRequest(validTx.ToBytes())
	if err != nil {
		t.Fatalf("expected valid transaction to pass verification: %v", err)
	}
	if reqInfo.ClientID != "client-1" || reqInfo.ID != txID {
		t.Fatalf("unexpected RequestInfo: %+v", reqInfo)
	}

	// 2. Empty request bytes
	if _, err := verifier.VerifyRequest(nil); err == nil {
		t.Fatalf("expected error for empty request")
	}

	// 3. Malformed transaction bytes (does not panic)
	if _, err := verifier.VerifyRequest([]byte("not-an-asn1-encoded-tx")); err == nil {
		t.Fatalf("expected error for malformed transaction bytes")
	}

	// 4. Missing client ID
	noClientTx := validTx
	noClientTx.ClientID = ""
	if _, err := verifier.VerifyRequest(noClientTx.ToBytes()); err == nil {
		t.Fatalf("expected error for missing clientID")
	}

	// 5. Missing data payload
	noDataTx := validTx
	noDataTx.Data = ""
	if _, err := verifier.VerifyRequest(noDataTx.ToBytes()); err == nil {
		t.Fatalf("expected error for missing data payload")
	}

	// 6. Mismatched transaction ID
	wrongIDTx := validTx
	wrongIDTx.ID = "wrong-computed-id"
	if _, err := verifier.VerifyRequest(wrongIDTx.ToBytes()); err == nil {
		t.Fatalf("expected error for mismatched transaction ID")
	}
}

func TestVerifyProposal(t *testing.T) {
	verifier := NewECDSAVerifier("test-channel")
	verifier.SetBatchLimits(10, 1024*1024)

	ts := int(time.Now().UnixNano() / 1000000)
	txID1 := consensus.ComputeTransactionDigest("test-channel", "client-1", ts, "data-1")
	txID2 := consensus.ComputeTransactionDigest("test-channel", "client-2", ts, "data-2")

	tx1 := Transaction{ClientID: "client-1", TS: ts, ID: txID1, Data: "data-1"}
	tx2 := Transaction{ClientID: "client-2", TS: ts, ID: txID2, Data: "data-2"}

	blockData := BlockData{
		Transactions: [][]byte{tx1.ToBytes(), tx2.ToBytes()},
	}.ToBytes()

	header := BlockHeader{
		Sequence: 1,
		PrevHash: "genesis-hash",
		DataHash: computeDigest(blockData),
	}.ToBytes()

	validProposal := consensus.Proposal{
		Header:               header,
		Payload:              blockData,
		Metadata:             []byte("metadata-bytes"),
		VerificationSequence: 1,
	}

	// 1. Valid proposal
	reqs, err := verifier.VerifyProposal(validProposal)
	if err != nil {
		t.Fatalf("expected valid proposal to pass verification: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests from proposal, got %d", len(reqs))
	}
	if reqs[0].ID != txID1 || reqs[1].ID != txID2 {
		t.Fatalf("unexpected extracted request IDs: %+v", reqs)
	}

	// 2. Missing header
	noHeaderProp := validProposal
	noHeaderProp.Header = nil
	if _, err := verifier.VerifyProposal(noHeaderProp); err == nil {
		t.Fatalf("expected error for missing proposal header")
	}

	// 3. Missing payload
	noPayloadProp := validProposal
	noPayloadProp.Payload = nil
	if _, err := verifier.VerifyProposal(noPayloadProp); err == nil {
		t.Fatalf("expected error for missing proposal payload")
	}

	// 4. Empty batch (0 transactions in payload)
	emptyBlockData := BlockData{Transactions: [][]byte{}}.ToBytes()
	emptyHeader := BlockHeader{
		Sequence: 1,
		PrevHash: "genesis-hash",
		DataHash: computeDigest(emptyBlockData),
	}.ToBytes()
	emptyBatchProp := consensus.Proposal{
		Header:  emptyHeader,
		Payload: emptyBlockData,
	}
	if _, err := verifier.VerifyProposal(emptyBatchProp); err == nil {
		t.Fatalf("expected error for empty batch in proposal")
	}

	// 5. DataHash mismatch
	corruptedHeader := BlockHeader{
		Sequence: 1,
		PrevHash: "genesis-hash",
		DataHash: "tampered-data-hash",
	}.ToBytes()
	corruptedHashProp := validProposal
	corruptedHashProp.Header = corruptedHeader
	if _, err := verifier.VerifyProposal(corruptedHashProp); err == nil {
		t.Fatalf("expected error for data hash mismatch")
	}

	// 6. Duplicate transaction within batch
	dupBlockData := BlockData{
		Transactions: [][]byte{tx1.ToBytes(), tx1.ToBytes()},
	}.ToBytes()
	dupHeader := BlockHeader{
		Sequence: 1,
		PrevHash: "genesis-hash",
		DataHash: computeDigest(dupBlockData),
	}.ToBytes()
	dupProp := consensus.Proposal{
		Header:  dupHeader,
		Payload: dupBlockData,
	}
	if _, err := verifier.VerifyProposal(dupProp); err == nil {
		t.Fatalf("expected error for duplicate transaction in proposal batch")
	}

	// 7. Exceed batch count limit
	strictVerifier := NewECDSAVerifier("test-channel")
	strictVerifier.SetBatchLimits(1, 1024*1024) // Max 1 transaction
	if _, err := strictVerifier.VerifyProposal(validProposal); err == nil {
		t.Fatalf("expected error when transaction count exceeds batchMaxCount")
	}

	// 8. Exceed batch byte size limit
	strictSizeVerifier := NewECDSAVerifier("test-channel")
	strictSizeVerifier.SetBatchLimits(10, 10) // Max 10 bytes
	if _, err := strictSizeVerifier.VerifyProposal(validProposal); err == nil {
		t.Fatalf("expected error when payload size exceeds batchMaxBytes")
	}
}

func TestRequestsFromProposal(t *testing.T) {
	verifier := NewECDSAVerifier("test-channel")

	ts := int(time.Now().UnixNano() / 1000000)
	txID := consensus.ComputeTransactionDigest("test-channel", "c1", ts, "d1")
	tx := Transaction{ClientID: "c1", TS: ts, ID: txID, Data: "d1"}
	blockData := BlockData{Transactions: [][]byte{tx.ToBytes()}}.ToBytes()

	proposal := consensus.Proposal{
		Header:  []byte("hdr"),
		Payload: blockData,
	}

	reqs := verifier.RequestsFromProposal(proposal)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].ID != txID || reqs[0].ClientID != "c1" {
		t.Fatalf("unexpected RequestInfo: %+v", reqs[0])
	}

	// Malformed payload does not panic
	malformedProp := consensus.Proposal{
		Header:  []byte("hdr"),
		Payload: []byte("malformed-payload-bytes"),
	}
	reqsMalformed := verifier.RequestsFromProposal(malformedProp)
	if len(reqsMalformed) != 0 {
		t.Fatalf("expected 0 requests for malformed proposal payload")
	}
}

func TestRequestInspectorSafeHandling(t *testing.T) {
	node := &Node{}

	ts := int(time.Now().UnixNano() / 1000000)
	txID := consensus.ComputeTransactionDigest("default-channel", "client-10", ts, "sample-data")
	tx := Transaction{ClientID: "client-10", TS: ts, ID: txID, Data: "sample-data"}

	info := node.RequestID(tx.ToBytes())
	if info.ClientID != "client-10" || info.ID != txID {
		t.Fatalf("unexpected RequestInfo from valid request: %+v", info)
	}

	// Malformed bytes do not panic and return deterministic fallback
	malformedInfo := node.RequestID([]byte("malformed-bytes"))
	if malformedInfo.ClientID != "malformed" || malformedInfo.ID == "" {
		t.Fatalf("unexpected RequestInfo for malformed request: %+v", malformedInfo)
	}

	// Empty input does not panic
	emptyInfo := node.RequestID(nil)
	if emptyInfo.ClientID != "invalid" {
		t.Fatalf("unexpected RequestInfo for empty request: %+v", emptyInfo)
	}
}

func TestRequestPoolValidationAndDeduplication(t *testing.T) {
	node := &Node{}
	submittedChan := make(chan struct{}, 10)

	pool := consensus.NewRequestPoolWithOptions(consensus.RequestPoolOptions{
		MaxSize:       100,
		Role:          consensus.RolePrimaryLeader,
		NodeID:        consensus.NodeID("1"),
		PrimaryLeader: consensus.NodeID("1"),
		Logger:        node.logger,
		SubmittedChan: submittedChan,
		Inspector:     node,
	})

	ts := int(time.Now().UnixNano() / 1000000)
	txID := consensus.ComputeTransactionDigest("default-channel", "client-1", ts, "val-1")
	tx := Transaction{ClientID: "client-1", TS: ts, ID: txID, Data: "val-1"}

	// 1. Submit valid request
	if err := pool.Submit(tx.ToBytes()); err != nil {
		t.Fatalf("Submit failed for valid request: %v", err)
	}

	// 2. Submit duplicate request (same client & ID)
	if err := pool.Submit(tx.ToBytes()); err != consensus.ErrReqAlreadyExists {
		t.Fatalf("expected ErrReqAlreadyExists for duplicate request, got: %v", err)
	}

	// 3. Submit empty request
	if err := pool.Submit(nil); err == nil {
		t.Fatalf("expected error for submitting empty request")
	}

	// 4. Submit malformed request with invalid client ID
	emptyClientTx := Transaction{ClientID: "", TS: ts, ID: "", Data: "d"}
	if err := pool.Submit(emptyClientTx.ToBytes()); err == nil {
		t.Fatalf("expected error for submitting transaction without client ID")
	}
}

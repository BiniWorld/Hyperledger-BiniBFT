package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

type testSigner struct {
	nodeID  NodeID
	privKey *ecdsa.PrivateKey
}

func (s *testSigner) Sign(msg []byte) []byte {
	h := sha256.Sum256(msg)
	sig, _ := ecdsa.SignASN1(rand.Reader, s.privKey, h[:])
	return sig
}

func (s *testSigner) SignDigest(digest []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, s.privKey, digest)
}

func (s *testSigner) SignProposal(proposal Proposal, data []byte) *Signature {
	sig, _ := s.SignDigest([]byte(proposal.Digest()))
	return &Signature{
		ID:    1,
		Value: sig,
		Msg:   []byte(proposal.Digest()),
	}
}

type testVerifier struct {
	pubKeys map[NodeID]*ecdsa.PublicKey
}

func (v *testVerifier) VerifySignature(nodeID NodeID, data []byte, signature []byte) error {
	pk, ok := v.pubKeys[nodeID]
	if !ok {
		return fmt.Errorf("unknown node ID: %s", nodeID)
	}
	h := sha256.Sum256(data)
	if !ecdsa.VerifyASN1(pk, h[:], signature) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func (v *testVerifier) VerifyDigestSignature(nodeID NodeID, digest []byte, signature []byte) error {
	pk, ok := v.pubKeys[nodeID]
	if !ok {
		return fmt.Errorf("unknown node ID: %s", nodeID)
	}
	if !ecdsa.VerifyASN1(pk, digest, signature) {
		return fmt.Errorf("invalid digest signature")
	}
	return nil
}

func (v *testVerifier) VerifyProposalSignature(nodeID NodeID, proposal Proposal, sig Signature) error {
	return v.VerifyDigestSignature(nodeID, []byte(proposal.Digest()), sig.Value)
}

func (v *testVerifier) VerifyRequest(request []byte) (RequestInfo, error) {
	if len(request) == 0 {
		return RequestInfo{}, fmt.Errorf("empty request")
	}
	return RequestInfo{ClientID: "test-client", ID: string(request)}, nil
}

func (v *testVerifier) VerifyProposal(proposal Proposal) ([]RequestInfo, error) {
	if len(proposal.Header) == 0 && len(proposal.Payload) == 0 {
		return nil, fmt.Errorf("empty proposal")
	}
	return []RequestInfo{{ClientID: "test-client", ID: "test-id"}}, nil
}

func (v *testVerifier) RequestsFromProposal(proposal Proposal) []RequestInfo {
	return []RequestInfo{{ClientID: "test-client", ID: "test-id"}}
}

type mockNetwork struct{}

func (m *mockNetwork) Send(nodeID NodeID, message Message) error              { return nil }
func (m *mockNetwork) Broadcast(nodeIDs []NodeID, message Message) error       { return nil }
func (m *mockNetwork) RegisterHandler(handler MessageHandler)                  {}
func (m *mockNetwork) SendTransaction(targetID NodeID, request []byte) error { return nil }

func setupTestViewWithCrypto(t *testing.T) (*View, *ecdsa.PrivateKey, *ecdsa.PrivateKey) {
	privPrimary, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	privFollower, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	primaryID := NodeID("1")
	followerID := NodeID("2")

	verifier := &testVerifier{
		pubKeys: map[NodeID]*ecdsa.PublicKey{
			primaryID:  &privPrimary.PublicKey,
			followerID: &privFollower.PublicKey,
		},
	}

	signer := &testSigner{
		nodeID:  followerID,
		privKey: privFollower,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	config := &Config{
		NodeID:           followerID,
		ShardID:          ShardID(1),
		Role:             RoleShardFollower,
		PrimaryLeader:    primaryID,
		ShardLeaders:     map[ShardID]NodeID{ShardID(1): primaryID},
		ShardNodes:       map[ShardID][]NodeID{ShardID(1): {primaryID, followerID}},
		MajorityRequired: 1,
		Timeout:          5 * time.Second,
		ChannelID:        "test-channel",
		Signer:           signer,
		Verifier:         verifier,
		Logger:           logger,
		Network:          &mockNetwork{},
	}

	view := NewView(primaryID, ShardID(1), config)
	return view, privPrimary, privFollower
}

func TestViewHandlePrePrepareVerification(t *testing.T) {
	view, privPrimary, privFollower := setupTestViewWithCrypto(t)

	proposal := Proposal{
		Header:   []byte("header-1"),
		Payload:  []byte("payload-1"),
		Metadata: []byte("metadata-1"),
	}
	propDigest := proposal.Digest()
	seq := uint64(1)

	// Construct valid PrePrep message signed by primary
	validPrePrepDigest := ComputePrePrepDigest("test-channel", view.Number, seq, propDigest)
	validSig, err := ecdsa.SignASN1(rand.Reader, privPrimary, validPrePrepDigest)
	if err != nil {
		t.Fatalf("SignASN1 failed: %v", err)
	}

	validMsg := &PrePrepMessage{
		Proposal:  proposal,
		View:      view.Number,
		Sequence:  seq,
		Digest:    propDigest,
		NodeID:    NodeID("1"),
		ShardID:   ShardID(1),
		Signature: validSig,
	}

	if err := view.HandlePrePrepare(validMsg); err != nil {
		t.Fatalf("HandlePrePrepare failed for valid message: %v", err)
	}

	// Message with forged signature (signed by follower instead of primary)
	forgedSig, _ := ecdsa.SignASN1(rand.Reader, privFollower, validPrePrepDigest)
	forgedMsg := &PrePrepMessage{
		Proposal:  proposal,
		View:      view.Number,
		Sequence:  seq + 1,
		Digest:    propDigest,
		NodeID:    NodeID("1"), // Claims to be from Primary
		ShardID:   ShardID(1),
		Signature: forgedSig, // But signed by Follower key
	}

	if err := view.HandlePrePrepare(forgedMsg); err == nil {
		t.Fatalf("expected HandlePrePrepare to reject forged signature")
	}

	// Message with tampered proposal digest
	tamperedMsg := &PrePrepMessage{
		Proposal:  proposal,
		View:      view.Number,
		Sequence:  seq + 2,
		Digest:    "tampered-digest",
		NodeID:    NodeID("1"),
		ShardID:   ShardID(1),
		Signature: validSig, // Signature over original digest
	}

	if err := view.HandlePrePrepare(tamperedMsg); err == nil {
		t.Fatalf("expected HandlePrePrepare to reject tampered proposal digest")
	}
}

func TestViewHandlePreparePhaseVerification(t *testing.T) {
	view, privPrimary, _ := setupTestViewWithCrypto(t)

	proposal := Proposal{
		Header:   []byte("header-1"),
		Payload:  []byte("payload-1"),
		Metadata: []byte("metadata-1"),
	}
	propDigest := proposal.Digest()
	seq := uint64(1)

	prepDigest := ComputePrepareVoteDigest("test-channel", view.Number, seq, ShardID(1), NodeID("1"), propDigest)
	validSig, _ := ecdsa.SignASN1(rand.Reader, privPrimary, prepDigest)

	validMsg := &PreparePhaseMessage{
		Proposal:  proposal,
		View:      view.Number,
		Sequence:  seq,
		Digest:    propDigest,
		NodeID:    NodeID("1"),
		ShardID:   ShardID(1),
		Signature: validSig,
	}

	if err := view.HandlePreparePhase(validMsg); err != nil {
		t.Fatalf("HandlePreparePhase failed for valid message: %v", err)
	}

	// Tampered signature
	corruptedSig := append([]byte{0xFF}, validSig[1:]...)
	invalidMsg := &PreparePhaseMessage{
		Proposal:  proposal,
		View:      view.Number,
		Sequence:  seq + 1,
		Digest:    propDigest,
		NodeID:    NodeID("1"),
		ShardID:   ShardID(1),
		Signature: corruptedSig,
	}

	if err := view.HandlePreparePhase(invalidMsg); err == nil {
		t.Fatalf("expected HandlePreparePhase to reject invalid signature")
	}
}

func TestViewHandleCommitRequestVerification(t *testing.T) {
	view, privPrimary, _ := setupTestViewWithCrypto(t)

	proposal := Proposal{
		Header:   []byte("header-1"),
		Payload:  []byte("payload-1"),
		Metadata: []byte("metadata-1"),
	}
	propDigest := proposal.Digest()
	seq := uint64(1)

	commitDigest := ComputeCommitVoteDigest("test-channel", view.Number, seq, ShardID(1), NodeID("1"), propDigest)
	validSig, _ := ecdsa.SignASN1(rand.Reader, privPrimary, commitDigest)

	validMsg := &CommitRequestMessage{
		Proposal:  proposal,
		View:      view.Number,
		Sequence:  seq,
		Digest:    propDigest,
		NodeID:    NodeID("1"),
		ShardID:   ShardID(1),
		Signature: validSig,
	}

	if err := view.HandleCommitRequest(validMsg); err != nil {
		t.Fatalf("HandleCommitRequest failed for valid message: %v", err)
	}

	// Invalid sender
	unknownSenderMsg := &CommitRequestMessage{
		Proposal:  proposal,
		View:      view.Number,
		Sequence:  seq + 1,
		Digest:    propDigest,
		NodeID:    NodeID("unknown-node"),
		ShardID:   ShardID(1),
		Signature: validSig,
	}

	if err := view.HandleCommitRequest(unknownSenderMsg); err == nil {
		t.Fatalf("expected HandleCommitRequest to reject unknown sender node")
	}
}

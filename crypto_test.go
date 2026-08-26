package main

import (
	"binibft-poc/consensus"
	"crypto/sha256"
	"testing"
)

func TestCryptoKeyGeneration(t *testing.T) {
	privKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	if privKey == nil {
		t.Fatalf("GenerateKeyPair returned nil private key")
	}

	// Test PEM round-trip for private key
	privPEM, err := PrivateKeyToPEM(privKey)
	if err != nil {
		t.Fatalf("PrivateKeyToPEM failed: %v", err)
	}
	restoredPriv, err := PrivateKeyFromPEM(privPEM)
	if err != nil {
		t.Fatalf("PrivateKeyFromPEM failed: %v", err)
	}
	if restoredPriv.D.Cmp(privKey.D) != 0 {
		t.Fatalf("Restored private key does not match original private scalar")
	}

	// Test PEM round-trip for public key
	pubPEM, err := PublicKeyToPEM(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("PublicKeyToPEM failed: %v", err)
	}
	restoredPub, err := PublicKeyFromPEM(pubPEM)
	if err != nil {
		t.Fatalf("PublicKeyFromPEM failed: %v", err)
	}
	if restoredPub.X.Cmp(privKey.PublicKey.X) != 0 || restoredPub.Y.Cmp(privKey.PublicKey.Y) != 0 {
		t.Fatalf("Restored public key coordinates do not match original")
	}
}

func TestCryptoSigningAndVerification(t *testing.T) {
	privKey1, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	privKey2, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	message := []byte("BiniBFT test transaction payload")
	digest := sha256.Sum256(message)

	sig, err := SignDigest(privKey1, digest[:])
	if err != nil {
		t.Fatalf("SignDigest failed: %v", err)
	}
	if len(sig) == 0 {
		t.Fatalf("SignDigest produced empty signature")
	}

	// Valid verification
	if !VerifyDigest(&privKey1.PublicKey, digest[:], sig) {
		t.Fatalf("Valid signature failed to verify")
	}

	// Wrong public key (node 2 verifying node 1's signature)
	if VerifyDigest(&privKey2.PublicKey, digest[:], sig) {
		t.Fatalf("Signature verified against wrong public key")
	}

	// Tampered digest
	tamperedDigest := digest
	tamperedDigest[0] ^= 0xFF
	if VerifyDigest(&privKey1.PublicKey, tamperedDigest[:], sig) {
		t.Fatalf("Signature verified against tampered digest")
	}

	// Tampered signature bytes
	tamperedSig := make([]byte, len(sig))
	copy(tamperedSig, sig)
	tamperedSig[len(tamperedSig)-1] ^= 0xFF
	if VerifyDigest(&privKey1.PublicKey, digest[:], tamperedSig) {
		t.Fatalf("Tampered signature verified successfully")
	}
}

func TestDomainSeparationDigests(t *testing.T) {
	channelID := "channel-test"
	view := uint64(1)
	seq := uint64(42)
	nodeID := consensus.NodeID("node-1")
	shardID := consensus.ShardID(1)
	propDigest := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	d1 := consensus.ComputePrePrepDigest(channelID, view, seq, propDigest)
	d2 := consensus.ComputePrePrepAckDigest(channelID, view, seq, shardID, nodeID, propDigest)
	d3 := consensus.ComputePrepareVoteDigest(channelID, view, seq, shardID, nodeID, propDigest)
	d4 := consensus.ComputeCommitVoteDigest(channelID, view, seq, shardID, nodeID, propDigest)

	// Verify all domain digests are distinct
	if string(d1) == string(d2) || string(d2) == string(d3) || string(d3) == string(d4) {
		t.Fatalf("Domain digests collided: d1=%x, d2=%x, d3=%x, d4=%x", d1, d2, d3, d4)
	}

	// Verify changing channelID produces different digest
	d1OtherChan := consensus.ComputePrePrepDigest("other-channel", view, seq, propDigest)
	if string(d1) == string(d1OtherChan) {
		t.Fatalf("Channel ID separation failed to alter digest")
	}

	// Verify changing view produces different digest
	d1OtherView := consensus.ComputePrePrepDigest(channelID, view+1, seq, propDigest)
	if string(d1) == string(d1OtherView) {
		t.Fatalf("View separation failed to alter digest")
	}

	// Verify changing sequence produces different digest
	d1OtherSeq := consensus.ComputePrePrepDigest(channelID, view, seq+1, propDigest)
	if string(d1) == string(d1OtherSeq) {
		t.Fatalf("Sequence separation failed to alter digest")
	}
}

func TestECDSAVerifierRegistry(t *testing.T) {
	verifier := NewECDSAVerifier("test-channel")

	privKey1, _ := GenerateKeyPair()
	privKey2, _ := GenerateKeyPair()

	node1 := consensus.NodeID("node-1")
	node2 := consensus.NodeID("node-2")
	nodeUnknown := consensus.NodeID("node-unknown")

	verifier.RegisterPublicKey(node1, &privKey1.PublicKey)
	verifier.RegisterPublicKey(node2, &privKey2.PublicKey)

	msg := []byte("Hello BiniBFT")
	h := sha256.Sum256(msg)

	sig1, err := SignDigest(privKey1, h[:])
	if err != nil {
		t.Fatalf("SignDigest failed: %v", err)
	}

	// Verify node 1 signature
	if err := verifier.VerifyDigestSignature(node1, h[:], sig1); err != nil {
		t.Fatalf("verifier.VerifyDigestSignature failed for node 1: %v", err)
	}

	// Verify node 1 signature claiming to be from node 2
	if err := verifier.VerifyDigestSignature(node2, h[:], sig1); err == nil {
		t.Fatalf("expected error when verifying node 1 signature as node 2")
	}

	// Verify signature for unregistered node
	if err := verifier.VerifyDigestSignature(nodeUnknown, h[:], sig1); err == nil {
		t.Fatalf("expected error for unregistered node")
	}
}

func TestProposalSigningAndVerification(t *testing.T) {
	privKey, _ := GenerateKeyPair()
	nodeID := consensus.NodeID("1")
	signer := NewECDSASigner(nodeID, privKey, "test-channel")
	verifier := NewECDSAVerifier("test-channel")
	verifier.RegisterPublicKey(nodeID, &privKey.PublicKey)

	proposal := consensus.Proposal{
		Header:               []byte("header-data"),
		Payload:              []byte("payload-data"),
		Metadata:             []byte("metadata-data"),
		VerificationSequence: 1,
	}

	sig := signer.SignProposal(proposal, proposal.Payload)
	if sig == nil || len(sig.Value) == 0 {
		t.Fatalf("SignProposal returned empty signature")
	}
	if sig.ID != 1 {
		t.Fatalf("expected signature ID 1, got %d", sig.ID)
	}

	// Valid verification
	if err := verifier.VerifyProposalSignature(nodeID, proposal, *sig); err != nil {
		t.Fatalf("VerifyProposalSignature failed: %v", err)
	}

	// Verification with modified proposal
	tamperedProposal := proposal
	tamperedProposal.Payload = []byte("tampered-payload")
	if err := verifier.VerifyProposalSignature(nodeID, tamperedProposal, *sig); err == nil {
		t.Fatalf("expected error when verifying tampered proposal")
	}

	// Verification with wrong node ID
	if err := verifier.VerifyProposalSignature(consensus.NodeID("2"), proposal, *sig); err == nil {
		t.Fatalf("expected error when verifying signature with mismatched node ID")
	}
}

func TestCrossDomainReplayRejection(t *testing.T) {
	privKey, _ := GenerateKeyPair()
	nodeID := consensus.NodeID("node-1")
	channelID := "channel-1"
	view := uint64(1)
	seq := uint64(10)
	propDigest := "digest-12345"

	// Create a valid signature for PrePrep
	prePrepDigest := consensus.ComputePrePrepDigest(channelID, view, seq, propDigest)
	prePrepSig, err := SignDigest(privKey, prePrepDigest)
	if err != nil {
		t.Fatalf("SignDigest failed: %v", err)
	}

	verifier := NewECDSAVerifier(channelID)
	verifier.RegisterPublicKey(nodeID, &privKey.PublicKey)

	// PrePrep digest verifies with prePrepSig
	if err := verifier.VerifyDigestSignature(nodeID, prePrepDigest, prePrepSig); err != nil {
		t.Fatalf("valid PrePrep signature failed: %v", err)
	}

	// Attempt to replay prePrepSig as a Commit vote signature
	commitDigest := consensus.ComputeCommitVoteDigest(channelID, view, seq, consensus.ShardID(1), nodeID, propDigest)
	if err := verifier.VerifyDigestSignature(nodeID, commitDigest, prePrepSig); err == nil {
		t.Fatalf("security violation: PrePrep signature successfully replayed as Commit signature!")
	}

	// Attempt to replay across different channel
	otherChannelPrePrepDigest := consensus.ComputePrePrepDigest("channel-2", view, seq, propDigest)
	if err := verifier.VerifyDigestSignature(nodeID, otherChannelPrePrepDigest, prePrepSig); err == nil {
		t.Fatalf("security violation: signature successfully replayed across different channels!")
	}
}

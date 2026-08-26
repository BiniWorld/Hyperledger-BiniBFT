package consensus

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
)

func TestVRFEvaluationAndVerification(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	input := []byte("BINIBFT:channel-1:term-1:node-1")

	output, proof, err := EvaluateVRF(privKey, input)
	if err != nil {
		t.Fatalf("EvaluateVRF failed: %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("expected 32-byte VRF output, got %d", len(output))
	}
	if len(proof) == 0 {
		t.Fatalf("expected non-empty VRF proof")
	}

	// 1. Valid verification
	valid, err := VerifyVRF(&privKey.PublicKey, input, output, proof)
	if err != nil {
		t.Fatalf("VerifyVRF error: %v", err)
	}
	if !valid {
		t.Fatalf("expected VerifyVRF to return true")
	}

	// 2. VRF score
	score := VRFScore(output)
	if score.Sign() <= 0 {
		t.Fatalf("expected positive VRF score, got: %s", score.String())
	}
}

func TestVRFDeterminism(t *testing.T) {
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	input := []byte("BINIBFT:channel-1:term-42:node-5")

	out1, proof1, _ := EvaluateVRF(privKey, input)
	out2, proof2, _ := EvaluateVRF(privKey, input)

	// Outputs must be strictly identical across independent evaluations
	if !bytes.Equal(out1, out2) {
		t.Fatalf("VRF outputs are not deterministic: out1=%x, out2=%x", out1, out2)
	}

	// Both proofs must verify successfully
	if valid, err := VerifyVRF(&privKey.PublicKey, input, out1, proof1); !valid || err != nil {
		t.Fatalf("proof1 failed verification")
	}
	if valid, err := VerifyVRF(&privKey.PublicKey, input, out2, proof2); !valid || err != nil {
		t.Fatalf("proof2 failed verification")
	}
}

func TestVRFTamperedInput(t *testing.T) {
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	input := []byte("BINIBFT:term-1:node-1")
	output, proof, _ := EvaluateVRF(privKey, input)

	tamperedInput := []byte("BINIBFT:term-2:node-1")
	valid, err := VerifyVRF(&privKey.PublicKey, tamperedInput, output, proof)
	if valid || err == nil {
		t.Fatalf("expected verification failure for tampered input")
	}
}

func TestVRFWrongPublicKey(t *testing.T) {
	privKey1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	privKey2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	input := []byte("BINIBFT:term-1:node-1")
	output, proof, _ := EvaluateVRF(privKey1, input)

	valid, err := VerifyVRF(&privKey2.PublicKey, input, output, proof)
	if valid || err == nil {
		t.Fatalf("expected verification failure against wrong public key")
	}
}

func TestVRFTamperedOutputAndProof(t *testing.T) {
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	input := []byte("BINIBFT:term-1:node-1")
	output, proof, _ := EvaluateVRF(privKey, input)

	// Tampered output
	tamperedOutput := make([]byte, len(output))
	copy(tamperedOutput, output)
	tamperedOutput[0] ^= 0xFF
	if valid, err := VerifyVRF(&privKey.PublicKey, input, tamperedOutput, proof); valid || err == nil {
		t.Fatalf("expected verification failure for tampered VRF output")
	}

	// Tampered proof
	tamperedProof := make([]byte, len(proof))
	copy(tamperedProof, proof)
	tamperedProof[len(tamperedProof)-1] ^= 0xFF
	if valid, err := VerifyVRF(&privKey.PublicKey, input, output, tamperedProof); valid || err == nil {
		t.Fatalf("expected verification failure for tampered VRF proof")
	}
}

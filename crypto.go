package main

import (
	"binibft-poc/consensus"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// Re-export domain constants from consensus package for convenience
const (
	DomainProposal        = consensus.DomainProposal
	DomainPrePrep         = consensus.DomainPrePrep
	DomainPrePrepAck      = consensus.DomainPrePrepAck
	DomainPrepareVote     = consensus.DomainPrepareVote
	DomainShardPrepareQC  = consensus.DomainShardPrepareQC
	DomainCommitVote      = consensus.DomainCommitVote
	DomainShardCommitQC   = consensus.DomainShardCommitQC
	DomainViewChange      = consensus.DomainViewChange
	DomainLeaderElection  = consensus.DomainLeaderElection
	DomainElectionAck     = consensus.DomainElectionAck
	DomainShardAssignment = consensus.DomainShardAssignment
	DomainHeartbeat       = consensus.DomainHeartbeat
	DomainRequest         = consensus.DomainRequest
)

// GenerateKeyPair generates a new ECDSA private key using the P-256 curve
func GenerateKeyPair() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// PrivateKeyToPEM encodes an ECDSA private key to PEM format
func PrivateKeyToPEM(priv *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal EC private key: %w", err)
	}
	block := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	}
	return pem.EncodeToMemory(block), nil
}

// PrivateKeyFromPEM decodes an ECDSA private key from PEM bytes
func PrivateKeyFromPEM(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// PublicKeyToPEM encodes an ECDSA public key to PKIX PEM format
func PublicKeyToPEM(pub *ecdsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PKIX public key: %w", err)
	}
	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}
	return pem.EncodeToMemory(block), nil
}

// PublicKeyFromPEM decodes an ECDSA public key from PKIX PEM bytes
func PublicKeyFromPEM(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKIX public key: %w", err)
	}
	pub, ok := pubInterface.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ECDSA")
	}
	return pub, nil
}

// SignDigest signs a 32-byte digest using ECDSA with SHA-256 ASN.1 encoding
func SignDigest(priv *ecdsa.PrivateKey, digest []byte) ([]byte, error) {
	if priv == nil {
		return nil, fmt.Errorf("private key is nil")
	}
	return ecdsa.SignASN1(rand.Reader, priv, digest)
}

// VerifyDigest verifies an ASN.1 encoded ECDSA signature over a digest
func VerifyDigest(pub *ecdsa.PublicKey, digest []byte, signature []byte) bool {
	if pub == nil || len(digest) == 0 || len(signature) == 0 {
		return false
	}
	return ecdsa.VerifyASN1(pub, digest, signature)
}

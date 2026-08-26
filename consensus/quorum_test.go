package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
)

func TestQuorumFormulas(t *testing.T) {
	// Intra-shard / Node Byzantine counts
	tests := []struct {
		n           int
		expectedF   int
		expectedQ   int
	}{
		{n: 0, expectedF: 0, expectedQ: 0},
		{n: 1, expectedF: 0, expectedQ: 1},
		{n: 2, expectedF: 0, expectedQ: 2},
		{n: 3, expectedF: 0, expectedQ: 2},
		{n: 4, expectedF: 1, expectedQ: 3},
		{n: 5, expectedF: 1, expectedQ: 3},
		{n: 6, expectedF: 1, expectedQ: 3},
		{n: 7, expectedF: 2, expectedQ: 5},
		{n: 8, expectedF: 2, expectedQ: 5},
		{n: 9, expectedF: 2, expectedQ: 5},
		{n: 10, expectedF: 3, expectedQ: 7},
	}

	for _, tt := range tests {
		f := CalculateMaxByzantineNodes(tt.n)
		if f != tt.expectedF {
			t.Errorf("CalculateMaxByzantineNodes(%d) = %d, expected %d", tt.n, f, tt.expectedF)
		}
		q := CalculateIntraShardQuorum(tt.n)
		if q != tt.expectedQ {
			t.Errorf("CalculateIntraShardQuorum(%d) = %d, expected %d", tt.n, q, tt.expectedQ)
		}
		qCross := CalculateCrossShardQuorum(tt.n)
		if qCross != tt.expectedQ {
			t.Errorf("CalculateCrossShardQuorum(%d) = %d, expected %d", tt.n, qCross, tt.expectedQ)
		}
	}
}

func TestShardQCVerification(t *testing.T) {
	channelID := "test-channel"
	view := uint64(1)
	seq := uint64(10)
	shardID := ShardID(1)
	digest := "sample-proposal-digest"

	// 4 nodes in shard: node-1, node-2, node-3, node-4 (f=1, Q=3)
	shardNodes := []NodeID{NodeID("1"), NodeID("2"), NodeID("3"), NodeID("4")}
	minQuorum := CalculateIntraShardQuorum(len(shardNodes)) // 3

	keys := make(map[NodeID]*ecdsa.PrivateKey)
	pubKeys := make(map[NodeID]*ecdsa.PublicKey)
	for _, n := range shardNodes {
		priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		keys[n] = priv
		pubKeys[n] = &priv.PublicKey
	}

	verifier := &testVerifier{pubKeys: pubKeys}

	// 1. Construct valid ShardQC with 3 signatures
	signatures := make(map[NodeID][]byte)
	for _, n := range shardNodes[:3] {
		prepDigest := ComputePrepareVoteDigest(channelID, view, seq, shardID, n, digest)
		sig, _ := ecdsa.SignASN1(rand.Reader, keys[n], prepDigest)
		signatures[n] = sig
	}

	shardQC := CreateShardQC(channelID, view, seq, "prepare", shardID, digest, signatures)

	if err := VerifyShardQC(verifier, shardQC, shardNodes, minQuorum); err != nil {
		t.Fatalf("expected valid ShardQC to verify: %v", err)
	}

	// 2. Insufficient signatures (2 < 3)
	insufficientSigs := make(map[NodeID][]byte)
	insufficientSigs[shardNodes[0]] = signatures[shardNodes[0]]
	insufficientSigs[shardNodes[1]] = signatures[shardNodes[1]]
	insufficientQC := CreateShardQC(channelID, view, seq, "prepare", shardID, digest, insufficientSigs)

	if err := VerifyShardQC(verifier, insufficientQC, shardNodes, minQuorum); err == nil {
		t.Fatalf("expected error for insufficient signatures in ShardQC")
	}

	// 3. Signature from unrecognized node
	foreignPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	foreignID := NodeID("foreign-node")
	foreignDigest := ComputePrepareVoteDigest(channelID, view, seq, shardID, foreignID, digest)
	foreignSig, _ := ecdsa.SignASN1(rand.Reader, foreignPriv, foreignDigest)

	unrecSigs := make(map[NodeID][]byte)
	unrecSigs[shardNodes[0]] = signatures[shardNodes[0]]
	unrecSigs[shardNodes[1]] = signatures[shardNodes[1]]
	unrecSigs[foreignID] = foreignSig
	unrecQC := CreateShardQC(channelID, view, seq, "prepare", shardID, digest, unrecSigs)

	if err := VerifyShardQC(verifier, unrecQC, shardNodes, minQuorum); err == nil {
		t.Fatalf("expected error for unrecognized node signature in ShardQC")
	}

	// 4. Corrupted signature
	corruptedSigs := make(map[NodeID][]byte)
	for k, v := range signatures {
		corruptedSigs[k] = v
	}
	corruptedSigs[shardNodes[0]] = []byte("corrupted-signature-bytes")
	corruptedQC := CreateShardQC(channelID, view, seq, "prepare", shardID, digest, corruptedSigs)

	if err := VerifyShardQC(verifier, corruptedQC, shardNodes, minQuorum); err == nil {
		t.Fatalf("expected error for corrupted signature in ShardQC")
	}
}

func TestCommitQCVerification(t *testing.T) {
	channelID := "test-channel"
	view := uint64(1)
	seq := uint64(5)
	digest := "commit-proposal-digest"

	// 4 Shards: Shard 1, Shard 2, Shard 3, Shard 4 (f_shards=1, Q_cross=3)
	shardNodesMap := map[ShardID][]NodeID{
		ShardID(1): {NodeID("1-1"), NodeID("1-2"), NodeID("1-3"), NodeID("1-4")},
		ShardID(2): {NodeID("2-1"), NodeID("2-2"), NodeID("2-3"), NodeID("2-4")},
		ShardID(3): {NodeID("3-1"), NodeID("3-2"), NodeID("3-3"), NodeID("3-4")},
		ShardID(4): {NodeID("4-1"), NodeID("4-2"), NodeID("4-3"), NodeID("4-4")},
	}

	pubKeys := make(map[NodeID]*ecdsa.PublicKey)
	privKeys := make(map[NodeID]*ecdsa.PrivateKey)

	for _, nodes := range shardNodesMap {
		for _, n := range nodes {
			priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			privKeys[n] = priv
			pubKeys[n] = &priv.PublicKey
		}
	}

	verifier := &testVerifier{pubKeys: pubKeys}
	minCrossQuorum := CalculateCrossShardQuorum(len(shardNodesMap)) // 3

	// Create valid ShardQCs for shards 1, 2, 3
	shardQCs := make(map[ShardID]*ShardQC)
	for sid := ShardID(1); sid <= 3; sid++ {
		sigs := make(map[NodeID][]byte)
		for _, nodeID := range shardNodesMap[sid][:3] {
			commitDigest := ComputeCommitVoteDigest(channelID, view, seq, sid, nodeID, digest)
			sig, _ := ecdsa.SignASN1(rand.Reader, privKeys[nodeID], commitDigest)
			sigs[nodeID] = sig
		}
		shardQCs[sid] = CreateShardQC(channelID, view, seq, "commit", sid, digest, sigs)
	}

	commitQC := CreateCommitQC(channelID, view, seq, digest, shardQCs)

	// 1. Valid CommitQC
	if err := VerifyCommitQC(verifier, commitQC, shardNodesMap, minCrossQuorum); err != nil {
		t.Fatalf("expected valid CommitQC to verify: %v", err)
	}

	// 2. Insufficient ShardQCs (2 < 3)
	insufficientShardQCs := map[ShardID]*ShardQC{
		ShardID(1): shardQCs[ShardID(1)],
		ShardID(2): shardQCs[ShardID(2)],
	}
	insufficientCommitQC := CreateCommitQC(channelID, view, seq, digest, insufficientShardQCs)
	if err := VerifyCommitQC(verifier, insufficientCommitQC, shardNodesMap, minCrossQuorum); err == nil {
		t.Fatalf("expected error for insufficient ShardQCs in CommitQC")
	}

	// 3. Sequence mismatch in ShardQC
	mismatchedSeqQC := CreateShardQC(channelID, view, seq+1, "commit", ShardID(1), digest, shardQCs[ShardID(1)].Signatures)
	mismatchedCommitQC := CreateCommitQC(channelID, view, seq, digest, map[ShardID]*ShardQC{
		ShardID(1): mismatchedSeqQC,
		ShardID(2): shardQCs[ShardID(2)],
		ShardID(3): shardQCs[ShardID(3)],
	})
	if err := VerifyCommitQC(verifier, mismatchedCommitQC, shardNodesMap, minCrossQuorum); err == nil {
		t.Fatalf("expected error for sequence mismatch in CommitQC")
	}
}

package main

import (
	"binibft-poc/consensus"
)

// RequestID extracts and validates RequestInfo from raw transaction bytes safely without panicking
func (n *Node) RequestID(req []byte) consensus.RequestInfo {
	if len(req) == 0 {
		return consensus.RequestInfo{
			ClientID: "invalid",
			ID:       "",
		}
	}

	txn, err := TransactionFromBytes(req)
	if err != nil {
		// If unmarshaling fails, derive a deterministic identifier over raw bytes
		digest := consensus.ComputeTransactionDigest("default-channel", "malformed", 0, string(req))
		return consensus.RequestInfo{
			ClientID: "malformed",
			ID:       digest,
		}
	}

	if txn.ClientID == "" {
		return consensus.RequestInfo{
			ClientID: "invalid-client",
			ID:       "",
		}
	}

	expectedID := txn.ID
	if expectedID == "" {
		expectedID = consensus.ComputeTransactionDigest("default-channel", txn.ClientID, txn.TS, txn.Data)
	}

	return consensus.RequestInfo{
		ClientID: txn.ClientID,
		ID:       expectedID,
	}
}

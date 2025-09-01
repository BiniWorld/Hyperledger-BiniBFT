package main

import (
	"binibft-poc/consensus"
)

func (*Node) RequestID(req []byte) consensus.RequestInfo {
	txn := TransactionFromBytes(req)
	return consensus.RequestInfo{
		ClientID: txn.ClientID,
		ID:       txn.ID,
	}
}

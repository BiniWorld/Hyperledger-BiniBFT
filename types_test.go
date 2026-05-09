package main

import (
	"reflect"
	"testing"
)

func TestTransactionRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		txn  Transaction
	}{
		{
			name: "simple transaction",
			txn: Transaction{
				ClientID: "client-1",
				TS:       1,
				ID:       "tx-1",
				Data:     "payload-a",
			},
		},
		{
			name: "transaction with larger payload",
			txn: Transaction{
				ClientID: "client-2",
				TS:       42,
				ID:       "tx-2",
				Data:     "payload-with-more-content",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TransactionFromBytes(tt.txn.ToBytes())
			if !reflect.DeepEqual(*got, tt.txn) {
				t.Fatalf("round-trip mismatch: got %+v want %+v", *got, tt.txn)
			}
		})
	}
}

func TestBlockDataRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		block BlockData
	}{
		{
			name:  "empty transactions",
			block: BlockData{Transactions: [][]byte{}},
		},
		{
			name: "multiple transactions",
			block: BlockData{Transactions: [][]byte{
				Transaction{ClientID: "c1", TS: 1, ID: "t1", Data: "a"}.ToBytes(),
				Transaction{ClientID: "c2", TS: 2, ID: "t2", Data: "b"}.ToBytes(),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BlockDataFromBytes(tt.block.ToBytes())
			if !reflect.DeepEqual(*got, tt.block) {
				t.Fatalf("round-trip mismatch: got %+v want %+v", *got, tt.block)
			}
		})
	}
}

func TestBlockHeaderRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		header BlockHeader
	}{
		{
			name: "first sequence",
			header: BlockHeader{
				Sequence: 1,
				PrevHash: "prev-hash-1",
				DataHash: "data-hash-1",
			},
		},
		{
			name: "later sequence",
			header: BlockHeader{
				Sequence: 99,
				PrevHash: "prev-hash-98",
				DataHash: "data-hash-99",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BlockHeaderFromBytes(tt.header.ToBytes())
			if !reflect.DeepEqual(*got, tt.header) {
				t.Fatalf("round-trip mismatch: got %+v want %+v", *got, tt.header)
			}
		})
	}
}

func TestBlockRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		block Block
	}{
		{
			name: "empty block",
			block: Block{
				Sequence:     1,
				PrevHash:     "genesis",
				Metadata:     []byte{},
				Transactions: []Transaction{},
			},
		},
		{
			name: "block with transactions",
			block: Block{
				Sequence: 2,
				PrevHash: "hash-1",
				Metadata: []byte{1, 2, 3},
				Transactions: []Transaction{
					{ClientID: "c1", TS: 10, ID: "t10", Data: "d10"},
					{ClientID: "c2", TS: 11, ID: "t11", Data: "d11"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BlockFromBytes(tt.block.ToBytes())
			if !reflect.DeepEqual(*got, tt.block) {
				t.Fatalf("round-trip mismatch: got %+v want %+v", *got, tt.block)
			}
		})
	}
}

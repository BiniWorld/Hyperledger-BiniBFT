package main

import (
	"reflect"
	"testing"
)

func TestTransaction_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		txn  Transaction
	}{
		{
			name: "Case 1: Standard transaction",
			txn: Transaction{
				ClientID: "client-1",
				TS:       1234567890,
				ID:       "txn-1",
				Data:     "data-1",
			},
		},
		{
			name: "Case 2: Empty fields transaction",
			txn: Transaction{
				ClientID: "",
				TS:       0,
				ID:       "",
				Data:     "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bytes := tt.txn.ToBytes()
			decoded := TransactionFromBytes(bytes)
			if !reflect.DeepEqual(&tt.txn, decoded) {
				t.Errorf("Transaction round trip failed: got %v, want %v", decoded, &tt.txn)
			}
		})
	}
}

func TestBlockData_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data BlockData
	}{
		{
			name: "Case 1: With transactions",
			data: BlockData{
				Transactions: [][]byte{
					[]byte("tx1"),
					[]byte("tx2"),
				},
			},
		},
		{
			name: "Case 2: Empty transactions",
			data: BlockData{
				Transactions: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bytes := tt.data.ToBytes()
			decoded := BlockDataFromBytes(bytes)

			if len(tt.data.Transactions) == 0 && len(decoded.Transactions) == 0 {
				tt.data.Transactions = nil
				decoded.Transactions = nil
			}

			if !reflect.DeepEqual(&tt.data, decoded) {
				t.Errorf("BlockData round trip failed: got %v, want %v", decoded, &tt.data)
			}
		})
	}
}

func TestBlockHeader_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		header BlockHeader
	}{
		{
			name: "Case 1: Standard block header",
			header: BlockHeader{
				Sequence: 1,
				PrevHash: "prev-hash-1",
				DataHash: "data-hash-1",
			},
		},
		{
			name: "Case 2: Empty block header",
			header: BlockHeader{
				Sequence: 0,
				PrevHash: "",
				DataHash: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bytes := tt.header.ToBytes()
			decoded := BlockHeaderFromBytes(bytes)
			if !reflect.DeepEqual(&tt.header, decoded) {
				t.Errorf("BlockHeader round trip failed: got %v, want %v", decoded, &tt.header)
			}
		})
	}
}

func TestBlock_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		block Block
	}{
		{
			name: "Case 1: Standard block",
			block: Block{
				Sequence: 1,
				PrevHash: "prev-hash",
				Metadata: []byte("meta"),
				Transactions: []Transaction{
					{ClientID: "client1", TS: 1, ID: "1", Data: "data1"},
				},
			},
		},
		{
			name: "Case 2: Empty block",
			block: Block{
				Sequence:     0,
				PrevHash:     "",
				Metadata:     nil,
				Transactions: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bytes := tt.block.ToBytes()
			decoded := BlockFromBytes(bytes)

			if len(tt.block.Metadata) == 0 && len(decoded.Metadata) == 0 {
				tt.block.Metadata = nil
				decoded.Metadata = nil
			}
			if len(tt.block.Transactions) == 0 && len(decoded.Transactions) == 0 {
				tt.block.Transactions = nil
				decoded.Transactions = nil
			}

			if !reflect.DeepEqual(&tt.block, decoded) {
				t.Errorf("Block round trip failed: got %v, want %v", decoded, &tt.block)
			}
		})
	}
}

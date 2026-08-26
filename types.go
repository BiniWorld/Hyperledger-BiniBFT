package main

import (
	"encoding/asn1"
	"fmt"
	"time"

	"github.com/golang/protobuf/proto"
)

type NodeInfo struct {
	ID         string
	Address    string
	OpsAddress string
}

type TXInput struct {
	Data     string `json:"data"`
	ClientID string `json:"clientID"`
}

type (
	Ingress map[int]<-chan proto.Message
	Egress  map[int]chan<- proto.Message
)

type NetworkOptions struct {
	NumNodes     int
	BatchSize    uint64
	BatchTimeout time.Duration
}

type Block struct {
	Sequence     int64         `json:"sequence"`
	PrevHash     string        `json:"prevHash"`
	Metadata     []byte        `json:"metadata"`
	Transactions []Transaction `json:"transactions"`
}

func (block Block) ToBytes() []byte {
	rawHeader, err := asn1.Marshal(block)
	if err != nil {
		return nil
	}
	return rawHeader
}

func (block Block) ToBytesChecked() ([]byte, error) {
	return asn1.Marshal(block)
}

func BlockFromBytes(rawHeader []byte) (*Block, error) {
	if len(rawHeader) == 0 {
		return nil, fmt.Errorf("cannot unmarshal empty block bytes")
	}
	var block Block
	rest, err := asn1.Unmarshal(rawHeader, &block)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("unexpected trailing bytes in block encoding")
	}
	return &block, nil
}

type BlockHeader struct {
	Sequence int64
	PrevHash string
	DataHash string
}

func (header BlockHeader) ToBytes() []byte {
	rawHeader, err := asn1.Marshal(header)
	if err != nil {
		return nil
	}
	return rawHeader
}

func (header BlockHeader) ToBytesChecked() ([]byte, error) {
	return asn1.Marshal(header)
}

func BlockHeaderFromBytes(rawHeader []byte) (*BlockHeader, error) {
	if len(rawHeader) == 0 {
		return nil, fmt.Errorf("cannot unmarshal empty block header bytes")
	}
	var header BlockHeader
	rest, err := asn1.Unmarshal(rawHeader, &header)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal block header: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("unexpected trailing bytes in block header encoding")
	}
	return &header, nil
}

type Transaction struct {
	ClientID string `json:"clientID"`
	TS       int    `json:"ts"`
	ID       string `json:"id"`
	Data     string `json:"data"`
}

func (txn Transaction) ToBytes() []byte {
	rawTxn, err := asn1.Marshal(txn)
	if err != nil {
		return nil
	}
	return rawTxn
}

func (txn Transaction) ToBytesChecked() ([]byte, error) {
	return asn1.Marshal(txn)
}

func TransactionFromBytes(rawTxn []byte) (*Transaction, error) {
	if len(rawTxn) == 0 {
		return nil, fmt.Errorf("cannot unmarshal empty transaction bytes")
	}
	var txn Transaction
	rest, err := asn1.Unmarshal(rawTxn, &txn)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("unexpected trailing bytes in transaction encoding")
	}
	return &txn, nil
}

type BlockData struct {
	Transactions [][]byte
}

func (b BlockData) ToBytes() []byte {
	rawBlock, err := asn1.Marshal(b)
	if err != nil {
		return nil
	}
	return rawBlock
}

func (b BlockData) ToBytesChecked() ([]byte, error) {
	return asn1.Marshal(b)
}

func BlockDataFromBytes(rawBlock []byte) (*BlockData, error) {
	if len(rawBlock) == 0 {
		return nil, fmt.Errorf("cannot unmarshal empty block data bytes")
	}
	var block BlockData
	rest, err := asn1.Unmarshal(rawBlock, &block)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal block data: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("unexpected trailing bytes in block data encoding")
	}
	return &block, nil
}

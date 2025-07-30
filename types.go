package main

import (
	"encoding/asn1"
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
		panic(err)
	}
	return rawHeader
}

func BlockFromBytes(rawHeader []byte) *Block {
	var block Block
	asn1.Unmarshal(rawHeader, &block)
	return &block
}

type BlockHeader struct {
	Sequence int64
	PrevHash string
	DataHash string
}

func (header BlockHeader) ToBytes() []byte {
	rawHeader, err := asn1.Marshal(header)
	if err != nil {
		panic(err)
	}
	return rawHeader
}

func BlockHeaderFromBytes(rawHeader []byte) *BlockHeader {
	var header BlockHeader
	asn1.Unmarshal(rawHeader, &header)
	return &header
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
		panic(err)
	}
	return rawTxn
}

func TransactionFromBytes(rawTxn []byte) *Transaction {
	var txn Transaction
	asn1.Unmarshal(rawTxn, &txn)
	return &txn
}

type BlockData struct {
	Transactions [][]byte
}

func (b BlockData) ToBytes() []byte {
	rawBlock, err := asn1.Marshal(b)
	if err != nil {
		panic(err)
	}
	return rawBlock
}

func BlockDataFromBytes(rawBlock []byte) *BlockData {
	var block BlockData
	asn1.Unmarshal(rawBlock, &block)
	return &block
}

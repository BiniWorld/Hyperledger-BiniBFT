package fabric

import (
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
)

// Envelope encapsulates a payload with a cryptographic signature (Fabric common.Envelope)
type Envelope struct {
	Payload   []byte `json:"payload"`
	Signature []byte `json:"signature"`
}

func (e *Envelope) ToBytes() ([]byte, error) {
	return asn1.Marshal(*e)
}

func EnvelopeFromBytes(data []byte) (*Envelope, error) {
	var env Envelope
	if _, err := asn1.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fabric envelope: %w", err)
	}
	return &env, nil
}

// BlockHeader defines the header of a Fabric block
type BlockHeader struct {
	Number       uint64 `json:"number"`
	PreviousHash []byte `json:"previous_hash"`
	DataHash     []byte `json:"data_hash"`
}

// BlockData defines the transaction payload data in a Fabric block
type BlockData struct {
	Data [][]byte `json:"data"`
}

// BlockMetadata holds block metadata including signatures, consensus metadata, and commit QCs
type BlockMetadata struct {
	Metadata [][]byte `json:"metadata"`
}

// Block represents a Hyperledger Fabric block (common.Block)
type Block struct {
	Header   *BlockHeader   `json:"header"`
	Data     *BlockData     `json:"data"`
	Metadata *BlockMetadata `json:"metadata"`
}

// ComputeHash computes the ASN.1 SHA-256 header hash for block chaining
func (b *Block) ComputeHash() []byte {
	if b.Header == nil {
		return nil
	}
	raw, err := asn1.Marshal(*b.Header)
	if err != nil {
		return nil
	}
	h := sha256.Sum256(raw)
	return h[:]
}

// Metadata represents signature and consenter metadata in Fabric
type Metadata struct {
	Value      []byte   `json:"value"`
	Signatures [][]byte `json:"signatures"`
}

// Consenter represents the Hyperledger Fabric consensus plugin factory interface
type Consenter interface {
	HandleChain(support ConsenterSupport, metadata *Metadata) (Chain, error)
}

// Chain represents a consensus chain instance for a specific Fabric channel
type Chain interface {
	Order(env *Envelope, configSeq uint64) error
	Configure(config *Envelope, configSeq uint64) error
	WaitReady() error
	Start()
	Halt()
	Errored() <-chan struct{}
}

// ConsenterSupport provides runtime dependencies and services from the Fabric Orderer
type ConsenterSupport interface {
	ChannelID() string
	Height() uint64
	Block(number uint64) *Block
	WriteBlock(block *Block, encodedMetadataValue []byte)
	WriteConfigBlock(block *Block, encodedMetadataValue []byte)
	Sign(msg []byte) ([]byte, error)
	VerifyBlockSignature(signatures []*Metadata, config *Block) error
}

// MockConsenterSupport implements ConsenterSupport for testing
type MockConsenterSupport struct {
	Channel        string
	Blocks         []*Block
	MetadataValues [][]byte
	ConfigBlocks   []*Block
	SignFunc       func([]byte) ([]byte, error)
}

func NewMockConsenterSupport(channel string) *MockConsenterSupport {
	return &MockConsenterSupport{
		Channel:        channel,
		Blocks:         make([]*Block, 0),
		MetadataValues: make([][]byte, 0),
		ConfigBlocks:   make([]*Block, 0),
	}
}

func (m *MockConsenterSupport) ChannelID() string {
	return m.Channel
}

func (m *MockConsenterSupport) Height() uint64 {
	return uint64(len(m.Blocks))
}

func (m *MockConsenterSupport) Block(number uint64) *Block {
	if number >= uint64(len(m.Blocks)) {
		return nil
	}
	return m.Blocks[number]
}

func (m *MockConsenterSupport) WriteBlock(block *Block, encodedMetadataValue []byte) {
	m.Blocks = append(m.Blocks, block)
	m.MetadataValues = append(m.MetadataValues, encodedMetadataValue)
}

func (m *MockConsenterSupport) WriteConfigBlock(block *Block, encodedMetadataValue []byte) {
	m.ConfigBlocks = append(m.ConfigBlocks, block)
	m.WriteBlock(block, encodedMetadataValue)
}

func (m *MockConsenterSupport) Sign(msg []byte) ([]byte, error) {
	if m.SignFunc != nil {
		return m.SignFunc(msg)
	}
	h := sha256.Sum256(msg)
	return h[:], nil
}

func (m *MockConsenterSupport) VerifyBlockSignature(signatures []*Metadata, config *Block) error {
	return nil
}

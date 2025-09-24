/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperledger/fabric-lib-go/bccsp"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/common/policies"
	"github.com/hyperledger/fabric/orderer/common/cluster"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/common/msgprocessor"
	types2 "github.com/hyperledger/fabric/orderer/common/types"
	"github.com/hyperledger/fabric/orderer/consensus"
	binibftconsensus "github.com/hyperledger/fabric/orderer/consensus/binibft/consensus"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// BFTChain implements Chain interface to wire with BiniBFT consensus library
type BFTChain struct {
	RuntimeConfig      *atomic.Value
	Channel            string
	Config             *Configuration
	clusterDialer      *cluster.PredicateDialer
	localConfigCluster localconfig.Cluster
	Comm               cluster.Communicator
	SignerSerializer   SignerSerializer
	PolicyManager      policies.Manager
	Logger             *flogging.FabricLogger
	WALDir             string
	consensus          *BiniBFTConsensus
	support            consensus.ConsenterSupport
	ClusterService     *cluster.ClusterService
	verifier           *Verifier
	assembler          *Assembler
	Metrics            *Metrics
	bccsp              bccsp.BCCSP

	statusReportMutex sync.Mutex
	consensusRelation types2.ConsensusRelation
	status            types2.Status

	// BiniBFT specific fields
	nodeRole      NodeRole
	shardID       ShardID
	primaryLeader NodeID
	shardLeaders  map[ShardID]NodeID
	shardNodes    map[ShardID][]NodeID
}

// NewChain creates new BiniBFT chain
func NewChain(
	cv ConfigBlockValidator,
	selfID uint64,
	config *Configuration,
	walDir string,
	clusterDialer *cluster.PredicateDialer,
	localConfigCluster localconfig.Cluster,
	comm cluster.Communicator,
	signerSerializer SignerSerializer,
	policyManager policies.Manager,
	support consensus.ConsenterSupport,
	metrics *Metrics,
	bccsp bccsp.BCCSP,
) (*BFTChain, error) {
	logger := flogging.MustGetLogger("orderer.consensus.binibft.chain").With(zap.String("channel", support.ChannelID()))

	c := &BFTChain{
		RuntimeConfig:      &atomic.Value{},
		Channel:            support.ChannelID(),
		Config:             config,
		WALDir:             walDir,
		Comm:               comm,
		support:            support,
		SignerSerializer:   signerSerializer,
		PolicyManager:      policyManager,
		clusterDialer:      clusterDialer,
		localConfigCluster: localConfigCluster,
		Logger:             logger,
		consensusRelation:  types2.ConsensusRelationConsenter,
		status:             types2.StatusActive,
		Metrics: &Metrics{
			ClusterSize:          metrics.ClusterSize.With("channel", support.ChannelID()),
			CommittedBlockNumber: metrics.CommittedBlockNumber.With("channel", support.ChannelID()),
			IsLeader:             metrics.IsLeader.With("channel", support.ChannelID()),
			LeaderID:             metrics.LeaderID.With("channel", support.ChannelID()),
		},
		bccsp: bccsp,
	}

	// Initialize BiniBFT specific configuration
	err := c.initializeBiniBFTConfig(selfID, support)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize BiniBFT configuration")
	}

	lastBlock := LastBlockFromLedgerOrPanic(support, c.Logger)
	lastConfigBlock := LastConfigBlockFromLedgerOrPanic(support, c.Logger)

	rtc := RuntimeConfig{
		logger: logger,
		id:     selfID,
	}
	rtc, err = rtc.BlockCommitted(lastConfigBlock, bccsp)
	if err != nil {
		return nil, errors.Wrap(err, "failed constructing RuntimeConfig")
	}
	rtc, err = rtc.BlockCommitted(lastBlock, bccsp)
	if err != nil {
		return nil, errors.Wrap(err, "failed constructing RuntimeConfig")
	}

	c.RuntimeConfig.Store(rtc)

	c.verifier = buildVerifier(cv, c.RuntimeConfig, support, policyManager)
	c.consensus = c.buildBiniBFTConsensus()

	// Setup communication with list of remote nodes for the new channel
	c.Comm.Configure(c.support.ChannelID(), rtc.RemoteNodes)

	logger.Infof("BiniBFT is now servicing chain")

	return c, nil
}

// initializeBiniBFTConfig initializes BiniBFT specific configuration from configtx
func (c *BFTChain) initializeBiniBFTConfig(selfID uint64, support consensus.ConsenterSupport) error {
	// Extract BiniBFT configuration from channel config
	ordererConfig := support.SharedConfig()
	binibftOptions, err := createBiniBFTConfig(ordererConfig)
	if err != nil {
		return errors.Wrap(err, "failed to create BiniBFT config from channel config")
	}

	// Extract shard configuration and determine node role
	c.nodeRole, c.shardID, c.primaryLeader, c.shardLeaders, c.shardNodes = extractShardConfig(binibftOptions, selfID)

	c.Logger.Infof("Initialized BiniBFT config from configtx: selfID=%d, role=%s, shardID=%d, primaryLeader=%s",
		selfID, c.nodeRole.String(), c.shardID, c.primaryLeader)
	c.Logger.Infof("Shard configuration: leaders=%v, nodes=%v", c.shardLeaders, c.shardNodes)

	return nil
}

// buildBiniBFTConsensus creates the BiniBFT consensus instance
func (c *BFTChain) buildBiniBFTConsensus() *BiniBFTConsensus {
	channelDecorator := zap.String("channel", c.support.ChannelID())
	logger := flogging.MustGetLogger("orderer.consensus.binibft.consensus").With(channelDecorator)

	c.assembler = &Assembler{
		RuntimeConfig:   c.RuntimeConfig,
		VerificationSeq: c.verifier.VerificationSequence,
		Logger:          flogging.MustGetLogger("orderer.consensus.binibft.assembler").With(channelDecorator),
	}

	// Create RPC adapter for cluster communication
	rpcAdapter := NewRPCAdapter(c.Channel, c.Comm, logger)

	// Create the proper consensus-level configuration
	// consensusConfig := c.createConsensusConfig(logger)

	consensus := &BiniBFTConsensus{
		Config:   c.Config, // Keep Fabric config for compatibility
		Logger:   logger,
		Verifier: c.verifier,
		Signer: &Signer{
			ID:               c.Config.SelfID,
			Logger:           flogging.MustGetLogger("orderer.consensus.binibft.signer").With(channelDecorator),
			SignerSerializer: c.SignerSerializer,
		},
		Application: c,
		Assembler:   c.assembler,
		Comm: &EgressComm{
			Channel:       c.Channel,
			RPC:           rpcAdapter,
			Logger:        logger,
			RuntimeConfig: c.RuntimeConfig,
		},

		// BiniBFT specific fields
		nodeRole:      c.nodeRole,
		shardID:       c.shardID,
		primaryLeader: c.primaryLeader,
		shardLeaders:  c.shardLeaders,
		shardNodes:    c.shardNodes,

		// Initialize consensus state
		currentView: 0,
		currentSeq:  1,
		isActive:    false,
		stopCh:      make(chan struct{}),

		// Store the consensus config for proper configuration
		// consensusConfig: consensusConfig,
	}

	return consensus
}

// Order accepts a message which has been processed at a given configSeq
func (c *BFTChain) Order(env *cb.Envelope, configSeq uint64) error {
	seq := c.support.Sequence()
	if configSeq < seq {
		c.Logger.Warnf("Normal message was validated against %d, although current config seq has advanced (%d)", configSeq, seq)
		if _, err := c.support.ProcessNormalMsg(env); err != nil {
			return errors.Errorf("bad normal message: %s", err)
		}
	}

	return c.submit(env)
}

// Configure accepts a message which reconfigures the channel
func (c *BFTChain) Configure(config *cb.Envelope, configSeq uint64) error {
	if err := c.verifier.ConfigValidator.ValidateConfig(config); err != nil {
		return err
	}
	seq := c.support.Sequence()
	if configSeq < seq {
		c.Logger.Warnf("Normal message was validated against %d, although current config seq has advanced (%d)", configSeq, seq)
		if configEnv, _, err := c.support.ProcessConfigMsg(config); err != nil {
			return errors.Errorf("bad normal message: %s", err)
		} else {
			return c.submit(configEnv)
		}
	}
	return c.submit(config)
}

// submit submits a request to the BiniBFT consensus
func (c *BFTChain) submit(env *cb.Envelope) error {
	if env == nil {
		return errors.New("failed to marshal request envelope: proto: Marshal called with nil")
	}
	reqBytes, err := proto.Marshal(env)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal request envelope")
	}

	c.Logger.Debugf("BiniBFT.SubmitRequest, node id %d", c.Config.SelfID)
	if err = c.consensus.SubmitRequest(reqBytes); err != nil {
		return errors.Wrapf(err, "failed to submit request")
	}
	return nil
}

// Deliver delivers proposal, writes block with transactions and metadata
func (c *BFTChain) Deliver(proposal BiniBFTProposal, signatures []BiniBFTSignature) BiniBFTReconfig {
	block, err := ProposalToBlock(proposal)
	if err != nil {
		c.Logger.Panicf("failed to read proposal, err: %s", err)
	}

	var sigs []*cb.MetadataSignature
	var ordererBlockMetadata []byte

	var signers []uint64

	for _, s := range signatures {
		sig := &Signature{}
		if err = sig.Unmarshal(s.Msg); err != nil {
			c.Logger.Errorf("Failed unmarshaling signature from %d: %v", s.ID, err)
			c.Logger.Errorf("Halting chain.")
			c.Halt()
			return BiniBFTReconfig{}
		}

		if ordererBlockMetadata == nil {
			ordererBlockMetadata = sig.OrdererBlockMetadata
		}

		sigs = append(sigs, &cb.MetadataSignature{
			Signature:        s.Value,
			IdentifierHeader: sig.IdentifierHeader,
		})

		signers = append(signers, s.ID)
	}

	block.Metadata.Metadata[cb.BlockMetadataIndex_SIGNATURES] = protoutil.MarshalOrPanic(&cb.Metadata{
		Value:      ordererBlockMetadata,
		Signatures: sigs,
	})

	c.Logger.Infof("Delivering proposal, writing block %d with %d transactions to the ledger with signatures from %v, node id %d",
		block.Header.Number,
		len(block.Data.Data),
		signers,
		c.Config.SelfID)

	c.Metrics.CommittedBlockNumber.Set(float64(block.Header.Number))
	c.reportIsLeader()

	if protoutil.IsConfigBlock(block) {
		c.support.WriteConfigBlock(block, nil)
	} else {
		c.support.WriteBlock(block, nil)
	}

	reconfig := c.updateRuntimeConfig(block)
	if reconfig.InLatestDecision {
		c.Logger.Infof("Reconfiguration was done and the current nodes are: %v", reconfig.CurrentNodes)
	}
	return reconfig
}

// WaitReady blocks waiting for consenter to be ready for accepting new messages
func (c *BFTChain) WaitReady() error {
	return nil
}

// Errored returns a channel which will close when an error has occurred
func (c *BFTChain) Errored() <-chan struct{} {
	return nil
}

// Start should allocate whatever resources are needed for staying up to date with the chain
func (c *BFTChain) Start() {
	if err := c.consensus.Start(context.Background()); err != nil {
		c.Logger.Panicf("Failed to start chain, aborting: %+v", err)
	}
	c.reportIsLeader()
}

// Halt frees the resources which were allocated for this Chain
func (c *BFTChain) Halt() {
	c.Logger.Infof("Shutting down chain")
	c.consensus.Stop()
}

// HandleMessage handles the message from the sender
func (c *BFTChain) HandleMessage(sender uint64, m *BiniBFTMessage) {
	c.Logger.Debugf("Received BiniBFT message from %d, type: %d", sender, m.Type)

	// Convert Fabric BiniBFT message to binibft-poc consensus message
	consensusMsg := c.convertToConsensusMessage(*m, sender)

	// Handle the message through the BiniBFT consensus
	if c.consensus != nil && c.consensus.consensus != nil {
		if err := c.consensus.consensus.HandleMessage(binibftconsensus.NodeID(fmt.Sprintf("%d", sender)), consensusMsg); err != nil {
			c.Logger.Errorf("Failed to handle message from %d: %v", sender, err)
		}
	}
}

// convertToConsensusMessage converts Fabric BiniBFT message to consensus message
func (c *BFTChain) convertToConsensusMessage(fabricMsg BiniBFTMessage, sender uint64) binibftconsensus.Message {
	return binibftconsensus.Message{
		Type:      c.convertMessageType(fabricMsg.Type),
		From:      binibftconsensus.NodeID(fmt.Sprintf("%d", sender)),
		To:        binibftconsensus.NodeID(fabricMsg.To),
		ShardID:   binibftconsensus.ShardID(fabricMsg.ShardID),
		Timestamp: fabricMsg.Timestamp,
		Payload:   fabricMsg.Payload,
	}
}

// convertMessageType converts Fabric MessageType to consensus MessageType
func (c *BFTChain) convertMessageType(fabricType MessageType) binibftconsensus.MessageType {
	switch fabricType {
	case MsgRequest:
		return binibftconsensus.MsgRequest
	case MsgPrePrep:
		return binibftconsensus.MsgPrePrep
	case MsgPreparePhase:
		return binibftconsensus.MsgPreparePhase
	case MsgCommitRequest:
		return binibftconsensus.MsgCommitRequest
	case MsgShardAck:
		return binibftconsensus.MsgShardAck
	case MsgIntraShardVote:
		return binibftconsensus.MsgIntraShardVote
	case MsgIntraShardVoteResponse:
		return binibftconsensus.MsgIntraShardVoteResponse
	case MsgCrossShardRequest:
		return binibftconsensus.MsgCrossShardRequest
	case MsgFinalizedBlock:
		return binibftconsensus.MsgFinalizedBlock
	default:
		return binibftconsensus.MsgRequest // Default fallback
	}
}

// HandleRequest handles the request from the sender
func (c *BFTChain) HandleRequest(sender uint64, req []byte) {
	c.Logger.Debugf("HandleRequest from %d", sender)
	c.consensus.HandleRequest(NodeID(fmt.Sprintf("%d", sender)), req)
}

func (c *BFTChain) updateRuntimeConfig(block *cb.Block) BiniBFTReconfig {
	prevRTC := c.RuntimeConfig.Load().(RuntimeConfig)
	newRTC, err := prevRTC.BlockCommitted(block, c.bccsp)
	if err != nil {
		c.Logger.Errorf("Failed constructing RuntimeConfig from block %d, halting chain", block.Header.Number)
		c.Halt()
		return BiniBFTReconfig{}
	}
	c.RuntimeConfig.Store(newRTC)
	if protoutil.IsConfigBlock(block) {
		c.Comm.Configure(c.Channel, newRTC.RemoteNodes)
		c.ClusterService.ConfigureNodeCerts(c.Channel, newRTC.consenters)
	}

	// For now, assume no reconfiguration
	return BiniBFTReconfig{
		InLatestDecision: false,
		CurrentNodes:     newRTC.Nodes,
		CurrentConfig:    newRTC.BFTConfig,
	}
}

func (c *BFTChain) reportIsLeader() {
	leaderID := c.consensus.GetLeaderID()
	c.Metrics.LeaderID.Set(float64(leaderID))

	if leaderID == c.Config.SelfID {
		c.Metrics.IsLeader.Set(1)
	} else {
		c.Metrics.IsLeader.Set(0)
	}
}

// StatusReport returns the ConsensusRelation & Status
func (c *BFTChain) StatusReport() (types2.ConsensusRelation, types2.Status) {
	c.statusReportMutex.Lock()
	defer c.statusReportMutex.Unlock()

	return c.consensusRelation, c.status
}

func buildVerifier(
	cv ConfigBlockValidator,
	runtimeConfig *atomic.Value,
	support consensus.ConsenterSupport,
	policyManager policies.Manager,
) *Verifier {
	channelDecorator := zap.String("channel", support.ChannelID())
	logger := flogging.MustGetLogger("orderer.consensus.binibft.verifier").With(channelDecorator)
	return &Verifier{
		Channel:               support.ChannelID(),
		ConfigValidator:       cv,
		VerificationSequencer: support,
		Logger:                logger,
		RuntimeConfig:         runtimeConfig,
		ConsenterVerifier: &consenterVerifier{
			logger:        logger,
			channel:       support.ChannelID(),
			policyManager: policyManager,
		},
		AccessController: &chainACL{
			policyManager: policyManager,
			Logger:        logger,
		},
		Ledger: support,
	}
}

type chainACL struct {
	policyManager policies.Manager
	Logger        *flogging.FabricLogger
}

// Evaluate evaluates signed data
func (c *chainACL) Evaluate(signatureSet []*protoutil.SignedData) error {
	policy, ok := c.policyManager.GetPolicy(policies.ChannelWriters)
	if !ok {
		return fmt.Errorf("could not find policy %s", policies.ChannelWriters)
	}

	err := policy.EvaluateSignedData(signatureSet)
	if err != nil {
		c.Logger.Debugf("SigFilter evaluation failed: %s, policyName: %s", err.Error(), policies.ChannelWriters)
		return errors.Wrap(errors.WithStack(msgprocessor.ErrPermissionDenied), err.Error())
	}
	return nil
}

// Helper types and functions

// BiniBFTSignature represents a signature in BiniBFT
type BiniBFTSignature struct {
	ID    uint64
	Value []byte
	Msg   []byte
}

// BiniBFTReconfig represents reconfiguration information
type BiniBFTReconfig struct {
	InLatestDecision bool
	CurrentNodes     []uint64
	CurrentConfig    interface{}
}

// BiniBFTRequest represents a request in BiniBFT
type BiniBFTRequest struct {
	ID        string
	Data      []byte
	ClientID  string
	Timestamp time.Time
}

// ProposalToBlock converts a BiniBFT proposal to a Fabric block
func ProposalToBlock(proposal BiniBFTProposal) (*cb.Block, error) {
	// Simple implementation - in production this would properly decode the proposal
	block := &cb.Block{
		Header: &cb.BlockHeader{
			Number:       0, // Would be set properly
			PreviousHash: nil,
			DataHash:     nil,
		},
		Data: &cb.BlockData{
			Data: [][]byte{proposal.Payload},
		},
		Metadata: &cb.BlockMetadata{
			Metadata: make([][]byte, 4),
		},
	}
	return block, nil
}

// LastBlockFromLedgerOrPanic gets the last block from ledger or panics
func LastBlockFromLedgerOrPanic(support consensus.ConsenterSupport, logger *flogging.FabricLogger) *cb.Block {
	lastBlockNumber := support.Height() - 1
	block := support.Block(lastBlockNumber)
	if block == nil {
		logger.Panicf("Failed to retrieve block %d from ledger", lastBlockNumber)
	}
	return block
}

// LastConfigBlockFromLedgerOrPanic gets the last config block from ledger or panics
func LastConfigBlockFromLedgerOrPanic(support consensus.ConsenterSupport, logger *flogging.FabricLogger) *cb.Block {
	lastBlockNumber := support.Height() - 1
	block := support.Block(lastBlockNumber)
	if block == nil {
		logger.Panicf("Failed to retrieve block %d from ledger", lastBlockNumber)
	}

	// Find the last config block
	for block != nil && !protoutil.IsConfigBlock(block) {
		if block.Header.Number == 0 {
			break
		}
		block = support.Block(block.Header.Number - 1)
	}

	if block == nil {
		logger.Panicf("Failed to retrieve config block from ledger")
	}
	return block
}

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"context"
	"fmt"
	"sync"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	binibftconsensus "github.com/hyperledger/fabric/orderer/consensus/binibft/consensus"
	"github.com/hyperledger/fabric/orderer/consensus/binibft/consensus/protos"
)

// BiniBFTConsensus implements the BiniBFT consensus protocol
type BiniBFTConsensus struct {
	Config   *Configuration
	Logger   *flogging.FabricLogger
	Verifier *Verifier
	Signer   *Signer

	Application ApplicationDelivery
	Assembler   *Assembler
	Comm        *EgressComm

	// BiniBFT consensus instance from binibft-poc
	consensus *binibftconsensus.Consensus

	// BiniBFT specific fields
	nodeRole      NodeRole
	shardID       ShardID
	primaryLeader NodeID
	shardLeaders  map[ShardID]NodeID
	shardNodes    map[ShardID][]NodeID

	// State management
	mu          sync.RWMutex
	isActive    bool
	currentView uint64
	currentSeq  uint64

	// Channels for internal communication
	stopCh chan struct{}

	// Performance metrics
	metrics *ConsensusMetrics
}

// ApplicationDelivery interface for delivering finalized proposals to the application
type ApplicationDelivery interface {
	Deliver(proposal BiniBFTProposal, signatures []BiniBFTSignature) BiniBFTReconfig
}

// Start begins the consensus protocol
func (c *BiniBFTConsensus) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isActive {
		return fmt.Errorf("consensus already active")
	}

	c.isActive = true

	// Initialize BiniBFT consensus from binibft-poc
	consensusConfig := c.createBiniBFTConfig()
	c.consensus = binibftconsensus.NewConsensus(consensusConfig)

	// Start the consensus
	if err := c.consensus.Start(ctx); err != nil {
		return fmt.Errorf("failed to start BiniBFT consensus: %w", err)
	}

	c.Logger.Info("BiniBFT consensus started",
		"nodeID", c.Config.SelfID,
		"role", c.nodeRole.String(),
		"shardID", c.shardID)

	return nil
}

// Stop stops the consensus protocol
func (c *BiniBFTConsensus) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isActive {
		return nil
	}

	c.isActive = false
	close(c.stopCh)

	// Stop the BiniBFT consensus
	if c.consensus != nil {
		if err := c.consensus.Stop(); err != nil {
			c.Logger.Error("Failed to stop BiniBFT consensus", "error", err)
		}
	}

	c.Logger.Info("BiniBFT consensus stopped")
	return nil
}

// SubmitRequest submits a new client request for consensus
func (c *BiniBFTConsensus) SubmitRequest(request []byte) error {
	if !c.isActive {
		c.Logger.Error("Consensus not active - cannot submit request")
		return fmt.Errorf("consensus not active")
	}

	c.Logger.Info("Submitting request to BiniBFT consensus engine")

	// Submit to BiniBFT consensus
	if c.consensus != nil {
		err := c.consensus.SubmitRequest(request)
		if err != nil {
			c.Logger.Error("Failed to submit request to consensus engine", "error", err)
		}
		c.Logger.Info("Successfully submitted request to consensus engine")

		return err
	}

	c.Logger.Error("Consensus engine not initialized")
	return fmt.Errorf("consensus not initialized")
}

// HandleMessage handles incoming consensus messages
func (c *BiniBFTConsensus) HandleMessage(from NodeID, message BiniBFTMessage) error {
	c.Logger.Debug("Handling message", "from", from, "type", message.Type)

	// Convert to consensus message and delegate to the consensus instance
	if c.consensus != nil {
		consensusMsg := binibftconsensus.Message{
			Type:      binibftconsensus.MessageType(message.Type),
			From:      binibftconsensus.NodeID(from),
			To:        binibftconsensus.NodeID(message.To),
			ShardID:   binibftconsensus.ShardID(message.ShardID),
			Timestamp: message.Timestamp,
			Payload:   message.Payload,
		}

		return c.consensus.HandleMessage(binibftconsensus.NodeID(from), consensusMsg)
	}

	return fmt.Errorf("consensus not initialized")
}

// HandleRequest handles incoming client requests
func (c *BiniBFTConsensus) HandleRequest(from NodeID, req []byte) error {
	c.Logger.Debug("Handling request from", "from", from)
	return c.SubmitRequest(req)
}

// GetLeaderID returns the current leader ID
func (c *BiniBFTConsensus) GetLeaderID() uint64 {
	return c.Config.SelfID // For now, assume self is leader
}

// GetStatus returns the current consensus status
func (c *BiniBFTConsensus) GetStatus() BiniBFTStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var currentView uint64
	var isPrimary bool

	if c.consensus != nil {
		status := c.consensus.GetStatus()
		currentView = status.CurrentView
		isPrimary = status.IsPrimary
	}

	return BiniBFTStatus{
		Role:        c.nodeRole,
		ShardID:     c.shardID,
		IsActive:    c.isActive,
		CurrentView: currentView,
		IsPrimary:   isPrimary,
	}
}

// createBiniBFTConfig creates configuration for the BiniBFT consensus
func (c *BiniBFTConsensus) createBiniBFTConfig() *binibftconsensus.Config {
	return &binibftconsensus.Config{
		NodeID:                  binibftconsensus.NodeID(fmt.Sprintf("%d", c.Config.SelfID)),
		RequestBatchMaxCount:    c.Config.RequestBatchMaxCount,
		RequestBatchMaxBytes:    c.Config.RequestBatchMaxBytes,
		RequestBatchMaxInterval: c.Config.RequestBatchMaxInterval,
		ShardID:                 binibftconsensus.ShardID(c.shardID),
		Role:                    c.convertNodeRole(c.nodeRole),
		ShardNodes:              c.convertShardNodes(c.shardNodes),
		PrimaryLeader:           binibftconsensus.NodeID(c.primaryLeader),
		ShardLeaders:            c.convertShardLeaders(c.shardLeaders),
		MajorityRequired:        2,
		Timeout:                 c.Config.ViewChangeTimeout,
		ViewChangeTimeout:       c.Config.ViewChangeTimeout,
		Network:                 &FabricNetworkAdapter{comm: c.Comm, logger: c.Logger},
		Logger:                  &FabricLoggerAdapter{logger: c.Logger},
		MaxFaultyNodes:          1,
		ShardMajorityThreshold:  0.67,
		CrossShardThreshold:     0.67,
		BatchSize:               int(c.Config.RequestBatchMaxCount),
		MaxBatchDelay:           c.Config.RequestBatchMaxInterval,
		Application:             &FabricApplicationAdapter{app: c.Application, logger: c.Logger},
		Assembler:               &FabricAssemblerAdapter{assembler: c.Assembler},
		Signer:                  &FabricSignerAdapter{signer: c.Signer},
		RequestInspector:        &FabricRequestInspectorAdapter{},
		Metadata: &protos.ViewMetadata{
			ViewId:         0,
			LatestSequence: 0,
		},
	}
}

// convertNodeRole converts Fabric NodeRole to BiniBFT NodeRole
func (c *BiniBFTConsensus) convertNodeRole(role NodeRole) binibftconsensus.NodeRole {
	switch role {
	case RolePrimaryLeader:
		return binibftconsensus.RolePrimaryLeader
	case RoleShardLeader:
		return binibftconsensus.RoleShardLeader
	case RoleShardFollower:
		return binibftconsensus.RoleShardFollower
	default:
		return binibftconsensus.RoleShardFollower
	}
}

// convertShardNodes converts Fabric shard nodes to BiniBFT format
func (c *BiniBFTConsensus) convertShardNodes(shardNodes map[ShardID][]NodeID) map[binibftconsensus.ShardID][]binibftconsensus.NodeID {
	result := make(map[binibftconsensus.ShardID][]binibftconsensus.NodeID)
	for shardID, nodes := range shardNodes {
		var convertedNodes []binibftconsensus.NodeID
		for _, node := range nodes {
			convertedNodes = append(convertedNodes, binibftconsensus.NodeID(node))
		}
		result[binibftconsensus.ShardID(shardID)] = convertedNodes
	}
	return result
}

// convertShardLeaders converts Fabric shard leaders to BiniBFT format
func (c *BiniBFTConsensus) convertShardLeaders(shardLeaders map[ShardID]NodeID) map[binibftconsensus.ShardID]binibftconsensus.NodeID {
	result := make(map[binibftconsensus.ShardID]binibftconsensus.NodeID)
	for shardID, leader := range shardLeaders {
		result[binibftconsensus.ShardID(shardID)] = binibftconsensus.NodeID(leader)
	}
	return result
}

// BiniBFTStatus represents the current status of BiniBFT consensus
type BiniBFTStatus struct {
	Role        NodeRole
	ShardID     ShardID
	IsActive    bool
	CurrentView uint64
	IsPrimary   bool
}

// ConsensusMetrics represents consensus performance metrics
type ConsensusMetrics struct {
	ProposalsReceived  uint64
	ProposalsCommitted uint64
	ViewChanges        uint64
}

package fabric

import (
	"binibft-poc/consensus"
	"fmt"
)

// ConsenterOptions configuration for creating a BiniBFTConsenter
type ConsenterOptions struct {
	NodeID        consensus.NodeID
	ShardID       consensus.ShardID
	Role          consensus.NodeRole
	PrimaryLeader consensus.NodeID
	ShardLeaders  map[consensus.ShardID]consensus.NodeID
	ShardNodes    map[consensus.ShardID][]consensus.NodeID
	Logger        consensus.Logger
	Network       consensus.NetworkInterface
	WALDir        string
	BlocksDir     string
}

// BiniBFTConsenter implements the Hyperledger Fabric consensus.Consenter interface
type BiniBFTConsenter struct {
	opts ConsenterOptions
}

// NewConsenter creates a new BiniBFTConsenter
func NewConsenter(opts ConsenterOptions) *BiniBFTConsenter {
	return &BiniBFTConsenter{
		opts: opts,
	}
}

// HandleChain creates and initializes a consensus.Chain instance for a given Fabric channel
func (c *BiniBFTConsenter) HandleChain(support ConsenterSupport, metadata *Metadata) (Chain, error) {
	if support == nil {
		return nil, fmt.Errorf("nil ConsenterSupport provided")
	}

	channelID := support.ChannelID()
	if channelID == "" {
		return nil, fmt.Errorf("empty channel ID in ConsenterSupport")
	}

	nodeID := c.opts.NodeID
	shardID := c.opts.ShardID
	role := c.opts.Role
	primaryLeader := c.opts.PrimaryLeader
	shardLeaders := c.opts.ShardLeaders
	shardNodes := c.opts.ShardNodes

	// If channel metadata specifies BiniBFT config, load and apply it
	if metadata != nil && len(metadata.Value) > 0 {
		cfg, err := UnmarshalConfig(metadata.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BiniBFT channel metadata: %w", err)
		}
		primaryLeader = consensus.NodeID(cfg.PrimaryLeader)
		if len(cfg.Shards) > 0 {
			shardLeaders = make(map[consensus.ShardID]consensus.NodeID)
			shardNodes = make(map[consensus.ShardID][]consensus.NodeID)
			for _, sh := range cfg.Shards {
				sid := consensus.ShardID(sh.ShardID)
				shardLeaders[sid] = consensus.NodeID(sh.Leader)
				nodes := []consensus.NodeID{consensus.NodeID(sh.Leader)}
				for _, f := range sh.Followers {
					nodes = append(nodes, consensus.NodeID(f))
				}
				shardNodes[sid] = nodes
			}
		}
	}

	signer := NewFabricSignerAdapter(nodeID, support)
	verifier := NewFabricVerifierAdapter(support)

	builder := consensus.NewConsensusBuilder()
	builder.WithChannelID(channelID)
	builder.WithNode(nodeID, shardID, role)
	builder.WithPrimaryLeader(primaryLeader)
	builder.WithSigner(signer)
	builder.WithVerifier(verifier)
	builder.WithRequestInspector(verifier)

	if c.opts.Network != nil {
		builder.WithNetwork(c.opts.Network)
	}
	if c.opts.Logger != nil {
		builder.WithLogger(c.opts.Logger)
	}

	for sid, leader := range shardLeaders {
		followers := shardNodes[sid]
		builder.WithShard(sid, leader, followers)
	}

	biniConsensus, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build BiniBFT consensus engine for channel %s: %w", channelID, err)
	}

	chain := NewBiniBFTChain(
		support,
		biniConsensus,
		nodeID,
		shardID,
		role,
		c.opts.Logger,
	)

	return chain, nil
}

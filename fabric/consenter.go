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

	signer := NewFabricSignerAdapter(c.opts.NodeID, support)
	verifier := NewFabricVerifierAdapter(support)

	builder := consensus.NewConsensusBuilder()
	builder.WithChannelID(channelID)
	builder.WithNode(c.opts.NodeID, c.opts.ShardID, c.opts.Role)
	builder.WithPrimaryLeader(c.opts.PrimaryLeader)
	builder.WithSigner(signer)
	builder.WithVerifier(verifier)
	builder.WithRequestInspector(verifier)

	if c.opts.Network != nil {
		builder.WithNetwork(c.opts.Network)
	}
	if c.opts.Logger != nil {
		builder.WithLogger(c.opts.Logger)
	}

	for shardID, leader := range c.opts.ShardLeaders {
		followers := c.opts.ShardNodes[shardID]
		builder.WithShard(shardID, leader, followers)
	}

	biniConsensus, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build BiniBFT consensus engine for channel %s: %w", channelID, err)
	}

	chain := NewBiniBFTChain(
		support,
		biniConsensus,
		c.opts.NodeID,
		c.opts.ShardID,
		c.opts.Role,
		c.opts.Logger,
	)

	return chain, nil
}

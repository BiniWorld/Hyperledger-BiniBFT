package fabric

import (
	"encoding/asn1"
	"encoding/json"
	"fmt"
	"time"
)

// BatchSizeConfig defines batch sizing parameters for BiniBFT within Fabric
type BatchSizeConfig struct {
	MaxMessageCount   uint32 `json:"max_message_count" yaml:"MaxMessageCount"`
	AbsoluteMaxBytes  uint32 `json:"absolute_max_bytes" yaml:"AbsoluteMaxBytes"`
	PreferredMaxBytes uint32 `json:"preferred_max_bytes" yaml:"PreferredMaxBytes"`
}

// ShardConfig defines the shard membership and leader designation
type ShardConfig struct {
	ShardID   int      `json:"shard_id" yaml:"ShardID"`
	Leader    string   `json:"leader" yaml:"Leader"`
	Followers []string `json:"followers" yaml:"Followers"`
}

// ConsenterNodeConfig defines a consenter node identity and TLS bindings
type ConsenterNodeConfig struct {
	ID            string `json:"id" yaml:"ID"`
	Host          string `json:"host" yaml:"Host"`
	Port          int    `json:"port" yaml:"Port"`
	MSPID         string `json:"msp_id" yaml:"MSPID"`
	Identity      []byte `json:"identity" yaml:"Identity"`
	ClientTLSCert []byte `json:"client_tls_cert" yaml:"ClientTLSCert"`
	ServerTLSCert []byte `json:"server_tls_cert" yaml:"ServerTLSCert"`
}

// BiniBFTConfig defines the complete consensus configuration schema for BiniBFT in Fabric
type BiniBFTConfig struct {
	NumShards     int                   `json:"num_shards" yaml:"NumShards"`
	BatchTimeout  time.Duration         `json:"batch_timeout" yaml:"BatchTimeout"`
	BatchSize     BatchSizeConfig       `json:"batch_size" yaml:"BatchSize"`
	PrimaryLeader string                `json:"primary_leader" yaml:"PrimaryLeader"`
	Shards        []ShardConfig         `json:"shards" yaml:"Shards"`
	Consenters    []ConsenterNodeConfig `json:"consenters" yaml:"Consenters"`
}

// Validate checks that the BiniBFT configuration satisfies consensus invariants
func (c *BiniBFTConfig) Validate() error {
	if c.NumShards < 1 {
		return fmt.Errorf("num_shards must be at least 1, got %d", c.NumShards)
	}

	if len(c.Consenters) == 0 {
		return fmt.Errorf("consenters list cannot be empty")
	}

	consenterMap := make(map[string]bool)
	for _, node := range c.Consenters {
		if node.ID == "" {
			return fmt.Errorf("consenter ID cannot be empty")
		}
		if consenterMap[node.ID] {
			return fmt.Errorf("duplicate consenter ID: %s", node.ID)
		}
		consenterMap[node.ID] = true
	}

	if c.PrimaryLeader == "" {
		return fmt.Errorf("primary_leader must be specified")
	}
	if !consenterMap[c.PrimaryLeader] {
		return fmt.Errorf("primary_leader %s not found in consenters list", c.PrimaryLeader)
	}

	if len(c.Shards) > 0 {
		if len(c.Shards) != c.NumShards {
			return fmt.Errorf("shards count mismatch: NumShards=%d but %d shards defined", c.NumShards, len(c.Shards))
		}

		assignedNodes := make(map[string]bool)
		for _, shard := range c.Shards {
			if shard.Leader == "" {
				return fmt.Errorf("shard %d must have a designated leader", shard.ShardID)
			}
			if !consenterMap[shard.Leader] {
				return fmt.Errorf("shard %d leader %s not in consenters list", shard.ShardID, shard.Leader)
			}
			if assignedNodes[shard.Leader] {
				return fmt.Errorf("node %s assigned to multiple shards", shard.Leader)
			}
			assignedNodes[shard.Leader] = true

			for _, f := range shard.Followers {
				if !consenterMap[f] {
					return fmt.Errorf("shard %d follower %s not in consenters list", shard.ShardID, f)
				}
				if assignedNodes[f] {
					return fmt.Errorf("node %s assigned to multiple shards", f)
				}
				assignedNodes[f] = true
			}
		}
	}

	return nil
}

// MarshalConfig serializes the BiniBFT configuration to JSON bytes for channel metadata storage
func MarshalConfig(cfg *BiniBFTConfig) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cannot marshal nil config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return json.Marshal(cfg)
}

// UnmarshalConfig parses BiniBFT configuration from raw metadata bytes
func UnmarshalConfig(data []byte) (*BiniBFTConfig, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty config data")
	}

	var cfg BiniBFTConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Fall back to ASN.1 decoding if applicable
		if _, asnErr := asn1.Unmarshal(data, &cfg); asnErr != nil {
			return nil, fmt.Errorf("failed to unmarshal BiniBFT config: %w", err)
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid BiniBFT config: %w", err)
	}

	return &cfg, nil
}

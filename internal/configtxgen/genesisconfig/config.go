/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package genesisconfig

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/hyperledger-labs/SmartBFT/pkg/types"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer/etcdraft"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer/smartbft"
	"github.com/hyperledger/fabric/common/viperutil"
	cf "github.com/hyperledger/fabric/core/config"
	"github.com/hyperledger/fabric/msp"
)

const (
	// EtcdRaft The type key for etcd based RAFT consensus.
	EtcdRaft = "etcdraft"
	BFT      = "BFT"
	BiniBFT  = "binibft"
)

var logger = flogging.MustGetLogger("common.tools.configtxgen.localconfig")

const (
	// SampleInsecureSoloProfile references the sample profile which does not
	// include any MSPs and uses solo for ordering.
	SampleInsecureSoloProfile = "SampleInsecureSolo"
	// SampleDevModeSoloProfile references the sample profile which requires
	// only basic membership for admin privileges and uses solo for ordering.
	SampleDevModeSoloProfile = "SampleDevModeSolo"
	// SampleSingleMSPSoloProfile references the sample profile which includes
	// only the sample MSP and uses solo for ordering.
	SampleSingleMSPSoloProfile = "SampleSingleMSPSolo"

	// SampleDevModeEtcdRaftProfile references the sample profile used for testing
	// the etcd/raft-based ordering service.
	SampleDevModeEtcdRaftProfile = "SampleDevModeEtcdRaft"

	// SampleAppChannelInsecureSoloProfile references the sample profile which
	// does not include any MSPs and uses solo for ordering.
	SampleAppChannelInsecureSoloProfile = "SampleAppChannelInsecureSolo"

	// SampleAppChannelEtcdRaftProfile references the sample profile used for
	// testing the etcd/raft-based ordering service using the channel
	// participation API.
	SampleAppChannelEtcdRaftProfile = "SampleAppChannelEtcdRaft"

	// SampleAppChannelSmartBftProfile references the sample profile used for
	// testing the smartbft-based ordering service using the channel
	// participation API.
	SampleAppChannelSmartBftProfile = "SampleAppChannelSmartBft"

	// SampleSingleMSPChannelProfile references the sample profile which
	// includes only the sample MSP and is used to create a channel
	SampleSingleMSPChannelProfile = "SampleSingleMSPChannel"

	// SampleConsortiumName is the sample consortium from the
	// sample configtx.yaml
	SampleConsortiumName = "SampleConsortium"
	// SampleOrgName is the name of the sample org in the sample profiles
	SampleOrgName = "SampleOrg"

	// AdminRoleAdminPrincipal is set as AdminRole to cause the MSP role of
	// type Admin to be used as the admin principal default
	AdminRoleAdminPrincipal = "Role.ADMIN"
)

// TopLevel consists of the structs used by the configtxgen tool.
type TopLevel struct {
	Profiles      map[string]*Profile        `yaml:"Profiles"`
	Organizations []*Organization            `yaml:"Organizations"`
	Channel       *Profile                   `yaml:"Channel"`
	Application   *Application               `yaml:"Application"`
	Orderer       *Orderer                   `yaml:"Orderer"`
	Capabilities  map[string]map[string]bool `yaml:"Capabilities"`
}

// Profile encodes orderer/application configuration combinations for the
// configtxgen tool.
type Profile struct {
	Consortium   string                 `yaml:"Consortium"`
	Application  *Application           `yaml:"Application"`
	Orderer      *Orderer               `yaml:"Orderer"`
	Consortiums  map[string]*Consortium `yaml:"Consortiums"`
	Capabilities map[string]bool        `yaml:"Capabilities"`
	Policies     map[string]*Policy     `yaml:"Policies"`
}

// Policy encodes a channel config policy
type Policy struct {
	Type string `yaml:"Type"`
	Rule string `yaml:"Rule"`
}

// Consortium represents a group of organizations which may create channels
// with each other
type Consortium struct {
	Organizations []*Organization `yaml:"Organizations"`
}

// Application encodes the application-level configuration needed in config
// transactions.
type Application struct {
	Organizations []*Organization    `yaml:"Organizations"`
	Capabilities  map[string]bool    `yaml:"Capabilities"`
	Policies      map[string]*Policy `yaml:"Policies"`
	ACLs          map[string]string  `yaml:"ACLs"`
}

// Organization encodes the organization-level configuration needed in
// config transactions.
type Organization struct {
	Name     string             `yaml:"Name"`
	ID       string             `yaml:"ID"`
	MSPDir   string             `yaml:"MSPDir"`
	MSPType  string             `yaml:"MSPType"`
	Policies map[string]*Policy `yaml:"Policies"`

	// Note: Viper deserialization does not seem to care for
	// embedding of types, so we use one organization struct
	// for both orderers and applications.
	AnchorPeers      []*AnchorPeer `yaml:"AnchorPeers"`
	OrdererEndpoints []string      `yaml:"OrdererEndpoints"`

	// AdminPrincipal is deprecated and may be removed in a future release
	// it was used for modifying the default policy generation, but policies
	// may now be specified explicitly so it is redundant and unnecessary
	AdminPrincipal string `yaml:"AdminPrincipal"`

	// SkipAsForeign indicates that this org definition is actually unknown to this
	// instance of the tool, so, parsing of this org's parameters should be ignored.
	SkipAsForeign bool
}

// AnchorPeer encodes the necessary fields to identify an anchor peer.
type AnchorPeer struct {
	Host string `yaml:"Host"`
	Port int    `yaml:"Port"`
}

// Orderer contains configuration associated to a channel.
type Orderer struct {
	OrdererType      string                   `yaml:"OrdererType"`
	Addresses        []string                 `yaml:"Addresses"`
	BatchTimeout     time.Duration            `yaml:"BatchTimeout"`
	BatchSize        BatchSize                `yaml:"BatchSize"`
	ConsenterMapping []*Consenter             `yaml:"ConsenterMapping"`
	EtcdRaft         *etcdraft.ConfigMetadata `yaml:"EtcdRaft"`
	SmartBFT         *smartbft.Options        `yaml:"SmartBFT"`
	BiniBFT          *BiniBFTOptions          `yaml:"BiniBFT"`
	Organizations    []*Organization          `yaml:"Organizations"`
	MaxChannels      uint64                   `yaml:"MaxChannels"`
	Capabilities     map[string]bool          `yaml:"Capabilities"`
	Policies         map[string]*Policy       `yaml:"Policies"`
}

// BatchSize contains configuration affecting the size of batches.
type BatchSize struct {
	MaxMessageCount   uint32 `yaml:"MaxMessageCount"`
	AbsoluteMaxBytes  uint32 `yaml:"AbsoluteMaxBytes"`
	PreferredMaxBytes uint32 `yaml:"PreferredMaxBytes"`
}

type Consenter struct {
	ID            uint32 `yaml:"ID"`
	Host          string `yaml:"Host"`
	Port          uint32 `yaml:"Port"`
	MSPID         string `yaml:"MSPID"`
	Identity      string `yaml:"Identity"`
	ClientTLSCert string `yaml:"ClientTLSCert"`
	ServerTLSCert string `yaml:"ServerTLSCert"`
}

// Shard represents a shard configuration in BiniBFT
type Shard struct {
	ShardID   uint32   `yaml:"ShardID"`
	Leader    uint32   `yaml:"Leader"`
	Followers []uint32 `yaml:"Followers"`
	Nodes     []uint32 `yaml:"Nodes"`
}

// BiniBFTOptions represents configuration options for BiniBFT consensus
type BiniBFTOptions struct {
	RequestBatchMaxCount      uint64        `yaml:"RequestBatchMaxCount"`
	RequestBatchMaxBytes      uint64        `yaml:"RequestBatchMaxBytes"`
	RequestBatchMaxInterval   time.Duration `yaml:"RequestBatchMaxInterval"`
	IncomingMessageBufferSize uint64        `yaml:"IncomingMessageBufferSize"`
	RequestPoolSize           uint64        `yaml:"RequestPoolSize"`
	RequestForwardTimeout     time.Duration `yaml:"RequestForwardTimeout"`
	RequestComplainTimeout    time.Duration `yaml:"RequestComplainTimeout"`
	RequestAutoRemoveTimeout  time.Duration `yaml:"RequestAutoRemoveTimeout"`
	ViewChangeResendInterval  time.Duration `yaml:"ViewChangeResendInterval"`
	ViewChangeTimeout         time.Duration `yaml:"ViewChangeTimeout"`
	LeaderHeartbeatTimeout    time.Duration `yaml:"LeaderHeartbeatTimeout"`
	LeaderHeartbeatCount      uint64        `yaml:"LeaderHeartbeatCount"`
	CollectTimeout            time.Duration `yaml:"CollectTimeout"`
	SyncOnStart               bool          `yaml:"SyncOnStart"`
	SpeedUpViewChange         bool          `yaml:"SpeedUpViewChange"`
	LeaderRotation            bool          `yaml:"LeaderRotation"`
	DecisionsPerLeader        uint64        `yaml:"DecisionsPerLeader"`
	RequestMaxBytes           uint64        `yaml:"RequestMaxBytes"`
	RequestPoolSubmitTimeout  time.Duration `yaml:"RequestPoolSubmitTimeout"`

	// Hierarchical sharding configuration
	ShardMajorityThreshold float64       `yaml:"ShardMajorityThreshold"`
	CrossShardThreshold    float64       `yaml:"CrossShardThreshold"`
	BatchSize              uint32        `yaml:"BatchSize"`
	MaxBatchDelay          time.Duration `yaml:"MaxBatchDelay"`
	Shards                 []*Shard      `yaml:"Shards"`
	PrimaryLeader          uint32        `yaml:"PrimaryLeader"`
	MaxFaultyNodes         uint32        `yaml:"MaxFaultyNodes"`
}

// BiniBFTOptionsProto represents the protobuf version of BiniBFT options
type BiniBFTOptionsProto struct {
	RequestBatchMaxCount      uint64              `protobuf:"varint,1,opt,name=request_batch_max_count,json=requestBatchMaxCount,proto3" json:"request_batch_max_count,omitempty"`
	RequestBatchMaxBytes      uint64              `protobuf:"varint,2,opt,name=request_batch_max_bytes,json=requestBatchMaxBytes,proto3" json:"request_batch_max_bytes,omitempty"`
	RequestBatchMaxInterval   string              `protobuf:"bytes,3,opt,name=request_batch_max_interval,json=requestBatchMaxInterval,proto3" json:"request_batch_max_interval,omitempty"`
	IncomingMessageBufferSize uint64              `protobuf:"varint,4,opt,name=incoming_message_buffer_size,json=incomingMessageBufferSize,proto3" json:"incoming_message_buffer_size,omitempty"`
	RequestPoolSize           uint64              `protobuf:"varint,5,opt,name=request_pool_size,json=requestPoolSize,proto3" json:"request_pool_size,omitempty"`
	RequestForwardTimeout     string              `protobuf:"bytes,6,opt,name=request_forward_timeout,json=requestForwardTimeout,proto3" json:"request_forward_timeout,omitempty"`
	RequestComplainTimeout    string              `protobuf:"bytes,7,opt,name=request_complain_timeout,json=requestComplainTimeout,proto3" json:"request_complain_timeout,omitempty"`
	RequestAutoRemoveTimeout  string              `protobuf:"bytes,8,opt,name=request_auto_remove_timeout,json=requestAutoRemoveTimeout,proto3" json:"request_auto_remove_timeout,omitempty"`
	ViewChangeResendInterval  string              `protobuf:"bytes,9,opt,name=view_change_resend_interval,json=viewChangeResendInterval,proto3" json:"view_change_resend_interval,omitempty"`
	ViewChangeTimeout         string              `protobuf:"bytes,10,opt,name=view_change_timeout,json=viewChangeTimeout,proto3" json:"view_change_timeout,omitempty"`
	LeaderHeartbeatTimeout    string              `protobuf:"bytes,11,opt,name=leader_heartbeat_timeout,json=leaderHeartbeatTimeout,proto3" json:"leader_heartbeat_timeout,omitempty"`
	LeaderHeartbeatCount      uint64              `protobuf:"varint,12,opt,name=leader_heartbeat_count,json=leaderHeartbeatCount,proto3" json:"leader_heartbeat_count,omitempty"`
	CollectTimeout            string              `protobuf:"bytes,13,opt,name=collect_timeout,json=collectTimeout,proto3" json:"collect_timeout,omitempty"`
	RequestPoolSubmitTimeout  string              `protobuf:"bytes,14,opt,name=request_pool_submit_timeout,json=requestPoolSubmitTimeout,proto3" json:"request_pool_submit_timeout,omitempty"`
	SyncOnStart               bool                `protobuf:"varint,15,opt,name=sync_on_start,json=syncOnStart,proto3" json:"sync_on_start,omitempty"`
	SpeedUpViewChange         bool                `protobuf:"varint,16,opt,name=speed_up_view_change,json=speedUpViewChange,proto3" json:"speed_up_view_change,omitempty"`
	LeaderRotation            bool                `protobuf:"varint,17,opt,name=leader_rotation,json=leaderRotation,proto3" json:"leader_rotation,omitempty"`
	DecisionsPerLeader        uint64              `protobuf:"varint,18,opt,name=decisions_per_leader,json=decisionsPerLeader,proto3" json:"decisions_per_leader,omitempty"`
	RequestMaxBytes           uint64              `protobuf:"varint,19,opt,name=request_max_bytes,json=requestMaxBytes,proto3" json:"request_max_bytes,omitempty"`
	ShardMajorityThreshold    float64             `protobuf:"fixed64,20,opt,name=shard_majority_threshold,json=shardMajorityThreshold,proto3" json:"shard_majority_threshold,omitempty"`
	CrossShardThreshold       float64             `protobuf:"fixed64,21,opt,name=cross_shard_threshold,json=crossShardThreshold,proto3" json:"cross_shard_threshold,omitempty"`
	BatchSize                 uint64              `protobuf:"varint,22,opt,name=batch_size,json=batchSize,proto3" json:"batch_size,omitempty"`
	MaxBatchDelay             string              `protobuf:"bytes,23,opt,name=max_batch_delay,json=maxBatchDelay,proto3" json:"max_batch_delay,omitempty"`
	Shards                    []*ShardConfigProto `protobuf:"bytes,24,rep,name=shards,proto3" json:"shards,omitempty"`
	PrimaryLeader             uint64              `protobuf:"varint,25,opt,name=primary_leader,json=primaryLeader,proto3" json:"primary_leader,omitempty"`
	MaxFaultyNodes            uint64              `protobuf:"varint,26,opt,name=max_faulty_nodes,json=maxFaultyNodes,proto3" json:"max_faulty_nodes,omitempty"`
}

// ShardConfigProto represents the protobuf version of shard configuration
type ShardConfigProto struct {
	ShardId   uint64   `protobuf:"varint,1,opt,name=shard_id,json=shardId,proto3" json:"shard_id,omitempty"`
	Leader    uint64   `protobuf:"varint,2,opt,name=leader,proto3" json:"leader,omitempty"`
	Followers []uint64 `protobuf:"varint,3,rep,packed,name=followers,proto3" json:"followers,omitempty"`
	Nodes     []uint64 `protobuf:"varint,4,rep,packed,name=nodes,proto3" json:"nodes,omitempty"`
}

// Implement protobuf methods for BiniBFTOptionsProto
func (m *BiniBFTOptionsProto) Reset()         { *m = BiniBFTOptionsProto{} }
func (m *BiniBFTOptionsProto) String() string { return proto.CompactTextString(m) }
func (*BiniBFTOptionsProto) ProtoMessage()    {}

// Implement protobuf methods for ShardConfigProto
func (m *ShardConfigProto) Reset()         { *m = ShardConfigProto{} }
func (m *ShardConfigProto) String() string { return proto.CompactTextString(m) }
func (*ShardConfigProto) ProtoMessage()    {}

// MarshalBiniBFTOptions marshals BiniBFT options to protobuf bytes
func MarshalBiniBFTOptions(opts *BiniBFTOptions) ([]byte, error) {
	if opts == nil {
		return []byte{}, nil
	}

	// Convert genesisconfig BiniBFTOptions to protobuf BiniBFTOptions
	pbOpts := convertToPbBiniBFTOptions(opts)

	// Marshal to bytes
	return proto.Marshal(pbOpts)
}

// convertToPbBiniBFTOptions converts genesisconfig BiniBFTOptions to protobuf format
func convertToPbBiniBFTOptions(opts *BiniBFTOptions) *BiniBFTOptionsProto {
	if opts == nil {
		return &BiniBFTOptionsProto{}
	}

	pbOpts := &BiniBFTOptionsProto{
		RequestBatchMaxCount:      opts.RequestBatchMaxCount,
		RequestBatchMaxBytes:      opts.RequestBatchMaxBytes,
		RequestBatchMaxInterval:   opts.RequestBatchMaxInterval.String(),
		IncomingMessageBufferSize: opts.IncomingMessageBufferSize,
		RequestPoolSize:           opts.RequestPoolSize,
		RequestForwardTimeout:     opts.RequestForwardTimeout.String(),
		RequestComplainTimeout:    opts.RequestComplainTimeout.String(),
		RequestAutoRemoveTimeout:  opts.RequestAutoRemoveTimeout.String(),
		ViewChangeResendInterval:  opts.ViewChangeResendInterval.String(),
		ViewChangeTimeout:         opts.ViewChangeTimeout.String(),
		LeaderHeartbeatTimeout:    opts.LeaderHeartbeatTimeout.String(),
		LeaderHeartbeatCount:      opts.LeaderHeartbeatCount,
		CollectTimeout:            opts.CollectTimeout.String(),
		RequestPoolSubmitTimeout:  opts.RequestPoolSubmitTimeout.String(),
		SyncOnStart:               opts.SyncOnStart,
		SpeedUpViewChange:         opts.SpeedUpViewChange,
		LeaderRotation:            opts.LeaderRotation,
		DecisionsPerLeader:        opts.DecisionsPerLeader,
		RequestMaxBytes:           opts.RequestMaxBytes,
		ShardMajorityThreshold:    opts.ShardMajorityThreshold,
		CrossShardThreshold:       opts.CrossShardThreshold,
		BatchSize:                 uint64(opts.BatchSize),
		MaxBatchDelay:             opts.MaxBatchDelay.String(),
		PrimaryLeader:             uint64(opts.PrimaryLeader),
		MaxFaultyNodes:            uint64(opts.MaxFaultyNodes),
	}

	// Convert shards
	for _, shard := range opts.Shards {
		pbShard := &ShardConfigProto{
			ShardId:   uint64(shard.ShardID),
			Leader:    uint64(shard.Leader),
			Followers: make([]uint64, len(shard.Followers)),
			Nodes:     make([]uint64, len(shard.Nodes)),
		}

		for i, follower := range shard.Followers {
			pbShard.Followers[i] = uint64(follower)
		}

		for i, node := range shard.Nodes {
			pbShard.Nodes[i] = uint64(node)
		}

		pbOpts.Shards = append(pbOpts.Shards, pbShard)
	}

	return pbOpts
}

var genesisDefaults = TopLevel{
	Orderer: &Orderer{
		OrdererType:  "solo",
		BatchTimeout: 2 * time.Second,
		BatchSize: BatchSize{
			MaxMessageCount:   500,
			AbsoluteMaxBytes:  10 * 1024 * 1024,
			PreferredMaxBytes: 2 * 1024 * 1024,
		},
		EtcdRaft: &etcdraft.ConfigMetadata{
			Options: &etcdraft.Options{
				TickInterval:         "500ms",
				ElectionTick:         10,
				HeartbeatTick:        1,
				MaxInflightBlocks:    5,
				SnapshotIntervalSize: 16 * 1024 * 1024, // 16 MB
			},
		},
		SmartBFT: &smartbft.Options{
			RequestBatchMaxCount:      types.DefaultConfig.RequestBatchMaxCount,
			RequestBatchMaxBytes:      types.DefaultConfig.RequestBatchMaxBytes,
			RequestBatchMaxInterval:   types.DefaultConfig.RequestBatchMaxInterval.String(),
			IncomingMessageBufferSize: types.DefaultConfig.IncomingMessageBufferSize,
			RequestPoolSize:           types.DefaultConfig.RequestPoolSize,
			RequestForwardTimeout:     types.DefaultConfig.RequestForwardTimeout.String(),
			RequestComplainTimeout:    types.DefaultConfig.RequestComplainTimeout.String(),
			RequestAutoRemoveTimeout:  types.DefaultConfig.RequestAutoRemoveTimeout.String(),
			ViewChangeResendInterval:  types.DefaultConfig.ViewChangeResendInterval.String(),
			ViewChangeTimeout:         types.DefaultConfig.ViewChangeTimeout.String(),
			LeaderHeartbeatTimeout:    types.DefaultConfig.LeaderHeartbeatTimeout.String(),
			LeaderHeartbeatCount:      types.DefaultConfig.LeaderHeartbeatCount,
			CollectTimeout:            types.DefaultConfig.CollectTimeout.String(),
			SyncOnStart:               types.DefaultConfig.SyncOnStart,
			SpeedUpViewChange:         types.DefaultConfig.SpeedUpViewChange,
		},
		BiniBFT: &BiniBFTOptions{
			RequestBatchMaxCount:      100,
			RequestBatchMaxBytes:      10 * 1024 * 1024, // 10MB
			RequestBatchMaxInterval:   50 * time.Millisecond,
			IncomingMessageBufferSize: 200,
			RequestPoolSize:           400,
			RequestForwardTimeout:     2 * time.Second,
			RequestComplainTimeout:    20 * time.Second,
			RequestAutoRemoveTimeout:  3 * time.Minute,
			ViewChangeResendInterval:  5 * time.Second,
			ViewChangeTimeout:         20 * time.Second,
			LeaderHeartbeatTimeout:    time.Minute,
			LeaderHeartbeatCount:      10,
			CollectTimeout:            time.Second,
			SyncOnStart:               true,
			SpeedUpViewChange:         false,
			LeaderRotation:            true,
			DecisionsPerLeader:        3,
			RequestMaxBytes:           6 * 1024 * 1024, // 6MB
			RequestPoolSubmitTimeout:  10 * time.Second,
			ShardMajorityThreshold:    0.67,
			CrossShardThreshold:       0.67,
			BatchSize:                 10,
			MaxBatchDelay:             100 * time.Millisecond,
			Shards:                    []*Shard{},
			PrimaryLeader:             1,
			MaxFaultyNodes:            1,
		},
	},
}

// LoadTopLevel simply loads the configtx.yaml file into the structs above and
// completes their initialization. Config paths may optionally be provided and
// will be used in place of the FABRIC_CFG_PATH env variable.
//
// Note, for environment overrides to work properly within a profile, Load
// should be used instead.
func LoadTopLevel(configPaths ...string) *TopLevel {
	config := viperutil.New()
	config.AddConfigPaths(configPaths...)
	config.SetConfigName("configtx")

	err := config.ReadInConfig()
	if err != nil {
		logger.Panicf("Error reading configuration: %s", err)
	}
	logger.Debugf("Using config file: %s", config.ConfigFileUsed())

	uconf, err := cache.load(config, config.ConfigFileUsed())
	if err != nil {
		logger.Panicf("failed to load configCache: %s", err)
	}
	uconf.completeInitialization(filepath.Dir(config.ConfigFileUsed()))
	logger.Infof("Loaded configuration: %s", config.ConfigFileUsed())

	return uconf
}

// Load returns the orderer/application config combination that corresponds to
// a given profile. Config paths may optionally be provided and will be used
// in place of the FABRIC_CFG_PATH env variable.
func Load(profile string, configPaths ...string) *Profile {
	config := viperutil.New()
	config.AddConfigPaths(configPaths...)
	config.SetConfigName("configtx")

	err := config.ReadInConfig()
	if err != nil {
		logger.Panicf("Error reading configuration: %s", err)
	}
	logger.Debugf("Using config file: %s", config.ConfigFileUsed())

	uconf, err := cache.load(config, config.ConfigFileUsed())
	if err != nil {
		logger.Panicf("Error loading config from config cache: %s", err)
	}

	result, ok := uconf.Profiles[profile]
	if !ok {
		logger.Panicf("Could not find profile: %s", profile)
	}

	result.completeInitialization(filepath.Dir(config.ConfigFileUsed()))

	logger.Infof("Loaded configuration: %s", config.ConfigFileUsed())

	return result
}

func (t *TopLevel) completeInitialization(configDir string) {
	for _, org := range t.Organizations {
		org.completeInitialization(configDir)
	}

	if t.Orderer != nil {
		t.Orderer.completeInitialization(configDir)
	}
}

func (p *Profile) completeInitialization(configDir string) {
	if p.Application != nil {
		for _, org := range p.Application.Organizations {
			org.completeInitialization(configDir)
		}
	}

	if p.Consortiums != nil {
		for _, consortium := range p.Consortiums {
			for _, org := range consortium.Organizations {
				org.completeInitialization(configDir)
			}
		}
	}

	if p.Orderer != nil {
		for _, org := range p.Orderer.Organizations {
			org.completeInitialization(configDir)
		}
		// Some profiles will not define orderer parameters
		p.Orderer.completeInitialization(configDir)
	}
}

func (org *Organization) completeInitialization(configDir string) {
	// set the MSP type; if none is specified we assume BCCSP
	if org.MSPType == "" {
		org.MSPType = msp.ProviderTypeToString(msp.FABRIC)
	}

	if org.AdminPrincipal == "" {
		org.AdminPrincipal = AdminRoleAdminPrincipal
	}
	translatePaths(configDir, org)
}

func (ord *Orderer) completeInitialization(configDir string) {
loop:
	for {
		switch {
		case ord.OrdererType == "":
			logger.Infof("Orderer.OrdererType unset, setting to %v", genesisDefaults.Orderer.OrdererType)
			ord.OrdererType = genesisDefaults.Orderer.OrdererType
		case ord.BatchTimeout == 0:
			logger.Infof("Orderer.BatchTimeout unset, setting to %s", genesisDefaults.Orderer.BatchTimeout)
			ord.BatchTimeout = genesisDefaults.Orderer.BatchTimeout
		case ord.BatchSize.MaxMessageCount == 0:
			logger.Infof("Orderer.BatchSize.MaxMessageCount unset, setting to %v", genesisDefaults.Orderer.BatchSize.MaxMessageCount)
			ord.BatchSize.MaxMessageCount = genesisDefaults.Orderer.BatchSize.MaxMessageCount
		case ord.BatchSize.AbsoluteMaxBytes == 0:
			logger.Infof("Orderer.BatchSize.AbsoluteMaxBytes unset, setting to %v", genesisDefaults.Orderer.BatchSize.AbsoluteMaxBytes)
			ord.BatchSize.AbsoluteMaxBytes = genesisDefaults.Orderer.BatchSize.AbsoluteMaxBytes
		case ord.BatchSize.PreferredMaxBytes == 0:
			logger.Infof("Orderer.BatchSize.PreferredMaxBytes unset, setting to %v", genesisDefaults.Orderer.BatchSize.PreferredMaxBytes)
			ord.BatchSize.PreferredMaxBytes = genesisDefaults.Orderer.BatchSize.PreferredMaxBytes
		default:
			break loop
		}
	}

	logger.Infof("orderer type: %s", ord.OrdererType)
	// Additional, consensus type-dependent initialization goes here
	// Also using this to panic on unknown orderer type.
	switch ord.OrdererType {
	case "solo":
		// nothing to be done here
	case EtcdRaft:
		if ord.EtcdRaft == nil {
			logger.Panicf("%s configuration missing", EtcdRaft)
		}
		if ord.EtcdRaft.Options == nil {
			logger.Infof("Orderer.EtcdRaft.Options unset, setting to %v", genesisDefaults.Orderer.EtcdRaft.Options)
			ord.EtcdRaft.Options = genesisDefaults.Orderer.EtcdRaft.Options
		}
	second_loop:
		for {
			switch {
			case ord.EtcdRaft.Options.TickInterval == "":
				logger.Infof("Orderer.EtcdRaft.Options.TickInterval unset, setting to %v", genesisDefaults.Orderer.EtcdRaft.Options.TickInterval)
				ord.EtcdRaft.Options.TickInterval = genesisDefaults.Orderer.EtcdRaft.Options.TickInterval

			case ord.EtcdRaft.Options.ElectionTick == 0:
				logger.Infof("Orderer.EtcdRaft.Options.ElectionTick unset, setting to %v", genesisDefaults.Orderer.EtcdRaft.Options.ElectionTick)
				ord.EtcdRaft.Options.ElectionTick = genesisDefaults.Orderer.EtcdRaft.Options.ElectionTick

			case ord.EtcdRaft.Options.HeartbeatTick == 0:
				logger.Infof("Orderer.EtcdRaft.Options.HeartbeatTick unset, setting to %v", genesisDefaults.Orderer.EtcdRaft.Options.HeartbeatTick)
				ord.EtcdRaft.Options.HeartbeatTick = genesisDefaults.Orderer.EtcdRaft.Options.HeartbeatTick

			case ord.EtcdRaft.Options.MaxInflightBlocks == 0:
				logger.Infof("Orderer.EtcdRaft.Options.MaxInflightBlocks unset, setting to %v", genesisDefaults.Orderer.EtcdRaft.Options.MaxInflightBlocks)
				ord.EtcdRaft.Options.MaxInflightBlocks = genesisDefaults.Orderer.EtcdRaft.Options.MaxInflightBlocks

			case ord.EtcdRaft.Options.SnapshotIntervalSize == 0:
				logger.Infof("Orderer.EtcdRaft.Options.SnapshotIntervalSize unset, setting to %v", genesisDefaults.Orderer.EtcdRaft.Options.SnapshotIntervalSize)
				ord.EtcdRaft.Options.SnapshotIntervalSize = genesisDefaults.Orderer.EtcdRaft.Options.SnapshotIntervalSize

			case len(ord.EtcdRaft.Consenters) == 0:
				logger.Panicf("%s configuration did not specify any consenter", EtcdRaft)

			default:
				break second_loop
			}
		}

		if _, err := time.ParseDuration(ord.EtcdRaft.Options.TickInterval); err != nil {
			logger.Panicf("Etcdraft TickInterval (%s) must be in time duration format", ord.EtcdRaft.Options.TickInterval)
		}

		// validate the specified members for Options
		if ord.EtcdRaft.Options.ElectionTick <= ord.EtcdRaft.Options.HeartbeatTick {
			logger.Panicf("election tick must be greater than heartbeat tick")
		}

		for _, c := range ord.EtcdRaft.GetConsenters() {
			if c.Host == "" {
				logger.Panicf("consenter info in %s configuration did not specify host", EtcdRaft)
			}
			if c.Port == 0 {
				logger.Panicf("consenter info in %s configuration did not specify port", EtcdRaft)
			}
			if c.ClientTlsCert == nil {
				logger.Panicf("consenter info in %s configuration did not specify client TLS cert", EtcdRaft)
			}
			if c.ServerTlsCert == nil {
				logger.Panicf("consenter info in %s configuration did not specify server TLS cert", EtcdRaft)
			}
			clientCertPath := string(c.GetClientTlsCert())
			cf.TranslatePathInPlace(configDir, &clientCertPath)
			c.ClientTlsCert = []byte(clientCertPath)
			serverCertPath := string(c.GetServerTlsCert())
			cf.TranslatePathInPlace(configDir, &serverCertPath)
			c.ServerTlsCert = []byte(serverCertPath)
		}
	case BFT:
		if ord.SmartBFT == nil {
			logger.Infof("Orderer.SmartBFT.Options unset, setting to %v", genesisDefaults.Orderer.SmartBFT)
			ord.SmartBFT = genesisDefaults.Orderer.SmartBFT
		}

		if len(ord.ConsenterMapping) == 0 {
			logger.Panicf("%s configuration did not specify any consenter", BFT)
		}

		for _, c := range ord.ConsenterMapping {
			if c.Host == "" {
				logger.Panicf("consenter info in %s configuration did not specify host", BFT)
			}
			if c.Port == 0 {
				logger.Panicf("consenter info in %s configuration did not specify port", BFT)
			}
			if c.ClientTLSCert == "" {
				logger.Panicf("consenter info in %s configuration did not specify client TLS cert", BFT)
			}
			if c.ServerTLSCert == "" {
				logger.Panicf("consenter info in %s configuration did not specify server TLS cert", BFT)
			}
			if len(c.MSPID) == 0 {
				logger.Panicf("consenter info in %s configuration did not specify MSP ID", BFT)
			}
			if len(c.Identity) == 0 {
				logger.Panicf("consenter info in %s configuration did not specify identity certificate", BFT)
			}

			cf.TranslatePathInPlace(configDir, &c.ClientTLSCert)
			cf.TranslatePathInPlace(configDir, &c.ServerTLSCert)
			cf.TranslatePathInPlace(configDir, &c.Identity)
		}
	case BiniBFT:
		if ord.BiniBFT == nil {
			logger.Infof("Orderer.BiniBFT.Options unset, setting to %v", genesisDefaults.Orderer.BiniBFT)
			ord.BiniBFT = genesisDefaults.Orderer.BiniBFT
		}

		if len(ord.ConsenterMapping) == 0 {
			logger.Panicf("%s configuration did not specify any consenter", BiniBFT)
		}

		for _, c := range ord.ConsenterMapping {
			if c.Host == "" {
				logger.Panicf("consenter info in %s configuration did not specify host", BiniBFT)
			}
			if c.Port == 0 {
				logger.Panicf("consenter info in %s configuration did not specify port", BiniBFT)
			}
			if c.ClientTLSCert == "" {
				logger.Panicf("consenter info in %s configuration did not specify client TLS cert", BiniBFT)
			}
			if c.ServerTLSCert == "" {
				logger.Panicf("consenter info in %s configuration did not specify server TLS cert", BiniBFT)
			}
			if len(c.MSPID) == 0 {
				logger.Panicf("consenter info in %s configuration did not specify MSP ID", BiniBFT)
			}
			if len(c.Identity) == 0 {
				logger.Panicf("consenter info in %s configuration did not specify identity certificate", BiniBFT)
			}

			cf.TranslatePathInPlace(configDir, &c.ClientTLSCert)
			cf.TranslatePathInPlace(configDir, &c.ServerTLSCert)
			cf.TranslatePathInPlace(configDir, &c.Identity)
		}
	default:
		logger.Panicf("unknown orderer type: %s", ord.OrdererType)
	}
}

func translatePaths(configDir string, org *Organization) {
	cf.TranslatePathInPlace(configDir, &org.MSPDir)
}

// configCache stores marshalled bytes of config structures that produced from
// EnhancedExactUnmarshal. Cache key is the path of the configuration file that was used.
type configCache struct {
	mutex sync.Mutex
	cache map[string][]byte
}

var cache = &configCache{
	cache: make(map[string][]byte),
}

// load loads the TopLevel config structure from configCache.
// if not successful, it unmarshals a config file, and populate configCache
// with marshaled TopLevel struct.
func (c *configCache) load(config *viperutil.ConfigParser, configPath string) (*TopLevel, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	conf := &TopLevel{}
	serializedConf, ok := c.cache[configPath]
	logger.Debugf("Loading configuration from cache: %t", ok)
	if !ok {
		err := config.EnhancedExactUnmarshal(conf)
		if err != nil {
			return nil, fmt.Errorf("Error unmarshalling config into struct: %s", err)
		}

		serializedConf, err = json.Marshal(conf)
		if err != nil {
			return nil, err
		}
		c.cache[configPath] = serializedConf
	}

	err := json.Unmarshal(serializedConf, conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}

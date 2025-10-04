/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"sort"
	"time"

	"github.com/hyperledger/fabric-lib-go/bccsp"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/msp"
	"github.com/hyperledger/fabric/common/channelconfig"
	"github.com/hyperledger/fabric/common/crypto"
	"github.com/hyperledger/fabric/common/deliverclient"
	"github.com/hyperledger/fabric/common/deliverclient/blocksprovider"
	"github.com/hyperledger/fabric/internal/pkg/identity"
	"github.com/hyperledger/fabric/orderer/common/cluster"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/orderer/consensus/etcdraft"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
)

// createBiniBFTConfig extracts BiniBFT configuration from orderer config
func createBiniBFTConfig(ordererConfig channelconfig.Orderer) (*BiniBFTOptions, error) {
	configOptions := &BiniBFTOptions{}

	// Unmarshal from consensus metadata
	metadata := ordererConfig.ConsensusMetadata()
	if len(metadata) == 0 {
		return nil, errors.New("no BiniBFT consensus metadata found in channel config")
	}

	if err := proto.Unmarshal(metadata, configOptions); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal BiniBFT consensus metadata")
	}

	// Set batch size from orderer config
	batchSize := ordererConfig.BatchSize()
	if configOptions.RequestBatchMaxCount == 0 {
		configOptions.RequestBatchMaxCount = uint64(batchSize.MaxMessageCount)
	}
	if configOptions.RequestBatchMaxBytes == 0 {
		configOptions.RequestBatchMaxBytes = uint64(batchSize.AbsoluteMaxBytes)
	}

	// Set default values if not specified
	setDefaultBiniBFTOptions(configOptions)

	return configOptions, nil
}

// setDefaultBiniBFTOptions sets default values for BiniBFT configuration
func setDefaultBiniBFTOptions(options *BiniBFTOptions) {
	if options.RequestBatchMaxInterval == "" {
		options.RequestBatchMaxInterval = "50ms"
	}
	if options.IncomingMessageBufferSize == 0 {
		options.IncomingMessageBufferSize = 200
	}
	if options.RequestPoolSize == 0 {
		options.RequestPoolSize = 400
	}
	if options.RequestForwardTimeout == "" {
		options.RequestForwardTimeout = "2s"
	}
	if options.RequestComplainTimeout == "" {
		options.RequestComplainTimeout = "20s"
	}
	if options.RequestAutoRemoveTimeout == "" {
		options.RequestAutoRemoveTimeout = "3m0s"
	}
	if options.ViewChangeResendInterval == "" {
		options.ViewChangeResendInterval = "5s"
	}
	if options.ViewChangeTimeout == "" {
		options.ViewChangeTimeout = "20s"
	}
	if options.LeaderHeartbeatTimeout == "" {
		options.LeaderHeartbeatTimeout = "1m0s"
	}
	if options.LeaderHeartbeatCount == 0 {
		options.LeaderHeartbeatCount = 10
	}
	if options.CollectTimeout == "" {
		options.CollectTimeout = "1s"
	}
	if options.RequestPoolSubmitTimeout == "" {
		options.RequestPoolSubmitTimeout = "10s"
	}
	if options.DecisionsPerLeader == 0 {
		options.DecisionsPerLeader = 3
	}
	if options.RequestMaxBytes == 0 {
		options.RequestMaxBytes = 6291456 // 6MB
	}
	if options.ShardMajorityThreshold == 0 {
		options.ShardMajorityThreshold = 0.67
	}
	if options.CrossShardThreshold == 0 {
		options.CrossShardThreshold = 0.67
	}
	if options.BatchSize == 0 {
		options.BatchSize = 10
	}
	if options.MaxBatchDelay == "" {
		options.MaxBatchDelay = "100ms"
	}
	if options.MaxFaultyNodes == 0 {
		options.MaxFaultyNodes = 1
	}
}

// extractShardConfig extracts shard configuration and determines node role
func extractShardConfig(options *BiniBFTOptions, selfID uint64) (NodeRole, ShardID, NodeID, map[ShardID]NodeID, map[ShardID][]NodeID) {
	primaryLeader := NodeID("1")
	if options.PrimaryLeader != 0 {
		primaryLeader = NodeID(string(rune(options.PrimaryLeader + '0')))
	}

	shardLeaders := make(map[ShardID]NodeID)
	shardNodes := make(map[ShardID][]NodeID)

	// Extract shard configuration from protobuf
	for _, shard := range options.Shards {
		shardID := ShardID(shard.ShardId)
		leader := NodeID(string(rune(shard.Leader + '0')))

		shardLeaders[shardID] = leader

		var nodes []NodeID
		for _, nodeID := range shard.Nodes {
			nodes = append(nodes, NodeID(string(rune(nodeID+'0'))))
		}
		shardNodes[shardID] = nodes
	}

	// Determine this node's role and shard
	var nodeRole NodeRole
	var nodeShardID ShardID

	if selfID == options.PrimaryLeader {
		nodeRole = RolePrimaryLeader
		nodeShardID = ShardID(0) // Primary leader doesn't belong to a specific shard
	} else {
		// Find which shard this node belongs to
		found := false
		for _, shard := range options.Shards {
			if shard.Leader == selfID {
				nodeRole = RoleShardLeader
				nodeShardID = ShardID(shard.ShardId)
				found = true
				break
			}
			for _, follower := range shard.Followers {
				if follower == selfID {
					nodeRole = RoleShardFollower
					nodeShardID = ShardID(shard.ShardId)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			// Default to follower in shard 1
			nodeRole = RoleShardFollower
			nodeShardID = ShardID(1)
		}
	}

	return nodeRole, nodeShardID, primaryLeader, shardLeaders, shardNodes
}

// RuntimeConfig defines the configuration of the consensus
// that is related to runtime.
type RuntimeConfig struct {
	BFTConfig              *Configuration
	isConfig               bool
	logger                 *flogging.FabricLogger
	id                     uint64
	LastCommittedBlockHash string
	RemoteNodes            []cluster.RemoteNode
	ID2Identities          NodeIdentitiesByID
	LastBlock              *cb.Block
	LastConfigBlock        *cb.Block
	Nodes                  []uint64
	consenters             []*cb.Consenter
}

// BlockCommitted updates the config from the block
func (rtc RuntimeConfig) BlockCommitted(block *cb.Block, bccsp bccsp.BCCSP) (RuntimeConfig, error) {
	if _, err := deliverclient.ConfigFromBlock(block); err == nil {
		return rtc.configBlockCommitted(block, bccsp)
	}
	return RuntimeConfig{
		consenters:             rtc.consenters,
		BFTConfig:              rtc.BFTConfig,
		id:                     rtc.id,
		logger:                 rtc.logger,
		LastCommittedBlockHash: hex.EncodeToString(protoutil.BlockHeaderHash(block.Header)),
		Nodes:                  rtc.Nodes,
		ID2Identities:          rtc.ID2Identities,
		RemoteNodes:            rtc.RemoteNodes,
		LastBlock:              block,
		LastConfigBlock:        rtc.LastConfigBlock,
	}, nil
}

func (rtc RuntimeConfig) configBlockCommitted(block *cb.Block, bccsp bccsp.BCCSP) (RuntimeConfig, error) {
	nodeConf, err := remoteNodesFromConfigBlock(block, rtc.logger, bccsp)
	if err != nil {
		return rtc, errors.Wrap(err, "remote nodes cannot be computed, rejecting config block")
	}

	bftConfig, err := configBlockToBFTConfig(rtc.id, block, bccsp)
	if err != nil {
		return RuntimeConfig{}, err
	}

	return RuntimeConfig{
		consenters:             nodeConf.consenters,
		BFTConfig:              bftConfig,
		id:                     rtc.id,
		logger:                 rtc.logger,
		LastCommittedBlockHash: hex.EncodeToString(protoutil.BlockHeaderHash(block.Header)),
		Nodes:                  nodeConf.nodeIDs,
		ID2Identities:          nodeConf.id2Identities,
		RemoteNodes:            nodeConf.remoteNodes,
		LastBlock:              block,
		LastConfigBlock:        block,
	}, nil
}

func configBlockToBFTConfig(selfID uint64, block *cb.Block, bccsp bccsp.BCCSP) (*Configuration, error) {
	if block == nil || block.Data == nil || len(block.Data.Data) == 0 {
		return &Configuration{}, errors.New("empty block")
	}

	env, err := protoutil.UnmarshalEnvelope(block.Data.Data[0])
	if err != nil {
		return &Configuration{}, err
	}
	bundle, err := channelconfig.NewBundleFromEnvelope(env, bccsp)
	if err != nil {
		return &Configuration{}, err
	}

	oc, ok := bundle.OrdererConfig()
	if !ok {
		return &Configuration{}, errors.New("no orderer config")
	}

	consensusConfigOptions, err := createBiniBFTConfig(oc)
	if err != nil {
		return &Configuration{}, err
	}

	return ConfigFromMetadataOptions(selfID, consensusConfigOptions)
}

// remoteNodesFromConfigBlock unmarshalls the node config from the block metadata
func remoteNodesFromConfigBlock(block *cb.Block, logger *flogging.FabricLogger, bccsp bccsp.BCCSP) (*nodeConfig, error) {
	env := &cb.Envelope{}
	if err := proto.Unmarshal(block.Data.Data[0], env); err != nil {
		return nil, errors.Wrap(err, "failed unmarshalling envelope of config block")
	}
	bundle, err := channelconfig.NewBundleFromEnvelope(env, bccsp)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting a new bundle from envelope of config block")
	}

	channelMSPs, err := bundle.MSPManager().GetMSPs()
	if err != nil {
		return nil, errors.Wrap(err, "failed obtaining MSPs from MSPManager")
	}

	oc, ok := bundle.OrdererConfig()
	if !ok {
		return nil, errors.New("no orderer config in config block")
	}

	_, err = createBiniBFTConfig(oc)
	if err != nil {
		return nil, err
	}

	var nodeIDs []uint64
	var remoteNodes []cluster.RemoteNode
	id2Identies := map[uint64][]byte{}
	for _, consenter := range oc.Consenters() {
		sanitizedID, err := crypto.SanitizeIdentity(protoutil.MarshalOrPanic(&msp.SerializedIdentity{
			IdBytes: consenter.Identity,
			Mspid:   consenter.MspId,
		}))
		if err != nil {
			logger.Panicf("Failed to sanitize identity: %v [%s]", err, string(consenter.Identity))
		}
		id2Identies[(uint64)(consenter.Id)] = sanitizedID
		logger.Infof("%s %d ---> %s", bundle.ConfigtxValidator().ChannelID(), consenter.Id, string(consenter.Identity))

		nodeIDs = append(nodeIDs, (uint64)(consenter.Id))

		serverCertAsDER, err := pemToDER(consenter.ServerTlsCert, (uint64)(consenter.Id), "server", logger)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		clientCertAsDER, err := pemToDER(consenter.ClientTlsCert, (uint64)(consenter.Id), "client", logger)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// Validate certificate structure
		for _, cert := range [][]byte{serverCertAsDER, clientCertAsDER} {
			if _, err := x509.ParseCertificate(cert); err != nil {
				pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
				logger.Errorf("Invalid certificate: %s", string(pemBytes))
				return nil, err
			}
		}

		nodeMSP, exists := channelMSPs[consenter.MspId]
		if !exists {
			return nil, errors.Errorf("no MSP found for MSP with ID of %s", consenter.MspId)
		}

		var rootCAs [][]byte
		rootCAs = append(rootCAs, nodeMSP.GetTLSRootCerts()...)
		rootCAs = append(rootCAs, nodeMSP.GetTLSIntermediateCerts()...)

		sanitizedCert, err := crypto.SanitizeX509Cert(consenter.Identity)
		if err != nil {
			return nil, err
		}

		remoteNodes = append(remoteNodes, cluster.RemoteNode{
			NodeAddress: cluster.NodeAddress{
				ID:       (uint64)(consenter.Id),
				Endpoint: fmt.Sprintf("%s:%d", consenter.Host, consenter.Port),
			},

			NodeCerts: cluster.NodeCerts{
				ClientTLSCert: clientCertAsDER,
				ServerTLSCert: serverCertAsDER,
				ServerRootCA:  rootCAs,
				Identity:      sanitizedCert,
			},
		})
	}

	sort.Slice(nodeIDs, func(i, j int) bool {
		return nodeIDs[i] < nodeIDs[j]
	})

	return &nodeConfig{
		consenters:    oc.Consenters(),
		remoteNodes:   remoteNodes,
		id2Identities: id2Identies,
		nodeIDs:       nodeIDs,
	}, nil
}

type nodeConfig struct {
	id2Identities NodeIdentitiesByID
	remoteNodes   []cluster.RemoteNode
	nodeIDs       []uint64
	consenters    []*cb.Consenter
}

// NodeIdentitiesByID stores Identities by id
type NodeIdentitiesByID map[uint64][]byte

// IdentityToID looks up the Identity in NodeIdentitiesByID and returns id and flag true if found
func (nibd NodeIdentitiesByID) IdentityToID(identity []byte) (uint64, bool) {
	sID := &msp.SerializedIdentity{}
	if err := proto.Unmarshal(identity, sID); err != nil {
		return 0, false
	}
	for id, currIdentity := range nibd {
		currentID := &msp.SerializedIdentity{}
		if err := proto.Unmarshal(currIdentity, currentID); err != nil {
			return 0, false
		}
		if proto.Equal(currentID, sID) {
			return id, true
		}
	}
	return 0, false
}

//go:generate counterfeiter -o mocks/bft_deliverer_factory.go --fake-name BFTDelivererFactory . BFTDelivererFactory

type BFTDelivererFactory interface {
	CreateBFTDeliverer(
		channelID string,
		blockHandler blocksprovider.BlockHandler,
		ledger blocksprovider.LedgerInfo,
		updatableBlockVerifier blocksprovider.UpdatableBlockVerifier,
		dialer blocksprovider.Dialer,
		orderersSourceFactory blocksprovider.OrdererConnectionSourceFactory,
		cryptoProvider bccsp.BCCSP,
		doneC chan struct{},
		signer identity.SignerSerializer,
		deliverStreamer blocksprovider.DeliverStreamer,
		censorshipDetectorFactory blocksprovider.CensorshipDetectorFactory,
		logger *flogging.FabricLogger,
		initialRetryInterval time.Duration,
		maxRetryInterval time.Duration,
		blockCensorshipTimeout time.Duration,
		maxRetryDuration time.Duration,
		maxRetryDurationExceededHandler blocksprovider.MaxRetryDurationExceededHandler,
	) BFTBlockDeliverer
}

type bftDelivererCreator struct{}

func (*bftDelivererCreator) CreateBFTDeliverer(
	channelID string,
	blockHandler blocksprovider.BlockHandler,
	ledger blocksprovider.LedgerInfo,
	updatableBlockVerifier blocksprovider.UpdatableBlockVerifier,
	dialer blocksprovider.Dialer,
	orderersSourceFactory blocksprovider.OrdererConnectionSourceFactory,
	cryptoProvider bccsp.BCCSP,
	doneC chan struct{},
	signer identity.SignerSerializer,
	deliverStreamer blocksprovider.DeliverStreamer,
	censorshipDetectorFactory blocksprovider.CensorshipDetectorFactory,
	logger *flogging.FabricLogger,
	initialRetryInterval time.Duration,
	maxRetryInterval time.Duration,
	blockCensorshipTimeout time.Duration,
	maxRetryDuration time.Duration,
	maxRetryDurationExceededHandler blocksprovider.MaxRetryDurationExceededHandler,
) BFTBlockDeliverer {
	bftDeliverer := &blocksprovider.BFTDeliverer{
		ChannelID:                       channelID,
		BlockHandler:                    blockHandler,
		Ledger:                          ledger,
		UpdatableBlockVerifier:          updatableBlockVerifier,
		Dialer:                          dialer,
		OrderersSourceFactory:           orderersSourceFactory,
		CryptoProvider:                  cryptoProvider,
		DoneC:                           doneC,
		Signer:                          signer,
		DeliverStreamer:                 deliverStreamer,
		CensorshipDetectorFactory:       censorshipDetectorFactory,
		Logger:                          logger,
		InitialRetryInterval:            initialRetryInterval,
		MaxRetryInterval:                maxRetryInterval,
		BlockCensorshipTimeout:          blockCensorshipTimeout,
		MaxRetryDuration:                maxRetryDuration,
		MaxRetryDurationExceededHandler: maxRetryDurationExceededHandler,
	}
	return bftDeliverer
}

//go:generate counterfeiter -o mocks/bft_block_deliverer.go --fake-name BFTBlockDeliverer . BFTBlockDeliverer
type BFTBlockDeliverer interface {
	Stop()
	DeliverBlocks()
	Initialize(channelConfig *cb.Config, selfEndpoint string)
}

//go:generate counterfeiter -o mocks/block_puller_factory.go --fake-name BlockPullerFactory . BlockPullerFactory

type BlockPullerFactory interface {
	CreateBlockPuller(
		support consensus.ConsenterSupport,
		clusterDialer *cluster.PredicateDialer,
		clusterConfig localconfig.Cluster,
		bccsp bccsp.BCCSP,
	) (BlockPuller, error)
}

type blockPullerCreator struct{}

func (*blockPullerCreator) CreateBlockPuller(
	support consensus.ConsenterSupport,
	clusterDialer *cluster.PredicateDialer,
	clusterConfig localconfig.Cluster,
	bccsp bccsp.BCCSP,
) (BlockPuller, error) {
	verifyBlockSequence := func(blocks []*cb.Block, _ string) error {
		return cluster.VerifyBlocksBFT(blocks, support.SignatureVerifier(), cluster.BlockVerifierBuilder(bccsp))
	}

	// Extract endpoints from support
	endpoints, err := etcdraft.EndpointconfigFromSupport(support, bccsp)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to extract endpoints from support")
	}

	stdDialer := &cluster.StandardDialer{
		Config: clusterDialer.Config.Clone(),
	}
	stdDialer.Config.AsyncConnect = false
	stdDialer.Config.SecOpts.VerifyCertificate = nil

	bp := &cluster.BlockPuller{
		VerifyBlockSequence: verifyBlockSequence,
		Logger:              flogging.MustGetLogger("orderer.common.cluster.puller").With("channel", support.ChannelID()),
		RetryTimeout:        clusterConfig.ReplicationRetryTimeout,
		MaxTotalBufferBytes: clusterConfig.ReplicationBufferSize,
		FetchTimeout:        clusterConfig.ReplicationPullTimeout,
		Endpoints:           endpoints,
		Signer:              support,
		TLSCert:             stdDialer.Config.SecOpts.Certificate,
		Channel:             support.ChannelID(),
		Dialer:              stdDialer,
	}

	return bp, nil
}

//go:generate counterfeiter -o mocks/block_puller.go --fake-name BlockPuller . BlockPuller

type BlockPuller interface {
	PullBlock(seq uint64) *cb.Block
	HeightsByEndpoints() (map[string]uint64, string, error)
	Close()
}

//go:generate counterfeiter -o mocks/verifier_factory.go --fake-name VerifierFactory . VerifierFactory

type VerifierFactory interface {
	CreateBlockVerifier(
		lastConfigBlock *cb.Block,
		lastBlock *cb.Block,
		cryptoProvider bccsp.BCCSP,
		logger *flogging.FabricLogger,
	) (deliverclient.CloneableUpdatableBlockVerifier, error)
}

type verifierCreator struct{}

func (*verifierCreator) CreateBlockVerifier(
	lastConfigBlock *cb.Block,
	lastBlock *cb.Block,
	cryptoProvider bccsp.BCCSP,
	logger *flogging.FabricLogger,
) (deliverclient.CloneableUpdatableBlockVerifier, error) {
	lastBlockNum := lastBlock.Header.Number
	lastCommittedBlockHash := protoutil.BlockHeaderHash(lastBlock.Header)

	// Extract config from the config block
	configEnv, err := deliverclient.ConfigFromBlock(lastConfigBlock)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to extract config from block")
	}

	return deliverclient.NewBlockVerificationAssistantFromConfig(
		configEnv.GetConfig(),
		lastBlockNum,
		lastCommittedBlockHash,
		"", // channel ID will be set by the caller
		cryptoProvider,
		logger,
	)
}

type ledgerInfoAdapter struct {
	support consensus.ConsenterSupport
}

func (lia *ledgerInfoAdapter) LedgerHeight() (uint64, error) {
	return lia.support.Height(), nil
}

func (lia *ledgerInfoAdapter) GetCurrentBlockHash() ([]byte, error) {
	height := lia.support.Height()
	if height == 0 {
		return nil, errors.New("ledger height is 0")
	}
	block := lia.support.Block(height - 1)
	if block == nil {
		return nil, errors.Errorf("failed to retrieve block at height %d", height-1)
	}
	return protoutil.BlockHeaderHash(block.Header), nil
}

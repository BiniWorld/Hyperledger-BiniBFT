/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"github.com/hyperledger/fabric-lib-go/bccsp"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/orderer/common/cluster"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
)

//go:generate counterfeiter -o mocks/synchronizer_factory.go --fake-name SynchronizerFactory . SynchronizerFactory

// SynchronizerFactory creates Synchronizer instances
type SynchronizerFactory interface {
	CreateSynchronizer(
		logger *flogging.FabricLogger,
		localConfigCluster localconfig.Cluster,
		runtimeConfig RuntimeConfig,
		blockToDecision func(*common.Block) *BiniBFTDecision,
		onCommit func(*common.Block) BiniBFTReconfig,
		updateRuntimeConfig func(*common.Block) BiniBFTReconfig,
		support consensus.ConsenterSupport,
		bccsp bccsp.BCCSP,
		clusterDialer *cluster.PredicateDialer,
	) BiniBFTSynchronizerInterface
}

type synchronizerFactory struct{}

func (sf *synchronizerFactory) CreateSynchronizer(
	logger *flogging.FabricLogger,
	localConfigCluster localconfig.Cluster,
	runtimeConfig RuntimeConfig,
	blockToDecision func(*common.Block) *BiniBFTDecision,
	onCommit func(*common.Block) BiniBFTReconfig,
	updateRuntimeConfig func(*common.Block) BiniBFTReconfig,
	support consensus.ConsenterSupport,
	bccsp bccsp.BCCSP,
	clusterDialer *cluster.PredicateDialer,
) BiniBFTSynchronizerInterface {
	switch localConfigCluster.ReplicationPolicy {
	case "consensus":
		logger.Debug("Creating a BiniBFT BFTSynchronizer")
		return &BiniBFTSynchronizer{
			selfID: runtimeConfig.id,
			LatestConfig: func() (*Configuration, []uint64) {
				return runtimeConfig.BFTConfig, runtimeConfig.Nodes
			},
			BlockToDecision: blockToDecision,
			OnCommit: func(block *common.Block) BiniBFTReconfig {
				return onCommit(block)
			},
			Support:             support,
			CryptoProvider:      bccsp,
			ClusterDialer:       clusterDialer,
			LocalConfigCluster:  localConfigCluster,
			BlockPullerFactory:  &blockPullerCreator{},
			VerifierFactory:     &verifierCreator{},
			BFTDelivererFactory: &bftDelivererCreator{},
			Logger:              logger,
		}
	case "simple":
		logger.Debug("Creating simple BiniBFT Synchronizer")
		return &BiniBFTSimpleSynchronizer{
			selfID:          runtimeConfig.id,
			BlockToDecision: blockToDecision,
			OnCommit: func(block *common.Block) BiniBFTReconfig {
				return onCommit(block)
			},
			Support:            support,
			CryptoProvider:     bccsp,
			ClusterDialer:      clusterDialer,
			LocalConfigCluster: localConfigCluster,
			BlockPullerFactory: &blockPullerCreator{},
			Logger:             logger,
			LatestConfig: func() (*Configuration, []uint64) {
				return runtimeConfig.BFTConfig, runtimeConfig.Nodes
			},
		}
	default:
		logger.Panicf("Unsupported Cluster.ReplicationPolicy: %s", localConfigCluster.ReplicationPolicy)
		return nil
	}
}

// NewSynchronizerFactory creates a new synchronizer factory
func NewSynchronizerFactory() SynchronizerFactory {
	return &synchronizerFactory{}
}

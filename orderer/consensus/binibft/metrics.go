/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"github.com/hyperledger/fabric-lib-go/common/metrics"
)

// Metrics holds the metrics for BiniBFT consensus
type Metrics struct {
	ClusterSize          metrics.Gauge
	CommittedBlockNumber metrics.Gauge
	IsLeader             metrics.Gauge
	LeaderID             metrics.Gauge
	ProposalFailures     metrics.Counter
	ConsensusLatency     metrics.Histogram
}

// NewMetrics creates a new Metrics instance
func NewMetrics(provider metrics.Provider) *Metrics {
	return &Metrics{
		ClusterSize: provider.NewGauge(metrics.GaugeOpts{
			Namespace:    "consensus",
			Subsystem:    "binibft",
			Name:         "cluster_size",
			Help:         "Number of nodes in the cluster",
			LabelNames:   []string{"channel"},
			StatsdFormat: "%{#fqname}.%{channel}",
		}),
		CommittedBlockNumber: provider.NewGauge(metrics.GaugeOpts{
			Namespace:    "consensus",
			Subsystem:    "binibft",
			Name:         "committed_block_number",
			Help:         "The number of the latest committed block",
			LabelNames:   []string{"channel"},
			StatsdFormat: "%{#fqname}.%{channel}",
		}),
		IsLeader: provider.NewGauge(metrics.GaugeOpts{
			Namespace:    "consensus",
			Subsystem:    "binibft",
			Name:         "is_leader",
			Help:         "1 if this node is the leader, 0 otherwise",
			LabelNames:   []string{"channel"},
			StatsdFormat: "%{#fqname}.%{channel}",
		}),
		LeaderID: provider.NewGauge(metrics.GaugeOpts{
			Namespace:    "consensus",
			Subsystem:    "binibft",
			Name:         "leader_id",
			Help:         "ID of the current leader",
			LabelNames:   []string{"channel"},
			StatsdFormat: "%{#fqname}.%{channel}",
		}),
		ProposalFailures: provider.NewCounter(metrics.CounterOpts{
			Namespace:    "consensus",
			Subsystem:    "binibft",
			Name:         "proposal_failures_total",
			Help:         "Total number of proposal failures",
			LabelNames:   []string{"channel"},
			StatsdFormat: "%{#fqname}.%{channel}",
		}),
		ConsensusLatency: provider.NewHistogram(metrics.HistogramOpts{
			Namespace:    "consensus",
			Subsystem:    "binibft",
			Name:         "consensus_latency",
			Help:         "Latency of consensus operations",
			LabelNames:   []string{"channel"},
			StatsdFormat: "%{#fqname}.%{channel}",
		}),
	}
}

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package server

import (
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric/orderer/common/cluster"
	"github.com/hyperledger/fabric/orderer/common/multichannel"
	"github.com/pkg/errors"
)

// MultiplexingHandler routes requests to the appropriate consensus mechanism
// based on the channel's consensus type configuration
type MultiplexingHandler struct {
	SmartBFTHandler cluster.Handler
	BiniBFTHandler  cluster.Handler
	Registrar       *multichannel.Registrar
}

// OnConsensus routes consensus requests to the appropriate handler based on channel configuration
func (m *MultiplexingHandler) OnConsensus(channel string, sender uint64, req *ab.ConsensusRequest) error {
	handler, err := m.getHandlerForChannel(channel)
	if err != nil {
		return err
	}
	return handler.OnConsensus(channel, sender, req)
}

// OnSubmit routes submit requests to the appropriate handler based on channel configuration
func (m *MultiplexingHandler) OnSubmit(channel string, sender uint64, req *ab.SubmitRequest) error {
	handler, err := m.getHandlerForChannel(channel)
	if err != nil {
		return err
	}
	return handler.OnSubmit(channel, sender, req)
}

// getHandlerForChannel determines which consensus handler to use based on the channel's configuration
func (m *MultiplexingHandler) getHandlerForChannel(channel string) (cluster.Handler, error) {
	cs := m.Registrar.GetChain(channel)
	if cs == nil {
		return nil, errors.Errorf("channel %s not found", channel)
	}

	consensusType := cs.SharedConfig().ConsensusType()
	switch consensusType {
	case "BFT":
		return m.SmartBFTHandler, nil
	case "binibft":
		return m.BiniBFTHandler, nil
	default:
		return nil, errors.Errorf("unsupported consensus type %s for channel %s", consensusType, channel)
	}
}

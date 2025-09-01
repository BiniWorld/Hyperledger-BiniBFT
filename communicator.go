package main

import (
	"binibft-poc/consensus"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
)

type Communicator struct {
	nodeId            consensus.NodeID
	mapNodes          map[string]*NodeInfo
	cachedHttpClients map[consensus.NodeID]*http.Client
	clientsMutex      sync.RWMutex
	logger            consensus.Logger
	handler           consensus.MessageHandler
}

func (c *Communicator) getOrCreateClient(targetID consensus.NodeID) *http.Client {
	// First, try to get the client with a read lock
	c.clientsMutex.RLock()
	http3Client, ok := c.cachedHttpClients[targetID]
	c.clientsMutex.RUnlock()

	if ok {
		return http3Client
	}

	// If not found, acquire write lock and create new client
	c.clientsMutex.Lock()
	defer c.clientsMutex.Unlock()

	// Double-check in case another goroutine created it while we were waiting
	if http3Client, ok := c.cachedHttpClients[targetID]; ok {
		return http3Client
	}

	rt := &http3.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"quic-echo-example"},
		},
		DisableCompression: true,
	}
	http3Client = &http.Client{
		Transport: rt,
	}
	c.cachedHttpClients[targetID] = http3Client
	return http3Client
}

// Broadcast implements consensus.NetworkInterface.
func (c *Communicator) Broadcast(nodeIDs []consensus.NodeID, message consensus.Message) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(nodeIDs))

	for _, nodeID := range nodeIDs {
		wg.Add(1)
		go func(id consensus.NodeID) {
			defer wg.Done()
			if err := c.Send(id, message); err != nil {
				c.logger.Error("Failed to send message", "to", id, "error", err)
				errCh <- err
			}
		}(nodeID)
	}

	wg.Wait()
	close(errCh)

	// Return the first error if any
	for err := range errCh {
		return err
	}

	return nil
}

// RegisterHandler implements consensus.NetworkInterface.
func (c *Communicator) RegisterHandler(handler consensus.MessageHandler) {
	c.handler = handler
}

// Send implements consensus.NetworkInterface.
func (c *Communicator) Send(targetID consensus.NodeID, message consensus.Message) error {
	endpoint, exists := c.mapNodes[string(targetID)]
	if !exists {
		return fmt.Errorf("node %s endpoint not found", targetID)
	}

	// Set message metadata
	message.From = c.nodeId
	message.To = targetID
	if message.Timestamp.IsZero() {
		message.Timestamp = time.Now()
	}

	// Encode message
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	http3Client := c.getOrCreateClient(targetID)
	// Send HTTP/3 request to consensus endpoint for inter-node communication
	url := fmt.Sprintf("https://%s/consensus", endpoint.Address)
	go func() {
		resp, err := http3Client.Post(url, "application/json", bytes.NewBuffer(data))
		if err != nil {
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.logger.Info(fmt.Sprintf("Node %s sent transaction to node %s", c.nodeId, targetID))
			return
		}
	}()

	return nil
}

func (c *Communicator) SendTransaction(targetID consensus.NodeID, request []byte) error {
	endpoint, exists := c.mapNodes[string(targetID)]
	if !exists {
		return fmt.Errorf("node %s endpoint not found", targetID)
	}

	http3Client := c.getOrCreateClient(targetID)
	c.logger.Info(fmt.Sprintf("node %s sending transaction to node %s address=%s", c.nodeId, targetID, endpoint.Address))
	url := fmt.Sprintf("https://%s/transaction?id=%s", endpoint.Address, c.nodeId)
	go func() {
		resp, err := http3Client.Post(url, "application/octet-stream", bytes.NewBuffer(request))
		if err != nil {
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.logger.Info(fmt.Sprintf("Node %s sent transaction to node %s", c.nodeId, targetID))
			return
		}
	}()
	return nil
}

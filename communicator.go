package main

import (
	"binibft-poc/consensus"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
)

type Communicator struct {
	nodeId            consensus.NodeID
	mapNodes          map[string]*NodeInfo
	cachedHttpClients map[consensus.NodeID]*http.Client
	logger            consensus.Logger
	handler           consensus.MessageHandler
}

func (c Communicator) getOrCreateClient(targetID consensus.NodeID) *http.Client {
	http3Client, ok := c.cachedHttpClients[targetID]
	if ok {
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
func (c Communicator) Broadcast(nodeIDs []consensus.NodeID, message consensus.Message) error {
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
func (c Communicator) RegisterHandler(handler consensus.MessageHandler) {
	c.handler = handler
}

// Send implements consensus.NetworkInterface.
func (c Communicator) Send(nodeID consensus.NodeID, message consensus.Message) error {
	endpoint, exists := c.mapNodes[string(nodeID)]

	if !exists {
		return fmt.Errorf("node %s endpoint not found", nodeID)
	}

	// Set message metadata
	message.From = c.nodeId
	message.To = nodeID
	if message.Timestamp.IsZero() {
		message.Timestamp = time.Now()
	}

	// Encode message
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	http3Client := c.getOrCreateClient(nodeID)
	// Send HTTP/3 request to consensus endpoint for inter-node communication
	url := fmt.Sprintf("https://%s/consensus", endpoint.Address)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	c.logger.Debug("Sending message", "to", nodeID, "type", message.Type, "endpoint", endpoint)

	resp, err := http3Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send message, status: %d, response: %s", resp.StatusCode, string(body))
	}

	return nil
}

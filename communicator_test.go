package main

import (
	"binibft-poc/consensus"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestCommunicatorTLSConfiguration(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	nodeInfoMap := map[string]*NodeInfo{
		"1": {Address: "127.0.0.1:9001"},
		"2": {Address: "127.0.0.1:9002"},
	}

	comm := &Communicator{
		nodeId:            consensus.NodeID("1"),
		mapNodes:          nodeInfoMap,
		cachedHttpClients: make(map[consensus.NodeID]*http.Client),
		logger:            logger,
	}

	// 1. Default TLS configuration
	client1 := comm.getOrCreateClient(consensus.NodeID("2"))
	if client1 == nil {
		t.Fatalf("expected non-nil client")
	}

	// 2. Custom mTLS configuration
	customTLS := &tls.Config{
		ServerName: "orderer.example.com",
		RootCAs:    x509.NewCertPool(),
		MinVersion: tls.VersionTLS13,
	}
	comm.SetTLSConfig(customTLS)
	comm.SetTimeout(2 * time.Second)

	client2 := comm.getOrCreateClient(consensus.NodeID("2"))
	if client2 == nil {
		t.Fatalf("expected non-nil client with custom TLS")
	}
	if client2.Timeout != 2*time.Second {
		t.Fatalf("expected timeout 2s, got %v", client2.Timeout)
	}

	// 3. Error on missing endpoint
	err := comm.Send(consensus.NodeID("999"), consensus.Message{})
	if err == nil {
		t.Fatalf("expected error for non-existent node endpoint")
	}
}

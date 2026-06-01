package main

import (
    "github.com/hyperledger/binibft-poc/consensus/binibftprotos"
    "bytes"
    "crypto/tls"
    "net/http"
    "strconv"
    "time"

    "github.com/golang/protobuf/proto"
    "github.com/quic-go/quic-go"
    "github.com/quic-go/quic-go/http3"
    log "github.com/sirupsen/logrus"
)

type Communicator struct {
    nodeId            uint64
    mapNodes          map[uint64]*NodeInfo
    cachedHttpClients map[uint64]*http.Client
}

func (c Communicator) SendConsensus(targetID uint64, m *binibftprotos.Message) {
    nodeInfo, ok := c.mapNodes[targetID]
    if !ok {
        log.Warnf("Node %d not found in configuration", targetID)
        return
    }
    // proto.message to bytes
    request, err := proto.Marshal(m)
    if err != nil {
        log.Warnf("Error marshaling proto.message: %v", err)
        return
    }
    http3Client := c.getOrCreateClient(targetID)
    // send request to node
    log.Debugf("node %d sent consensus to node %d address=%s", c.nodeId, targetID, nodeInfo.Address)
    go func() {
        r := bytes.NewBuffer(request)
        req, err := http3Client.Post("https://"+nodeInfo.Address+"/consensus?id="+strconv.FormatUint(c.nodeId, 10), "application/octet-stream", r)
        if err != nil {
            log.Warnf("Error sending request to node: %v", err)
            return
        }
        req.Body.Close()
        log.Debugf("Node %d sent consensus to node %d", c.nodeId, targetID)
    }()
}

func (c Communicator) SendTransaction(targetID uint64, request []byte) {
    nodeInfo, ok := c.mapNodes[targetID]
    if !ok {
        log.Warnf("Node %d not found in configuration", targetID)
        return
    }
    msg := &FwdMessage{
        Payload: request,
        Sender:  c.nodeId,
    }
    // proto.message to bytes
    bodyReq, err := proto.Marshal(msg)
    if err != nil {
        log.Warnf("Error marshaling proto.message: %v", err)
        return
    }
    http3Client := c.getOrCreateClient(targetID)
    // send request to node
    log.Infof("node %d sending transaction to node %d address=%s", c.nodeId, targetID, nodeInfo.Address)
    go func() {
        req, err := http3Client.Post("https://"+nodeInfo.Address+"/transaction?id="+strconv.FormatUint(c.nodeId, 10), "application/octet-stream", bytes.NewBuffer(bodyReq))
        if err != nil {
            log.Warnf("Error sending request to node: %v", err)
            return
        }
        req.Body.Close()
        log.Infof("Node %d sent transaction to node %d", c.nodeId, targetID)
    }()
}

func (c Communicator) getOrCreateClient(targetID uint64) *http.Client {
    http3Client, ok := c.cachedHttpClients[targetID]
    if ok {
        return http3Client
    }
    rt := &http3.RoundTripper{
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: true,
            NextProtos:         []string{"quic-echo-example"},
        },
        DisableCompression: true,
        QuicConfig:         &quic.Config{MaxIdleTimeout: 10 * time.Second},
    }
    http3Client = &http.Client{
        Transport: rt,
    }
    c.cachedHttpClients[targetID] = http3Client
    return http3Client
}

func (c Communicator) Nodes() []uint64 {
    nodes := make([]uint64, 0, len(c.mapNodes))
    for id := range c.mapNodes {
        nodes = append(nodes, id)
    }

    // Sort nodes to ensure deterministic order across all nodes
    // This is critical for hierarchical consensus role assignment
    for i := 0; i < len(nodes); i++ {
        for j := i + 1; j < len(nodes); j++ {
            if nodes[i] > nodes[j] {
                nodes[i], nodes[j] = nodes[j], nodes[i]
            }
        }
    }

    return nodes
}
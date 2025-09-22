package main

import (
	"binibft-poc/consensus"
	"time"
)

type Heartbeat struct {
	From      consensus.NodeID // Node ID of the sender
	Timestamp time.Time        // Time the heartbeat was sent
}

func (n *Node) startHeartbeatSender(interval time.Duration) {
	if n.role != consensus.RolePrimaryLeader && n.role != consensus.RoleShardLeader {
		return
	}

	n.heartbeatTicker = time.NewTicker(interval)
	n.heartbeatStop = make(chan struct{})
	n.heartbeatWG.Add(1)

	go func() {
		defer n.heartbeatWG.Done()
		for {
			select {
			case <-n.heartbeatTicker.C:
				now := time.Now()
				n.heartbeatMutex.Lock()
				n.lastHeartbeats[n.id] = now
				n.heartbeatSeen[n.id] = true
				n.heartbeatMutex.Unlock()
				n.ReceiveHeartbeat(n.id, now)
				n.logger.Info("[Heartbeat] Leader updated heartbeat", "leader", string(n.id), "time", now.Format(time.RFC3339))

				// Send heartbeat messages to all nodes except self
				for nodeIDStr := range n.mapNodes {
					nodeID := consensus.NodeID(nodeIDStr)
					if nodeID != n.id {
						go func(targetID consensus.NodeID) {
							n.sendHeartbeatMessage(targetID, now)
						}(nodeID)
					}
				}

			case <-n.heartbeatStop:
				n.logger.Info("[Heartbeat] Sender stopped")
				return
			}
		}
	}()
}

func (n *Node) startHeartbeatMonitor(timeout time.Duration) {
	if n.role != consensus.RoleShardFollower {
		return
	}

	n.heartbeatMonitorTicker = time.NewTicker(timeout)
	n.heartbeatMonitorWG.Add(1)

	go func() {
		defer func() {
			n.heartbeatMonitorTicker.Stop()
			n.heartbeatMonitorWG.Done()
		}()

		for {
			select {
			case <-n.heartbeatMonitorTicker.C:
				now := time.Now()

				n.heartbeatMutex.RLock()
				primaryLast, primaryOk := n.lastHeartbeats[n.primaryId]
				shardLeaderLast, shardOk := n.lastHeartbeats[n.shardLeaderId]

				primarySeen := n.heartbeatSeen[n.primaryId]
				shardLeaderSeen := n.heartbeatSeen[n.shardLeaderId]
				n.heartbeatMutex.RUnlock()

				// Wait for first heartbeat from both leaders
				if !primarySeen || !shardLeaderSeen {
					n.logger.Info("[Heartbeat] Waiting for first heartbeat(s)", "primarySeen", primarySeen, "shardLeaderSeen", shardLeaderSeen)
					continue
				}

				primaryAlive := primaryOk && now.Sub(primaryLast) <= timeout
				shardLeaderAlive := shardOk && now.Sub(shardLeaderLast) <= timeout

				if !primaryAlive && !shardLeaderAlive {
					n.logger.Info("[Heartbeat] Both primary (%s) and shard leader (%s) are down. Triggering view change...",
						n.primaryId, n.shardLeaderId)
					n.triggerViewChange()
					return
				} else if !primaryAlive || !shardLeaderAlive {
					n.logger.Info("[Heartbeat] One leader down. Primary alive: %v, ShardLeader alive: %v. Triggering view change...",
						primaryAlive, shardLeaderAlive)
					n.triggerViewChange()
					return
				} else {
					n.logger.Info("[Heartbeat] Leaders alive? Primary: %v, ShardLeader: %v", n.primaryId, shardLeaderAlive)
				}

			case <-n.stopChan:
				n.logger.Info("[Heartbeat] Monitor stopped")
				return
			}
		}
	}()
}



func (n *Node) sendHeartbeatMessage(followerID consensus.NodeID, timestamp time.Time) {
	heartbeatMsg := consensus.HeartbeatMessage{
		NodeID:    n.id,
		ShardID:   n.shardId,
		Timestamp: timestamp,
	}

	msg := consensus.Message{
		Type:      consensus.MsgHeartbeat,
		From:      n.id,
		To:        followerID,
		ShardID:   n.shardId,
		Timestamp: timestamp,
		Payload:   heartbeatMsg,
	}

	err := n.comm.Send(followerID, msg)
	if err != nil {
		n.logger.Error("[Heartbeat] Failed to send heartbeat to %s: %v", followerID, err)
	} else {
		n.logger.Debug("[Heartbeat] Sent heartbeat to %s at %s", followerID, timestamp.Format(time.RFC3339))
	}
}


func (n *Node) stopHeartbeatMonitor() {
	if n.heartbeatMonitorTicker != nil {
		n.heartbeatMonitorTicker.Stop()
	}
	n.heartbeatMonitorWG.Wait()
}

func (n *Node) stopHeartbeat() {
	if n.heartbeatStop != nil {
		close(n.heartbeatStop)
	}
	if n.heartbeatTicker != nil {
		n.heartbeatTicker.Stop()
	}
	n.heartbeatWG.Wait()

	n.stopHeartbeatMonitor()
}

func (n *Node) triggerViewChange() {
	n.logger.Info("[ViewChange] Triggered by node %s", n.id)

	// Trigger leader election on heartbeat failure
	if n.consensus != nil {
		if err := n.consensus.TriggerLeaderElection(); err != nil {
			n.logger.Error("[ViewChange] Failed to trigger leader election", "error", err)
		} else {
			n.logger.Info("[ViewChange] Leader election triggered successfully")
		}
	} else {
		n.logger.Error("[ViewChange] Consensus instance not available")
	}
}

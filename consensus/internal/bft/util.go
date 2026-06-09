// Copyright IBM Corp. All Rights Reserved.
//
// SPDX-License-Identifier: Apache-2.0
//

package bft

import (
	"bytes"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	protos "github.com/hyperledger/binibft-poc/consensus/binibftprotos"
	"github.com/hyperledger/binibft-poc/consensus/pkg/api"
	"github.com/hyperledger/binibft-poc/consensus/pkg/types"

	"github.com/golang/protobuf/proto"
)

type proposalInfo struct {
	digest string
	view   uint64
	seq    uint64
}

func viewNumber(m *protos.Message) uint64 {
	if pp := m.GetPrePrepare(); pp != nil {
		return pp.GetView()
	}

	if prp := m.GetPrepare(); prp != nil {
		return prp.GetView()
	}

	if cmt := m.GetCommit(); cmt != nil {
		return cmt.GetView()
	}

	return math.MaxUint64
}

func proposalSequence(m *protos.Message) uint64 {
	if pp := m.GetPrePrepare(); pp != nil {
		return pp.Seq
	}

	if prp := m.GetPrepare(); prp != nil {
		return prp.Seq
	}

	if cmt := m.GetCommit(); cmt != nil {
		return cmt.Seq
	}

	return math.MaxUint64
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MarshalOrPanic marshals or panics when an error occurs
func MarshalOrPanic(msg proto.Message) []byte {
	b, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return b
}

func getLeaderID(
	view uint64,
	n uint64,
	nodes []uint64,
	leaderRotation bool,
	decisionsInView uint64,
	decisionsPerLeader uint64,
	blacklist []uint64,
) uint64 {
	blackListed := make(map[uint64]struct{})
	for _, i := range blacklist {
		blackListed[i] = struct{}{}
	}

	if !leaderRotation {
		return nodes[view%n]
	}

	for i := 0; i < len(nodes); i++ {
		index := (view + (decisionsInView / decisionsPerLeader)) + uint64(i)
		node := nodes[index%n]
		_, exists := blackListed[node]
		if !exists {
			return node
		}
	}

	panic(fmt.Sprintf("all %d nodes are blacklisted", len(nodes)))
}

// GetNodeRole determines the role of a node in hierarchical consensus deterministically
// Uses ComputeHierarchy to ensure consistency
func GetNodeRole(nodeID uint64, view uint64, totalNodes uint64, nodesList []uint64, decisionsInView uint64, decisionsPerLeader uint64) NodeRole {
	// Use the exact same deterministic approach as SmartBFT leader selection
	leaderRotationIndex := view + (decisionsInView / decisionsPerLeader)

	// Convert to 0-based indexing for ComputeHierarchy
	viewIndex := int(leaderRotationIndex % totalNodes)

	// Use ComputeHierarchy for consistent role assignment
	primary, secondaries, _, _ := ComputeHierarchy(int(totalNodes), viewIndex)

	// Convert nodeID to 0-based index for comparison
	nodeIndex := -1
	for i, id := range nodesList {
		if id == nodeID {
			nodeIndex = i
			break
		}
	}

	if nodeIndex == -1 {
		panic(fmt.Sprintf("nodeID %d not found in nodesList %v", nodeID, nodesList))
	}

	// Check role based on ComputeHierarchy results
	if nodeIndex == primary {
		return PRIMARY
	}

	for _, sec := range secondaries {
		if nodeIndex == sec {
			return SECONDARY
		}
	}

	return FOLLOWER
}

// ComputeHierarchy determines the hierarchical structure for N nodes at view v
func ComputeHierarchy(N, v int) (primary int, secondaries []int, followersOf map[int][]int, S int) {
	if N < 2 {
		panic("need at least 2 nodes")
	}

	// Step 1: compute optimal number of secondaries (ceil version)
	Sstar := (-1 + math.Sqrt(float64(4*N-3))) / 2
	S = int(math.Ceil(Sstar))
	if S > N-1 {
		S = N - 1
	}
	if S < 1 {
		S = 1
	}

	// Step 2: assign primary and secondary IDs
	primary = v % N
	secondaries = make([]int, S)
	for k := 1; k <= S; k++ {
		secondaries[k-1] = (primary + k) % N
	}

	// Step 3: compute followers
	allNodes := make([]int, N)
	for i := 0; i < N; i++ {
		allNodes[i] = i
	}
	exclude := map[int]bool{primary: true}
	for _, sec := range secondaries {
		exclude[sec] = true
	}
	var followersSet []int
	for _, n := range allNodes {
		if !exclude[n] {
			followersSet = append(followersSet, n)
		}
	}
	slices.Sort(followersSet)
	F := len(followersSet)
	q := F / S
	r := F % S

	// Step 4: distribute followers evenly among secondaries
	followersOf = make(map[int][]int)
	idx := 0
	for k, sec := range secondaries {
		count := q
		if k < r {
			count++
		}
		end := idx + count
		if end > len(followersSet) {
			end = len(followersSet)
		}
		followersOf[sec] = followersSet[idx:end]
		idx = end
	}
	return primary, secondaries, followersOf, S
}

// GetPrimaryNode returns the primary node ID deterministically
func GetPrimaryNode(view uint64, totalNodes uint64, nodesList []uint64, decisionsInView uint64, decisionsPerLeader uint64) uint64 {
	if len(nodesList) == 0 {
		panic("empty nodes list")
	}

	// Use the same deterministic approach as GetNodeRole
	leaderRotationIndex := view + (decisionsInView / decisionsPerLeader)
	viewIndex := int(leaderRotationIndex % totalNodes)

	// Use ComputeHierarchy for consistent role assignment
	primary, _, _, _ := ComputeHierarchy(int(totalNodes), viewIndex)

	return nodesList[primary]
}

// GetSecondaryNodes returns all secondary node IDs deterministically
func GetSecondaryNodes(view uint64, totalNodes uint64, nodesList []uint64, decisionsInView uint64, decisionsPerLeader uint64) []uint64 {
	// Use the same deterministic approach as GetNodeRole
	leaderRotationIndex := view + (decisionsInView / decisionsPerLeader)
	viewIndex := int(leaderRotationIndex % totalNodes)

	// Use ComputeHierarchy for consistent role assignment
	_, secondaries, _, _ := ComputeHierarchy(int(totalNodes), viewIndex)

	// Convert indices to node IDs
	secondaryIDs := make([]uint64, len(secondaries))
	for i, secIndex := range secondaries {
		secondaryIDs[i] = nodesList[secIndex]
	}

	return secondaryIDs
}

// GetSecondaryForFollower returns the secondary that manages this follower deterministically
func GetSecondaryForFollower(followerID uint64, view uint64, totalNodes uint64, nodesList []uint64, decisionsInView uint64, decisionsPerLeader uint64) uint64 {
	// Use the same deterministic approach as GetNodeRole
	leaderRotationIndex := view + (decisionsInView / decisionsPerLeader)
	viewIndex := int(leaderRotationIndex % totalNodes)

	// Use ComputeHierarchy for consistent role assignment
	_, _, followersOf, _ := ComputeHierarchy(int(totalNodes), viewIndex)

	// Find which secondary manages this follower
	followerIndex := -1
	for i, id := range nodesList {
		if id == followerID {
			followerIndex = i
			break
		}
	}

	if followerIndex == -1 {
		panic(fmt.Sprintf("followerID %d not found in nodesList %v", followerID, nodesList))
	}

	// Find the secondary that manages this follower
	for secIndex, followers := range followersOf {
		for _, follower := range followers {
			if follower == followerIndex {
				return nodesList[secIndex]
			}
		}
	}

	panic(fmt.Sprintf("follower %d not found in any secondary's follower list", followerID))
}

// GetFollowersForSecondary returns all followers managed by this secondary deterministically
func GetFollowersForSecondary(secondaryID uint64, view uint64, totalNodes uint64, nodesList []uint64, decisionsInView uint64, decisionsPerLeader uint64) []uint64 {
	// Use the same deterministic approach as GetNodeRole
	leaderRotationIndex := view + (decisionsInView / decisionsPerLeader)
	viewIndex := int(leaderRotationIndex % totalNodes)

	// Use ComputeHierarchy for consistent role assignment
	_, _, followersOf, _ := ComputeHierarchy(int(totalNodes), viewIndex)

	// Find secondary index
	secondaryIndex := -1
	for i, id := range nodesList {
		if id == secondaryID {
			secondaryIndex = i
			break
		}
	}

	if secondaryIndex == -1 {
		return []uint64{} // Not a secondary
	}

	// Get followers for this secondary
	followerIndices, exists := followersOf[secondaryIndex]
	if !exists {
		return []uint64{}
	}

	// Convert indices to node IDs
	followers := make([]uint64, len(followerIndices))
	for i, followerIndex := range followerIndices {
		followers[i] = nodesList[followerIndex]
	}

	return followers
}

type vote struct {
	*protos.Message
	sender uint64
}

type voteSet struct {
	validVote func(voter uint64, message *protos.Message) bool
	voted     map[uint64]struct{}
	votes     chan *vote
}

func (vs *voteSet) clear(n uint64) {
	// Drain the votes channel
	for len(vs.votes) > 0 {
		<-vs.votes
	}

	vs.voted = make(map[uint64]struct{}, n)
	vs.votes = make(chan *vote, n)
}

func (vs *voteSet) registerVote(voter uint64, message *protos.Message) {
	if !vs.validVote(voter, message) {
		return
	}

	_, hasVoted := vs.voted[voter]
	if hasVoted {
		// Received double vote
		return
	}

	vs.voted[voter] = struct{}{}
	vs.votes <- &vote{Message: message, sender: voter}
}

type nextViews struct {
	n map[uint64]uint64
}

func (nv *nextViews) clear() {
	nv.n = make(map[uint64]uint64)
}

func (nv *nextViews) registerNext(next uint64, sender uint64) {
	if next <= nv.n[sender] {
		return
	}

	nv.n[sender] = next
}

func (nv *nextViews) sendRecv(next uint64, sender uint64) bool {
	return next == nv.n[sender]
}

type incMsg struct {
	*protos.Message
	sender uint64
}

// computeQuorum calculates the quorums size Q, given a cluster size N.
//
// The calculation satisfies the following:
// Given a cluster size of N nodes, which tolerates f failures according to:
//
//	f = argmax ( N >= 3f+1 )
//
// Q is the size of the quorum such that:
//
//	any two subsets q1, q2 of size Q, intersect in at least f+1 nodes.
//
// Note that this is different from N-f (the number of correct nodes), when N=3f+3. That is, we have two extra nodes
// above the minimum required to tolerate f failures.
func computeQuorum(n uint64) (q int, f int) {
	f = (int(n) - 1) / 3
	q = int(math.Ceil((float64(n) + float64(f) + 1) / 2.0))
	return
}

// InFlightData records proposals that are in-flight,
// as well as their corresponding prepares.
type InFlightData struct {
	lock sync.RWMutex
	v    *inFlightProposalData
}

type inFlightProposalData struct {
	proposal *types.Proposal
	prepared bool
}

// InFlightProposal returns an in-flight proposal or nil if there is no such.
func (ifp *InFlightData) InFlightProposal() *types.Proposal {
	ifp.lock.RLock()
	defer ifp.lock.RUnlock()

	if ifp.v == nil {
		return nil
	}

	return ifp.v.proposal
}

// IsInFlightPrepared returns true if the in-flight proposal is prepared.
func (ifp *InFlightData) IsInFlightPrepared() bool {
	ifp.lock.RLock()
	defer ifp.lock.RUnlock()

	if ifp.v == nil {
		return false
	}

	return ifp.v.prepared
}

// StoreProposal stores an in-flight proposal.
func (ifp *InFlightData) StoreProposal(prop types.Proposal) {
	p := prop

	ifp.lock.Lock()
	defer ifp.lock.Unlock()

	ifp.v = &inFlightProposalData{proposal: &p}
}

// StorePrepares stores alongside the already stored in-flight proposal that it is prepared.
func (ifp *InFlightData) StorePrepares(view, seq uint64) {
	prop := ifp.InFlightProposal()
	if prop == nil {
		panic("stored prepares but proposal is not initialized")
	}
	p := prop

	ifp.lock.Lock()
	defer ifp.lock.Unlock()

	ifp.v = &inFlightProposalData{proposal: p, prepared: true}
}

func (ifp *InFlightData) clear() {
	ifp.lock.Lock()
	defer ifp.lock.Unlock()

	ifp.v = nil
}

// ProposalMaker implements ProposerBuilder
type ProposalMaker struct {
	DecisionsPerLeader uint64
	N                  uint64
	SelfID             uint64
	Decider            Decider
	FailureDetector    FailureDetector
	Sync               Synchronizer
	Logger             api.Logger
	MetricsBlacklist   *api.MetricsBlacklist
	MetricsView        *api.MetricsView
	Comm               Comm
	Verifier           api.Verifier
	Signer             api.Signer
	MembershipNotifier api.MembershipNotifier
	State              State
	InMsqQSize         int
	ViewSequences      *atomic.Value
	restoreOnceFromWAL sync.Once
	Checkpoint         *types.Checkpoint
}

// NewProposer returns a new view
func (pm *ProposalMaker) NewProposer(leader, proposalSequence, viewNum, decisionsInView uint64, quorumSize int) (proposer Proposer, phase Phase) {
	// For hierarchical consensus, use the PRIMARY node as the leader
	nodesList := pm.Comm.Nodes()
	hierarchicalPrimary := GetPrimaryNode(viewNum, pm.N, nodesList, decisionsInView, pm.DecisionsPerLeader)

	view := &View{
		RetrieveCheckpoint: pm.Checkpoint.Get,
		DecisionsPerLeader: pm.DecisionsPerLeader,
		N:                  pm.N,
		LeaderID:           hierarchicalPrimary, // Use hierarchical primary instead of traditional leader
		SelfID:             pm.SelfID,
		Quorum:             quorumSize,
		Number:             viewNum,
		Decider:            pm.Decider,
		FailureDetector:    pm.FailureDetector,
		Sync:               pm.Sync,
		Logger:             pm.Logger,
		Comm:               pm.Comm,
		Verifier:           pm.Verifier,
		Signer:             pm.Signer,
		MembershipNotifier: pm.MembershipNotifier,
		ProposalSequence:   proposalSequence,
		DecisionsInView:    decisionsInView,
		State:              pm.State,
		InMsgQSize:         pm.InMsqQSize,
		ViewSequences:      pm.ViewSequences,
		MetricsBlacklist:   pm.MetricsBlacklist,
		MetricsView:        pm.MetricsView,
	}

	view.ViewSequences.Store(ViewSequence{
		ViewActive:  true,
		ProposalSeq: proposalSequence,
	})

	pm.restoreOnceFromWAL.Do(func() {
		err := pm.State.Restore(view)
		if err != nil {
			pm.Logger.Panicf("Failed restoring view from WAL: %v", err)
		}
	})

	if proposalSequence > view.ProposalSequence {
		view.ProposalSequence = proposalSequence
		view.DecisionsInView = decisionsInView
	}

	if viewNum > view.Number {
		view.Number = viewNum
		view.DecisionsInView = decisionsInView
	}

	view.MetricsView.ViewNumber.Set(float64(view.Number))
	view.MetricsView.LeaderID.Set(float64(view.LeaderID))
	view.MetricsView.ProposalSequence.Set(float64(view.ProposalSequence))
	view.MetricsView.DecisionsInView.Set(float64(view.DecisionsInView))
	view.MetricsView.Phase.Set(float64(view.Phase))

	return view, view.Phase
}

// NodeRole defines the role of a node in hierarchical consensus
type NodeRole uint8

const (
	PRIMARY NodeRole = iota
	SECONDARY
	FOLLOWER
)

func (r NodeRole) String() string {
	switch r {
	case PRIMARY:
		return "PRIMARY"
	case SECONDARY:
		return "SECONDARY"
	case FOLLOWER:
		return "FOLLOWER"
	default:
		return "UNKNOWN"
	}
}

// HierarchicalInfo contains runtime hierarchical information for a node
type HierarchicalInfo struct {
	Role        NodeRole
	Secondary   uint64   // For followers, which secondary they belong to
	Followers   []uint64 // For secondaries, which followers they manage
	Secondaries []uint64 // For primary, which secondaries it manages
}

// ViewSequence indicates if a view is currently active and its current proposal sequence
type ViewSequence struct {
	ViewActive  bool
	ProposalSeq uint64
}

// MsgToString converts a given message to a printable string
func MsgToString(m *protos.Message) string {
	if m == nil {
		return "empty message"
	}
	switch m.GetContent().(type) {
	case *protos.Message_PrePrepare:
		return prePrepareToString(m.GetPrePrepare())
	case *protos.Message_NewView:
		return newViewToString(m.GetNewView())
	case *protos.Message_ViewData:
		return signedViewDataToString(m.GetViewData())
	case *protos.Message_HeartBeat:
		return heartBeatToString(m.GetHeartBeat())
	case *protos.Message_HeartBeatResponse:
		return heartBeatResponseToString(m.GetHeartBeatResponse())
	default:
		return m.String()
	}
}

func prePrepareToString(prp *protos.PrePrepare) string {
	if prp == nil {
		return "<empty PrePrepare>"
	}
	if prp.Proposal == nil {
		return fmt.Sprintf("<PrePrepare with view: %d, seq: %d, empty proposal>", prp.View, prp.Seq)
	}
	return fmt.Sprintf("<PrePrepare with view: %d, seq: %d, payload of %d bytes, header: %s>",
		prp.View, prp.Seq, len(prp.Proposal.Payload), base64.StdEncoding.EncodeToString(prp.Proposal.Header))
}

func newViewToString(nv *protos.NewView) string {
	if nv == nil || nv.SignedViewData == nil {
		return "<empty NewView>"
	}
	buff := bytes.Buffer{}
	buff.WriteString("< NewView with ")
	for i, svd := range nv.SignedViewData {
		buff.WriteString(signedViewDataToString(svd))
		if i == len(nv.SignedViewData)-1 {
			break
		}
		buff.WriteString(", ")
	}
	buff.WriteString(">")
	return buff.String()
}

func signedViewDataToString(svd *protos.SignedViewData) string {
	if svd == nil {
		return "empty ViewData"
	}
	vd := &protos.ViewData{}
	if err := proto.Unmarshal(svd.RawViewData, vd); err != nil {
		return fmt.Sprintf("<malformed viewdata from %d>", svd.Signer)
	}

	return fmt.Sprintf("<ViewData signed by %d with NextView: %d>",
		svd.Signer, vd.NextView)
}

func heartBeatToString(hb *protos.HeartBeat) string {
	if hb == nil {
		return "empty HeartBeat"
	}

	return fmt.Sprintf("<HeartBeat with view: %d, seq: %d", hb.View, hb.Seq)
}

func heartBeatResponseToString(hbr *protos.HeartBeatResponse) string {
	if hbr == nil {
		return "empty HeartBeatResponse"
	}

	return fmt.Sprintf("<HeartBeatResponse with view: %d", hbr.View)
}

type blacklist struct {
	currentLeader      uint64
	leaderRotation     bool
	prevMD             *protos.ViewMetadata
	n                  uint64
	nodes              []uint64
	currView           uint64
	preparesFrom       map[uint64]*protos.PreparesFrom
	logger             api.Logger
	metricsBlacklist   *api.MetricsBlacklist
	f                  int
	decisionsPerLeader uint64
}

func (bl blacklist) computeUpdate() []uint64 {
	newBlacklist := bl.prevMD.BlackList
	viewBeforeViewChanges := bl.prevMD.ViewId

	bl.logger.Debugf("view before: %d, current view: %d", viewBeforeViewChanges, bl.currView)

	// In case the previous view is different from this view, then we had a view change.
	// Thus, we need to add some nodes to the blacklist.
	if viewBeforeViewChanges != bl.currView {
		// If we are in the first proposal, then the leader ID of the previous view is not computed with an offset.
		// However, if we are in any subsequent proposal, then the previous leader's ID was computed by adding 1 to the
		// latest decisions in view that was committed.
		offset := uint64(1)
		if bl.prevMD.LatestSequence == 0 {
			offset = 0
		}

		// Locate every leader of all views previous to this views.
		for viewPreviousToThisView := viewBeforeViewChanges; viewPreviousToThisView < bl.currView; viewPreviousToThisView++ {
			bl.logger.Debugf("viewPreviousToThisView: %d, N: %d, Nodes: %v, rotation: %v, decisions in view: %d, decisions per leader: %d, blacklist: %v",
				viewPreviousToThisView, bl.n, bl.nodes, bl.leaderRotation, bl.prevMD.DecisionsInView, bl.decisionsPerLeader, bl.prevMD.BlackList)
			leaderID := getLeaderID(viewPreviousToThisView, bl.n, bl.nodes, bl.leaderRotation, bl.prevMD.DecisionsInView+offset, bl.decisionsPerLeader, bl.prevMD.BlackList)
			if leaderID == bl.currentLeader {
				bl.logger.Debugf("Skipping blacklisting current node (%d)", leaderID)
				continue
			}
			// Add that leader to the blacklist, because it did not drive any proposal, hence we skipped it because of view changes.
			newBlacklist = append(newBlacklist, leaderID)
			bl.logger.Infof("Blacklisting %d", leaderID)
		}
	} else {
		// We are in the same view, hence we can remove some nodes from the blacklist, if applicable,
		// because they helped us drive from the previous sequence to this sequence.
		// Compute the new blacklist according to your collected attestations on prepares sent
		// in previous round.
		newBlacklist = pruneBlacklist(newBlacklist, bl.preparesFrom, bl.f, bl.nodes, bl.logger)
	}

	// If blacklist is too big, remove items from its beginning
	for len(newBlacklist) > bl.f {
		bl.logger.Infof("Removing %d from %d sized blacklist due to size constraint", newBlacklist[0], len(newBlacklist))
		newBlacklist = newBlacklist[1:]
	}

	if len(bl.prevMD.BlackList) != len(newBlacklist) {
		bl.logger.Infof("Blacklist changed: %v --> %v", bl.prevMD.BlackList, newBlacklist)
	}

	newBlacklistMap := make(map[uint64]bool, len(newBlacklist))
	for _, node := range newBlacklist {
		newBlacklistMap[node] = true
	}
	for _, node := range bl.nodes {
		inBlacklist := newBlacklistMap[node]
		bl.metricsBlacklist.NodesInBlackList.With(
			bl.metricsBlacklist.LabelsForWith("blackid", strconv.FormatUint(node, 10))...,
		).Set(btoi(inBlacklist))
	}
	bl.metricsBlacklist.CountBlackList.Set(float64(len(newBlacklist)))

	return newBlacklist
}

func btoi(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// pruneBlacklist receives the previous blacklist, prepare acknowledgements from nodes, and returns
// the new blacklist such that a node that was observed by more than f observers is removed from the blacklist,
// and all nodes that no longer exist are also removed from the blacklist.
func pruneBlacklist(prevBlacklist []uint64, preparesFrom map[uint64]*protos.PreparesFrom, f int, nodes []uint64, logger api.Logger) []uint64 {
	if len(prevBlacklist) == 0 {
		logger.Debugf("Blacklist empty, nothing to prune")
		return prevBlacklist
	}
	logger.Debugf("Pruning blacklist %v with %d acknowledgements, f=%d, n=%d", prevBlacklist, len(preparesFrom), f, len(nodes))
	// Build a set of all nodes
	currentNodeIDs := make(map[uint64]struct{})
	for _, n := range nodes {
		currentNodeIDs[n] = struct{}{}
	}

	// For each sender of a prepare, count the number of commit signatures which acknowledge receiving a prepare from it.
	nodeID2Acks := make(map[uint64]int)
	for from, gotPrepareFrom := range preparesFrom {
		logger.Debugf("%d observed prepares from %v", from, gotPrepareFrom)
		for _, prepareSender := range gotPrepareFrom.Ids {
			nodeID2Acks[prepareSender]++
		}
	}

	var newBlackList []uint64
	for _, blackListedNode := range prevBlacklist {
		// Purge nodes that were removed by a reconfiguration
		if _, exists := currentNodeIDs[blackListedNode]; !exists {
			logger.Infof("Node %d no longer exists, removing it from the blacklist", blackListedNode)
			continue
		}

		// Purge nodes that have enough attestations of being alive
		observers := nodeID2Acks[blackListedNode]
		if observers > f {
			logger.Infof("Node %d was observed sending a prepare by %d nodes, removing it from blacklist", blackListedNode, observers)
			continue
		}
		newBlackList = append(newBlackList, blackListedNode)
	}

	return newBlackList
}

func equalIntLists(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func CommitSignaturesDigest(sigs []*protos.Signature) []byte {
	if len(sigs) == 0 {
		return nil
	}
	idb := IntDoubleBytes{}
	for _, sig := range sigs {
		s := IntDoubleByte{
			A: int64(sig.Signer),
			B: sig.Value,
			C: sig.Msg,
		}
		idb.A = append(idb.A, s)
	}

	serializedSignatures, err := asn1.Marshal(idb)
	if err != nil {
		panic(fmt.Sprintf("failed serializing signatures: %v", err))
	}

	h := sha256.New()
	h.Write(serializedSignatures)
	return h.Sum(nil)
}

type IntDoubleByte struct {
	A    int64
	B, C []byte
}

type IntDoubleBytes struct {
	A []IntDoubleByte
}

// GetHierarchicalInfo returns complete hierarchical information for a node
func GetHierarchicalInfo(nodeID uint64, view uint64, totalNodes uint64, nodesList []uint64, decisionsInView uint64, decisionsPerLeader uint64) *HierarchicalInfo {
	role := GetNodeRole(nodeID, view, totalNodes, nodesList, decisionsInView, decisionsPerLeader)
	info := &HierarchicalInfo{
		Role: role,
	}

	switch role {
	case PRIMARY:
		info.Secondaries = GetSecondaryNodes(view, totalNodes, nodesList, decisionsInView, decisionsPerLeader)
	case SECONDARY:
		info.Followers = GetFollowersForSecondary(nodeID, view, totalNodes, nodesList, decisionsInView, decisionsPerLeader)
	case FOLLOWER:
		info.Secondary = GetSecondaryForFollower(nodeID, view, totalNodes, nodesList, decisionsInView, decisionsPerLeader)
	}

	return info
}

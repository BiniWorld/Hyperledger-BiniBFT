package consensus

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
)

var (
	ErrReqAlreadyExists    = fmt.Errorf("request already exists")
	ErrReqAlreadyProcessed = fmt.Errorf("request already processed")
	ErrRequestTooBig       = fmt.Errorf("submitted request is too big")
	ErrSubmitTimeout       = fmt.Errorf("timeout submitting to request pool")
)

// RequestPoolInterface defines the interface for request pools
type RequestPoolInterface interface {
	Submit(request []byte) error
	GetRequest(requestID string) (*Request, bool)
	RemoveRequest(requestID string) error
	UpdateRequestPhase(requestID string, phase RequestPhase)
	Size() int
	Close()
	GetRequestsByPhase(phase RequestPhase) []*Request
	NextRequests(maxCount int, maxSizeBytes uint64, check bool) (batch [][]byte, full bool)
}

type requestItem struct {
	request           []byte
	timeout           *time.Timer
	additionTimestamp time.Time
}

// RequestPool manages pending requests with enhanced features
type RequestPool struct {
	requests      map[string]*Request
	mu            sync.RWMutex
	maxSize       uint64
	timeouts      map[string]*time.Timer
	logger        Logger
	closed        bool
	fifo          *list.List
	inspector     RequestInspector
	existMap      map[RequestInfo]*list.Element
	delMap        map[RequestInfo]struct{}
	submittedChan chan struct{}
	sizeBytes     uint64
	network       NetworkInterface
	nodeID        NodeID
	role          NodeRole
	primaryLeader NodeID
}

// RequestPoolOptions for configuring the request pool
type RequestPoolOptions struct {
	MaxSize       uint64
	Logger        Logger
	Inspector     RequestInspector
	submittedChan chan struct{}
	Network       NetworkInterface
	NodeID        NodeID
	Role          NodeRole
	PrimaryLeader NodeID
}

// NewRequestPool creates a new request pool
func NewRequestPool() *RequestPool {
	return NewRequestPoolWithOptions(RequestPoolOptions{
		MaxSize: 10000, // Default max size
	})
}

// NewRequestPoolWithOptions creates a new request pool with options
func NewRequestPoolWithOptions(opts RequestPoolOptions) *RequestPool {
	if opts.MaxSize <= 0 {
		opts.MaxSize = 10000
	}

	return &RequestPool{
		requests:      make(map[string]*Request),
		maxSize:       opts.MaxSize,
		timeouts:      make(map[string]*time.Timer),
		logger:        opts.Logger,
		closed:        false,
		fifo:          list.New(),
		inspector:     opts.Inspector,
		submittedChan: opts.submittedChan,
		network:       opts.Network,
		nodeID:        opts.NodeID,
		role:          opts.Role,
		primaryLeader: opts.PrimaryLeader,
		existMap:      make(map[RequestInfo]*list.Element),
		delMap:        make(map[RequestInfo]struct{}),
	}
}

// Submit adds a request to the pool or forwards it to the primary leader
func (rp *RequestPool) Submit(request []byte) error {
	reqInfo := rp.inspector.RequestID(request)

	if rp.isClosed() {
		return errors.Errorf("pool closed, request rejected: %s", reqInfo)
	}

	if uint64(len(request)) > rp.maxSize {
		return fmt.Errorf(
			"submitted request (%d) is bigger than request max bytes (%d)",
			len(request),
			rp.maxSize,
		)
	}

	// If this node is not the primary leader, forward the request to the primary leader
	if rp.role != RolePrimaryLeader && rp.network != nil && rp.primaryLeader != "" && rp.primaryLeader != rp.nodeID {
		rp.logger.Info("Forwarding request to primary leader",
			"nodeID", rp.nodeID,
			"role", rp.role.String(),
			"primaryLeader", rp.primaryLeader,
			"reqInfo", reqInfo)

		return rp.network.SendTransaction(rp.primaryLeader, request)
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()

	if uint64(len(rp.requests)) >= rp.maxSize {
		return fmt.Errorf(
			"submitted request (%d) is bigger than request max bytes (%d)",
			len(request),
			rp.maxSize,
		)
	}

	_, alreadyExists := rp.existMap[reqInfo]
	_, alreadyDelete := rp.delMap[reqInfo]

	if alreadyExists {
		rp.logger.Debug("request already exists in the pool", "reqInfo", reqInfo)
		return ErrReqAlreadyExists
	}

	if alreadyDelete {
		rp.logger.Debug("request %s already processed", "reqInfo", reqInfo)
		return ErrReqAlreadyProcessed
	}

	reqItem := &requestItem{
		request: request,
		// timeout:           to,
		additionTimestamp: time.Now(),
	}

	element := rp.fifo.PushBack(reqItem)
	rp.existMap[reqInfo] = element

	// Verify consistency after adding
	if len(rp.existMap) != rp.fifo.Len() {
		rp.logger.Error("RequestPool map and list are of different length after adding",
			"map", len(rp.existMap),
			"list", rp.fifo.Len(),
			"reqInfo", reqInfo)
		// Try to fix the inconsistency by removing the element we just added
		rp.fifo.Remove(element)
		delete(rp.existMap, reqInfo)
		return fmt.Errorf("internal consistency error in request pool")
	}

	rp.logger.Debug("Request submitted to local pool", "reqInfo", reqInfo, "nodeID", rp.nodeID)

	// notify that a request was submitted
	select {
	case rp.submittedChan <- struct{}{}:
	default:
	}

	rp.sizeBytes += uint64(len(element.Value.(*requestItem).request))

	return nil
}

// GetRequest retrieves a request from the pool
func (rp *RequestPool) GetRequest(requestID string) (*Request, bool) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	req, exists := rp.requests[requestID]
	return req, exists
}

// UpdateRequestPhase updates the phase of a request
func (rp *RequestPool) UpdateRequestPhase(requestID string, phase RequestPhase) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if req, exists := rp.requests[requestID]; exists {
		oldPhase := req.Phase
		req.Phase = phase
		if rp.logger != nil {
			rp.logger.Debug("Updated request phase", "requestID", requestID, "oldPhase", oldPhase.String(), "newPhase", phase.String())
		}
	}
}

// RemoveRequest removes a request from the pool
func (rp *RequestPool) RemoveRequest(requestID string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if _, exists := rp.requests[requestID]; !exists {
		return fmt.Errorf("request %s not found", requestID)
	}

	delete(rp.requests, requestID)

	// Clean up timeout if exists
	if timer, exists := rp.timeouts[requestID]; exists {
		timer.Stop()
		delete(rp.timeouts, requestID)
	}

	if rp.logger != nil {
		rp.logger.Debug("Removed request from pool", "requestID", requestID)
	}

	return nil
}

// Size returns the number of requests in the pool
func (rp *RequestPool) Size() int {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	return len(rp.requests)
}

// Close closes the request pool
func (rp *RequestPool) Close() {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	rp.closed = true

	// Stop all timers
	for _, timer := range rp.timeouts {
		timer.Stop()
	}

	// Clear all data
	rp.requests = make(map[string]*Request)
	rp.timeouts = make(map[string]*time.Timer)

	if rp.logger != nil {
		rp.logger.Debug("Request pool closed")
	}
}

// GetRequestsByPhase returns all requests in a specific phase
func (rp *RequestPool) GetRequestsByPhase(phase RequestPhase) []*Request {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	var requests []*Request
	for _, req := range rp.requests {
		if req.Phase == phase {
			requests = append(requests, req)
		}
	}
	return requests
}

func (rp *RequestPool) NextRequests(maxCount int, maxSizeBytes uint64, check bool) (batch [][]byte, full bool) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	count := minInt(rp.fifo.Len(), maxCount)
	var totalSize uint64
	batch = make([][]byte, 0, count)
	var elementsToRemove []*list.Element
	var requestInfosToRemove []RequestInfo
	element := rp.fifo.Front()

	for i := 0; i < count && element != nil; i++ {
		req := element.Value.(*requestItem).request
		reqLen := uint64(len(req))
		if totalSize+reqLen > maxSizeBytes {
			rp.logger.Debug(fmt.Sprintf("Returning batch of %d requests totalling %dB as it exceeds threshold of %dB",
				len(batch), totalSize, maxSizeBytes))
			break
		}
		batch = append(batch, req)
		totalSize += reqLen

		// If this is not just a check, mark element for removal
		if !check {
			elementsToRemove = append(elementsToRemove, element)
			reqInfo := rp.inspector.RequestID(req)
			requestInfosToRemove = append(requestInfosToRemove, reqInfo)
		}

		element = element.Next()
	}

	// Remove elements from FIFO and existMap if this was not just a check
	if !check {
		for i, elem := range elementsToRemove {
			rp.fifo.Remove(elem)
			rp.sizeBytes -= uint64(len(elem.Value.(*requestItem).request))

			// Also remove from existMap and add to delMap to maintain consistency
			reqInfo := requestInfosToRemove[i]
			delete(rp.existMap, reqInfo)
			rp.delMap[reqInfo] = struct{}{}
		}

		// Verify consistency after removal
		if len(rp.existMap) != rp.fifo.Len() {
			rp.logger.Error("RequestPool map and list are of different length after removal",
				"map", len(rp.existMap),
				"list", rp.fifo.Len())
		}
	}

	fullS := totalSize >= maxSizeBytes
	fullC := len(batch) == maxCount
	full = fullS || fullC
	if len(batch) > 0 {
		rp.logger.Debug(fmt.Sprintf("Returning batch of %d requests totalling %dB",
			len(batch), totalSize))
	}
	return batch, full
}

func (rp *RequestPool) isClosed() bool {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	return rp.closed
}

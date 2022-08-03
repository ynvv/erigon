package engineapi

import (
	"sync"
	"sync/atomic"

	"github.com/emirpasic/gods/maps/treemap"

	"github.com/ledgerwatch/erigon-lib/gointerfaces/remote"
	"github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/core/types"
)

// This is the status of a newly execute block.
// Hash: Block hash
// Status: block's status
type PayloadStatus struct {
	Status          remote.EngineStatus
	LatestValidHash common.Hash
	ValidationError error
	CriticalError   error
}

// The message we are going to send to the stage sync in ForkchoiceUpdated
type ForkChoiceMessage struct {
	HeadBlockHash      common.Hash
	SafeBlockHash      common.Hash
	FinalizedBlockHash common.Hash
}

type RequestStatus int

const ( // RequestStatus values
	StatusNew = iota
	StatusSyncing
	StatusSynced
)

type RequestWithStatus struct {
	Message interface{} // *Block or *ForkChoiceMessage
	Status  RequestStatus
}

type Interrupt int

const ( // Interrupt values
	InterruptNone = iota
	InterruptSynced
	InterruptStopping
)

type RequestList struct {
	requestId int
	requests  *treemap.Map // map[int]*RequestWithStatus
	interrupt Interrupt
	waiting   uint32
	syncCond  *sync.Cond
}

func NewRequestList() *RequestList {
	rl := &RequestList{
		requests: treemap.NewWithIntComparator(),
		syncCond: sync.NewCond(&sync.Mutex{}),
	}
	return rl
}

func (rl *RequestList) AddPayloadRequest(message *types.Block) {
	rl.syncCond.L.Lock()
	defer rl.syncCond.L.Unlock()

	rl.requestId++

	rl.requests.Put(rl.requestId, &RequestWithStatus{
		Message: message,
		Status:  StatusNew,
	})

	rl.syncCond.Broadcast()
}

func (rl *RequestList) AddForkChoiceRequest(message *ForkChoiceMessage) {
	rl.syncCond.L.Lock()
	defer rl.syncCond.L.Unlock()

	rl.requestId++

	// purge previous fork choices that are still syncing
	rl.requests = rl.requests.Select(func(key interface{}, value interface{}) bool {
		req := value.(*RequestWithStatus)
		_, isForkChoice := req.Message.(*ForkChoiceMessage)
		return req.Status == StatusNew || !isForkChoice
	})
	// TODO(yperbasis): potentially purge some non-syncing old fork choices?

	rl.requests.Put(rl.requestId, &RequestWithStatus{
		Message: message,
		Status:  StatusNew,
	})

	rl.syncCond.Broadcast()
}

func (rl *RequestList) firstRequest() (id int, request *RequestWithStatus) {
	foundKey, foundValue := rl.requests.Min()
	if foundKey == nil {
		return 0, nil
	}
	id = foundKey.(int)
	request = foundValue.(*RequestWithStatus)
	if request.Status == StatusSyncing {
		// wait for sync to finish
		return 0, nil
	}
	return
}

func (rl *RequestList) WaitForRequest(noWait bool) (interrupt Interrupt, id int, request *RequestWithStatus) {
	rl.syncCond.L.Lock()
	defer rl.syncCond.L.Unlock()

	atomic.StoreUint32(&rl.waiting, 1)
	defer atomic.StoreUint32(&rl.waiting, 0)

	for {
		interrupt = rl.interrupt
		if interrupt != InterruptNone {
			if interrupt != InterruptStopping {
				// clear the interrupt
				rl.interrupt = InterruptNone
			}
			return
		}
		id, request = rl.firstRequest()
		if request != nil || noWait {
			return
		}
		rl.syncCond.Wait()
	}
}

func (rl *RequestList) IsWaiting() bool {
	return atomic.LoadUint32(&rl.waiting) != 0
}

func (rl *RequestList) Interrupt(kind Interrupt) {
	rl.syncCond.L.Lock()
	defer rl.syncCond.L.Unlock()

	rl.interrupt = kind

	rl.syncCond.Broadcast()
}

func (rl *RequestList) Remove(id int) {
	rl.syncCond.L.Lock()
	defer rl.syncCond.L.Unlock()

	rl.requests.Remove(id)
	// no need to broadcast
}

func (rl *RequestList) SetStatus(id int, status RequestStatus) {
	rl.syncCond.L.Lock()
	defer rl.syncCond.L.Unlock()

	value, found := rl.requests.Get(id)
	if found {
		value.(*RequestWithStatus).Status = status
	}

	rl.syncCond.Broadcast()
}

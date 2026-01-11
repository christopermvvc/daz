package eventbus

import (
	"container/heap"
	"sync"
	"time"
)

// priorityMessage wraps an eventMessage with priority and sequence information
type priorityMessage struct {
	*eventMessage
	priority  int
	sequence  int64 // For FIFO ordering within same priority
	timestamp time.Time
}

// priorityQueue implements heap.Interface for priority-based message delivery
type priorityQueue []*priorityMessage

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// Higher priority values should come first
	if pq[i].priority != pq[j].priority {
		return pq[i].priority > pq[j].priority
	}
	// Within same priority, maintain FIFO order using sequence
	return pq[i].sequence < pq[j].sequence
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *priorityQueue) Push(x interface{}) {
	item := x.(*priorityMessage)
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*pq = old[0 : n-1]
	return item
}

// messageQueue manages a priority queue with thread safety
type messageQueue struct {
	mu       sync.Mutex
	pq       priorityQueue
	sequence int64
	cond     *sync.Cond
	closed   bool
	maxSize  int
}

// newMessageQueue creates a new priority-based message queue
func newMessageQueue(maxSize int) *messageQueue {
	mq := &messageQueue{
		pq:      make(priorityQueue, 0),
		maxSize: maxSize,
	}
	mq.cond = sync.NewCond(&mq.mu)
	heap.Init(&mq.pq)
	return mq
}

// push adds a message to the queue with the given priority
func (mq *messageQueue) push(msg *eventMessage, priority int) bool {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	if mq.closed {
		return false
	}
	if mq.maxSize > 0 && len(mq.pq) >= mq.maxSize {
		return false
	}

	// Increment sequence for FIFO ordering
	mq.sequence++

	pm := &priorityMessage{
		eventMessage: msg,
		priority:     priority,
		sequence:     mq.sequence,
		timestamp:    time.Now(),
	}

	heap.Push(&mq.pq, pm)
	mq.cond.Signal() // Wake up waiting goroutines
	return true
}

// pop removes and returns the highest priority message
// It blocks if the queue is empty and returns nil, false if closed
func (mq *messageQueue) pop() (*eventMessage, bool) {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	// Wait while queue is empty and not closed
	for len(mq.pq) == 0 && !mq.closed {
		mq.cond.Wait()
	}

	if len(mq.pq) == 0 && mq.closed {
		return nil, false
	}

	pm := heap.Pop(&mq.pq).(*priorityMessage)
	return pm.eventMessage, true
}

// close closes the queue and wakes up waiting goroutines
func (mq *messageQueue) close() {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	mq.closed = true
	mq.cond.Broadcast() // Wake up all waiting goroutines
}

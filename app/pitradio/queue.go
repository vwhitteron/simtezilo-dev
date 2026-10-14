package pitradio

import (
	"errors"
	"sync"
	"time"
)

const (
	QueueBurst            = 5                // Token bucket size: messages allowed back to back
	QueueRefillInterval   = 10 * time.Second // One rate limit token is restored per interval
	QueueMaxAge           = 45 * time.Second // Messages older than this are dropped unplayed
	QueuePriorityHeadroom = 2                // Extra slots reserved for rate limit exempt messages

	// QueueCapacity is the most messages that can be held at once: the burst, the
	// refills that fit within the maximum age, and headroom for priority messages.
	QueueCapacity = QueueBurst + int((QueueMaxAge+QueueRefillInterval-1)/QueueRefillInterval) + QueuePriorityHeadroom
)

// ErrRateLimited is returned by Push when a non-priority message exceeds the rate limit.
var ErrRateLimited = errors.New("pit radio rate limit reached")

// queueEntry is a queued message and the time it was queued.
type queueEntry struct {
	msg      Message
	queuedAt time.Time
}

// Queue is a bounded, rate-limited, expiring FIFO of pit radio messages. Push
// never blocks, so a stalled consumer cannot stall the producer.
type Queue struct {
	mu         sync.Mutex
	entries    [QueueCapacity]queueEntry // Ring buffer of queued messages
	head       int                       // Index of the oldest entry
	count      int                       // Number of entries held
	tokens     float64                   // Rate limit tokens available
	lastRefill time.Time                 // Time the tokens were last refilled
	ready      chan struct{}             // Signalled when a message may be available
	now        func() time.Time          // Clock, replaceable in tests
}

// NewQueue creates an empty queue with a full token bucket.
func NewQueue() *Queue {
	queue := &Queue{
		tokens: QueueBurst,
		ready:  make(chan struct{}, 1),
		now:    time.Now,
	}
	queue.lastRefill = queue.now()

	return queue
}

// Push adds a message to the queue. Non-priority messages are rejected with
// ErrRateLimited when the token bucket is empty. It never blocks.
func (pq *Queue) Push(msg Message) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	now := pq.now()

	// Purging here keeps the depth bound true even when the consumer is stuck
	// (e.g. a blocked Discord session open) and never calls Pop.
	pq.purgeExpired(now)

	if !msg.Priority {
		elapsed := now.Sub(pq.lastRefill)
		if elapsed > 0 {
			pq.tokens = min(QueueBurst, pq.tokens+float64(elapsed)/float64(QueueRefillInterval))
			pq.lastRefill = now
		}

		if pq.tokens < 1 {
			return ErrRateLimited
		}

		pq.tokens--
	}

	// Safety net: the rate limit and expiry should keep the queue below capacity.
	if pq.count == QueueCapacity {
		pq.dropHead()
	}

	pq.entries[(pq.head+pq.count)%QueueCapacity] = queueEntry{msg: msg, queuedAt: now}
	pq.count++

	pq.signal()

	return nil
}

// Pop removes and returns the oldest unexpired message, or false when none remain.
func (pq *Queue) Pop() (Message, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.purgeExpired(pq.now())

	if pq.count == 0 {
		return Message{}, false
	}

	msg := pq.entries[pq.head].msg
	pq.dropHead()

	if pq.count > 0 {
		pq.signal()
	}

	return msg, true
}

// Ready returns a channel that is signalled when a message may be available.
func (pq *Queue) Ready() <-chan struct{} {
	return pq.ready
}

// Len returns the number of queued messages.
func (pq *Queue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	return pq.count
}

// purgeExpired drops expired entries from the head. Entries are in time order,
// so expired ones are always at the head. The caller must hold the lock.
func (pq *Queue) purgeExpired(now time.Time) {
	for pq.count > 0 && now.Sub(pq.entries[pq.head].queuedAt) > QueueMaxAge {
		pq.dropHead()
	}
}

// dropHead removes the oldest entry. The caller must hold the lock.
func (pq *Queue) dropHead() {
	pq.entries[pq.head] = queueEntry{}
	pq.head = (pq.head + 1) % QueueCapacity
	pq.count--
}

// signal wakes the consumer without blocking.
func (pq *Queue) signal() {
	select {
	case pq.ready <- struct{}{}:
	default:
	}
}

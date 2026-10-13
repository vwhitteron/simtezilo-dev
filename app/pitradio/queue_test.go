package pitradio //nolint:testpackage // white-box testing to inject a fake clock

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a controllable clock for queue tests.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestQueue creates a queue driven by a fake clock.
func newTestQueue() (*Queue, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	queue := NewQueue()
	queue.now = clock.now
	queue.lastRefill = clock.t

	return queue, clock
}

func testQueueBurst(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, _ := newTestQueue()

	// Act
	for range QueueBurst {
		require.NoError(t, queue.Push(Message{Text: "x"}))
	}

	err := queue.Push(Message{Text: "x"})

	// Assert
	require.ErrorIs(t, err, ErrRateLimited)
	assert.Equal(t, QueueBurst, queue.Len())
}

func testQueueRefill(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, clock := newTestQueue()

	for range QueueBurst {
		require.NoError(t, queue.Push(Message{Text: "x"}))
	}

	// Act
	clock.advance(QueueRefillInterval)

	errFirst := queue.Push(Message{Text: "x"})
	errSecond := queue.Push(Message{Text: "x"})

	// Assert
	require.NoError(t, errFirst)
	require.ErrorIs(t, errSecond, ErrRateLimited)
}

func testQueuePriorityBypass(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, clock := newTestQueue()

	for range QueueBurst {
		require.NoError(t, queue.Push(Message{Text: "x"}))
	}

	// Act
	errPriority := queue.Push(Message{Text: "p", Priority: true})
	errPriorityAgain := queue.Push(Message{Text: "p", Priority: true})

	clock.advance(QueueRefillInterval)

	errNormal := queue.Push(Message{Text: "x"})

	// Assert
	require.NoError(t, errPriority)
	require.NoError(t, errPriorityAgain)
	require.NoError(t, errNormal, "priority pushes must not consume tokens")
}

func testQueuePopExpiry(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, clock := newTestQueue()
	require.NoError(t, queue.Push(Message{Text: "old"}))
	clock.advance(QueueMaxAge + time.Second)

	// Act
	_, found := queue.Pop()

	// Assert
	assert.False(t, found)
	assert.Equal(t, 0, queue.Len())
}

func testQueuePushExpiry(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, clock := newTestQueue()
	require.NoError(t, queue.Push(Message{Text: "old1"}))
	require.NoError(t, queue.Push(Message{Text: "old2"}))
	clock.advance(QueueMaxAge + time.Second)

	// Act
	require.NoError(t, queue.Push(Message{Text: "new"}))

	// Assert
	assert.Equal(t, 1, queue.Len())

	msg, found := queue.Pop()
	require.True(t, found)
	assert.Equal(t, "new", msg.Text)
}

func testQueueCapacity(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, _ := newTestQueue()
	total := QueueCapacity + 5

	// Act
	for i := range total {
		require.NoError(t, queue.Push(Message{Text: strconv.Itoa(i), Priority: true}))
	}

	// Assert
	assert.Equal(t, QueueCapacity, queue.Len())

	msg, found := queue.Pop()
	require.True(t, found)
	assert.Equal(t, strconv.Itoa(total-QueueCapacity), msg.Text, "oldest entries are dropped")
}

func testQueueReady(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, _ := newTestQueue()

	require.NoError(t, queue.Push(Message{Text: "a"}))
	require.NoError(t, queue.Push(Message{Text: "b"}))

	// Act & Assert - push signals
	select {
	case <-queue.Ready():
	default:
		require.Fail(t, "push did not signal ready")
	}

	// Act & Assert - pop with remaining items re-signals
	_, found := queue.Pop()
	require.True(t, found)

	select {
	case <-queue.Ready():
	default:
		require.Fail(t, "pop with remaining items did not signal ready")
	}

	// Act & Assert - pop of the last item does not signal
	_, found = queue.Pop()
	require.True(t, found)

	select {
	case <-queue.Ready():
		require.Fail(t, "pop of the last item signalled ready")
	default:
	}
}

func testQueueFIFO(t *testing.T) {
	t.Parallel()

	// Arrange
	queue, _ := newTestQueue()

	for i := range QueueBurst {
		require.NoError(t, queue.Push(Message{Text: strconv.Itoa(i)}))
	}

	// Act & Assert
	for i := range QueueBurst {
		msg, found := queue.Pop()
		require.True(t, found)
		assert.Equal(t, strconv.Itoa(i), msg.Text)
	}
}

func testQueueCapacityValue(t *testing.T) {
	t.Parallel()

	// Assert
	assert.Equal(t, 12, QueueCapacity)
}

func TestQueue(t *testing.T) {
	t.Parallel()

	t.Run("testQueueBurst", testQueueBurst)
	t.Run("testQueueRefill", testQueueRefill)
	t.Run("testQueuePriorityBypass", testQueuePriorityBypass)
	t.Run("testQueuePopExpiry", testQueuePopExpiry)
	t.Run("testQueuePushExpiry", testQueuePushExpiry)
	t.Run("testQueueCapacity", testQueueCapacity)
	t.Run("testQueueReady", testQueueReady)
	t.Run("testQueueFIFO", testQueueFIFO)
	t.Run("testQueueCapacityValue", testQueueCapacityValue)
}

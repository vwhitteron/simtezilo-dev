package timesync //nolint:testpackage // needs statusFn to inject a fake status

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func withStatusFn(t *testing.T, fn func() State) {
	t.Helper()

	original := statusFn
	statusFn = fn

	t.Cleanup(func() {
		statusFn = original
	})
}

func TestWaitReturnsImmediatelyWhenAlreadySynced(t *testing.T) { //nolint:paralleltest // mutates the shared statusFn package var
	withStatusFn(t, func() State { return StateSynced })

	got := Wait(context.Background(), 100*time.Millisecond, 10*time.Millisecond, zerolog.Nop())
	if got != StateSynced {
		t.Errorf("Wait() = %v, want %v", got, StateSynced)
	}
}

func TestWaitReturnsImmediatelyOnUnknown(t *testing.T) { //nolint:paralleltest // mutates the shared statusFn package var
	withStatusFn(t, func() State { return StateUnknown })

	got := Wait(context.Background(), 100*time.Millisecond, 10*time.Millisecond, zerolog.Nop())
	if got != StateUnknown {
		t.Errorf("Wait() = %v, want %v", got, StateUnknown)
	}
}

func TestWaitPollsUntilSynced(t *testing.T) { //nolint:paralleltest // mutates the shared statusFn package var
	var calls atomic.Int32

	withStatusFn(t, func() State {
		if calls.Add(1) >= 3 {
			return StateSynced
		}

		return StateUnsynced
	})

	got := Wait(context.Background(), 100*time.Millisecond, 10*time.Millisecond, zerolog.Nop())
	if got != StateSynced {
		t.Errorf("Wait() = %v, want %v", got, StateSynced)
	}
}

func TestWaitTimesOutWhenNeverSynced(t *testing.T) { //nolint:paralleltest // mutates the shared statusFn package var
	withStatusFn(t, func() State { return StateUnsynced })

	got := Wait(context.Background(), 100*time.Millisecond, 10*time.Millisecond, zerolog.Nop())
	if got != StateUnsynced {
		t.Errorf("Wait() = %v, want %v", got, StateUnsynced)
	}
}

func TestWaitReturnsUnsyncedWhenContextCancelled(t *testing.T) { //nolint:paralleltest // mutates the shared statusFn package var
	withStatusFn(t, func() State { return StateUnsynced })

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	got := Wait(ctx, 1*time.Hour, 10*time.Millisecond, zerolog.Nop())
	if got != StateUnsynced {
		t.Errorf("Wait() = %v, want %v", got, StateUnsynced)
	}
}

package tuneassist //nolint:testpackage // reaches the unexported acquire method

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestServiceAcquireBlocksSecondCallerUntilRelease checks that the heavy job slot
// admits only one caller at a time, and that releasing it unblocks a waiting one.
func TestServiceAcquireBlocksSecondCallerUntilRelease(t *testing.T) {
	t.Parallel()

	// Arrange
	svc := New(Options{Log: zerolog.Nop(), ReplayDir: func() string { return t.TempDir() }})

	firstRelease, err := svc.acquire(context.Background())
	require.NoError(t, err)

	secondDone := make(chan error, 1)

	// Act
	go func() {
		_, acquireErr := svc.acquire(context.Background())
		secondDone <- acquireErr
	}()

	select {
	case <-secondDone:
		t.Fatal("second acquire returned before the first was released")
	case <-time.After(50 * time.Millisecond):
	}

	firstRelease()

	// Assert
	select {
	case err = <-secondDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second acquire did not return after release")
	}
}

// TestServiceAcquireCancelledContextLeavesSlotFree checks that a caller whose
// context is cancelled before the slot is free gets ctx.Err(), and that the slot
// remains free for a later caller.
func TestServiceAcquireCancelledContextLeavesSlotFree(t *testing.T) {
	t.Parallel()

	// Arrange
	svc := New(Options{Log: zerolog.Nop(), ReplayDir: func() string { return t.TempDir() }})

	release, err := svc.acquire(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	blockedRelease, acquireErr := svc.acquire(ctx)

	// Assert
	require.Nil(t, blockedRelease)
	require.ErrorIs(t, acquireErr, context.Canceled)

	release()

	freeRelease, err := svc.acquire(context.Background())
	require.NoError(t, err)
	freeRelease()
}

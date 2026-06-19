package app //nolint:testpackage // white-box testing

import (
	"context"
	"image/color"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/fancontroller"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

// TestFanModeIconColor pins the mode/control-state colour matrix: grey whenever
// the device holds control, and the mode's active colour (indigo for open, deep
// purple for all) when the app drives the fan. Manual never drives, so it stays grey.
func TestFanModeIconColor(t *testing.T) {
	t.Parallel()

	grey := gui.MaterialGrey()
	indigo := gui.MaterialIndigo()
	deepPurple := gui.MaterialDeepPurple()

	cases := []struct {
		name        string
		mode        string
		hostControl bool
		want        color.Color
	}{
		{"manual is grey under manual control", "manual", false, grey},
		{"manual is grey under host control", "manual", true, grey},
		{"open is grey under manual control", "open", false, grey},
		{"open is indigo under host control", "open", true, indigo},
		{"all is grey under manual control", "all", false, grey},
		{"all is deep purple under host control", "all", true, deepPurple},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, fanModeIconColor(tc.mode, tc.hostControl))
		})
	}
}

// fakeFanClient is an in-memory implementation of fanClient used by tests.
type fakeFanClient struct {
	mu            sync.Mutex
	dutyWrites    []int
	displayImages int
	displayStarts int
	displayCancel int
	displayBlock  bool
	takeCount     int
	releaseCount  int
	disc          chan struct{}
}

func newFakeFanClient() *fakeFanClient {
	return &fakeFanClient{
		disc: make(chan struct{}),
	}
}

func (f *fakeFanClient) Connect(_ context.Context) error { return nil }

func (f *fakeFanClient) Close() error { return nil }

func (f *fakeFanClient) DeviceAddress() string { return "fake" }

func (f *fakeFanClient) SetFanDuty(_ context.Context, duty int) (int, error) {
	f.mu.Lock()
	f.dutyWrites = append(f.dutyWrites, duty)
	f.mu.Unlock()

	return duty, nil
}

func (f *fakeFanClient) TakeControl(_ context.Context) error {
	f.mu.Lock()
	f.takeCount++
	f.mu.Unlock()

	return nil
}

func (f *fakeFanClient) ReleaseControl(_ context.Context) error {
	f.mu.Lock()
	f.releaseCount++
	f.mu.Unlock()

	return nil
}

func (f *fakeFanClient) SetDisplayImage(ctx context.Context, _ []byte, _, _ int) error {
	f.mu.Lock()
	f.displayStarts++
	block := f.displayBlock
	f.mu.Unlock()

	// When blocking is enabled, model a slow transfer that only ends when the
	// upload is preempted/cancelled, so preemption can be observed.
	if block {
		<-ctx.Done()

		f.mu.Lock()
		f.displayCancel++
		f.mu.Unlock()

		return ctx.Err()
	}

	f.mu.Lock()
	f.displayImages++
	f.mu.Unlock()

	return nil
}

func (f *fakeFanClient) Disconnected() <-chan struct{} {
	return f.disc
}

// duties returns a copy of all recorded duty writes.
func (f *fakeFanClient) duties() []int {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]int, len(f.dutyWrites))
	copy(out, f.dutyWrites)

	return out
}

// counts returns the number of TakeControl and ReleaseControl calls.
func (f *fakeFanClient) counts() (take, release int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.takeCount, f.releaseCount
}

// newTestApp builds the minimal App needed to exercise runFanControlDutyCycle.
func newTestApp(t *testing.T, fake *fakeFanClient) (*App, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	// Use default config — GetFanCommandTimeoutMs returns 1000 ms by default,
	// so context.WithTimeout in the duty cycle is never instantly expired.
	cfg := config.NewFromJSON([]byte(`{}`), zerolog.Nop())

	a := &App{
		ctx:            ctx,
		config:         cfg,
		log:            zerolog.Nop(),
		i18n:           createTestI18n(),
		fanControlChan: make(chan fanCommand, 1),
		fanEventChan:   make(chan fancontroller.Event, 8),
		fanController:  fake,
	}

	return a, cancel
}

func TestRunFanControlDutyCycleDrivesAndReleases(t *testing.T) {
	t.Parallel()

	fake := newFakeFanClient()
	a, cancel := newTestApp(t, fake)

	retCh := make(chan bool, 1)

	go func() {
		retCh <- a.runFanControlDutyCycle()
	}()

	// A drive command takes control of the fan and writes the duty.
	a.fanControlChan <- fanCommand{drive: true, duty: 42}

	require.Eventually(t, func() bool {
		ds := fake.duties()

		return slices.Contains(ds, 42)
	}, time.Second, 5*time.Millisecond, "SetFanDuty(42) not recorded")

	take, _ := fake.counts()
	require.Equal(t, 1, take, "drive should TakeControl exactly once")

	cancel()

	select {
	case reconnect := <-retCh:
		require.False(t, reconnect, "shutdown should return false")
	case <-time.After(time.Second):
		t.Fatal("runFanControlDutyCycle did not return after context cancel")
	}

	// On shutdown while holding control, the app releases control so the device
	// restores the user's manual setpoint (rather than forcing duty 0).
	_, release := fake.counts()
	require.Equal(t, 1, release, "shutdown while holding control must ReleaseControl")
}

func TestRunFanControlDutyCycleReleasesOnIdleCommand(t *testing.T) {
	t.Parallel()

	fake := newFakeFanClient()
	a, cancel := newTestApp(t, fake)

	defer cancel()

	retCh := make(chan bool, 1)

	go func() {
		retCh <- a.runFanControlDutyCycle()
	}()

	a.fanControlChan <- fanCommand{drive: true, duty: 30}

	require.Eventually(t, func() bool {
		take, _ := fake.counts()

		return take == 1
	}, time.Second, 5*time.Millisecond, "TakeControl not recorded")

	// An idle command (drive=false) hands control back to the device.
	a.fanControlChan <- fanCommand{drive: false}

	require.Eventually(t, func() bool {
		_, release := fake.counts()

		return release == 1
	}, time.Second, 5*time.Millisecond, "ReleaseControl not recorded on idle command")

	cancel()
	<-retCh
}

func TestHandleFanEventButtonCyclesModeAndUpdatesDisplay(t *testing.T) {
	t.Parallel()

	fake := newFakeFanClient()
	a, cancel := newTestApp(t, fake)

	defer cancel()

	state := &fanControlState{display: make(chan fanDisplayJob, 1)}

	before := a.config.GetFanMode()
	a.handleFanEvent(fancontroller.EventButton, state)
	after := a.config.GetFanMode()

	require.NotEqual(t, before, after, "button press should cycle the fan mode")
	require.Len(t, state.display, 1, "button press should queue a mode icon for upload")
}

// TestFanDisplayUploaderUploads checks the uploader streams a queued icon to the
// device.
func TestFanDisplayUploaderUploads(t *testing.T) {
	t.Parallel()

	fake := newFakeFanClient()
	a, cancel := newTestApp(t, fake)

	defer cancel()

	ctx := t.Context()

	jobs := make(chan fanDisplayJob, 1)
	go a.runFanDisplayUploader(ctx, fake, jobs)

	jobs <- fanDisplayJob{name: "fan2", foreground: color.White}

	require.Eventually(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()

		return fake.displayImages >= 1
	}, time.Second, 5*time.Millisecond, "queued icon was not uploaded")
}

// TestFanDisplayUploaderPreemptsInflight checks that queuing a newer icon while
// one is uploading cancels the in-flight transfer and starts the new one.
func TestFanDisplayUploaderPreemptsInflight(t *testing.T) {
	t.Parallel()

	fake := newFakeFanClient()
	fake.displayBlock = true

	a, cancel := newTestApp(t, fake)

	defer cancel()

	ctx := t.Context()

	jobs := make(chan fanDisplayJob, 1)
	go a.runFanDisplayUploader(ctx, fake, jobs)

	jobs <- fanDisplayJob{name: "fan2", foreground: color.White}

	require.Eventually(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()

		return fake.displayStarts == 1
	}, time.Second, 5*time.Millisecond, "first upload did not start")

	jobs <- fanDisplayJob{name: "wind-auto", foreground: color.White}

	require.Eventually(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()

		return fake.displayCancel >= 1 && fake.displayStarts == 2
	}, time.Second, 5*time.Millisecond, "newer icon did not preempt the in-flight upload")
}

func TestRunFanControlDutyCycleReturnsTrueOnDisconnect(t *testing.T) {
	t.Parallel()

	fake := newFakeFanClient()
	a, cancel := newTestApp(t, fake)

	defer cancel()

	retCh := make(chan bool, 1)

	go func() {
		retCh <- a.runFanControlDutyCycle()
	}()

	close(fake.disc)

	select {
	case reconnect := <-retCh:
		require.True(t, reconnect, "unsolicited disconnect should return true (reconnect)")
	case <-time.After(time.Second):
		t.Fatal("runFanControlDutyCycle did not return after disconnect signal")
	}
}

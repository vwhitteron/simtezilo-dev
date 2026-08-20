//go:build linux

package audio

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// isolatedCPUListPath reports the CPUs reserved by the isolcpus kernel command
// line. Pinning to a CPU the kernel did not isolate gives no benefit and costs
// the producer the use of every other core, so the pin is gated on this file.
const isolatedCPUListPath = "/sys/devices/system/cpu/isolated"

// applyRealtime puts the calling OS thread on the SCHED_FIFO policy at the given
// priority. The caller must hold the thread with runtime.LockOSThread first,
// because the policy belongs to the thread and not to the goroutine.
func applyRealtime(priority int) error {
	attr := unix.SchedAttr{
		Policy:   unix.SCHED_FIFO,
		Priority: uint32(priority),
	}

	// A pid of 0 targets the calling thread.
	if err := unix.SchedSetAttr(0, &attr, 0); err != nil {
		return fmt.Errorf("sched_setattr SCHED_FIFO priority %d: %w", priority, err)
	}

	return nil
}

// pinThread restricts the calling OS thread to cpu. It refuses when the kernel
// did not isolate that CPU, so an unprovisioned machine keeps every core.
func pinThread(cpu int) error {
	isolated, err := isolatedCPUs()
	if err != nil {
		return err
	}

	if !isolated[cpu] {
		return fmt.Errorf("cpu %d absent from %s: %w", cpu, isolatedCPUListPath, errCPUNotIsolated)
	}

	var set unix.CPUSet

	set.Zero()
	set.Set(cpu)

	if err := unix.SchedSetaffinity(0, &set); err != nil {
		return fmt.Errorf("sched_setaffinity cpu %d: %w", cpu, err)
	}

	return nil
}

// isolatedCPUs parses the kernel's isolated CPU list, which is a comma
// separated set of indices and ranges such as "2,4-6". An empty file means no
// CPU was isolated.
func isolatedCPUs() (map[int]bool, error) {
	raw, err := os.ReadFile(isolatedCPUListPath)
	if err != nil {
		// A kernel without the attribute has isolated nothing, which is the
		// expected state rather than a fault.
		if os.IsNotExist(err) {
			return nil, errCPUNotIsolated
		}

		return nil, fmt.Errorf("read %s: %w", isolatedCPUListPath, err)
	}

	isolated := make(map[int]bool)

	for _, field := range strings.Split(strings.TrimSpace(string(raw)), ",") {
		if field == "" {
			continue
		}

		low, high, err := parseCPURange(field)
		if err != nil {
			return nil, err
		}

		for cpu := low; cpu <= high; cpu++ {
			isolated[cpu] = true
		}
	}

	return isolated, nil
}

// parseCPURange expands one "N" or "N-M" field into its inclusive bounds.
func parseCPURange(field string) (int, int, error) {
	low, high, found := strings.Cut(field, "-")

	first, err := strconv.Atoi(low)
	if err != nil {
		return 0, 0, fmt.Errorf("parse cpu list %q: %w", field, err)
	}

	if !found {
		return first, first, nil
	}

	last, err := strconv.Atoi(high)
	if err != nil {
		return 0, 0, fmt.Errorf("parse cpu list %q: %w", field, err)
	}

	return first, last, nil
}

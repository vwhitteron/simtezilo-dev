# Realtime audio tuning

This document explains why the haptic audio producer asks for a realtime
scheduling policy, how to measure whether the tuning helped, and how to undo it.

Read the measurement section before you change any boot configuration. Stages 3
and 5 cost real resources, and on a machine that already runs clean they buy
nothing.

## The problem

Simtezilo synthesises haptics at 48 kHz and dispatches telemetry at 120 Hz, an
8.333 ms period (`hapticFrameRate` in [app/app_constants.go](../app/app_constants.go)).

The output path already tolerates jitter. `AsyncSource`
([app/audio/async.go](../app/audio/async.go)) decouples synthesis from the
PortAudio callback with a ring buffer. The producer goroutine runs the mix and
the resample; the device callback only copies finished samples out. When the ring
runs dry the callback emits silence rather than stalling, so it never misses its
deadline.

The remaining source of dropouts is therefore not CPU throughput. It is
scheduler jitter: the producer competes with the web UI, Discord, the updater,
the display driver, and network interrupt handling. When the producer is
descheduled for longer than the ring holds, the ring drains and the callback
pads with silence.

Raw compute is not the constraint. The signal path is one-pole filters, noise,
oscillators, and sample playback across a few channels, which is a few million
operations per second. A Pi has ample headroom. Punctuality is the constraint.

## Measure first

### Why underrun count is the wrong metric

`underruns` is a rare-event counter. A healthy Pi records a handful of events in
ten minutes, so two runs cannot be separated without many repetitions. It also
only moves after the safety margin is already gone. A change that doubles the
margin from a zero-underrun baseline would show nothing at all.

### Measure the margin instead

The ring fill depth is the margin, and the device callback samples it thousands
of times per second. `AsyncSource` records two things for this purpose:

- **`MinFill`** — the low-water mark in samples since the last `ResetPeak`. This
  is the primary before/after number.
- **`FillBuckets`** — a histogram of post-callback fill in eighths, which gives
  the shape of the distribution rather than a single worst point.

Both appear in `Health()`, in the 5 s `haptic latency monitor` debug log as
`min_fill` and `min_fill_ratio`, and on the audio health chart in the web UI dev
page as the dashed `Async Min Fill` series.

### The load harness is mandatory

Realtime priority only matters under contention. On an idle Pi the before and
after runs are identical and the change looks useless. Stage 3 in particular
cannot show a result without network interrupt load.

Apply the same load to every run:

1. Open the web UI dev page with the charts live.
2. Connect Discord pit radio.
3. `stress-ng --cpu 4 --cpu-load 60 --timeout 10m`
4. `iperf3 -c <host> -t 600 -b 20M`

### Four measurement layers

| Layer | Instrument | Answers |
| --- | --- | --- |
| Kernel | `cyclictest -m -p 80 -i 200 -h 1000 -q -D 10m` | Did the OS tuning work, independent of the app? |
| Thread | `MinFill`, `FillBuckets` | Did the producer become more punctual? |
| Pipeline | `seqJitterMs`, `driftMs` ([app/audiomon](../app/audiomon/audiomon.go)) | Did end-to-end timing improve? |
| Output | `audioqa.ZeroRuns` ([app/audio/audioqa](../app/audio/audioqa/audioqa.go)) | Did audible dropouts fall? |

`audioqa.ZeroRuns` finds the silence-padding signature of an underrun directly in
a recorded WAV. It checks the result without trusting the app's own counters.

`cyclictest` comes from the `rt-tests` package.

### The regression control

`CaptureHaptics` ([app/haptic_capture.go](../app/haptic_capture.go)) drives the
real generators over a replay with no device attached. It passes an empty
`RealtimeConfig`, so scheduling cannot reach it.

Capture `data/replays/demo.gtz` before and after each stage. The two files must
be **bit-identical**. This proves the change altered timing only and never the
signal. It separates a real improvement from an accidental change to the audio.

### Procedure and decision rule

1. Run five ten-minute replays of `data/replays/demo.gtz` under the load above.
2. Record `min_fill`, the fill histogram, `seq_jitter_ms`, `drift_ms`,
   `underruns`, and the `cyclictest` maximum.
3. Report the median across runs **and the worst single run**. Decide on the
   worst run, because this is a tail problem. A change that improves the median
   but not the tail has not fixed anything.
4. Confirm the offline capture is still bit-identical.
5. Repeat after every stage, and record the numbers below.

Pyroscope already runs with `SetBlockProfileRate(5)` and
`SetMutexProfileFraction(5)` ([app/profiler](../app/profiler/profiler.go)). Diff
the block profile across the change to see whether the producer's `sync.Cond`
wait pattern moved, and to catch priority inversion on the ring mutex.

## Stage 1 — Realtime priority (automatic)

No action is needed. The producer requests `SCHED_FIFO` for itself at startup.

`AsyncSource.produce()` calls `runtime.LockOSThread` before its first block,
because a scheduling policy belongs to an OS thread and not to a goroutine. The
cond-var wait that paces the loop parks the goroutine without releasing the
thread, so the policy survives every iteration.

The priority is 10 (`hapticRealtimePriority`). Keep it low. The ALSA interrupt
threads and the PortAudio device callback must both outrank the producer, or
synthesis work starves the soundcard clock and turns a scheduling win into a
dropout.

A failed request is never fatal. The app logs one warning and continues at normal
priority:

```
WRN realtime scheduling unavailable, running at normal priority component="audio producer" error=...
```

On success it logs once at info level with the priority granted. On macOS and
Windows it logs nothing, because those platforms expose no equivalent control.

### Turn it off without a rebuild

Set `app.realtimeScheduling` to `false` in the configuration file:

```json
{
    "app": {
        "realtimeScheduling": false
    }
}
```

The producer then runs at normal priority on every core. The switch also
disables the CPU pin of Stage 3, because the pin is only useful under a realtime
policy. It defaults to `true`, so an existing configuration file keeps the
current behaviour.

The policy belongs to the producer thread, which is created once at audio
startup. Restart the application after a change. The log then carries this line
in place of the success line:

```
INF realtime scheduling disabled by configuration, running at normal priority component="audio producer" result=disabled
```

Use this switch for the A/B comparison of the measurement procedure above. Do
not try to disable the policy with `LimitRTPRIO=0` in the unit file: the service
runs as root, and `CAP_SYS_NICE` bypasses that limit.

### Known risks

- **Priority inversion.** The producer and the PortAudio callback share the ring
  mutex, and Go mutexes carry no priority inheritance. `Health()` also takes it
  from the web telemetry goroutine at normal priority. The hold is two field
  reads, so the window is nanoseconds, and priority 10 keeps the producer below
  the callback thread. Watch the mutex profile if dropouts appear under web UI
  load specifically.
- **Garbage collection.** A `SCHED_FIFO` thread that never yields can delay a
  stop-the-world pause. The producer blocks on the cond var every iteration once
  it reaches its target fill, so it yields regularly.

## Stage 2 — Grant the privilege (systemd)

[init/simtezilo.service](../init/simtezilo.service) carries:

```ini
LimitRTPRIO=95
LimitMEMLOCK=infinity
```

Without `LimitRTPRIO` the request in Stage 1 fails and the app falls back. The
line helps two threads, not one: the producer, and the PortAudio ALSA callback
thread, which requests `SCHED_FIFO` inside the C library and falls back silently
when `rtprio` is capped.

For a launch outside systemd, grant the same limit through
`/etc/security/limits.d/99-simtezilo.conf`:

```
@audio  -  rtprio  95
@audio  -  memlock unlimited
```

Under Docker, use `--cap-add=sys_nice --ulimit rtprio=95`.

Stages 1 and 2 are safe and reversible. Most of the available benefit is here.

## Updating a device that is already provisioned

Stages 1 and 2 need no reboot and no provisioning script.

1. Copy the changed files to the device:

   | File | Why |
   | --- | --- |
   | `bin/simtezilo` | The producer now requests its own realtime policy |
   | `init/simtezilo.service` | Adds `LimitRTPRIO` and the `ExecStartPre` |
   | `init/rt-tune.sh` | New file; must be executable. Only needed for Stage 3 |
   | `init/recover.sh` | Changed in the same release |

2. Reload and restart:

   ```sh
   sudo chmod +x /opt/simtezilo/init/rt-tune.sh
   sudo systemctl daemon-reload
   sudo systemctl restart simtezilo
   ```

`systemctl enable` links to the unit inside `/opt/simtezilo/init/`, so editing
that file in place is enough. There is nothing to copy into `/etc`.

3. Confirm the privilege was granted:

   ```sh
   journalctl -u simtezilo | grep 'realtime scheduling'
   chrt -p "$(pgrep -x simtezilo)"
   ```

   An info line naming the priority means Stage 1 and Stage 2 are working. A
   warning means the process did not get `CAP_SYS_NICE`, so check that the
   `LimitRTPRIO` line reached the running unit with `systemctl cat simtezilo`.

Nothing else is required. Stage 3 is separate and opt-in.

## Stage 3 — Core isolation and interrupt affinity (opt-in)

Apply this only when Stage 0 measurements after Stage 2 still show underruns or a
low `min_fill`. It costs a quarter of the CPU on a four-core Pi.

The tuning has a static half and a per-boot half.

### Static half — apply once

Raspberry Pi images are produced by the pi-gen build, so the durable home for
these three settings is the image generator's `05-simtezilo` stage. On an
existing device, apply them by hand.

1. Reserve the core on the kernel command line. `cmdline.txt` is a single line,
   so append to it rather than adding a new one:

   ```sh
   sudo cp /boot/firmware/cmdline.txt /boot/firmware/cmdline.txt.bak
   sudo sed -i '1s|$| isolcpus=3 nohz_full=3 rcu_nocbs=3|' /boot/firmware/cmdline.txt
   ```

   `nohz_full` stops the periodic timer tick on the isolated core and
   `rcu_nocbs` moves its RCU callbacks elsewhere. Without both, the core is
   reserved but still interrupted, and the isolation buys far less than it
   appears to.

   The core number must match `hapticRealtimeCPU` in
   [app/app_constants.go](../app/app_constants.go), which is 3.

2. Keep the general threads off that core:

   ```sh
   sudo mkdir -p /etc/systemd/system/simtezilo.service.d
   printf '[Service]\nCPUAffinity=0,1,2\n' | \
       sudo tee /etc/systemd/system/simtezilo.service.d/10-realtime.conf
   sudo systemctl daemon-reload
   ```

3. Stop `irqbalance` moving interrupts back onto it, if it is installed:

   ```sh
   printf 'IRQBALANCE_BANNED_CPULIST=3\n' | sudo tee -a /etc/default/irqbalance
   ```

   Banning the core is better than masking the service: `irqbalance` keeps
   working on the remaining cores and the setting survives a package upgrade.

Then reboot.

### The one per-boot step

Interrupt affinity in `/proc/irq/*/smp_affinity_list` resets at every boot, so it
cannot be provisioned once. [init/rt-tune.sh](../init/rt-tune.sh) applies it,
and `simtezilo.service` runs it automatically:

```ini
ExecStartPre=-/opt/simtezilo/init/rt-tune.sh
```

The `-` prefix is load bearing. The unit has `Restart=always`, so without it a
fault in the tuning script would block startup and turn an optional optimisation
into a five-second restart loop. With it, systemd records the failure and starts
the application anyway.

The script is also safe on an unprovisioned machine: with no isolated CPU it
prints a message and exits 0. It marks `/run/simtezilo-rt-tuned` so a restart
loop does not rewrite every interrupt mask every five seconds, and because `/run`
is a tmpfs the marker clears at the next boot.

No second service is needed, and nothing extra has to be enabled.

### How the pin works

`CPUAffinity=` sets only the initial mask, and a thread may widen it later. The
producer widens its own affinity back to the isolated core once it holds its
realtime policy. The pin is refused unless the CPU appears in
`/sys/devices/system/cpu/isolated`, so leaving `hapticRealtimeCPU` set costs
nothing on an untuned machine.

### Rollback

```sh
sudo rm -rf /etc/systemd/system/simtezilo.service.d
sudo sed -i '/IRQBALANCE_BANNED_CPULIST/d' /etc/default/irqbalance
sudo mv /boot/firmware/cmdline.txt.bak /boot/firmware/cmdline.txt
sudo systemctl daemon-reload
sudo reboot
```

`rt-tune.sh` needs no rollback. Once no CPU is isolated it does nothing.

## Stage 4 — Decide whether to continue

Repeat the measurement procedure. Expect:

- `MinFill` raised on the **worst** run, and the low fill buckets emptied.
- `seq_jitter_ms` reduced.
- `cyclictest` maximum reduced.
- `underruns` at or near zero. Treat this as confirmation, not as the decision.
- The offline capture still bit-identical.

Proceed to Stage 5 only if the `cyclictest` maximum still exceeds the ring depth
in milliseconds. The ring depth comes from `hapticBufferFrames` in
[app/app.go](../app/app.go) with a default `latencyMs` of 20. Below that figure
the ring already absorbs the jitter, and an RT kernel changes nothing measurable.

## Stage 5 — PREEMPT_RT kernel (conditional)

Raspberry Pi OS ships a `PREEMPT` kernel, which uses voluntary preemption. It
does **not** ship `PREEMPT_RT` through `apt`. A full realtime kernel needs a
source build:

1. Fetch the matching `raspberrypi/linux` branch.
2. Apply the `PREEMPT_RT` patch series for that kernel version.
3. Set `CONFIG_PREEMPT_RT=y` and build with the standard Pi cross-compile flow.
4. Install, then repeat the measurement procedure.

This is a supported but optional path. It removes the ability to take stock
kernel updates, so treat it as a last resort rather than a default.

## Results log

Record measurements here so the numbers stay with the reasoning.

| Date | Stage | Hardware | min_fill worst | seq_jitter_ms | underruns | cyclictest max |
| --- | --- | --- | --- | --- | --- | --- |
| | baseline | | | | | |

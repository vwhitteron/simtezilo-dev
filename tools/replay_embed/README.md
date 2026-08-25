# replay_embed

`replay_embed` embeds a GT7 telemetry replay into a video file as a timed
metadata track. The video and audio streams are copied without re-encoding.

```
go run ./tools/replay_embed \
  -video demo-spa-gr010.mp4 \
  -replay data/replays/demo.gtz \
  -out demo-with-telemetry.mp4
```

## Track layout

The tool adds one data track holding exactly one 344-byte telemetry packet per
video frame. **Sample index equals video frame index.** A consumer indexes
telemetry by frame number and needs no timing arithmetic.

| Property | Value |
| --- | --- |
| Handler | `meta`, named "GT Telemetry" |
| Sample entry | `gpmd` |
| ffprobe codec | `bin_data` |
| Sample size | 344 bytes, fixed |
| Timescale | The video's frame rate numerator |
| Sample delta | The video's frame rate denominator |

The track shares the video timebase, so its duration matches the video exactly.

The `gpmd` four character code is GoPro's. It is reused because ffmpeg maps
only a known metadata tag onto its `bin_data` codec. A track tagged `gtlm` or
`mebx` is read as codec `none` and cannot be copied into an MP4.

## Payload

Each sample is a raw deciphered GT7 packet, unchanged. Feed it straight into
`gt-telemetry`'s transformer. The magic `0S7G` starts every packet and the
`uint32` little-endian sequence ID sits at byte offset 112.

Extract the track with:

```
ffmpeg -i out.mp4 -map 0:d:0 -c copy -f data telemetry.bin
```

## Synchronisation

A screen recording and a replay capture share no timecode, so alignment is
manual. Use `-offset` in seconds, positive to take telemetry from later in the
replay. Frames before the replay starts repeat the first packet; frames after
it ends, or inside a sequence gap, repeat the last packet.

## Dropped telemetry frames

Frame indices follow the packet sequence ID, not the packet's position in the
file. A frame the recorder dropped therefore leaves a hole, and every later
packet keeps its correct frame. Telemetry does not drift.

A hole holds the last good packet for its duration. Playback resumes in exact
sync on the first packet after the hole.

If the sequence ID jumps backwards, repeats, or leaps more than an hour, the
tool treats it as a counter reset rather than a gap. Such a replay holds more
than one session. Frames after the reset are numbered contiguously so the tail
is never lost, and the tool warns that alignment past that point is not
trustworthy.

`-map` selects how replay packets are assigned to frames:

- `sequential` (default) maps packet N to frame N. Use it when the recorder
  captured every game frame, whatever rate the container declares. A 59.94 fps
  container recorded from a 60 Hz game is the normal case.
- `realtime` treats the replay as exactly 60 Hz and indexes it by wall clock
  time. Use it when the recorder truly sampled at its declared rate. On a
  59.94 fps video the two modes diverge by 0.1%, about 127 ms over two minutes.

The tool prints how many frames came from real packets and how many were
padded. Check that summary before trusting an alignment.

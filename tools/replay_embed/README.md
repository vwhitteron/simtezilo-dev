# replay_embed

`replay_embed` embeds a GT7 telemetry replay into a video file as a timed
metadata track.

## Command line

The video and audio streams are copied without re-encoding. The input must
therefore already be H.264 or HEVC in MP4 or MOV. The tool refuses anything
else and points you at the web UI.

```
go run ./tools/replay_embed \
  -video demo-spa-gr010.mp4 \
  -replay data/replays/demo.gtz \
  -out demo-with-telemetry.mp4
```

## Alignment web UI

`-serve` opens a page that aligns a video against a replay, marks a cut, and
exports in one action.

```
go run ./tools/replay_embed -serve :8099 \
  -video-dir tools/replay_embed \
  -replay-dir data/replays
```

The flags set the starting directories. The cog at the top right of the Files
panel opens a dialog that changes all three while the tool runs. A directory
that is missing, or that is a file, is refused with the reason.

Load a video and a replay, then scrub each timeline until the circuit map
matches the picture.

Both tracks always show their own playhead. Sync decides how the two relate:

- **Sync off.** The playheads move independently. Moving one leaves the other
  where it is, and the offset readout becomes whatever the new gap is. This is
  the state you align in.
- **Sync on.** The offset is frozen at the value it held when you turned Sync
  on, and moving either playhead moves both. This is the state you review and
  cut in.

Controls:

Each track has a header box at its left end, as a video editor does. The box
names the track and carries that track's own readouts: the video box shows the
frame, the timecode and the cut range, and the telemetry box shows the frame,
the lap with its running time, and the marker legend.

One transport bar drives whichever track has focus. The focused track is
highlighted. Click a track header, click a timeline, or press `Tab` to move
focus. The play button always drives the video.

| Key | Action |
| --- | --- |
| `Tab` | Move focus between the video and telemetry lanes |
| `←` `→` | Step the focused lane by one frame |
| `Shift` + `←` `→` | Step the focused lane by ten frames |
| `Alt` + `←` `→` | Step the focused lane by one second |
| `↑` `↓` | Step telemetry by one frame, whichever lane has focus |
| `Shift` + `↑` `↓` | Step telemetry by ten frames |
| `Space` | Play or pause |
| `i` `o` | Set the in and out point |
| `c` | Clear the cut back to the whole video |
| `[` `]` | Jump to the in and out point |
| `Enter` or `l` | Toggle Sync |
| `,` `.` | Nudge the offset by one frame |

A telemetry step changes the offset while Sync is off, so the video stays put.
With Sync on, both streams move together.

### The marker strip

The bottom band of the telemetry timeline holds one marker per lap start and
per gear change. Laps are blue, upshifts green, downshifts amber. Click or drag
inside the band to snap the telemetry playhead onto the nearest marker. The
status line names the marker it picked, because ticks collide where shifts come
close together.

Gear changes are the strongest alignment cue. Find a downshift under braking in
the video, snap the telemetry head onto the matching amber tick, then refine
with the arrow keys.

A press above the band scrubs freely, as before.

The **Auto cut** box lists every lap. Click a row to set the cut to that lap.
The **Extend** field adds that many seconds before and after the lap, and
defaults to 3.0. Set it to 0 for an exact lap. The value survives a reload. Save then cuts the video, converts
it, builds the telemetry track for the cut range, and muxes the result. Data
outside the cut is dropped.

Save picks its own path. A source that is already H.264 or HEVC in MP4, with no
crop and no HDR, is cut with a stream copy. The copy start snaps back to the
nearest keyframe, so the output can begin a little before the marked in point.
Every other source is re-encoded, which makes the cut frame exact.

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

## PlayStation 5 captures

A PS5 records 4K clips as HDR10 VP9 in WebM. Such a file needs three changes
before it can carry a telemetry track:

1. A crop. The stream is coded 1920x1088 and the container asks for 1080.
2. A colour conversion. The transfer function is `smpte2084`, not BT.709.
3. A codec change. VP9 in MP4 does not play in every browser.

The web UI performs all three. Its probe reports each one, so check the status
line after the video loads.

Matroska declares no frame count and no frame rate. The probe counts video
packets and rounds the derived rate onto the nearest standard rate, so a
`19001/317` estimate becomes the `60000/1001` the telemetry track needs.

### The tone map needs libzimg

The HDR to SDR conversion uses the `zscale` filter. An ffmpeg built without
libzimg does not provide it, and the UI reports `canToneMap: false`. Install an
ffmpeg built with libzimg, or set the colour selector to **Keep HDR**, which
re-encodes to 10 bit HEVC and restates the source colour tags.

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

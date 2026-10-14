// Extracts the embedded telemetry track from a local MP4 without uploading the video.
//
// Uses mp4box.js 0.5.4 (global bundle, exposes MP4Box) as an index parser only:
//   https://cdn.jsdelivr.net/npm/mp4box@0.5.4/dist/mp4box.all.min.js
// The sample bytes are read with File.slice(), so only moov and the telemetry
// samples are ever read into memory.
//
// extractVideoTelemetry(file) resolves to
//   { packets: Uint8Array, video: { name, frameCount, timescale, sampleDelta, firstSeq } }
// where video matches the metadata.video the host builds for a replay-directory MP4.
(function () {
    const INDEX_CHUNK = 1 << 20;          // 1 MiB per index read
    const SEQUENCE_ID_OFFSET = 112;       // videotelemetry.SequenceIDOffset

    // Translated message for an Error, with the English text as the fallback until the
    // translations load. {detail} is filled from detail.
    function failure(key, fallback, detail) {
        const text = (window.t && window.t(key)) || fallback;
        return new Error(text.replace('{detail}', () => String(detail)));
    }

    // Feeds the file to mp4box until it has parsed moov. appendBuffer returns the next
    // position it needs, so a large mdat ahead of moov is skipped rather than read.
    function readIndex(file) {
        return new Promise((resolve, reject) => {
            const mp4 = MP4Box.createFile(false);   // false: do not keep mdat data
            let info = null;
            mp4.onError = (e) => reject(failure('runmode.tuneassist.status.video.parseerror', 'MP4 parse error: {detail}', e));
            mp4.onReady = (i) => { info = i; };

            (async () => {
                let pos = 0;
                while (!info && pos < file.size) {
                    const buf = await file.slice(pos, Math.min(pos + INDEX_CHUNK, file.size)).arrayBuffer();
                    buf.fileStart = pos;
                    const next = mp4.appendBuffer(buf);
                    pos = (typeof next === 'number' && next > pos) ? next : pos + buf.byteLength;
                }
                mp4.flush();
                if (!info) throw failure('runmode.tuneassist.status.video.nomoov', 'MP4 has no moov box');
                resolve({ mp4, info });
            })().catch(reject);
        });
    }

    // Joins samples that sit back to back in the file into [start, end) ranges.
    function joinRanges(samples) {
        const ranges = [];
        for (const s of samples) {
            const last = ranges[ranges.length - 1];
            if (last && last.end === s.offset) {
                last.end += s.size;
            } else {
                ranges.push({ start: s.offset, end: s.offset + s.size });
            }
        }
        return ranges;
    }

    async function extractVideoTelemetry(file) {
        const { mp4, info } = await readIndex(file);

        const tracks = info.tracks.filter(t => t.codec === 'gpmd');
        if (tracks.length < 1) {
            throw failure('runmode.tuneassist.status.video.notrack', 'replay video is missing telemetry track');
        }

        const track = tracks[0];
        const samples = mp4.getTrackById(track.id).samples;
        if (!samples || samples.length === 0) throw failure('runmode.tuneassist.status.video.nosamples', 'telemetry track has no samples');

        const sampleDelta = samples[0].duration;
        if (samples.some(s => s.duration !== sampleDelta)) {
            throw failure('runmode.tuneassist.status.video.durations', 'telemetry sample durations are not constant');
        }

        let total = 0;
        samples.forEach(s => { total += s.size; });

        const packets = new Uint8Array(total);
        let at = 0;
        for (const range of joinRanges(samples)) {
            const chunk = new Uint8Array(await file.slice(range.start, range.end).arrayBuffer());
            packets.set(chunk, at);
            at += chunk.length;
        }

        if (samples[0].size < SEQUENCE_ID_OFFSET + 4) throw failure('runmode.tuneassist.status.video.shortsample', 'first telemetry sample is too short');
        const firstSeq = new DataView(packets.buffer).getUint32(SEQUENCE_ID_OFFSET, true);

        return {
            packets,
            video: {
                name: file.name,
                frameCount: samples.length,
                timescale: track.timescale,
                sampleDelta,
                firstSeq,
            },
        };
    }

    window.extractVideoTelemetry = extractVideoTelemetry;
})();

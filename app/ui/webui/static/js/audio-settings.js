// audio-settings.js — dynamic behaviour for the audio device controls, which
// live across the System (backend), Haptics (haptics output) and Pit Radio
// (local audio output) settings panels.
//
// Static fields bind to config via their data-config attributes (handled by
// settings.js). This module adds the dynamic parts: populating device
// dropdowns from /api/audio/devices, reflecting which backends are compiled in,
// and wiring the per-device test-tone buttons to /api/audio/test.
(function () {
    'use strict';

    // Cached audio config (from /api/config) used to restore saved device
    // selections after their option lists are (re)built.
    let audioConfig = {
        backend: 'beep',
        availableBackends: ['beep'],
        haptics: { device: '', deviceName: '', channels: 2 },
        pitRadio: { device: '', deviceName: '' },
    };

    // Last device list fetched from /api/audio/devices, retained so the two
    // dropdowns can be rebuilt (e.g. to re-apply mutual exclusion) without a
    // refetch.
    let lastDevices = [];

    // Optgroup ordering/labels by semantic device type. Anything unrecognised
    // falls under "Other".
    const TYPE_GROUPS = [
        { type: 'builtin', label: 'Built-in' },
        { type: 'usb', label: 'USB' },
        { type: 'hdmi', label: 'HDMI' },
        { type: 'bluetooth', label: 'Bluetooth' },
    ];

    function el(id) {
        return document.getElementById(id);
    }

    // Friendly option label: prefer the curated DisplayName and always surface the
    // device's channel count in brackets, e.g. "HiFiBerry DAC (2ch)". The count is
    // only appended when showChannels is set (the haptics role, where the channel
    // count drives output routing); it is meaningless for pit-radio voice output.
    function deviceLabel(device, showChannels) {
        const name = device.DisplayName || device.Name || '';
        if (!showChannels) {
            return name;
        }
        const channels = device.MaxChannels || 2;
        return name + ' (' + channels + 'ch)';
    }

    async function fetchAudioConfig() {
        try {
            const response = await fetch('/api/config?t=' + Date.now());
            if (!response.ok) {
                return;
            }

            const config = await response.json();
            if (config) {
                const hardware = config.hardware || {};
                const hapticsOutput = (config.haptics && config.haptics.output) || {};
                const pitRadioAudio = (config.pitRadio && config.pitRadio.audio) || {};

                audioConfig.backend = hardware.audioBackend || audioConfig.backend;
                audioConfig.availableBackends =
                    hardware.availableBackends || audioConfig.availableBackends;
                audioConfig.haptics = {
                    device: hapticsOutput.device || '',
                    deviceName: hapticsOutput.deviceName || '',
                    channels: parseInt(hapticsOutput.channels, 10) || 2,
                };
                audioConfig.pitRadio = {
                    device: pitRadioAudio.device || '',
                    deviceName: pitRadioAudio.deviceName || '',
                };
            }
        } catch (err) {
            console.error('audio-settings: failed to load config', err);
        }
    }

    // After a haptics device change the server re-derives the channel count from
    // the device. Re-read the config and broadcast a 'configloaded' event so the
    // channel-gains and routing controls resize to match. A short delay lets the
    // device/deviceName saves (fired asynchronously by settings.js) land
    // server-side before we read the derived count back.
    function refreshDerivedChannels() {
        setTimeout(async () => {
            try {
                const response = await fetch('/api/config?t=' + Date.now());
                if (!response.ok) {
                    return;
                }
                const config = await response.json();
                const hapticsOutput = (config.haptics && config.haptics.output) || {};
                audioConfig.haptics.channels = parseInt(hapticsOutput.channels, 10) || 2;
                document.dispatchEvent(new CustomEvent('configloaded', { detail: config }));
            } catch (err) {
                console.error('audio-settings: failed to refresh channel count', err);
            }
        }, 700);
    }

    function selectedBackend() {
        const select = el('audio-backend');
        return (select && select.value) || audioConfig.backend || 'beep';
    }

    // Disable backend options that are not compiled into this binary.
    function applyBackendAvailability() {
        const select = el('audio-backend');
        if (!select) {
            return;
        }

        const available = audioConfig.availableBackends || ['beep'];

        Array.from(select.options).forEach(option => {
            const ok = available.includes(option.value);
            option.disabled = !ok;
            option.textContent = ok ? option.value : option.value + ' (not built)';
        });
    }

    // Enable test buttons only for backends that own an independent device.
    function applyTestButtonState() {
        const disabled = selectedBackend() === 'beep';
        const title = disabled
            ? 'Test tones require the portaudio backend'
            : '';

        ['audio-haptics-test', 'audio-pitradio-test'].forEach(id => {
            const button = el(id);
            if (button) {
                button.disabled = disabled;
                button.title = title;
            }
        });
    }

    // Mirror the selected option's device name into the companion hidden field
    // (data-config="…deviceName") so it is persisted alongside the device ID. The
    // name is the stable, backend-agnostic selection key; the ID is a tiebreaker.
    function syncNameField(select, hiddenId, dispatch) {
        const hidden = el(hiddenId);
        if (!hidden) {
            return;
        }

        const opt = select.options[select.selectedIndex];
        const name = (opt && opt.dataset && opt.dataset.name) || '';

        if (hidden.value === name) {
            return;
        }

        hidden.value = name;

        // settings.js auto-saves [data-config] fields on their `change` event, but
        // a programmatic value assignment fires no such event. On a user-initiated
        // device change, dispatch one so the name is persisted alongside the ID.
        // Skipped during population (dispatch=false) to avoid a save on load.
        if (dispatch) {
            hidden.dispatchEvent(new Event('change', { bubbles: true }));
        }
    }

    // Rebuild a device <select>, restoring the saved selection by name first
    // (stable across backend switches and portaudio index reshuffles) and the
    // saved ID as a tiebreaker. Keeps the companion hidden name field in sync.
    //
    // Devices are grouped into <optgroup>s by semantic type. excludeName hides
    // the device chosen for the other output role (mutual exclusion) — voice to
    // a haptic transducer (or vice-versa) is pointless, and it also stops the two
    // streams contending over one exclusive device — but this select's own saved
    // selection is never hidden. excludeBluetooth drops all Bluetooth outputs:
    // their A2DP link adds 100-200 ms of latency, which is unusable for haptics
    // (the feedback would lag the on-screen event), so they are never offered
    // for the haptic role.
    function populateDeviceSelect(select, hiddenId, devices, saved, excludeName, excludeBluetooth, showChannels) {
        if (!select) {
            return;
        }

        const savedId = (saved && saved.device) || '';
        const savedName = (saved && saved.deviceName) || '';

        select.innerHTML = '';

        const defaultOption = document.createElement('option');
        defaultOption.value = '';
        defaultOption.textContent = showChannels ? 'System default (2ch)' : 'System default';
        defaultOption.dataset.name = '';
        select.appendChild(defaultOption);

        const list = (devices || []).filter(device =>
            (!excludeName || device.Name !== excludeName || device.Name === savedName) &&
            (!excludeBluetooth || device.Type !== 'bluetooth' || device.Name === savedName));

        // Bucket devices by type, then emit groups in a fixed, sensible order.
        const buckets = {};
        TYPE_GROUPS.forEach(group => { buckets[group.type] = []; });
        const otherBucket = [];

        list.forEach(device => {
            const bucket = buckets[device.Type];
            (bucket || otherBucket).push(device);
        });

        function appendGroup(label, items) {
            if (!items.length) {
                return;
            }
            const group = document.createElement('optgroup');
            group.label = label;
            items.forEach(device => {
                const option = document.createElement('option');
                option.value = device.ID;
                option.textContent = deviceLabel(device, showChannels);
                option.dataset.name = device.Name;
                group.appendChild(option);
            });
            select.appendChild(group);
        }

        TYPE_GROUPS.forEach(group => appendGroup(group.label, buckets[group.type]));
        appendGroup('Other', otherBucket);

        const options = Array.from(select.options);
        let chosen = '';

        // Prefer a name match (the option whose ID also matches wins ties).
        if (savedName) {
            const byName = options.filter(o => o.dataset.name === savedName);
            if (byName.length) {
                chosen = (byName.find(o => o.value === savedId) || byName[0]).value;
            }
        }

        // Fall back to an ID match, then to keeping the selection visible.
        if (!chosen && savedId && options.some(o => o.value === savedId)) {
            chosen = savedId;
        }

        if (!chosen && (savedId || savedName)) {
            const option = document.createElement('option');
            option.value = savedId;
            option.dataset.name = savedName;
            option.textContent = (savedName || savedId) + ' (unavailable)';
            select.appendChild(option);
            chosen = savedId;
        }

        select.value = chosen || '';
        syncNameField(select, hiddenId, false);
    }

    async function refreshDevices() {
        const backend = selectedBackend();

        applyBackendAvailability();
        applyTestButtonState();

        try {
            const response = await fetch('/api/audio/devices?backend=' + encodeURIComponent(backend));
            const data = await response.json();

            if (data && Array.isArray(data.availableBackends)) {
                audioConfig.availableBackends = data.availableBackends;
                applyBackendAvailability();
            }

            lastDevices = (data && data.status === 'success') ? (data.devices || []) : [];
            repopulateDevices();
        } catch (err) {
            console.error('audio-settings: failed to list devices', err);
        }
    }

    // (Re)build both device dropdowns from the cached list, excluding each
    // other's current selection so a device can't be picked for both roles.
    function repopulateDevices() {
        populateDeviceSelect(el('audio-haptics-device'), 'audio-haptics-devicename',
            lastDevices, audioConfig.haptics, audioConfig.pitRadio.deviceName || '', true, true);
        populateDeviceSelect(el('audio-pitradio-device'), 'audio-pitradio-devicename',
            lastDevices, audioConfig.pitRadio, audioConfig.haptics.deviceName || '', false, false);
    }

    async function playTest(kind, button, statusEl) {
        const backend = selectedBackend();
        // The haptics tone plays through the live synthesizer pipeline (calibrator),
        // so it works on any backend. The pit-radio tone opens its own sink, which
        // conflicts with beep's single shared device, so it stays portaudio-only.
        if (backend === 'beep' && kind !== 'haptics') {
            return;
        }

        let device = '';
        let channel = -1;
        let channels = 2;
        let sampleRate = 0;

        if (kind === 'haptics') {
            device = (el('audio-haptics-device') || {}).value || '';
            // Channel comes from the shared Channels dropdown (also used for routing).
            channel = parseInt((el('routing-channel-select') || {}).value, 10);
            if (isNaN(channel)) {
                channel = 0;
            }
            // Channel count is device-derived; use the loaded config's value.
            channels = audioConfig.haptics.channels || 2;
            sampleRate = parseInt((el('audio-haptics-samplerate') || {}).value, 10) || 0;
        } else {
            device = (el('audio-pitradio-device') || {}).value || '';
            channel = -1;
            channels = 2;
            sampleRate = parseInt((el('audio-pitradio-samplerate') || {}).value, 10) || 0;
        }

        // Highlight the button green while the tone plays (the request stays in
        // flight for the duration of playback).
        if (button) {
            button.classList.remove('btn-outline-secondary');
            button.classList.add('btn-success');
        }

        try {
            const response = await fetch('/api/audio/test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    target: kind === 'haptics' ? 'haptics' : 'pitradio',
                    backend: backend,
                    device: device,
                    channel: channel,
                    channels: channels,
                    sampleRate: sampleRate,
                }),
            });

            const data = await response.json();

            // Only surface errors; success is conveyed by the green highlight.
            if (statusEl) {
                if (data && data.status === 'success') {
                    statusEl.textContent = '';
                } else {
                    statusEl.textContent = 'Error: ' + ((data && data.message) || 'unknown');
                    setTimeout(() => { statusEl.textContent = ''; }, 3000);
                }
            }
        } catch (err) {
            console.error('audio-settings: test tone failed', err);
            if (statusEl) {
                statusEl.textContent = 'Error';
            }
        } finally {
            if (button) {
                button.classList.remove('btn-success');
                button.classList.add('btn-outline-secondary');
            }
        }
    }

    function wireEvents() {
        const backendSelect = el('audio-backend');
        if (backendSelect) {
            backendSelect.addEventListener('change', refreshDevices);
        }

        // The Bluetooth panel pairs/forgets/connects devices, which changes the
        // available output list. Re-read the config first (a forgotten device may
        // have had its saved pit-radio selection cleared server-side), then rebuild
        // the dropdowns, so neither needs a page reload to stay current.
        document.addEventListener('bluetooth-devices-changed', async () => {
            await fetchAudioConfig();
            await refreshDevices();
        });

        // Keep each hidden device-name field in sync when the user picks a device,
        // mirror the choice into audioConfig, and rebuild the other dropdown so
        // mutual exclusion reflects the new selection.
        [
            ['audio-haptics-device', 'audio-haptics-devicename', 'haptics'],
            ['audio-pitradio-device', 'audio-pitradio-devicename', 'pitRadio'],
        ].forEach(([selectId, hiddenId, role]) => {
            const select = el(selectId);
            if (select) {
                select.addEventListener('change', () => {
                    syncNameField(select, hiddenId, true);

                    const option = select.options[select.selectedIndex];
                    audioConfig[role].device = select.value;
                    audioConfig[role].deviceName = (option && option.dataset && option.dataset.name) || '';

                    repopulateDevices();

                    if (role === 'haptics') {
                        refreshDerivedChannels();
                    }
                });
            }
        });

        const hapticsTest = el('audio-haptics-test');
        if (hapticsTest) {
            hapticsTest.addEventListener('click', () => {
                playTest('haptics', hapticsTest, el('audio-haptics-test-status'));
            });
        }

        const pitRadioTest = el('audio-pitradio-test');
        if (pitRadioTest) {
            pitRadioTest.addEventListener('click', () => {
                playTest('pitradio', pitRadioTest, el('audio-pitradio-test-status'));
            });
        }
    }

    async function init() {
        // Only run on pages that contain the audio panel.
        if (!el('audio-backend')) {
            return;
        }

        wireEvents();
        await fetchAudioConfig();

        // Reflect the saved backend before listing devices for it.
        const backendSelect = el('audio-backend');
        if (backendSelect && audioConfig.backend) {
            backendSelect.value = audioConfig.backend;
        }

        await refreshDevices();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();

// audio-settings.js — dynamic behaviour for the Audio Devices settings panel.
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
        haptics: { device: '' },
        pitRadio: { device: '' },
    };

    function el(id) {
        return document.getElementById(id);
    }

    async function fetchAudioConfig() {
        try {
            const response = await fetch('/api/config?t=' + Date.now());
            if (!response.ok) {
                return;
            }

            const config = await response.json();
            if (config && config.audio) {
                audioConfig = Object.assign(audioConfig, config.audio);
            }
        } catch (err) {
            console.error('audio-settings: failed to load config', err);
        }
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
            ? 'Test tones require the malgo or portaudio backend'
            : '';

        ['audio-haptics-test', 'audio-pitradio-test'].forEach(id => {
            const button = el(id);
            if (button) {
                button.disabled = disabled;
                button.title = title;
            }
        });
    }

    // Rebuild a device <select>, preserving (or re-adding) the saved selection.
    function populateDeviceSelect(select, devices, savedValue) {
        if (!select) {
            return;
        }

        select.innerHTML = '';

        const defaultOption = document.createElement('option');
        defaultOption.value = '';
        defaultOption.textContent = 'System default';
        select.appendChild(defaultOption);

        let savedPresent = savedValue === '' || savedValue === undefined;

        (devices || []).forEach(device => {
            const option = document.createElement('option');
            option.value = device.ID;
            option.textContent = device.Name + ' (' + device.MaxChannels + 'ch)';
            select.appendChild(option);

            if (device.ID === savedValue) {
                savedPresent = true;
            }
        });

        // Keep an unavailable saved device visible so the selection isn't lost.
        if (!savedPresent) {
            const option = document.createElement('option');
            option.value = savedValue;
            option.textContent = savedValue + ' (unavailable)';
            select.appendChild(option);
        }

        select.value = savedValue || '';
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

            const devices = (data && data.status === 'success') ? data.devices : [];

            populateDeviceSelect(el('audio-haptics-device'), devices,
                (audioConfig.haptics && audioConfig.haptics.device) || '');
            populateDeviceSelect(el('audio-pitradio-device'), devices,
                (audioConfig.pitRadio && audioConfig.pitRadio.device) || '');
        } catch (err) {
            console.error('audio-settings: failed to list devices', err);
        }
    }

    async function playTest(kind, statusEl) {
        const backend = selectedBackend();
        if (backend === 'beep') {
            return;
        }

        let device = '';
        let channel = -1;
        let channels = 2;
        let sampleRate = 0;

        if (kind === 'haptics') {
            device = (el('audio-haptics-device') || {}).value || '';
            channel = parseInt((el('audio-haptics-test-channel') || {}).value, 10);
            if (isNaN(channel)) {
                channel = 0;
            }
            channels = parseInt((el('audio-haptics-channels') || {}).value, 10) || 2;
            sampleRate = parseInt((el('audio-haptics-samplerate') || {}).value, 10) || 0;
        } else {
            device = (el('audio-pitradio-device') || {}).value || '';
            channel = -1;
            channels = 2;
            sampleRate = parseInt((el('audio-pitradio-samplerate') || {}).value, 10) || 0;
        }

        if (statusEl) {
            statusEl.textContent = 'Playing…';
        }

        try {
            const response = await fetch('/api/audio/test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    backend: backend,
                    device: device,
                    channel: channel,
                    channels: channels,
                    sampleRate: sampleRate,
                }),
            });

            const data = await response.json();

            if (statusEl) {
                statusEl.textContent = data && data.status === 'success'
                    ? 'Done'
                    : ('Error: ' + ((data && data.message) || 'unknown'));

                setTimeout(() => { statusEl.textContent = ''; }, 3000);
            }
        } catch (err) {
            console.error('audio-settings: test tone failed', err);
            if (statusEl) {
                statusEl.textContent = 'Error';
            }
        }
    }

    function wireEvents() {
        const backendSelect = el('audio-backend');
        if (backendSelect) {
            backendSelect.addEventListener('change', refreshDevices);
        }

        const hapticsTest = el('audio-haptics-test');
        if (hapticsTest) {
            hapticsTest.addEventListener('click', () => {
                playTest('haptics', el('audio-haptics-test-status'));
            });
        }

        const pitRadioTest = el('audio-pitradio-test');
        if (pitRadioTest) {
            pitRadioTest.addEventListener('click', () => {
                playTest('pitradio', el('audio-pitradio-test-status'));
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

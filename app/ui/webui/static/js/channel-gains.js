// channel-gains.js — per-channel output gain/mute control for the synthesizer.
// Owns the Haptics > Channels channel <select> (routing-channel-select): it
// populates the Ch0..ChN options and uses the selection to pick one output
// channel, whose gain input + mute toggle reflect and edit that channel. Reads
// synthesizer.channelGain / channelMute and haptics.output.channels from
// /api/config and saves the full arrays back via POST, mirroring the pattern
// used by routing-matrix.js / settings.js.
(function () {
    'use strict';

    // Shared label formatter used by every channel <select>/pill across the
    // settings page: renders a channel as "name (n)", falling back to the
    // default "Channel" label when the channel has no user-assigned name.
    // Defined with a guard so whichever module parses first owns the single
    // canonical implementation.
    window.channelDisplayLabel = window.channelDisplayLabel || function (ch, names) {
        let name = Array.isArray(names) && typeof names[ch] === 'string' ? names[ch].trim() : '';
        if (!name) {
            name = 'Channel';
        }

        return name + ' (' + (ch + 1) + ')';
    };

    // In-memory copy of the per-channel gain (dB), mute and name state.
    let channelGain = [];
    let channelMute = [];
    let channelName = [];
    let numChannels = 2;
    let selectedChannel = 0;

    // Debounce handles: one per array, so rapid edits within a channel are
    // coalesced into a single POST rather than firing per change.
    const saveTimeouts = {};

    function el(id) {
        return document.getElementById(id);
    }

    // Sync the selected channel from the channel dropdown (routing-channel-select),
    // clamping to the current channel count.
    function syncSelectedChannel() {
        if (selectedChannel >= numChannels) {
            selectedChannel = numChannels - 1;
        }
        if (selectedChannel < 0) {
            selectedChannel = 0;
        }

        const select = el('routing-channel-select');
        if (select && select.value !== '') {
            const n = parseInt(select.value, 10);
            if (!isNaN(n)) {
                selectedChannel = Math.min(Math.max(n, 0), numChannels - 1);
            }
        }
    }

    // Populate the channel dropdown with Ch0..ChN-1, preserving the current
    // selection when still valid (clamped if the channel count shrank).
    function renderChannelOptions() {
        const select = el('routing-channel-select');
        if (!select) {
            return;
        }

        if (selectedChannel >= numChannels) {
            selectedChannel = numChannels - 1;
        }
        if (selectedChannel < 0) {
            selectedChannel = 0;
        }

        select.innerHTML = '';
        for (let ch = 0; ch < numChannels; ch++) {
            const option = document.createElement('option');
            option.value = String(ch);
            option.textContent = window.channelDisplayLabel(ch, channelName);
            select.appendChild(option);
        }
        select.value = String(selectedChannel);
    }

    // Reflect the gain/mute of the selected channel onto the controls.
    function updateControls() {
        const gainInput = el('synth-channel-gain');
        if (gainInput && document.activeElement !== gainInput) {
            const gain = typeof channelGain[selectedChannel] === 'number' ? channelGain[selectedChannel] : 0;
            gainInput.value = (window.configManager &&
                typeof window.configManager.formatGainValue === 'function')
                ? window.configManager.formatGainValue(gain)
                : gain.toFixed(2);
        }

        const muteCheckbox = el('synth-channel-mute');
        if (muteCheckbox) {
            muteCheckbox.checked = channelMute[selectedChannel] === true;
            if (window.configManager &&
                typeof window.configManager.updateMuteIconForCheckbox === 'function') {
                window.configManager.updateMuteIconForCheckbox(muteCheckbox);
            }
        }

        const nameInput = el('synth-channel-name');
        if (nameInput && document.activeElement !== nameInput) {
            nameInput.value = typeof channelName[selectedChannel] === 'string'
                ? channelName[selectedChannel]
                : '';
        }
    }

    // Called when the gain input changes for the selected channel.
    function onGainChange() {
        const gainInput = el('synth-channel-gain');
        if (!gainInput) {
            return;
        }

        let value = parseFloat(gainInput.value);
        if (isNaN(value)) {
            value = 0;
        }
        value = Math.min(0, Math.max(-60, value));
        gainInput.value = (window.configManager &&
            typeof window.configManager.formatGainValue === 'function')
            ? window.configManager.formatGainValue(value)
            : value.toFixed(2);

        channelGain[selectedChannel] = value;
        scheduleSave('channelGain');
    }

    // Called when the channel name input changes for the selected channel.
    // Updates local state, refreshes the dropdown labels so the change is
    // visible immediately, and broadcasts so the routing/EQ selects relabel too.
    function onNameChange() {
        const nameInput = el('synth-channel-name');
        if (!nameInput) {
            return;
        }

        channelName[selectedChannel] = nameInput.value.trim();

        renderChannelOptions();
        broadcastNames();
        scheduleSave('channelName');
    }

    // Notify the other settings modules (routing matrix, EQ) that channel names
    // changed so they can relabel their own controls without a full config reload.
    function broadcastNames() {
        document.dispatchEvent(new CustomEvent('channelnameschanged', {
            detail: channelName.slice(),
        }));
    }

    // Called when the mute checkbox changes for the selected channel.
    function onMuteChange() {
        const muteCheckbox = el('synth-channel-mute');
        if (!muteCheckbox) {
            return;
        }

        channelMute[selectedChannel] = muteCheckbox.checked;
        scheduleSave('channelMute');
    }

    // Debounce saves per array so rapid edits collapse into one POST.
    function scheduleSave(field) {
        clearTimeout(saveTimeouts[field]);
        saveTimeouts[field] = setTimeout(function () {
            saveField(field);
        }, 300);
    }

    // POST the current array for one field to /api/config.
    async function saveField(field) {
        let value;
        if (field === 'channelGain') {
            value = channelGain.slice();
        } else if (field === 'channelName') {
            value = channelName.slice();
        } else {
            value = channelMute.slice();
        }

        if (typeof window.showNavbarStatus === 'function') {
            window.showNavbarStatus('saving');
        }

        try {
            const response = await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    synthesizer: {
                        [field]: value,
                    },
                }),
            });

            if (!response.ok) {
                console.error('channel-gains: save failed for field ' + field,
                    response.status);
                if (typeof window.showNavbarStatus === 'function') {
                    window.showNavbarStatus('error');
                }
                return;
            }

            if (typeof window.showNavbarStatus === 'function') {
                window.showNavbarStatus('success');
            }
        } catch (err) {
            console.error('channel-gains: network error saving field ' + field, err);
            if (typeof window.showNavbarStatus === 'function') {
                window.showNavbarStatus('error');
            }
        }
    }

    // Resize the gain/mute arrays to the given channel count, defaulting new
    // slots to -30dB / unmuted.
    function resizeRows(n) {
        const nextGain = [];
        const nextMute = [];
        const nextName = [];
        for (let ch = 0; ch < n; ch++) {
            nextGain.push(typeof channelGain[ch] === 'number' ? channelGain[ch] : -30);
            nextMute.push(channelMute[ch] === true);
            nextName.push(typeof channelName[ch] === 'string' ? channelName[ch] : '');
        }
        channelGain = nextGain;
        channelMute = nextMute;
        channelName = nextName;
        numChannels = n;
    }

    // Ingest a fresh config payload (from /api/config) into local state and
    // refresh the control.
    function applyConfig(config) {
        const hapticsOutput = (config.haptics && config.haptics.output) || {};
        const newChannels = (hapticsOutput.channels && parseInt(hapticsOutput.channels, 10)) || 2;
        const synth = config.synthesizer || {};
        const backendGain = Array.isArray(synth.channelGain) ? synth.channelGain : [];
        const backendMute = Array.isArray(synth.channelMute) ? synth.channelMute : [];
        const backendName = Array.isArray(synth.channelName) ? synth.channelName : [];

        const newGain = [];
        const newMute = [];
        const newName = [];
        for (let ch = 0; ch < newChannels; ch++) {
            newGain.push(typeof backendGain[ch] === 'number' ? backendGain[ch] : -30);
            newMute.push(backendMute[ch] === true);
            newName.push(typeof backendName[ch] === 'string' ? backendName[ch] : '');
        }

        channelGain = newGain;
        channelMute = newMute;
        channelName = newName;
        numChannels = newChannels;

        renderChannelOptions();
        syncSelectedChannel();
        updateControls();
    }

    // Wire the shared channel dropdown and the gain/mute controls.
    function wireControls() {
        const select = el('routing-channel-select');
        if (select) {
            select.addEventListener('change', function () {
                const n = parseInt(select.value, 10);
                selectedChannel = isNaN(n) ? 0 : n;
                updateControls();
            });
        }

        const gainInput = el('synth-channel-gain');
        if (gainInput) {
            gainInput.addEventListener('change', onGainChange);
        }

        const muteCheckbox = el('synth-channel-mute');
        if (muteCheckbox) {
            muteCheckbox.addEventListener('change', onMuteChange);
        }

        const nameInput = el('synth-channel-name');
        if (nameInput) {
            nameInput.addEventListener('change', onNameChange);
        }
    }

    async function init() {
        // Only run on pages that contain the channel gain control.
        if (!el('synth-channel-gain')) {
            return;
        }

        wireControls();

        // Load the current config and render.
        try {
            const response = await fetch('/api/config?t=' + Date.now());
            if (response.ok) {
                const config = await response.json();
                applyConfig(config);
            }
        } catch (err) {
            console.error('channel-gains: failed to load config', err);
        }
    }

    // Expose applyConfig so settings.js can notify us when a config reload
    // completes via a global 'configloaded' custom event.
    document.addEventListener('configloaded', function (event) {
        if (event.detail) {
            applyConfig(event.detail);
        }
    });

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();

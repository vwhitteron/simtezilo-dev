// routing-matrix.js — output routing control for the synthesizer. Mirrors the
// EQ panel idiom: a channel <select> picks one output channel, and three
// form-switch toggles (Engine / Chassis / Transmission) reflect and edit the
// routing for that channel. Reads synthesizer.routing and haptics.output.channels
// from /api/config and saves per-source rows back via POST, mirroring the pattern
// used by settings.js / audio-settings.js.
(function () {
    'use strict';

    // Ordered list of sources mapped to their switch element ids. Keys must match
    // the backend constants: config.RoutingSourceEngine / Chassis / Transmission.
    const SOURCES = [
        { key: 'engine',       toggleId: 'routing-engine' },
        { key: 'chassis',      toggleId: 'routing-chassis' },
        { key: 'transmission', toggleId: 'routing-transmission' },
    ];

    // In-memory copy of the routing state, keyed by source name.
    // { engine: [true, true, ...], chassis: [...], transmission: [...] }
    let routing = {};
    let numChannels = 2;
    let selectedChannel = 0;

    // Debounce handles: one per source, so rapid toggles within a channel are
    // coalesced into a single POST rather than firing per change.
    const saveTimeouts = {};

    function el(id) {
        return document.getElementById(id);
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
            option.textContent = 'Ch' + ch;
            select.appendChild(option);
        }
        select.value = String(selectedChannel);
    }

    // Reflect the routing of the selected channel onto the three switches.
    function updateSwitches() {
        SOURCES.forEach(function (source) {
            const toggle = el(source.toggleId);
            if (!toggle) {
                return;
            }
            const row = routing[source.key] || [];
            toggle.checked = row[selectedChannel] !== false;
            updateButtonIcon(source);
        });
    }

    // Swap the button icon to reflect selected (circle-check) / unselected
    // (circle-xmark) state, mirroring the mute-button icon pattern in settings.js.
    async function updateButtonIcon(source) {
        const toggle = el(source.toggleId);
        if (!toggle || typeof IconHelper === 'undefined') {
            return;
        }

        const label = document.querySelector('label[for="' + source.toggleId + '"]');
        const iconSpan = label && label.querySelector('.routing-icon');
        if (!iconSpan) {
            return;
        }

        const svg = await IconHelper.loadIcon(toggle.checked ? 'fa-circle-check' : 'fa-circle-xmark');
        if (svg) {
            iconSpan.innerHTML = svg;
        }
    }

    // Called when a source switch is toggled for the selected channel.
    function onSwitchChange(source) {
        const toggle = el(source.toggleId);
        if (!toggle) {
            return;
        }

        if (!routing[source.key]) {
            routing[source.key] = new Array(numChannels).fill(true);
        }
        routing[source.key][selectedChannel] = toggle.checked;

        updateButtonIcon(source);
        scheduleSave(source.key);
    }

    // Debounce saves per source so rapid toggles collapse into one POST.
    function scheduleSave(source) {
        clearTimeout(saveTimeouts[source]);
        saveTimeouts[source] = setTimeout(function () {
            saveSource(source);
        }, 300);
    }

    // POST the current row for one source to /api/config.
    async function saveSource(source) {
        const row = routing[source] || new Array(numChannels).fill(true);

        try {
            const response = await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    synthesizer: {
                        routing: {
                            [source]: row.slice(),
                        },
                    },
                }),
            });

            if (!response.ok) {
                console.error('routing-matrix: save failed for source ' + source,
                    response.status);
            }
        } catch (err) {
            console.error('routing-matrix: network error saving source ' + source, err);
        }
    }

    // Resize every routing row to the given channel count, defaulting new slots to
    // true (matching backend normalisation in config.normaliseRouting).
    function resizeRows(n) {
        SOURCES.forEach(function (source) {
            const current = routing[source.key] || [];
            const next = [];
            for (let ch = 0; ch < n; ch++) {
                next.push(current[ch] !== false);
            }
            routing[source.key] = next;
        });
        numChannels = n;
    }

    // Ingest a fresh config payload (from /api/config) into local state and
    // refresh the control.
    function applyConfig(config) {
        const hapticsOutput = (config.haptics && config.haptics.output) || {};
        const newChannels = (hapticsOutput.channels && parseInt(hapticsOutput.channels, 10)) || 2;
        const synthRouting = (config.synthesizer && config.synthesizer.routing) || {};

        const newRouting = {};
        SOURCES.forEach(function (source) {
            const backendRow = synthRouting[source.key] || [];
            const row = [];
            for (let ch = 0; ch < newChannels; ch++) {
                row.push(backendRow[ch] !== false);
            }
            newRouting[source.key] = row;
        });

        routing = newRouting;
        numChannels = newChannels;

        renderChannelOptions();
        updateSwitches();
    }

    // Wire the channel dropdown and the three source switches.
    function wireControls() {
        const select = el('routing-channel-select');
        if (select) {
            select.addEventListener('change', function () {
                const n = parseInt(select.value, 10);
                selectedChannel = isNaN(n) ? 0 : n;
                updateSwitches();
            });
        }

        SOURCES.forEach(function (source) {
            const toggle = el(source.toggleId);
            if (toggle) {
                toggle.addEventListener('change', function () {
                    onSwitchChange(source);
                });
            }
        });
    }

    // Wire into the channel-count input so the control updates immediately when the
    // user changes the channel count (before a config reload fires).
    function wireChannelInput() {
        const channelInput = el('audio-haptics-channels');
        if (!channelInput) {
            return;
        }

        channelInput.addEventListener('change', function () {
            const n = parseInt(channelInput.value, 10);
            if (!n || n < 1) {
                return;
            }

            resizeRows(n);
            renderChannelOptions();
            updateSwitches();
        });
    }

    async function init() {
        // Only run on pages that contain the routing control.
        if (!el('routing-channel-select')) {
            return;
        }

        wireControls();
        wireChannelInput();

        // Load the current config and render.
        try {
            const response = await fetch('/api/config?t=' + Date.now());
            if (response.ok) {
                const config = await response.json();
                applyConfig(config);
            }
        } catch (err) {
            console.error('routing-matrix: failed to load config', err);
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

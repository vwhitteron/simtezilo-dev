// routing-matrix.js — per-source output channel routing for the synthesizer.
// Each haptics source (Engine / Chassis / Transmission) owns a pill-style
// multi-select under its Output Level row: enabled output channels are shown as
// removable pills, and clicking the field pops up a list of the channels that
// aren't yet routed. Reads synthesizer.routing and haptics.output.channels from
// /api/config and saves per-source rows back via POST, mirroring the pattern
// used by settings.js / channel-gains.js.
(function () {
    'use strict';

    // Shared channel label formatter ("name (n)"); guard-defined so a single
    // canonical implementation is shared with channel-gains.js / settings.js.
    window.channelDisplayLabel = window.channelDisplayLabel || function (ch, names) {
        let name = Array.isArray(names) && typeof names[ch] === 'string' ? names[ch].trim() : '';
        if (!name) {
            name = 'Channel';
        }

        return name + ' (' + (ch + 1) + ')';
    };

    // Ordered list of sources mapped to their DOM element ids. Keys must match
    // the backend constants: config.RoutingSourceEngine / Chassis / Transmission.
    const SOURCES = [
        { key: 'engine',       pillsId: 'routing-engine-pills',       menuId: 'routing-engine-menu',       placeholderId: 'routing-engine-placeholder' },
        { key: 'chassis',      pillsId: 'routing-chassis-pills',      menuId: 'routing-chassis-menu',      placeholderId: 'routing-chassis-placeholder' },
        { key: 'texture',      pillsId: 'routing-texture-pills',      menuId: 'routing-texture-menu',      placeholderId: 'routing-texture-placeholder' },
        { key: 'transmission', pillsId: 'routing-transmission-pills', menuId: 'routing-transmission-menu', placeholderId: 'routing-transmission-placeholder' },
    ];

    // In-memory copy of the routing state, keyed by source name.
    // { engine: [true, true, ...], chassis: [...], transmission: [...] }
    let routing = {};
    let numChannels = 2;
    let channelNames = [];

    // Debounce handles: one per source, so rapid edits within a source are
    // coalesced into a single POST rather than firing per change.
    const saveTimeouts = {};

    function el(id) {
        return document.getElementById(id);
    }

    // The field wrapper for a source (carries data-routing-source + .open state).
    function fieldEl(source) {
        return document.querySelector('.channel-pill-select[data-routing-source="' + source.key + '"]');
    }

    // Render the pills, the add-menu items and the placeholder for one source.
    function renderSource(source) {
        const row = routing[source.key] || [];

        const list = el(source.pillsId);
        if (list) {
            list.innerHTML = '';
            for (let ch = 0; ch < numChannels; ch++) {
                if (row[ch] === false) {
                    continue;
                }
                list.appendChild(buildPill(source, ch));
            }
        }

        const available = [];
        for (let ch = 0; ch < numChannels; ch++) {
            if (row[ch] === false) {
                available.push(ch);
            }
        }

        const menu = el(source.menuId);
        if (menu) {
            menu.innerHTML = '';
            available.forEach(function (ch) {
                menu.appendChild(buildMenuItem(source, ch));
            });
        }

        // Placeholder hint is shown only while there's still a channel to add.
        const placeholder = el(source.placeholderId);
        if (placeholder) {
            placeholder.style.display = available.length ? '' : 'none';
        }

        // If the open menu just ran out of options, close it.
        if (!available.length) {
            closeMenu(source);
        }
    }

    // Build a removable pill element for one channel of one source.
    function buildPill(source, ch) {
        const pill = document.createElement('span');
        pill.className = 'channel-pill';

        const label = document.createElement('span');
        label.className = 'channel-pill-label';
        label.textContent = window.channelDisplayLabel(ch, channelNames);
        pill.appendChild(label);

        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'channel-pill-remove';
        const removeLabel = window.t ? window.t('runmode.settings.routing.remove') : 'Remove';
        remove.setAttribute('aria-label', removeLabel + ' ' + window.channelDisplayLabel(ch, channelNames));
        remove.innerHTML = '&times;';
        remove.addEventListener('click', function (event) {
            // Don't let the field's click handler treat this as an open request.
            event.stopPropagation();
            setChannel(source, ch, false);
        });
        pill.appendChild(remove);

        return pill;
    }

    // Build one clickable "add this channel" row for the popup menu.
    function buildMenuItem(source, ch) {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'channel-pill-menu-item';
        item.textContent = window.channelDisplayLabel(ch, channelNames);
        item.addEventListener('click', function (event) {
            // Keep the menu open across adds so multiple channels can be picked.
            event.stopPropagation();
            setChannel(source, ch, true);
        });
        return item;
    }

    // Open the popup for one source, closing any other that's open.
    function openMenu(source) {
        SOURCES.forEach(function (other) {
            if (other !== source) {
                closeMenu(other);
            }
        });
        const field = fieldEl(source);
        if (field) {
            field.classList.add('open');
            field.setAttribute('aria-expanded', 'true');
        }
    }

    function closeMenu(source) {
        const field = fieldEl(source);
        if (field) {
            field.classList.remove('open');
            field.setAttribute('aria-expanded', 'false');
        }
    }

    function isOpen(source) {
        const field = fieldEl(source);
        return !!(field && field.classList.contains('open'));
    }

    // Enable or disable one channel for one source, then re-render and save.
    function setChannel(source, ch, enabled) {
        if (!routing[source.key]) {
            routing[source.key] = new Array(numChannels).fill(true);
        }
        routing[source.key][ch] = enabled;

        renderSource(source);
        scheduleSave(source.key);
    }

    // Debounce saves per source so rapid edits collapse into one POST.
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

    // Ingest a fresh config payload (from /api/config) into local state and
    // refresh every source control.
    function applyConfig(config) {
        const hapticsOutput = (config.haptics && config.haptics.output) || {};
        const newChannels = (hapticsOutput.channels && parseInt(hapticsOutput.channels, 10)) || 2;
        const synthRouting = (config.synthesizer && config.synthesizer.routing) || {};
        channelNames = (config.synthesizer && Array.isArray(config.synthesizer.channelName))
            ? config.synthesizer.channelName
            : [];

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

        SOURCES.forEach(renderSource);
    }

    // Wire the click-to-open popup for each source, plus a document handler that
    // closes any open popup on an outside click.
    function wireControls() {
        SOURCES.forEach(function (source) {
            const field = fieldEl(source);
            if (field) {
                field.addEventListener('click', function () {
                    if (isOpen(source)) {
                        closeMenu(source);
                    } else if (el(source.menuId) && el(source.menuId).children.length) {
                        openMenu(source);
                    }
                });

                field.addEventListener('keydown', function (event) {
                    if (event.key === 'Enter' || event.key === ' ' || event.key === 'Spacebar') {
                        event.preventDefault();
                        if (isOpen(source)) {
                            closeMenu(source);
                        } else if (el(source.menuId) && el(source.menuId).children.length) {
                            openMenu(source);
                        }
                    } else if (event.key === 'Escape') {
                        closeMenu(source);
                        field.focus();
                    }
                });
            }
        });

        document.addEventListener('click', function (event) {
            SOURCES.forEach(function (source) {
                const field = fieldEl(source);
                if (field && !field.contains(event.target)) {
                    closeMenu(source);
                }
            });
        });
    }

    function init() {
        // Only run on pages that contain the routing controls.
        if (!el('routing-engine-pills')) {
            return;
        }

        wireControls();

        // Load the current config and render.
        fetch('/api/config?t=' + Date.now())
            .then(function (response) {
                return response.ok ? response.json() : null;
            })
            .then(function (config) {
                if (config) {
                    applyConfig(config);
                }
            })
            .catch(function (err) {
                console.error('routing-matrix: failed to load config', err);
            });
    }

    // Expose applyConfig so settings.js can notify us when a config reload
    // completes via a global 'configloaded' custom event.
    document.addEventListener('configloaded', function (event) {
        if (event.detail) {
            applyConfig(event.detail);
        }
    });

    // Relabel the pills/menus live when channel names are edited elsewhere on
    // the page (channel-gains.js), without waiting for a full config reload.
    document.addEventListener('channelnameschanged', function (event) {
        if (Array.isArray(event.detail)) {
            channelNames = event.detail;
            SOURCES.forEach(renderSource);
        }
    });

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();

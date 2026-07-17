// bluetooth-settings.js — dynamic behaviour for the Bluetooth settings panel.
//
// The panel lets the user scan for, pair, connect, disconnect and forget
// Bluetooth devices. It talks to /api/bluetooth/devices (GET, optionally
// ?scan=true) and /api/bluetooth/action (POST). The whole section is hidden when
// no Bluetooth adapter is available (non-simtezilo builds or no hardware).
(function () {
    'use strict';

    // How often to refresh the device list while the panel is visible, to keep
    // connection state live (matches the config-status poll cadence).
    const POLL_INTERVAL_MS = 4000;

    let pollTimer = null;
    let scanning = false;
    let available = false;
    // Unpaired devices found by the last scan. Retained so the periodic poll
    // (which lists paired/connected devices only) doesn't drop them from view
    // before the user has had a chance to pair them.
    let discovered = [];
    // Last rendered data, kept so the view can be re-drawn with fresh
    // translations when the language changes without re-hitting the API.
    let lastDevices = null;
    let lastAdapter;

    function el(id) {
        return document.getElementById(id);
    }

    // Translate key, falling back to the English text. The fallback covers both
    // a missing translation and the brief window before i18n has loaded (t()
    // returns '' until then), so labels are never blank.
    function tr(key, fallback) {
        const value = (typeof t === 'function') ? t(key) : '';
        return value || fallback;
    }

    // Semantic device type → embedded SVG icon name (/images/icons/NAME.svg).
    function deviceIcon(type) {
        switch (type) {
            case 'speaker':
                return 'volume';
            case 'headset':
                return 'headset';
            case 'headphones':
                return 'headphones';
            case 'fan':
                return 'fan';
            default:
                return 'bluetooth-b';
        }
    }

    function statusBadge(device) {
        if (device.connected) {
            return '<span class="badge bg-success">' +
                escapeHtml(tr('runmode.settings.bluetooth.state.connected', 'Connected')) + '</span>';
        }
        if (device.paired) {
            return '<span class="badge bg-secondary">' +
                escapeHtml(tr('runmode.settings.bluetooth.state.paired', 'Paired')) + '</span>';
        }
        return '<span class="badge bg-light text-dark">' +
            escapeHtml(tr('runmode.settings.bluetooth.state.available', 'Available')) + '</span>';
    }

    // Build the action buttons appropriate to a device's state.
    function actionButtons(device) {
        const addr = device.address;
        const buttons = [];

        if (device.connected) {
            buttons.push(btn('disconnect', addr, 'btn-outline-secondary',
                tr('runmode.settings.bluetooth.action.disconnect', 'Disconnect')));
        } else if (device.paired) {
            buttons.push(btn('connect', addr, 'btn-outline-primary',
                tr('runmode.settings.bluetooth.action.connect', 'Connect')));
        } else {
            buttons.push(btn('pair', addr, 'btn-outline-primary',
                tr('runmode.settings.bluetooth.action.pair', 'Pair')));
        }

        // Forget removes a known device from BlueZ. Gate on "known to BlueZ"
        // rather than paired alone: some devices (e.g. the simtezilo fan) connect
        // without ever bonding, so paired stays false while the device is very
        // much present and removable. bt-remove works regardless of bond state.
        // A fresh scan-only "Available" device (all flags false) has nothing to
        // forget, so it still gets only Pair.
        if (device.paired || device.connected || device.trusted) {
            buttons.push(btn('remove', addr, 'btn-outline-danger',
                tr('runmode.settings.bluetooth.action.forget', 'Forget')));
        }

        return buttons.join(' ');
    }

    function btn(action, address, cls, label) {
        return '<button type="button" class="btn btn-sm ' + cls +
            ' bluetooth-action" data-action="' + action +
            '" data-address="' + escapeAttr(address) + '">' + escapeHtml(label) + '</button>';
    }

    function escapeHtml(s) {
        const div = document.createElement('div');
        div.textContent = s == null ? '' : String(s);
        return div.innerHTML;
    }

    function escapeAttr(s) {
        return escapeHtml(s).replace(/"/g, '&quot;');
    }

    function renderDevices(devices) {
        const tbody = el('bluetooth-device-list');
        const empty = el('bluetooth-empty');
        if (!tbody) {
            return;
        }

        devices = devices || [];
        lastDevices = devices;

        if (!devices.length) {
            tbody.innerHTML = '';
            if (empty) {
                empty.style.display = '';
            }
            return;
        }

        if (empty) {
            empty.style.display = 'none';
        }

        tbody.innerHTML = devices.map(device => {
            return '<tr>' +
                '<td><span class="icon me-2" data-icon="' + deviceIcon(device.type) + '"></span>' +
                escapeHtml(device.name) + '</td>' +
                '<td class="small text-muted">' + escapeHtml(device.address) + '</td>' +
                '<td>' + statusBadge(device) + '</td>' +
                '<td class="text-end">' + actionButtons(device) + '</td>' +
                '</tr>';
        }).join('');

        applyIcons(tbody);
        wireActionButtons();
    }

    // Render the embedded SVG for each [data-icon] placeholder within container.
    // The page's one-time data-icon scanner only runs at load, so dynamically
    // built rows must be resolved here (same approach as the mute buttons).
    function applyIcons(container) {
        if (typeof IconHelper === 'undefined') {
            return;
        }

        container.querySelectorAll('[data-icon]').forEach(async (element) => {
            const name = element.getAttribute('data-icon');
            if (!name) {
                return;
            }

            const svg = await IconHelper.loadIcon(name);
            if (svg) {
                element.innerHTML = svg;
            }
        });
    }

    function updateAdapterStatus(adapter) {
        lastAdapter = adapter;
        const statusEl = el('bluetooth-adapter-status');
        if (!statusEl) {
            return;
        }

        const label = tr('runmode.settings.bluetooth.adapter', 'Adapter') + ': ';

        // state renders the status word in a coloured span (green = on, red = off/
        // unavailable) alongside the plain-text label.
        const state = (text, cls) =>
            '<span class="' + cls + '">' + escapeHtml(text) + '</span>';

        if (!adapter || !adapter.present) {
            statusEl.innerHTML = label +
                state(tr('runmode.settings.bluetooth.adapter.notpresent', 'not present'), 'text-danger');
            return;
        }

        let html = label + (adapter.powered
            ? state(tr('runmode.settings.bluetooth.adapter.on', 'on'), 'text-success')
            : state(tr('runmode.settings.bluetooth.adapter.off', 'off'), 'text-danger'));
        if (adapter.discovering) {
            html += ' <span class="text-muted">(' +
                escapeHtml(tr('runmode.settings.bluetooth.adapter.scanning', 'scanning')) + ')</span>';
        }
        statusEl.innerHTML = html;
    }

    async function fetchDevices(scan) {
        const url = '/api/bluetooth/devices' + (scan ? '?scan=true' : '');
        const response = await fetch(url);
        return response.json();
    }

    async function refresh(scan) {
        try {
            const data = await fetchDevices(scan);

            available = !!(data && data.available);
            applyAvailability();

            if (!available) {
                return;
            }

            updateAdapterStatus(data.adapter);

            const devices = data.devices || [];

            if (scan) {
                // Remember the unpaired finds so later polls keep showing them.
                discovered = devices.filter(d => !d.paired && !d.connected);
                renderDevices(devices);
            } else {
                // A poll lists paired/connected devices only; merge back any
                // still-unpaired discovered devices so they don't vanish.
                renderDevices(mergeDiscovered(devices));
            }
        } catch (err) {
            console.error('bluetooth-settings: failed to list devices', err);
        }
    }

    // Append retained discovered devices that aren't already in the listed set
    // (a discovered device that has since been paired appears in `listed` and
    // takes precedence, so it won't be duplicated).
    function mergeDiscovered(listed) {
        const seen = new Set(listed.map(d => d.address));
        return listed.concat(discovered.filter(d => !seen.has(d.address)));
    }

    // Show or hide the Bluetooth panel (in the System section) depending on
    // adapter availability.
    function applyAvailability() {
        const panel = el('bluetooth-settings');
        if (panel) {
            panel.style.display = available ? '' : 'none';
        }
    }

    async function doScan() {
        if (scanning) {
            return;
        }
        scanning = true;

        const button = el('bluetooth-scan');
        const status = el('bluetooth-scan-status');
        if (button) {
            button.disabled = true;
        }
        if (status) {
            status.textContent = tr('runmode.settings.bluetooth.scanning', 'Scanning…');
        }

        try {
            await refresh(true);
        } finally {
            scanning = false;
            if (button) {
                button.disabled = false;
            }
            if (status) {
                status.textContent = '';
            }
        }
    }

    // Show or clear the inline action status banner. kind is 'success' or
    // 'error'; passing no message hides the banner.
    function showActionStatus(kind, message) {
        const banner = el('bluetooth-action-status');
        if (!banner) {
            return;
        }
        if (!message) {
            banner.style.display = 'none';
            banner.textContent = '';
            return;
        }
        banner.className = 'alert py-2 px-3 small alert-' + (kind === 'success' ? 'success' : 'danger');
        banner.textContent = message;
        banner.style.display = '';
    }

    async function doAction(action, address) {
        showActionStatus();
        let succeeded = false;
        try {
            const response = await fetch('/api/bluetooth/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: action, address: address }),
            });
            const data = await response.json();
            if (data && data.status === 'success') {
                succeeded = true;
            } else {
                const detail = (data && data.message) ? data.message : tr('runmode.settings.bluetooth.requestfailed', 'request failed');
                console.error('bluetooth-settings: action failed', data);
                showActionStatus('error', tr('runmode.settings.bluetooth.actionfailed', 'Action failed') + ': ' + detail);
            }
        } catch (err) {
            console.error('bluetooth-settings: action error', err);
            showActionStatus('error', tr('runmode.settings.bluetooth.actionfailed', 'Action failed') + ': ' + err.message);
        }

        await refresh(false);

        // Pairing/forgetting a device adds or removes its bluealsa output, and
        // connecting changes which one the bridge is labelled with, so let the
        // audio-device dropdowns (which fetch only at load) rebuild their lists
        // without a page reload.
        if (succeeded) {
            document.dispatchEvent(new CustomEvent('bluetooth-devices-changed'));
        }
    }

    // Redraw the device list and adapter status from cached data so a language
    // change (or i18n finishing its initial load after the first render) takes
    // effect without another API round-trip.
    function rerender() {
        if (lastDevices !== null) {
            renderDevices(lastDevices);
        }
        if (lastAdapter !== undefined) {
            updateAdapterStatus(lastAdapter);
        }
    }

    function wireActionButtons() {
        document.querySelectorAll('.bluetooth-action').forEach(button => {
            button.addEventListener('click', () => {
                const action = button.dataset.action;
                const address = button.dataset.address;
                button.disabled = true;
                doAction(action, address);
            });
        });
    }

    // Poll only while the Bluetooth section is the active one, to avoid
    // shelling out to the helper when the user isn't looking at it.
    function isSectionActive() {
        const card = document.querySelector('[data-section-content="system"]');
        return card && card.classList.contains('active');
    }

    function startPolling() {
        if (pollTimer) {
            return;
        }
        pollTimer = setInterval(() => {
            if (isSectionActive() && available && !scanning) {
                refresh(false);
            }
        }, POLL_INTERVAL_MS);
    }

    function wireEvents() {
        const scanButton = el('bluetooth-scan');
        if (scanButton) {
            scanButton.addEventListener('click', doScan);
        }

        // Refresh immediately when the user opens the System section (which now
        // hosts the Bluetooth panel).
        document.querySelectorAll('.settings-nav-link[data-section="system"]').forEach(link => {
            link.addEventListener('click', () => refresh(false));
        });
    }

    async function init() {
        // Only run on pages that contain the Bluetooth panel.
        if (!el('bluetooth-device-list')) {
            return;
        }

        wireEvents();
        // Re-render with the right language once translations arrive and whenever
        // the user switches language, since rows are built in JS (not via
        // data-i18n, which only the static markup carries).
        window.addEventListener('i18nLoaded', rerender);
        window.addEventListener('i18nLanguageChanged', rerender);
        await refresh(false);
        startPolling();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();

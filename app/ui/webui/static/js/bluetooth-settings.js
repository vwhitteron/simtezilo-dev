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

    function el(id) {
        return document.getElementById(id);
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
            default:
                return 'bluetooth-b';
        }
    }

    function statusBadge(device) {
        if (device.connected) {
            return '<span class="badge bg-success">Connected</span>';
        }
        if (device.paired) {
            return '<span class="badge bg-secondary">Paired</span>';
        }
        return '<span class="badge bg-light text-dark">Available</span>';
    }

    // Build the action buttons appropriate to a device's state.
    function actionButtons(device) {
        const addr = device.address;
        const buttons = [];

        if (device.connected) {
            buttons.push(btn('disconnect', addr, 'btn-outline-secondary', 'Disconnect'));
        } else if (device.paired) {
            buttons.push(btn('connect', addr, 'btn-outline-primary', 'Connect'));
        } else {
            buttons.push(btn('pair', addr, 'btn-outline-primary', 'Pair'));
        }

        if (device.paired) {
            buttons.push(btn('remove', addr, 'btn-outline-danger', 'Forget'));
        }

        return buttons.join(' ');
    }

    function btn(action, address, cls, label) {
        return '<button type="button" class="btn btn-sm ' + cls +
            ' bluetooth-action" data-action="' + action +
            '" data-address="' + escapeAttr(address) + '">' + label + '</button>';
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
        const statusEl = el('bluetooth-adapter-status');
        if (!statusEl) {
            return;
        }

        if (!adapter || !adapter.present) {
            statusEl.textContent = 'Adapter: not present';
            return;
        }

        let text = 'Adapter: ' + (adapter.powered ? 'on' : 'off');
        if (adapter.discovering) {
            text += ' (scanning)';
        }
        statusEl.textContent = text;
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

    // Show or hide the Bluetooth nav item depending on adapter availability.
    function applyAvailability() {
        const navItem = el('bluetooth-nav-item');
        if (navItem) {
            navItem.style.display = available ? '' : 'none';
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
            status.textContent = 'Scanning…';
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
        try {
            const response = await fetch('/api/bluetooth/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: action, address: address }),
            });
            const data = await response.json();
            if (!data || data.status !== 'success') {
                const detail = (data && data.message) ? data.message : 'request failed';
                console.error('bluetooth-settings: action failed', data);
                showActionStatus('error', capitalize(action) + ' failed: ' + detail);
            }
        } catch (err) {
            console.error('bluetooth-settings: action error', err);
            showActionStatus('error', capitalize(action) + ' failed: ' + err.message);
        }

        await refresh(false);
    }

    function capitalize(s) {
        return s ? s.charAt(0).toUpperCase() + s.slice(1) : s;
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
        const card = document.querySelector('[data-section-content="bluetooth"]');
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

        // Refresh immediately when the user opens the Bluetooth section.
        document.querySelectorAll('.settings-nav-link[data-section="bluetooth"]').forEach(link => {
            link.addEventListener('click', () => refresh(false));
        });
    }

    async function init() {
        // Only run on pages that contain the Bluetooth panel.
        if (!el('bluetooth-device-list')) {
            return;
        }

        wireEvents();
        await refresh(false);
        startPolling();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();

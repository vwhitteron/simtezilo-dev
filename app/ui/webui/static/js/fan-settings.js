// fan-settings.js — populates the fan device dropdown on the Fan settings panel.
//
// The fan/wind-simulator is a Bluetooth device paired through the Bluetooth
// panel. This module fills the #fan-device <select> with the paired devices the
// platform helper classifies as type "fan" (see /api/bluetooth/devices), keyed
// by MAC address. Static binding to config (fan.deviceAddress) is handled by
// settings.js via the data-config attribute; this module builds the option list,
// restores the saved selection, and keeps the cached friendly name
// (fan.deviceName, bound to a hidden input) current so an offline device shows
// its name rather than a bare MAC address.
(function () {
    'use strict';

    function el(id) {
        return document.getElementById(id);
    }

    // Saved values from /api/config, used to restore the selection and to label
    // the device when it is offline (absent from the live Bluetooth list).
    let savedAddress = '';
    let savedName = '';
    // The deviceName last known to be persisted server-side, so we only write
    // back when it actually changes (a new sighting or a different selection).
    let persistedName = '';

    async function fetchSaved() {
        try {
            const response = await fetch('/api/config?t=' + Date.now());
            if (!response.ok) {
                return;
            }
            const config = await response.json();
            const fan = (config && config.fan) || {};
            savedAddress = fan.deviceAddress || '';
            savedName = fan.deviceName || '';
            persistedName = savedName;
        } catch (err) {
            console.error('fan-settings: failed to load config', err);
        }
    }

    // Persist the cached friendly name when it changes. Like the device-address
    // select, this auto-saves a single field (every /api/config POST is written
    // to file), so the name survives even when the device later goes offline.
    async function persistDeviceName(name) {
        name = name || '';
        if (name === persistedName) {
            return;
        }
        persistedName = name;
        setHiddenName(name);
        try {
            await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ fan: { deviceName: name } }),
            });
        } catch (err) {
            console.error('fan-settings: failed to persist device name', err);
        }
    }

    async function fetchFanDevices() {
        try {
            const response = await fetch('/api/bluetooth/devices');
            const data = await response.json();
            if (!data || !data.available) {
                return { available: false, devices: [] };
            }
            const devices = (data.devices || []).filter(d => d.type === 'fan' && d.paired);
            return { available: true, devices: devices };
        } catch (err) {
            console.error('fan-settings: failed to list devices', err);
            return { available: false, devices: [] };
        }
    }

    function placeholderText() {
        const opt = document.querySelector('#fan-device option[value=""]');
        return (opt && opt.textContent) || '(pair in Bluetooth panel)';
    }

    // Mirror the cached friendly name into the hidden input so it is persisted
    // through the normal data-config save path alongside the address.
    function setHiddenName(name) {
        const hidden = el('fan-device-name');
        if (hidden) {
            hidden.value = name || '';
        }
    }

    // Add an <option>, stashing the clean device name in a data attribute so the
    // change handler can recover it without parsing any display suffix.
    function addOption(select, value, label, name) {
        const option = document.createElement('option');
        option.value = value;
        option.textContent = label;
        option.dataset.deviceName = name || '';
        select.appendChild(option);
        return option;
    }

    // Rebuild the <select>, restoring the saved address. The saved device is
    // always kept selectable so the stored config value is never silently
    // dropped; when it is offline (not in the live list) it is labelled with its
    // cached friendly name rather than a bare MAC address.
    function populate(devices) {
        const select = el('fan-device');
        if (!select) {
            return;
        }

        select.innerHTML = '';
        addOption(select, '', placeholderText(), '');

        let liveName = '';
        devices.forEach(device => {
            const name = device.name || device.address;
            addOption(select, device.address, name, name);
            if (device.address === savedAddress) {
                liveName = name;
            }
        });

        // The saved device was seen live: refresh the cached name if it changed.
        if (savedAddress && liveName && liveName !== savedName) {
            savedName = liveName;
        }

        // The saved device is offline: keep it selectable, labelled by its cached
        // name (falling back to the address only if we have never seen a name).
        const known = devices.some(d => d.address === savedAddress);
        if (savedAddress && !known) {
            const display = savedName || savedAddress;
            addOption(select, savedAddress, display + ' (not connected)', savedName);
        }

        select.value = savedAddress || '';

        // Keep the persisted name in step with the current selection, writing it
        // back if the live name has changed since it was last cached.
        const selected = select.options[select.selectedIndex];
        const selectedName = selected ? selected.dataset.deviceName : '';
        setHiddenName(selectedName);
        if (savedAddress) {
            persistDeviceName(selectedName);
        }
    }

    async function refresh() {
        if (!el('fan-device')) {
            return;
        }
        await fetchSaved();
        const result = await fetchFanDevices();
        populate(result.devices);
    }

    function wireEvents() {
        // Refresh when the user opens the Fan section (devices may have been
        // paired since load).
        document.querySelectorAll('.settings-nav-link[data-section="fan"]').forEach(link => {
            link.addEventListener('click', refresh);
        });

        // Keep the saved address/name current when the user picks a device, so a
        // later refresh restores the right option and the name is persisted.
        const select = el('fan-device');
        if (select) {
            select.addEventListener('change', () => {
                savedAddress = select.value;
                const selected = select.options[select.selectedIndex];
                savedName = selected ? (selected.dataset.deviceName || '') : '';
                persistDeviceName(savedName);
            });
        }
    }

    async function init() {
        if (!el('fan-device')) {
            return;
        }
        wireEvents();
        await refresh();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();

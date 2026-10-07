// Navigation component for Simtezilo Web UI

// Theme selector support. The theme itself is managed by theme.js
// (window.simtezilo.theme); this file only renders and wires the selector.
const THEME_OPTIONS = [
    { value: 'dark', labelKey: 'runmode.theme.dark', fallback: 'Dark' },
    { value: 'light', labelKey: 'runmode.theme.light', fallback: 'Light' },
    { value: 'system', labelKey: 'runmode.theme.system', fallback: 'System' }
];

const THEME_SVG_ATTRS = 'width="16" height="16" viewBox="0 0 16 16" fill="currentColor" focusable="false"';
const THEME_ICONS = {
    dark: `<svg ${THEME_SVG_ATTRS}><path d="M6 .278a.77.77 0 0 1 .08.858 7.2 7.2 0 0 0-.878 3.46c0 4.021 3.278 7.277 7.318 7.277q.792-.001 1.533-.16a.79.79 0 0 1 .81.316.73.73 0 0 1-.031.893A8.35 8.35 0 0 1 8.344 16C3.734 16 0 12.286 0 7.71 0 4.266 2.114 1.312 5.124.06A.75.75 0 0 1 6 .278"/></svg>`,
    light: `<svg ${THEME_SVG_ATTRS}><path d="M8 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8M8 0a.5.5 0 0 1 .5.5v2a.5.5 0 0 1-1 0v-2A.5.5 0 0 1 8 0m0 13a.5.5 0 0 1 .5.5v2a.5.5 0 0 1-1 0v-2A.5.5 0 0 1 8 13m8-5a.5.5 0 0 1-.5.5h-2a.5.5 0 0 1 0-1h2a.5.5 0 0 1 .5.5M3 8a.5.5 0 0 1-.5.5h-2a.5.5 0 0 1 0-1h2A.5.5 0 0 1 3 8m10.657-5.657a.5.5 0 0 1 0 .707l-1.414 1.415a.5.5 0 1 1-.707-.708l1.414-1.414a.5.5 0 0 1 .707 0m-9.193 9.193a.5.5 0 0 1 0 .707L3.05 13.657a.5.5 0 0 1-.707-.707l1.414-1.414a.5.5 0 0 1 .707 0m9.193 2.121a.5.5 0 0 1-.707 0l-1.414-1.414a.5.5 0 0 1 .707-.707l1.414 1.414a.5.5 0 0 1 0 .707M4.464 4.465a.5.5 0 0 1-.707 0L2.343 3.05a.5.5 0 1 1 .707-.707l1.414 1.414a.5.5 0 0 1 0 .708"/></svg>`,
    system: `<svg ${THEME_SVG_ATTRS}><path d="M8 15A7 7 0 1 0 8 1zm0 1A8 8 0 1 1 8 0a8 8 0 0 1 0 16"/></svg>`
};
const THEME_CHECK = `<svg ${THEME_SVG_ATTRS}><path d="M13.854 3.646a.5.5 0 0 1 0 .708l-7 7a.5.5 0 0 1-.708 0l-3.5-3.5a.5.5 0 1 1 .708-.708L6.5 10.293l6.646-6.647a.5.5 0 0 1 .708 0"/></svg>`;

// The "dark" logo has white lettering for dark backgrounds; the "light" one is for light backgrounds.
function themeLogoSrc(effective) {
    return effective === 'light' ? '/images/simtezilo-logo-light.svg' : '/images/simtezilo-logo-dark.svg';
}

function syncThemeUI() {
    const api = window.simtezilo && window.simtezilo.theme;
    if (!api) return;
    const pref = api.get();

    const logo = document.getElementById('navbar-logo');
    if (logo) logo.src = themeLogoSrc(api.effective());

    const toggleIcon = document.getElementById('theme-toggle-icon');
    if (toggleIcon) toggleIcon.innerHTML = THEME_ICONS[pref] || THEME_ICONS.dark;

    document.querySelectorAll('[data-theme-option]').forEach(btn => {
        const isActive = btn.getAttribute('data-theme-option') === pref;
        btn.classList.toggle('active', isActive);
        btn.setAttribute('aria-pressed', String(isActive));
        const check = btn.querySelector('.theme-check');
        if (check) check.style.visibility = isActive ? 'visible' : 'hidden';
    });
}

document.addEventListener('click', event => {
    const btn = event.target.closest && event.target.closest('[data-theme-option]');
    if (!btn) return;
    const api = window.simtezilo && window.simtezilo.theme;
    if (api) api.set(btn.getAttribute('data-theme-option'));
});
document.addEventListener('themechange', syncThemeUI);
document.addEventListener('themepreferencechange', syncThemeUI);
function createNavigation(currentPage) {
    // Build telemetry dropdown - conditionally include Developer page based on devToolsEnabled
    const telemetryDropdown = [
        { path: '/telemetry', nameKey: 'runmode.nav.race', fallback: 'Race', id: 'telemetry' }
    ];

    // Only add Developer page if dev tools is enabled
    if (window.devToolsEnabled) {
        telemetryDropdown.push({ path: '/dev', nameKey: 'runmode.nav.developer', fallback: 'Developer', id: 'dev' });
    }

    const pages = [
        { path: '/settings', nameKey: 'runmode.nav.settings', fallback: 'Settings', id: 'settings' }
    ];

    // "Tools" dropdown. Logs is always available; the developer tools below it are
    // only listed when dev tools is enabled.
    const toolsDropdown = [
        { path: '/logs', nameKey: 'runmode.nav.logs', fallback: 'Logs', id: 'logs' }
    ];

    if (window.devToolsEnabled) {
        toolsDropdown.push({ path: '/tuneassist', nameKey: 'runmode.nav.tuneassist', fallback: 'Tune Assist', id: 'tuneassist' });
        toolsDropdown.push({ path: '/hardware', nameKey: 'runmode.nav.hardware', fallback: 'Hardware', id: 'hardware' });
    }

    const telemetryActive = telemetryDropdown.some(item => item.id === currentPage);
    const toolsActive = toolsDropdown.some(item => item.id === currentPage);

    const themeApi = window.simtezilo && window.simtezilo.theme;
    const themePref = themeApi ? themeApi.get() : 'dark';

    let navHTML = `
        <nav class="navbar navbar-expand-lg" style="background-color: var(--bs-content-bg); border-bottom: var(--bs-border-width) solid var(--bs-content-border-color);">
            <div class="container-fluid">
                <a class="navbar-brand" href="/">
                    <img id="navbar-logo" src="${themeLogoSrc(themeApi ? themeApi.effective() : 'dark')}" alt="Simtezilo" height="32" class="d-inline-block align-text-top">
                </a>
                <button class="navbar-toggler" type="button" data-bs-toggle="collapse" data-bs-target="#navbar-collapse-1" aria-controls="navbar-collapse-1" aria-expanded="false" aria-label="Toggle navigation">
                    <span class="navbar-toggler-icon"></span>
                </button>
                <div class="collapse navbar-collapse" id="navbar-collapse-1">
                    <ul class="navbar-nav me-auto mb-2 mb-lg-0">
                        <li class="nav-item dropdown">
                            <a class="nav-link dropdown-toggle${telemetryActive ? ' active' : ''}"${telemetryActive ? ' aria-current="page"' : ''} href="#" role="button" data-bs-toggle="dropdown" aria-expanded="false" data-i18n="runmode.nav.telemetry">
                                Telemetry
                            </a>
                            <ul class="dropdown-menu mt-lg-2 rounded-top-0">`;

    telemetryDropdown.forEach((item, index) => {
        const isActive = item.id === currentPage;
        navHTML += `
                                <li><a class="dropdown-item${isActive ? ' active' : ''}" href="${item.path}" data-i18n="${item.nameKey}">${item.fallback}</a></li>`;
    });

    navHTML += `
                            </ul>
                        </li>`;

    pages.forEach(page => {
        const isActive = page.id === currentPage;
        navHTML += `
                        <li class="nav-item">
                            <a class="nav-link${isActive ? ' active' : ''}"${isActive ? ' aria-current="page"' : ''} href="${page.path}" data-i18n="${page.nameKey}">${page.fallback}</a>
                        </li>`;
    });

    // Add Tools dropdown
    navHTML += `
                        <li class="nav-item dropdown">
                            <a class="nav-link dropdown-toggle${toolsActive ? ' active' : ''}"${toolsActive ? ' aria-current="page"' : ''} href="#" role="button" data-bs-toggle="dropdown" aria-expanded="false" data-i18n="runmode.nav.tools">
                                Tools
                            </a>
                            <ul class="dropdown-menu mt-lg-2 rounded-top-0">`;

    toolsDropdown.forEach(item => {
        const isActive = item.id === currentPage;
        navHTML += `
                                <li><a class="dropdown-item${isActive ? ' active' : ''}" href="${item.path}" data-i18n="${item.nameKey}">${item.fallback}</a></li>`;
    });

    navHTML += `
                            </ul>
                        </li>`;

    navHTML += `
                        <li class="nav-item">
                            <a class="nav-link" href="#" id="info-nav-link" role="button" aria-haspopup="dialog" aria-expanded="false" data-i18n="runmode.nav.info">Info</a>
                        </li>`;

    navHTML += `
                    </ul>`;

    // Add status indicators at the far right
    navHTML += `
                    <div class="d-flex align-items-center me-3">`;

    // Add server-unavailable indicator. Shown by the health monitor when the
    // backend stops responding, so the user knows the page is stale rather than
    // silently seeing frozen data.
    navHTML += `
                        <div id="server-unavailable-indicator" class="server-unavailable-indicator" role="status" aria-live="polite" style="display: none; margin-right: 1rem;">
                            <span id="server-unavailable-icon" class="icon" aria-hidden="true"></span>
                            <span data-i18n="runmode.status.serverunavailable">No connection</span>
                        </div>`;

    navHTML += `
                        <button type="button" id="restart-required-indicator" class="btn btn-outline-danger btn-sm" style="font-weight: 600; white-space: nowrap; display: none; margin-right: 1rem;">
                            <span data-i18n="runmode.settings.restart.required">Restart Required</span>
                        </button>`;

    // Add unified status indicator (used for both telemetry connection and settings save status)
    navHTML += `
                        <div id="nav-status-indicator" class="d-flex align-items-center justify-content-center" style="width: 24px; height: 24px; visibility: hidden;">
                            <div id="nav-spinner" class="spinner-border spinner-border-sm text-warning" role="status" style="display: none;">
                                <span class="visually-hidden">Loading...</span>
                            </div>
                            <span id="nav-success-icon" class="icon text-success" style="display: none; font-size: 1.25rem;"></span>
                            <span id="nav-error-icon" class="icon text-danger" style="display: none; font-size: 1.25rem;"></span>
                        </div>
                    </div>`;

    // Theme selector at the far right (stays in the collapsed mobile menu too)
    navHTML += `
                    <ul class="navbar-nav ms-lg-auto mb-2 mb-lg-0">
                        <li class="nav-item dropdown">
                            <button type="button" id="theme-dropdown-toggle" class="nav-link dropdown-toggle d-flex align-items-center gap-1" data-bs-toggle="dropdown" aria-expanded="false" aria-label="Theme" title="Theme" data-i18n-aria-label="runmode.theme.title" data-i18n-title="runmode.theme.title">
                                <span id="theme-toggle-icon" class="theme-icon" aria-hidden="true">${THEME_ICONS[themePref] || THEME_ICONS.dark}</span>
                            </button>
                            <ul class="dropdown-menu dropdown-menu-lg-end mt-lg-2 rounded-top-0" aria-label="Theme" data-i18n-aria-label="runmode.theme.title">`;

    THEME_OPTIONS.forEach(opt => {
        const isActive = opt.value === themePref;
        navHTML += `
                                <li><button type="button" class="dropdown-item theme-option d-flex align-items-center gap-2${isActive ? ' active' : ''}" data-theme-option="${opt.value}" aria-pressed="${isActive}">
                                    <span class="theme-icon" aria-hidden="true">${THEME_ICONS[opt.value]}</span>
                                    <span class="flex-grow-1" data-i18n="${opt.labelKey}">${opt.fallback}</span>
                                    <span class="theme-check" aria-hidden="true" style="visibility: ${isActive ? 'visible' : 'hidden'};">${THEME_CHECK}</span>
                                </button></li>`;
    });

    navHTML += `
                            </ul>
                        </li>
                    </ul>
                </div>
            </div>
        </nav>`;

    return navHTML;
}

// Helper functions to control the navbar status indicator
window.showNavbarStatus = function (type) {
    const indicator = document.getElementById('nav-status-indicator');
    const spinner = document.getElementById('nav-spinner');
    const successIcon = document.getElementById('nav-success-icon');
    const errorIcon = document.getElementById('nav-error-icon');

    if (!indicator) return;

    spinner.style.display = 'none';
    successIcon.style.display = 'none';
    errorIcon.style.display = 'none';

    indicator.style.visibility = 'visible';

    // Show appropriate indicator
    switch (type) {
        case 'saving':
            spinner.style.display = 'block';
            break;
        case 'success':
            successIcon.style.display = 'block';
            setTimeout(() => window.hideNavbarStatus(), 3000);
            break;
        case 'error':
            errorIcon.style.display = 'block';
            setTimeout(() => window.hideNavbarStatus(), 3000);
            break;
    }
};

window.hideNavbarStatus = function () {
    const indicator = document.getElementById('nav-status-indicator');
    const spinner = document.getElementById('nav-spinner');
    const successIcon = document.getElementById('nav-success-icon');
    const errorIcon = document.getElementById('nav-error-icon');

    if (indicator) {
        indicator.style.visibility = 'hidden';
    }

    // Also hide individual icons
    if (spinner) spinner.style.display = 'none';
    if (successIcon) successIcon.style.display = 'none';
    if (errorIcon) errorIcon.style.display = 'none';
};

// Shared application-restart flow, usable from any page. Both the settings-page
// "Restart" button and the top-right "Restart Required" callout delegate here so
// there is a single implementation of the confirm -> POST -> overlay -> poll
// sequence.
window.triggerRestart = async function () {
    if (!confirm(t('runmode.settings.confirm.restart'))) {
        return;
    }

    try {
        if (typeof window.showNavbarStatus === 'function') {
            window.showNavbarStatus('saving');
        }

        // Capture the current process instance ID before restarting so the poll
        // can tell the old, still-draining server apart from the new one. If this
        // fails we fall back to the down-then-up detection in pollForReconnection.
        const previousInstanceID = await window.fetchInstanceID();

        const response = await fetch('/api/system/restart', { method: 'POST' });
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        if (typeof window.showNavbarStatus === 'function') {
            window.showNavbarStatus('success');
        }

        // Hide the restart-required callout since we are restarting now.
        const indicator = document.getElementById('restart-required-indicator');
        if (indicator) {
            indicator.style.display = 'none';
        }

        window.showRestartOverlay();
        window.pollForReconnection(previousInstanceID);
    } catch (error) {
        console.error('Failed to restart application:', error);
        if (typeof window.showNavbarStatus === 'function') {
            window.showNavbarStatus('error');
        }
        alert(t('runmode.settings.error.restartfailed') + error.message);
    }
};

window.showRestartOverlay = function () {
    let overlay = document.getElementById('restart-overlay');
    if (!overlay) {
        overlay = document.createElement('div');
        overlay.id = 'restart-overlay';
        overlay.innerHTML = `
            <div class="restart-message">
                <div style="display: flex; justify-content: center; margin-bottom: 1rem;">
                    <div class="spinner-border text-light" role="status">
                        <span class="visually-hidden">${t('runmode.settings.restart.overlay.restarting')}</span>
                    </div>
                </div>
                <h3 style="text-align: center;">${t('runmode.settings.restart.overlay.title')}</h3>
                <p style="text-align: center;">${t('runmode.settings.restart.overlay.pleasewait')}</p>
            </div>
        `;
        document.body.appendChild(overlay);
    }

    overlay.style.cssText = `
        display: flex !important;
        position: fixed !important;
        top: 0 !important;
        left: 0 !important;
        right: 0 !important;
        bottom: 0 !important;
        width: 100vw !important;
        height: 100vh !important;
        background-color: rgba(0, 0, 0, 0.9) !important;
        z-index: 999999 !important;
        justify-content: center !important;
        align-items: center !important;
        margin: 0 !important;
        padding: 0 !important;
    `;

    document.body.classList.add('restart-blur');
};

window.hideRestartOverlay = function () {
    const overlay = document.getElementById('restart-overlay');
    if (overlay) {
        overlay.style.display = 'none';
    }

    document.body.classList.remove('restart-blur');
};

// Fetch the current process instance ID, or null if the server is unreachable
// or does not report one.
window.fetchInstanceID = async function () {
    try {
        const response = await fetch('/api/system/health', {
            method: 'GET',
            cache: 'no-cache'
        });

        if (!response.ok) {
            return null;
        }

        const data = await response.json();
        return data && data.instanceID ? data.instanceID : null;
    } catch (error) {
        return null;
    }
};

// Poll for the app to finish restarting, then reload the page. The overlay stays
// up until a *new* process is serving. When we captured the previous instance ID
// we simply wait for a different one to appear. If we could not capture it, we
// fall back to observing the server go down and then come back up, so we never
// reload into the old, still-draining server.
window.pollForReconnection = function (previousInstanceID) {
    const maxAttempts = 90; // Try for ~90 seconds (audio-engine reinit can be slow).
    let attempts = 0;
    let sawServerDown = false;

    const poll = async () => {
        attempts++;

        const instanceID = await window.fetchInstanceID();

        if (instanceID === null) {
            // Server is unreachable - the old process has stopped serving.
            sawServerDown = true;
        } else if (previousInstanceID) {
            // A different instance ID means the new process is up and serving.
            if (instanceID !== previousInstanceID) {
                window.hideRestartOverlay();
                window.location.reload();
                return;
            }
            // Same ID: old server still draining - keep the overlay up.
        } else if (sawServerDown) {
            // Fallback path: we never captured the previous ID, but we have now
            // seen the server go down and come back up, so it is safe to reload.
            window.hideRestartOverlay();
            window.location.reload();
            return;
        }

        if (attempts < maxAttempts) {
            setTimeout(poll, 1000);
        } else {
            window.hideRestartOverlay();
            alert(t('runmode.settings.error.reconnectfailed'));
        }
    };

    // Wait before the first poll to give the app time to start shutting down.
    setTimeout(poll, 2000);
};

// Initialize navigation when DOM is loaded
document.addEventListener('DOMContentLoaded', function () {
    // Fetch devToolsEnabled setting before initializing navigation
    fetch('/api/config')
        .then(response => response.ok ? response.json() : null)
        .then(config => {
            if (config && config.app) {
                window.devToolsEnabled = config.app.enableDevTools === true;
            }
            initializeNavigation();
        })
        .catch(error => {
            console.error('Failed to fetch config for navigation:', error);
            initializeNavigation();
        });
});

// Function to initialize or reinitialize navigation
function initializeNavigation() {
    const navContainer = document.getElementById('navigation');
    if (navContainer && typeof currentPageId !== 'undefined') {
        // Create navigation immediately with fallback text
        navContainer.innerHTML = createNavigation(currentPageId);

        // Make the "Restart Required" callout trigger a restart from any page,
        // so the user does not have to navigate to the settings page first.
        const restartIndicator = document.getElementById('restart-required-indicator');
        if (restartIndicator) {
            restartIndicator.addEventListener('click', () => window.triggerRestart());
            restartIndicator.addEventListener('keydown', (event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    window.triggerRestart();
                }
            });
        }

        // Add popover element to body if not exists
        if (!document.getElementById('info-popover')) {
            const popover = document.createElement('div');
            popover.id = 'info-popover';
            popover.className = 'card shadow-lg';
            popover.style.cssText = 'position: absolute; display: none; z-index: 9999; min-width: 280px;';
            popover.setAttribute('role', 'dialog');
            popover.setAttribute('aria-modal', 'false');
            popover.setAttribute('tabindex', '-1');
            popover.setAttribute('data-i18n-aria-label', 'runmode.nav.info');
            popover.innerHTML = `
                <div class="card-body">
                    <div class="mb-2">
                        <small class="text-muted" data-i18n="runmode.info.version">Version:</small>
                        <div id="info-version" class="fw-semibold">-</div>
                    </div>
                    <div class="mb-2">
                        <small class="text-muted" data-i18n="runmode.info.commitHash">Commit Hash:</small>
                        <div id="info-commit-hash" class="fw-semibold">-</div>
                    </div>
                    <div class="mb-2">
                        <small class="text-muted" data-i18n="runmode.info.buildDate">Build Date:</small>
                        <div id="info-build-date" class="fw-semibold">-</div>
                    </div>
                    <div class="mb-2">
                        <small class="text-muted" data-i18n="runmode.info.targetPlatform">Target Platform:</small>
                        <div id="info-build-platform" class="fw-semibold">-</div>
                    </div>
                    <div class="mb-0">
                        <small class="text-muted" data-i18n="runmode.info.hardware">Hardware:</small>
                        <div id="info-hardware" class="fw-semibold">-</div>
                    </div>
                </div>
            `;
            document.body.appendChild(popover);

            // Apply translations to the popover content
            if (typeof applyTranslations === 'function') {
                applyTranslations();
            }
        }

        // Setup info nav link popover (need to do this every time navigation is recreated)
        setupInfoPopover();

        // Check config status from backend if on settings page
        if (currentPageId === 'settings' && typeof window.configManager !== 'undefined') {
            // Config manager will handle showing restart indicator via status polling
        } else {
            // For other pages, check backend status once
            fetch('/api/config/status')
                .then(response => response.ok ? response.json() : null)
                .then(status => {
                    if (status && status.restartRequired) {
                        const indicator = document.getElementById('restart-required-indicator');
                        if (indicator) {
                            indicator.style.display = 'inline-block';
                        }
                    }
                })
                .catch(error => console.error('Failed to check config status:', error));
        }

        // Apply translations when i18n loads
        if (typeof i18nLoaded !== 'undefined' && i18nLoaded && typeof applyTranslations === 'function') {
            applyTranslations();
        } else {
            window.addEventListener('i18nLoaded', function () {
                if (typeof applyTranslations === 'function') {
                    applyTranslations();
                }
            }, { once: true });
        }

        // Also listen for language changes
        window.addEventListener('i18nLanguageChanged', function () {
            if (typeof applyTranslations === 'function') {
                applyTranslations();
            }
        });

        // Initialize navigation icons
        if (typeof IconHelper !== 'undefined') {
            IconHelper.loadIcon('circle-check').then(svg => {
                const successIcon = document.getElementById('nav-success-icon');
                if (successIcon && svg) {
                    successIcon.innerHTML = svg;
                }
            });
            IconHelper.loadIcon('circle-xmark').then(svg => {
                const errorIcon = document.getElementById('nav-error-icon');
                if (errorIcon && svg) {
                    errorIcon.innerHTML = svg;
                }
            });
            IconHelper.loadIcon('triangle-exclamation').then(svg => {
                const offlineIcon = document.getElementById('server-unavailable-icon');
                if (offlineIcon && svg) {
                    offlineIcon.innerHTML = svg;
                }
            });
        }

        // Begin polling backend health so the offline indicator reflects reality.
        startServerHealthMonitor();
    }
}

// Poll the backend health endpoint and toggle the top-right "Server Unavailable"
// indicator. This runs on every page (navigation is shared), giving a single,
// consistent signal when the server goes away. It is deliberately independent of
// the WebSocket connection state, which is only active on telemetry pages.
let serverHealthMonitorStarted = false;
function startServerHealthMonitor() {
    if (serverHealthMonitorStarted) {
        return;
    }
    serverHealthMonitorStarted = true;

    const pollIntervalMs = 5000;
    const requestTimeoutMs = 4000;

    const setUnavailable = (unavailable) => {
        const indicator = document.getElementById('server-unavailable-indicator');
        if (!indicator) {
            return;
        }
        // Never surface the indicator while a deliberate restart overlay is up;
        // that flow owns the messaging and the server is expected to be down.
        const overlay = document.getElementById('restart-overlay');
        const restarting = overlay && overlay.style.display !== 'none';
        indicator.style.display = unavailable && !restarting ? 'inline-flex' : 'none';
    };

    const poll = async () => {
        let ok = false;
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), requestTimeoutMs);
        try {
            const response = await fetch('/api/system/health', {
                method: 'GET',
                cache: 'no-cache',
                signal: controller.signal
            });
            ok = response.ok;
        } catch (error) {
            ok = false;
        } finally {
            clearTimeout(timeout);
        }
        setUnavailable(!ok);
    };

    poll();
    setInterval(poll, pollIntervalMs);
}

// Track popover visibility state
let popoverVisible = false;

// Function to set up info popover event handlers
function setupInfoPopover() {
    const infoLink = document.getElementById('info-nav-link');
    const popover = document.getElementById('info-popover');

    if (infoLink && popover) {
        // Remove any existing click listeners by cloning the element
        const newInfoLink = infoLink.cloneNode(true);
        infoLink.parentNode.replaceChild(newInfoLink, infoLink);

        newInfoLink.addEventListener('click', function (e) {
            e.preventDefault();

            if (popoverVisible) {
                closeInfoPopover();
            } else {
                // Position popover below the nav link
                const rect = newInfoLink.getBoundingClientRect();
                popover.style.left = rect.left + 'px';
                popover.style.top = (rect.bottom + 5) + 'px';
                popover.style.display = 'block';
                popoverVisible = true;
                newInfoLink.setAttribute('aria-expanded', 'true');

                // Move focus into the popover for keyboard/assistive tech users
                popover.focus();

                // Fetch and populate system info
                fetch('/api/system/info')
                    .then(response => response.json())
                    .then(data => {
                        document.getElementById('info-version').textContent = data.version || '-';
                        document.getElementById('info-commit-hash').textContent = data.commitHash || '-';
                        document.getElementById('info-build-date').textContent = data.buildTime || '-';
                        document.getElementById('info-build-platform').textContent = data.buildPlatform || '-';
                        document.getElementById('info-hardware').textContent = data.hardware || '-';
                    })
                    .catch(error => console.error('Failed to fetch system info:', error));
            }
        });

        // Close the popover on Escape and return focus to the trigger button
        popover.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') {
                e.preventDefault();
                closeInfoPopover();
                newInfoLink.focus();
            }
        });
    }
}

// Close the info popover, if open, and update trigger state.
function closeInfoPopover() {
    const popover = document.getElementById('info-popover');
    const infoLink = document.getElementById('info-nav-link');

    if (popover) {
        popover.style.display = 'none';
    }
    if (infoLink) {
        infoLink.setAttribute('aria-expanded', 'false');
    }
    popoverVisible = false;
}

// Close popover when clicking outside (only set up once)
document.addEventListener('click', function (e) {
    const popover = document.getElementById('info-popover');
    const infoLink = document.getElementById('info-nav-link');

    if (popoverVisible && popover && infoLink && !popover.contains(e.target) && e.target !== infoLink) {
        closeInfoPopover();
    }
});
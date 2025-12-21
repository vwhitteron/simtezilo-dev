// Navigation component for Simtezilo Web UI
function createNavigation(currentPage) {
    const telemetryDropdown = [
        { path: '/telemetry', nameKey: 'runmode.nav.race', fallback: 'Race', id: 'telemetry' },
        { path: '/dev', nameKey: 'runmode.nav.developer', fallback: 'Developer', id: 'dev' }
    ];

    const pages = [
        { path: '/settings', nameKey: 'runmode.nav.settings', fallback: 'Settings', id: 'settings' },
        { path: '/logs', nameKey: 'runmode.nav.logs', fallback: 'Logs', id: 'logs' }
    ];

    const telemetryActive = telemetryDropdown.some(item => item.id === currentPage);

    let navHTML = `
        <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css">
        
        <nav class="navbar navbar-expand-lg" style="background-color: var(--bs-content-bg); border-bottom: var(--bs-border-width) solid var(--bs-content-border-color);">
            <div class="container-fluid">
                <a class="navbar-brand" href="/">
                    <img src="/images/simtezilo-logo-dark.svg" alt="Simtezilo" height="32" class="d-inline-block align-text-top">
                </a>
                <button class="navbar-toggler" type="button" data-bs-toggle="collapse" data-bs-target="#navbar-collapse-1" aria-controls="navbar-collapse-1" aria-expanded="false" aria-label="Toggle navigation">
                    <span class="navbar-toggler-icon"></span>
                </button>
                <div class="collapse navbar-collapse" id="navbar-collapse-1">
                    <ul class="navbar-nav me-auto mb-2 mb-lg-0">
                        <li class="nav-item dropdown">
                            <a class="nav-link dropdown-toggle${telemetryActive ? ' active' : ''}"${telemetryActive ? ' aria-current="page"' : ''} href="#" role="button" data-bs-toggle="dropdown" aria-expanded="false">
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

    navHTML += `
                    </ul>`;

    // Add status spinner at the far right
    navHTML += `
                    <div class="d-flex align-items-center me-3">`;

    // Add connection status if it exists in the current page
    if (typeof connectionStatusHTML !== 'undefined') {
        navHTML += `
                        ${connectionStatusHTML}`;
    }

    // Add config status spinner
    navHTML += `
                        <div id="nav-status-indicator" class="d-flex align-items-center ms-3" style="display: none !important;">
                            <div id="nav-spinner" class="spinner-border spinner-border-sm text-warning" role="status" style="display: none;">
                                <span class="visually-hidden">Saving...</span>
                            </div>
                            <i id="nav-success-icon" class="fas fa-circle-check text-success" style="display: none; font-size: 1.25rem;"></i>
                            <i id="nav-error-icon" class="fas fa-circle-xmark text-danger" style="display: none; font-size: 1.25rem;"></i>
                        </div>
                    </div>`;

    navHTML += `
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

    // Hide all indicators first
    spinner.style.display = 'none';
    successIcon.style.display = 'none';
    errorIcon.style.display = 'none';

    // Show the container
    indicator.style.display = 'flex !important';
    indicator.style.removeProperty('display');

    // Show appropriate indicator
    switch (type) {
        case 'saving':
            spinner.style.display = 'block';
            break;
        case 'success':
            successIcon.style.display = 'block';
            // Auto-hide after 3 seconds
            setTimeout(() => window.hideNavbarStatus(), 3000);
            break;
        case 'error':
            errorIcon.style.display = 'block';
            // Auto-hide after 3 seconds
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
        indicator.style.display = 'none';
    }

    // Also hide individual icons
    if (spinner) spinner.style.display = 'none';
    if (successIcon) successIcon.style.display = 'none';
    if (errorIcon) errorIcon.style.display = 'none';
};

// Initialize navigation when DOM is loaded
document.addEventListener('DOMContentLoaded', function () {
    const navContainer = document.getElementById('navigation');
    if (navContainer && typeof currentPageId !== 'undefined') {
        // Create navigation immediately with fallback text
        navContainer.innerHTML = createNavigation(currentPageId);

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
    }
});
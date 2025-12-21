// Navigation component for Simtezilo Web UI
function createNavigation(currentPage) {
    const pages = [
        { path: '/telemetry', nameKey: 'runmode.nav.telemetry', fallback: 'Telemetry', id: 'telemetry' },
        { path: '/dev', nameKey: 'runmode.nav.developer', fallback: 'Developer', id: 'dev' },
        { path: '/settings', nameKey: 'runmode.nav.settings', fallback: 'Settings', id: 'settings' },
        { path: '/logs', nameKey: 'runmode.nav.logs', fallback: 'Logs', id: 'logs' }
    ];

    let navHTML = `
        <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css">
        <style>
            .nav-container {
                padding: 15px 20px;
                margin-bottom: 20px;
                display: flex;
                justify-content: space-between;
                align-items: center;
            }
            
            .nav-logo {
                transition: opacity 0.3s ease;
            }
            
            .nav-logo:hover {
                opacity: 0.8;
            }
            
            .nav-tabs {
                display: flex;
                margin: 0;
                margin-left: 40px;
                padding: 0;
                list-style: none;
                align-items: center;
                gap: 15px;
            }
            
            .nav-tab a {
                display: block;
                padding: 10px 0;
                text-decoration: none;
                color: #dddddd;
                font-weight: 500;
                transition: all 0.3s ease;
                position: relative;
            }
            
            .nav-tab a:hover {
                color: #fff;
            }
            
            .nav-tab.active a {
                color: #fff;
                font-weight: 700;
            }
            
            .nav-right {
                display: flex;
                align-items: center;
                gap: 20px;
            }
        </style>
        
        <div class="nav-container">
            <div style="display: flex; align-items: center;">
                <a href="/" class="nav-logo">
                    <img src="/images/simtezilo-logo-dark.svg" alt="Simtezilo" height="32" width="160">
                </a>
                
                <ul class="nav-tabs">`;

    pages.forEach(page => {
        const isActive = page.id === currentPage;
        navHTML += `
                    <li class="nav-tab${isActive ? ' active' : ''}">
                        <a href="${page.path}" data-i18n="${page.nameKey}">${page.fallback}</a>
                    </li>`;
    });

    navHTML += `
                </ul>
            </div>`;

    // Add connection status if it exists in the current page
    if (typeof connectionStatusHTML !== 'undefined') {
        navHTML += `
            <div class="nav-right">
                ${connectionStatusHTML}
            </div>`;
    }

    navHTML += `
        </div>`;

    return navHTML;
}

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
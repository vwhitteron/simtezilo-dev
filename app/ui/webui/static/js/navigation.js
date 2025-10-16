// Navigation component for Simtezilo Web UI
function createNavigation(currentPage) {
    const pages = [
        { path: '/', name: 'Home', id: 'home' },
        { path: '/telemetry', name: 'Telemetry', id: 'telemetry' },
        { path: '/dev', name: 'Developer', id: 'dev' },
        { path: '/settings', name: 'Settings', id: 'settings' },
        { path: '/logs', name: 'Logs', id: 'logs' }
    ];

    let navHTML = `
        <style>
            .nav-container {
                background: linear-gradient(135deg, rgba(20, 20, 20, 0.95), rgba(30, 30, 30, 0.95));
                border-bottom: 2px solid #333;
                padding: 0;
                margin-bottom: 20px;
                backdrop-filter: blur(10px);
                box-shadow: 0 2px 20px rgba(0, 0, 0, 0.3);
            }
            
            .nav-header {
                display: flex;
                justify-content: space-between;
                align-items: center;
                padding: 15px 20px 10px 20px;
            }
            
            .nav-tabs {
                display: flex;
                margin: 0;
                padding: 0 20px;
                list-style: none;
                border-bottom: 1px solid #444;
            }
            
            .nav-tab {
                margin-right: 2px;
            }
            
            .nav-tab a {
                display: block;
                padding: 12px 24px;
                text-decoration: none;
                color: #dddddd;
                background: rgba(40, 40, 40, 0.8);
                border: 1px solid #555;
                border-bottom: none;
                border-radius: 8px 8px 0 0;
                transition: all 0.3s ease;
                font-weight: 500;
                position: relative;
                overflow: hidden;
            }
            
            .nav-tab a:hover {
                background: rgba(60, 60, 60, 0.9);
                color: #fff;
                transform: translateY(-2px);
                box-shadow: 0 4px 12px rgba(249, 230, 127, 0.2);
            }
            
            .nav-tab.active a {
                background: linear-gradient(135deg, #dddddd, #ffffff);
                color: #000;
                border-color: #dddddd;
                font-weight: 600;
                box-shadow: 0 -2px 10px rgba(249, 230, 127, 0.3);
            }
            
            .nav-tab.active a::before {
                content: '';
                position: absolute;
                bottom: -1px;
                left: 0;
                right: 0;
                height: 2px;
                background: #dddddd;
            }
            
            .nav-logo {
                transition: opacity 0.3s ease;
            }
            
            .nav-logo:hover {
                opacity: 0.8;
            }
        </style>
        
        <div class="nav-container">
            <div class="nav-header">
                <a href="/" class="nav-logo">
                    <img src="/images/simtezilo-logo-dark.svg" alt="Simtezilo" height="32" width="160">
                </a>`;

    // Add connection status if it exists in the current page
    if (typeof connectionStatusHTML !== 'undefined') {
        navHTML += connectionStatusHTML;
    }

    navHTML += `
            </div>
            <ul class="nav-tabs">`;

    pages.forEach(page => {
        const isActive = page.id === currentPage || (currentPage === 'index' && page.id === 'home');
        navHTML += `
                <li class="nav-tab${isActive ? ' active' : ''}">
                    <a href="${page.path}">${page.name}</a>
                </li>`;
    });

    navHTML += `
            </ul>
        </div>`;

    return navHTML;
}

// Initialize navigation when DOM is loaded
document.addEventListener('DOMContentLoaded', function () {
    const navContainer = document.getElementById('navigation');
    if (navContainer && typeof currentPageId !== 'undefined') {
        navContainer.innerHTML = createNavigation(currentPageId);
    }
});
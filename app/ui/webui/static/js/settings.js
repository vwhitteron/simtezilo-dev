// Settings page JavaScript functionality
class ConfigManager {
    constructor() {
        this.config = {};
        this.loadTimeout = null;
        this.saveTimeout = null;
        this.inputSaveTimeouts = new Map(); // Track individual input save timeouts
        this.previousValues = new Map(); // Store previous valid values
        this.configTimestamp = 0; // Backend config timestamp
        this.statusCheckInterval = null; // Interval for checking config status
        this.init();
    }

    async init() {
        await this.loadLanguages();
        await this.loadConfiguration();
        this.setupEventListeners();
        this.populateForm();
        await this.checkSetupModeAvailability();
        await this.checkHardwarePlatform();
        this.startStatusPolling();
        this.initAdvancedToggle();
        this.initEngineProfiles();
        this.initEqualizer();
    }

    // Initialize advanced settings toggle
    initAdvancedToggle() {
        const advancedToggle = document.getElementById('advanced-settings-toggle');
        const advancedSettings = document.getElementById('advancedSettings');

        if (advancedToggle && advancedSettings) {
            console.log('Advanced toggle initialized');
            advancedToggle.addEventListener('change', (e) => {
                console.log('Toggle changed:', e.target.checked);
                advancedSettings.style.display = e.target.checked ? 'block' : 'none';
            });
        } else {
            console.error('Advanced toggle or settings not found:', {
                toggle: !!advancedToggle,
                settings: !!advancedSettings
            });
        }
    }

    async checkHardwarePlatform() {
        try {
            const response = await fetch('/api/system/info');
            if (!response.ok) {
                return;
            }
            const data = await response.json();

            // Hide hardware navigation item if not on RPI platform
            if (data.hardware !== 'rpi') {
                const hardwareNavItem = document.querySelector('.settings-nav-link[data-section="hardware"]');
                if (hardwareNavItem) {
                    hardwareNavItem.parentElement.style.display = 'none';
                }

                const hardwareOption = document.querySelector('#sectionSelect option[value="hardware"]');
                if (hardwareOption) {
                    hardwareOption.style.display = 'none';
                }
            }
        } catch (error) {
            console.error('Failed to check hardware platform:', error);
        }
    }

    // Start polling backend for config status changes
    startStatusPolling() {
        // Check every 2 seconds for config changes
        this.statusCheckInterval = setInterval(() => {
            this.checkConfigStatus();
        }, 2000);
    }

    // Stop status polling
    stopStatusPolling() {
        if (this.statusCheckInterval) {
            clearInterval(this.statusCheckInterval);
            this.statusCheckInterval = null;
        }
    }

    // Check config status from backend
    async checkConfigStatus() {
        try {
            const response = await fetch('/api/config/status');
            if (!response.ok) {
                return;
            }

            const status = await response.json();

            // Update restart indicator based on backend status
            if (status.restartRequired) {
                this.showRestartRequired();
            } else {
                this.hideRestartRequired();
            }

            // Check if backend config is newer than our local copy
            if (status.lastUpdate > this.configTimestamp) {
                console.log('Backend config is newer, reloading configuration');
                await this.loadConfiguration();
            }
        } catch (error) {
            console.error('Failed to check config status:', error);
        }
    }

    // Show status in navbar for a specific input
    showInputStatus(input, type) {
        // Show in navbar if available
        if (typeof window.showNavbarStatus === 'function') {
            window.showNavbarStatus(type);
        }
    }

    // Show restart required indicator
    showRestartRequired() {
        const indicator = document.getElementById('restart-required-indicator');
        if (indicator) {
            indicator.style.display = 'inline-block';
        }
    }

    // Hide restart required indicator
    hideRestartRequired() {
        const indicator = document.getElementById('restart-required-indicator');
        if (indicator) {
            indicator.style.display = 'none';
        }
    }

    async loadLanguages() {
        try {
            const response = await fetch('/api/languages');
            if (!response.ok) {
                throw new Error('Failed to load languages');
            }
            const languages = await response.json();

            const languageSelect = document.getElementById('app-language');
            if (languageSelect) {
                // Clear existing options
                languageSelect.innerHTML = '';

                // Sort languages alphabetically by name
                languages.sort((a, b) => a.name.localeCompare(b.name));

                // Populate language select with fetched languages
                languages.forEach(lang => {
                    const option = document.createElement('option');
                    option.value = lang.code;
                    option.textContent = lang.name;
                    languageSelect.appendChild(option);
                });
            }
        } catch (error) {
            console.error('Error loading languages:', error);
            // Languages will stay as hardcoded fallback in HTML
        }
    }

    setupEventListeners() {
        // Export button
        document.getElementById('export-config').addEventListener('click', () => {
            this.exportConfiguration();
        });

        // Import button
        document.getElementById('import-config').addEventListener('click', () => {
            document.getElementById('import-file-input').click();
        });

        // File input change handler
        document.getElementById('import-file-input').addEventListener('change', (event) => {
            const file = event.target.files[0];
            if (file) {
                this.importConfiguration(file);
            }
            // Reset file input so the same file can be selected again
            event.target.value = '';
        });

        // Reset button
        document.getElementById('reset-config').addEventListener('click', () => {
            this.resetConfiguration();
        });

        // Restart app button
        document.getElementById('restart-app').addEventListener('click', () => {
            this.restartApp();
        });

        // Setup mode button
        document.getElementById('setup-mode').addEventListener('click', () => {
            this.enterSetupMode();
        });

        // Factory reset button
        document.getElementById('factory-reset').addEventListener('click', () => {
            this.factoryReset();
        });

        // Auto-save on input change (debounced per input)
        const inputs = document.querySelectorAll('[data-config]');
        inputs.forEach(input => {
            // Store initial value as previous value
            const configPath = input.dataset.config;
            const currentValue = input.type === 'checkbox' ? input.checked : input.value;
            this.previousValues.set(configPath, currentValue);

            input.addEventListener('change', () => {
                // Check if language changed to reload translations
                if (input.id === 'app-language') {
                    this.handleLanguageChange(input.value);
                } else {
                    this.debounceInputSave(input);
                }
            });

            input.addEventListener('input', () => {
                if (input.type === 'range' || input.type === 'number') {
                    this.debounceInputSave(input);
                }
            });
        });
    }

    // Debounce save for individual inputs
    debounceInputSave(input) {
        const inputId = input.id || input.dataset.config;
        const configPath = input.dataset.config;

        // Clear existing timeout for this input
        clearTimeout(this.inputSaveTimeouts.get(inputId));

        // Show saving indicator
        this.showInputStatus(input, 'saving');

        // Set new timeout
        this.inputSaveTimeouts.set(inputId, setTimeout(async () => {
            await this.saveInputConfiguration(input);
        }, 1000));
    }

    // Save configuration for a specific input
    async saveInputConfiguration(input) {
        const configPath = input.dataset.config;
        let newValue;

        if (input.type === 'checkbox') {
            newValue = input.checked;
        } else if (input.type === 'number' || input.dataset.type === 'number') {
            newValue = parseFloat(input.value);
            if (isNaN(newValue)) {
                newValue = 0;
            }
        } else {
            newValue = input.value;
        }

        // Store the attempted value
        const attemptedValue = newValue;
        const previousValue = this.previousValues.get(configPath);

        try {
            // Build form data with just this value
            const formData = {};
            this.setNestedValue(formData, configPath, newValue);

            // Send to server
            const updateResponse = await fetch('/api/config', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(formData)
            });

            if (!updateResponse.ok) {
                throw new Error(`HTTP error! status: ${updateResponse.status}`);
            }

            // Update successful
            this.showInputStatus(input, 'success');
            this.previousValues.set(configPath, attemptedValue);

            // Update local config
            this.setNestedValue(this.config, configPath, newValue);

        } catch (error) {
            console.error(`Failed to save ${configPath}:`, error);
            this.showInputStatus(input, 'error');

            // Keep the attempted value visible while error icon is showing
            // After 3 seconds (when icon disappears), revert to previous valid value
            const inputId = input.id || input.dataset.config;
            this.inputSaveTimeouts.set(inputId + '-revert', setTimeout(() => {
                if (input.type === 'checkbox') {
                    input.checked = previousValue;
                } else {
                    input.value = previousValue;
                }
            }, 3000));
        }
    }

    async handleLanguageChange(newLanguage) {
        const languageInput = document.getElementById('app-language');

        try {
            // Show saving indicator
            this.showInputStatus(languageInput, 'saving');

            // Build the config update with just the language change
            const formData = {};
            this.setNestedValue(formData, 'app.language', newLanguage);

            // Update in-memory config
            const updateResponse = await fetch('/api/config', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(formData)
            });

            if (!updateResponse.ok) {
                throw new Error(`HTTP error! status: ${updateResponse.status}`);
            }

            // Verify the config was actually saved by reading it back with retries
            // Give more time for disk I/O to complete
            let verified = false;
            for (let i = 0; i < 8; i++) {
                await new Promise(resolve => setTimeout(resolve, 300));
                const verifyResponse = await fetch('/api/config?t=' + Date.now());
                if (verifyResponse.ok) {
                    const config = await verifyResponse.json();
                    if (config.app && config.app.language === newLanguage) {
                        verified = true;
                        break;
                    }
                }
            }

            if (!verified) {
                throw new Error('Could not verify language change in config');
            }

            // Additional wait to ensure config file is fully flushed to disk
            await new Promise(resolve => setTimeout(resolve, 500));

            // Show success briefly before reload
            this.showInputStatus(languageInput, 'success');
            this.previousValues.set('app.language', newLanguage);

            // Reload with aggressive cache busting
            setTimeout(() => {
                window.location.reload(true);
            }, 2000);

        } catch (error) {
            console.error('Failed to change language:', error);
            this.showInputStatus(languageInput, 'error');

            // Revert to previous language after icon disappears
            const previousLanguage = this.previousValues.get('app.language');
            setTimeout(() => {
                if (previousLanguage && languageInput) {
                    languageInput.value = previousLanguage;
                }
            }, 3000);
        }
    }

    debounceAutoSave() {
        clearTimeout(this.saveTimeout);

        // Show "saving" indicator immediately
        this.showStatus('Saving...', 'info');

        this.saveTimeout = setTimeout(async () => {
            this.collectFormData();
            await this.saveConfiguration(true); // Silent save
        }, 1000);
    }

    async loadConfiguration() {
        try {
            this.showStatus('Loading configuration...', 'info');

            const response = await fetch('/api/config');
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            this.config = await response.json();
            this.populateForm();
            this.showStatus('Configuration loaded successfully', 'success');

            // Get and store the backend timestamp
            const statusResponse = await fetch('/api/config/status');
            if (statusResponse.ok) {
                const status = await statusResponse.json();
                this.configTimestamp = status.lastUpdate;

                // Update restart indicator based on backend status
                if (status.restartRequired) {
                    this.showRestartRequired();
                } else {
                    this.hideRestartRequired();
                }
            }

        } catch (error) {
            console.error('Failed to load configuration:', error);
            this.showStatus('Failed to load configuration: ' + error.message, 'error');
        }
    }

    populateForm() {
        const inputs = document.querySelectorAll('[data-config]');

        inputs.forEach(input => {
            const configPath = input.dataset.config;
            const value = this.getNestedValue(this.config, configPath);

            if (value !== undefined) {
                if (input.type === 'checkbox') {
                    input.checked = value;
                } else {
                    input.value = value;
                }
                // Update previous values
                this.previousValues.set(configPath, value);
            }
        });
    }

    collectFormData() {
        const inputs = document.querySelectorAll('[data-config]');
        const formData = {};

        inputs.forEach(input => {
            const configPath = input.dataset.config;
            let value;

            if (input.type === 'checkbox') {
                value = input.checked;
            } else if (input.type === 'number') {
                value = parseFloat(input.value);
                if (isNaN(value)) {
                    value = 0;
                }
            } else {
                value = input.value;
            }

            this.setNestedValue(formData, configPath, value);
        });

        return formData;
    }

    async saveConfiguration(silent = false) {
        try {
            if (!silent) {
                this.showStatus('Saving configuration...', 'info');
            }

            const formData = this.collectFormData();

            // First update the in-memory configuration
            const updateResponse = await fetch('/api/config', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(formData)
            });

            if (!updateResponse.ok) {
                throw new Error(`HTTP error! status: ${updateResponse.status}`);
            }

            // If not silent, also save to file with backup
            if (!silent) {
                const saveResponse = await fetch('/api/config/save', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                    }
                });

                if (!saveResponse.ok) {
                    throw new Error(`Failed to save configuration to file! status: ${saveResponse.status}`);
                }

                const saveResult = await saveResponse.json();

                if (saveResult.backupPath) {
                    this.showStatus(`Configuration saved successfully. Backup created: ${saveResult.backupPath}`, 'success');
                } else {
                    this.showStatus('Configuration saved successfully', 'success');
                }
            } else {
                // Silent save completed - show brief success feedback
                this.showStatus('✓ Saved', 'success');
            }

            // Update local config with server response
            this.config = { ...this.config, ...formData };

        } catch (error) {
            console.error('Failed to save configuration:', error);
            this.showStatus('Failed to save configuration: ' + error.message, 'error');
        }
    }

    async resetConfiguration() {
        if (!confirm('Are you sure you want to reset all settings to their default values? This action cannot be undone.')) {
            return;
        }

        try {
            this.showStatus('Resetting configuration to defaults...', 'info');

            const response = await fetch('/api/config/reset', {
                method: 'POST'
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            this.config = await response.json();
            this.populateForm();
            this.showStatus('Configuration reset to defaults successfully', 'success');

        } catch (error) {
            console.error('Failed to reset configuration:', error);
            this.showStatus('Failed to reset configuration: ' + error.message, 'error');
        }
    }

    exportConfiguration() {
        try {
            // Fetch the full config file from the server instead of using the cached JSON payload
            const link = document.createElement('a');
            link.href = '/api/config/export';
            link.download = ''; // Let server decide filename

            // Trigger download
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);

            console.log('Configuration export triggered');
        } catch (error) {
            console.error('Failed to export configuration:', error);
            alert('Failed to export configuration: ' + error.message);
        }
    }

    async importConfiguration(file) {
        try {
            // Validate file extension
            if (!file.name.endsWith('.conf') && !file.name.endsWith('.toml')) {
                throw new Error('Invalid file type. Please select a .conf or .toml file.');
            }

            // Create FormData to send file
            const formData = new FormData();
            formData.append('config', file);

            // Show loading state
            const importBtn = document.getElementById('import-config');
            const originalText = importBtn.textContent;
            importBtn.disabled = true;
            importBtn.innerHTML = '<i class="fas fa-spinner fa-spin"></i> Importing...';

            // Send to server
            const response = await fetch('/api/config/import', {
                method: 'POST',
                body: formData
            });

            const result = await response.json();

            // Restore button state
            importBtn.disabled = false;
            importBtn.textContent = originalText;

            if (!response.ok) {
                // Handle validation errors specially
                if (result.validationErrors && result.validationErrors.length > 0) {
                    let errorMessage = 'Configuration validation failed:\n\n';
                    result.validationErrors.forEach(err => {
                        errorMessage += `• ${err.field}: ${err.message}\n`;
                    });
                    throw new Error(errorMessage);
                }
                throw new Error(result.error || 'Failed to import configuration');
            }

            console.log('Configuration imported successfully:', result);

            // Show success message
            alert(result.message + (result.backup ? '\n\nBackup created at: ' + result.backup : ''));

            // Reload the configuration
            await this.loadConfiguration();
        } catch (error) {
            console.error('Failed to import configuration:', error);
            alert('Failed to import configuration: ' + error.message);

            // Restore button state if error occurred
            const importBtn = document.getElementById('import-config');
            if (importBtn) {
                importBtn.disabled = false;
                importBtn.textContent = 'Import Configuration';
            }
        }
    }

    getNestedValue(obj, path) {
        return path.split('.').reduce((current, key) => {
            return current && current[key] !== undefined ? current[key] : undefined;
        }, obj);
    }

    setNestedValue(obj, path, value) {
        const keys = path.split('.');
        const lastKey = keys.pop();

        const target = keys.reduce((current, key) => {
            if (!current[key] || typeof current[key] !== 'object') {
                current[key] = {};
            }
            return current[key];
        }, obj);

        target[lastKey] = value;
    }

    showStatus(message, type) {
        // Status is now shown in navbar indicator only
        // This method kept for backward compatibility but does nothing
    }

    async checkSetupModeAvailability() {
        try {
            const response = await fetch('/api/system/info');
            if (response.ok) {
                const result = await response.json();
                if (result.setupModeAvailable) {
                    // Show setup mode and factory reset buttons
                    document.getElementById('setup-mode').style.display = '';
                    document.getElementById('factory-reset').style.display = '';
                }
            }
        } catch (error) {
            console.error('Failed to check setup mode availability:', error);
        }
    }

    async restartApp() {
        if (!confirm('Are you sure you want to restart the application?')) {
            return;
        }

        try {
            const response = await fetch('/api/restart', {
                method: 'POST'
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            // Disable the button after successful request
            document.getElementById('restart-app').disabled = true;

        } catch (error) {
            console.error('Failed to restart application:', error);
            alert('Failed to restart application: ' + error.message);
        }
    }

    async enterSetupMode() {
        if (!confirm('Are you sure you want to enter setup mode? The application will exit and restart in setup mode.')) {
            return;
        }

        try {
            const response = await fetch('/api/mode/setup', {
                method: 'POST'
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            // Disable the button after successful request
            document.getElementById('setup-mode').disabled = true;

        } catch (error) {
            console.error('Failed to enter setup mode:', error);
            alert('Failed to enter setup mode: ' + error.message);
        }
    }

    async factoryReset() {
        // Show the modal dialog
        const modal = document.getElementById('factory-reset-modal');
        const input = document.getElementById('factory-reset-confirm-input');
        const confirmBtn = document.getElementById('factory-reset-confirm');
        const cancelBtn = document.getElementById('factory-reset-cancel');

        modal.style.display = 'flex';
        input.value = '';
        input.focus();
        confirmBtn.disabled = true;

        // Enable/disable confirm button based on input
        const checkInput = () => {
            confirmBtn.disabled = input.value.toLowerCase() !== 'reset';
        };

        input.addEventListener('input', checkInput);

        // Handle cancel
        const handleCancel = () => {
            modal.style.display = 'none';
            input.removeEventListener('input', checkInput);
            cancelBtn.removeEventListener('click', handleCancel);
            confirmBtn.removeEventListener('click', handleConfirm);
        };

        // Handle confirm
        const handleConfirm = async () => {
            if (input.value.toLowerCase() !== 'reset') {
                return;
            }

            // Close modal
            modal.style.display = 'none';
            input.removeEventListener('input', checkInput);
            cancelBtn.removeEventListener('click', handleCancel);
            confirmBtn.removeEventListener('click', handleConfirm);

            // Disable the factory reset button
            document.getElementById('factory-reset').disabled = true;

            try {
                const response = await fetch('/api/factory-reset', {
                    method: 'POST'
                });

                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }

                // Connection will be lost, no need to show progress
                // The app will restart in setup mode

            } catch (error) {
                console.error('Failed to perform factory reset:', error);
                alert('Failed to perform factory reset: ' + error.message);
                document.getElementById('factory-reset').disabled = false;
            }
        };

        cancelBtn.addEventListener('click', handleCancel);
        confirmBtn.addEventListener('click', handleConfirm);

        // Allow Enter key to confirm if text is correct
        input.addEventListener('keypress', (e) => {
            if (e.key === 'Enter' && input.value.toLowerCase() === 'reset') {
                handleConfirm();
            }
        });
    }

    // Utility method to validate input values
    validateInput(input) {
        const value = input.value;
        const min = parseFloat(input.min);
        const max = parseFloat(input.max);

        if (input.type === 'number') {
            const numValue = parseFloat(value);
            if (isNaN(numValue)) {
                return false;
            }
            if (!isNaN(min) && numValue < min) {
                return false;
            }
            if (!isNaN(max) && numValue > max) {
                return false;
            }
        }

        if (input.required && !value.trim()) {
            return false;
        }

        return true;
    }

    // Format engine profile key into human-readable name
    formatEngineProfileName(key) {
        // Engine layout names
        const layouts = {
            's': 'Two-Stroke',
            'i': 'Inline',
            'v': 'V',
            'w': 'W',
            'h': 'Flat',
            'k': 'Wankel'
        };

        // Wankel rotor count names
        const rotorNames = {
            '1': 'Single Rotor',
            '2': 'Twin Rotor',
            '3': 'Triple Rotor',
            '4': 'Quad Rotor'
        };

        // RPM category names
        const rpmCategories = {
            'rstd': 'standard RPM',
            'rhigh': 'high RPM',
            'rmed': 'medium RPM'
        };

        // Parse the key
        const match = key.match(/^([a-z]+)(\d+)(?:_b(\d+))?(?:_c(\d+))?(?:_(rstd|rhigh|rmed))?$/i);

        if (!match) {
            return key; // Return original if format doesn't match
        }

        const [, layout, cylinders, bank, crank, rpm] = match;

        // Special case: VR6 (V6 with 15° bank angle)
        if (layout.toLowerCase() === 'v' && cylinders === '6' && bank === '15') {
            let name = 'VR6';
            if (crank) {
                name += `, ${crank}º crank plane`;
            }
            if (rpm) {
                name += `, ${rpmCategories[rpm] || rpm}`;
            }
            return name;
        }

        // Special case: Single cylinder two-stroke
        if (layout.toLowerCase() === 's' && cylinders === '1') {
            let name = 'Two-Stroke';
            if (rpm) {
                name += `, ${rpmCategories[rpm] || rpm}`;
            }
            return name;
        }

        // Build the formatted name
        let name;

        // Special handling for Wankel rotary engines
        if (layout.toLowerCase() === 'k') {
            name = rotorNames[cylinders] || `${cylinders} Rotor`;
        } else {
            const layoutName = layouts[layout.toLowerCase()] || layout.toUpperCase();
            // V and W engines have no space between layout and cylinder count
            if (layout.toLowerCase() === 'v' || layout.toLowerCase() === 'w') {
                name = `${layoutName}${cylinders}`;
            } else {
                // Other layouts (Inline, Flat, Two-Stroke) have a space
                name = `${layoutName} ${cylinders}`;
            }
        }

        if (bank) {
            name += `, ${bank}º bank`;
        }

        if (crank) {
            name += `, ${crank}º crank plane`;
        }

        if (rpm) {
            name += `, ${rpmCategories[rpm] || rpm}`;
        }

        return name;
    }

    // Initialize engine profiles dropdown and settings
    initEngineProfiles() {
        const profileSelect = document.getElementById('engine-profile-select');
        const profileSettings = document.getElementById('engine-profile-settings');

        if (!profileSelect || !this.config.synthesizer || !this.config.synthesizer.engineProfiles) {
            return;
        }

        // Populate dropdown with engine profiles
        profileSelect.innerHTML = '<option value="" disabled selected>Select an engine profile...</option>';

        const profiles = this.config.synthesizer.engineProfiles;

        // Define custom layout order: Inline, Flat, V, W, Wankel, Two-Stroke
        const layoutOrder = { 'i': 1, 'h': 2, 'v': 3, 'w': 4, 'k': 5, 's': 6 };

        // Sort by layout type, then cylinder count, then alphabetically
        const sortedKeys = Object.keys(profiles).sort((a, b) => {
            // Extract layout and cylinder count from keys
            const matchA = a.match(/^([a-z]+)(\d+)/i);
            const matchB = b.match(/^([a-z]+)(\d+)/i);

            if (!matchA || !matchB) return a.localeCompare(b);

            const layoutA = matchA[1].toLowerCase();
            const layoutB = matchB[1].toLowerCase();
            const cylindersA = parseInt(matchA[2]);
            const cylindersB = parseInt(matchB[2]);

            // Sort by custom layout order
            const orderA = layoutOrder[layoutA] || 999;
            const orderB = layoutOrder[layoutB] || 999;

            if (orderA !== orderB) {
                return orderA - orderB;
            }

            // Then by cylinder count
            if (cylindersA !== cylindersB) {
                return cylindersA - cylindersB;
            }

            // Finally alphabetically by full key
            return a.localeCompare(b);
        });

        sortedKeys.forEach(key => {
            const option = document.createElement('option');
            option.value = key;
            option.textContent = this.formatEngineProfileName(key);
            profileSelect.appendChild(option);
        });

        // Auto-select first profile if available
        if (sortedKeys.length > 0) {
            const firstProfile = sortedKeys[0];
            profileSelect.value = firstProfile;
            this.loadEngineProfile(firstProfile, profiles[firstProfile]);
            profileSettings.style.display = 'block';
        }

        // Handle profile selection
        profileSelect.addEventListener('change', () => {
            const selectedProfile = profileSelect.value;
            if (selectedProfile && profiles[selectedProfile]) {
                this.loadEngineProfile(selectedProfile, profiles[selectedProfile]);
                profileSettings.style.display = 'block';
            } else {
                profileSettings.style.display = 'none';
            }
        });

        // Setup input handlers for engine profile settings
        ['engine-primarybalance', 'engine-secondarybalance', 'engine-gain', 'engine-pulsescale'].forEach(id => {
            const input = document.getElementById(id);
            if (input) {
                input.addEventListener('change', () => this.saveEngineProfile());
                input.addEventListener('input', () => this.debounceEngineProfileSave());
            }
        });
    }

    // Load engine profile data into form
    loadEngineProfile(profileName, profile) {
        document.getElementById('engine-primarybalance').value = profile.PrimaryBalance;
        document.getElementById('engine-secondarybalance').value = profile.SecondaryBalance;
        document.getElementById('engine-gain').value = profile.Gain;
        document.getElementById('engine-pulsescale').value = profile.PulseScale;
    }

    // Debounce save for engine profile
    debounceEngineProfileSave() {
        clearTimeout(this.engineProfileSaveTimeout);
        this.engineProfileSaveTimeout = setTimeout(() => this.saveEngineProfile(), 1000);
    }

    // Save engine profile changes
    async saveEngineProfile() {
        const profileSelect = document.getElementById('engine-profile-select');
        const selectedProfile = profileSelect.value;

        if (!selectedProfile) return;

        const profile = {
            PrimaryBalance: parseFloat(document.getElementById('engine-primarybalance').value),
            SecondaryBalance: parseFloat(document.getElementById('engine-secondarybalance').value),
            Gain: parseFloat(document.getElementById('engine-gain').value),
            PulseScale: parseFloat(document.getElementById('engine-pulsescale').value)
        };

        try {
            // Update local config
            if (!this.config.synthesizer.engineProfiles) {
                this.config.synthesizer.engineProfiles = {};
            }
            this.config.synthesizer.engineProfiles[selectedProfile] = profile;

            // Save to server
            const formData = {
                synthesizer: {
                    engineProfiles: {
                        [selectedProfile]: profile
                    }
                }
            };

            const response = await fetch('/api/config', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(formData)
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            console.log('Engine profile saved:', selectedProfile);
        } catch (error) {
            console.error('Failed to save engine profile:', error);
            alert('Failed to save engine profile: ' + error.message);
        }
    }

    // Initialize equalizer controls
    initEqualizer() {
        const eqContainer = document.getElementById('equalizer-controls');
        const resetBtn = document.getElementById('eq-reset-btn');

        if (!eqContainer || !this.config.synthesizer) {
            return;
        }

        // Get EQ array or create default
        let eq = this.config.synthesizer.eq || Array(40).fill(1.0);

        // Generate 40 EQ sliders (10-49 Hz)
        eqContainer.innerHTML = '';
        for (let i = 0; i < 40; i++) {
            const freq = 10 + i;
            const col = document.createElement('div');
            col.className = 'col-6 col-sm-4 col-md-3 col-lg-2';

            col.innerHTML = `
                <label for="eq-${freq}" class="form-label small">${freq} Hz</label>
                <input type="range" class="form-range" id="eq-${freq}" 
                       min="0" max="2" step="0.1" value="${eq[i]}"
                       data-eq-index="${i}">
                <div class="text-center small text-muted" id="eq-${freq}-value">${eq[i].toFixed(1)}</div>
            `;

            eqContainer.appendChild(col);

            const slider = col.querySelector('input');
            const valueDisplay = col.querySelector('.text-muted');

            slider.addEventListener('input', (e) => {
                valueDisplay.textContent = parseFloat(e.target.value).toFixed(1);
                this.debounceEqSave();
            });
        }

        // Setup reset button
        if (resetBtn) {
            resetBtn.addEventListener('click', () => {
                for (let i = 0; i < 40; i++) {
                    const freq = 10 + i;
                    const slider = document.getElementById(`eq-${freq}`);
                    const valueDisplay = document.getElementById(`eq-${freq}-value`);
                    if (slider && valueDisplay) {
                        slider.value = 1.0;
                        valueDisplay.textContent = '1.0';
                    }
                }
                this.saveEqualizer();
            });
        }
    }

    // Debounce save for equalizer
    debounceEqSave() {
        clearTimeout(this.eqSaveTimeout);
        this.eqSaveTimeout = setTimeout(() => this.saveEqualizer(), 1000);
    }

    // Save equalizer settings
    async saveEqualizer() {
        const eq = [];

        for (let i = 0; i < 40; i++) {
            const freq = 10 + i;
            const slider = document.getElementById(`eq-${freq}`);
            if (slider) {
                eq.push(parseFloat(slider.value));
            }
        }

        if (eq.length !== 40) {
            console.error('EQ array must have exactly 40 values');
            return;
        }

        try {
            // Update local config
            this.config.synthesizer.eq = eq;

            // Save to server
            const formData = {
                synthesizer: {
                    eq: eq
                }
            };

            const response = await fetch('/api/config', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(formData)
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            console.log('Equalizer saved');
        } catch (error) {
            console.error('Failed to save equalizer:', error);
            alert('Failed to save equalizer: ' + error.message);
        }
    }
}

// Initialize the configuration manager when the page loads
document.addEventListener('DOMContentLoaded', () => {
    window.configManager = new ConfigManager();
});

// Add some utility functions for enhanced UX
document.addEventListener('DOMContentLoaded', () => {
    // Add input validation feedback
    const inputs = document.querySelectorAll('[data-config]');
    inputs.forEach(input => {
        input.addEventListener('blur', () => {
            if (window.configManager && !window.configManager.validateInput(input)) {
                input.style.borderColor = '#e74c3c';
            } else {
                input.style.borderColor = '';
            }
        });
    });

    // Add tooltips for complex settings (if needed)
    const complexInputs = {
        'hardware-displayorientation': 'Display rotation for screens that are mounted in different orientations',
        'haptics-jerkcurve': 'Controls the responsiveness curve for jerk feedback (higher = more responsive)',
        'haptics-snapcurve': 'Controls the responsiveness curve for snap feedback (higher = more responsive)',
        'synth-mastergain': 'Overall volume level for all haptic feedback in decibels',
        'telemetry-source': 'UDP endpoint where telemetry data is received from the racing game'
    };

    Object.entries(complexInputs).forEach(([id, tooltip]) => {
        const element = document.getElementById(id);
        if (element) {
            element.title = tooltip;
        }
    });
});
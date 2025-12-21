// Settings page JavaScript functionality
class ConfigManager {
    constructor() {
        this.config = {};
        this.loadTimeout = null;
        this.saveTimeout = null;
        this.inputSaveTimeouts = new Map(); // Track individual input save timeouts
        this.previousValues = new Map(); // Store previous valid values
        this.init();
    }

    async init() {
        await this.loadLanguages();
        await this.loadConfiguration();
        this.setupEventListeners();
        this.populateForm();
        await this.checkSetupModeAvailability();
    }

    // Show status in navbar for a specific input
    showInputStatus(input, type) {
        // Show in navbar if available
        if (typeof window.showNavbarStatus === 'function') {
            window.showNavbarStatus(type);
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
        // Load button
        document.getElementById('load-config').addEventListener('click', () => {
            this.loadConfiguration();
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
        } else if (input.type === 'number') {
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
            const response = await fetch('/api/mode/setup');
            if (response.ok) {
                const result = await response.json();
                if (result.available) {
                    document.getElementById('system-actions-section').style.display = 'block';
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
            const statusElement = document.getElementById('restart-app-status');
            statusElement.textContent = 'Restarting application...';
            statusElement.className = 'config-status info';
            statusElement.style.display = 'block';

            const response = await fetch('/api/restart', {
                method: 'POST'
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            const result = await response.json();
            statusElement.textContent = result.message || 'Application restarting...';
            statusElement.className = 'config-status success';

            // Disable the button after successful request
            document.getElementById('restart-app').disabled = true;

        } catch (error) {
            console.error('Failed to restart application:', error);
            const statusElement = document.getElementById('restart-app-status');
            statusElement.textContent = 'Failed to restart application: ' + error.message;
            statusElement.className = 'config-status error';
            statusElement.style.display = 'block';
        }
    }

    async enterSetupMode() {
        if (!confirm('Are you sure you want to enter setup mode? The application will exit and restart in setup mode.')) {
            return;
        }

        try {
            const statusElement = document.getElementById('setup-mode-status');
            statusElement.textContent = 'Entering setup mode...';
            statusElement.className = 'config-status info';
            statusElement.style.display = 'block';

            const response = await fetch('/api/mode/setup', {
                method: 'POST'
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            const result = await response.json();
            statusElement.textContent = result.message || 'Setup mode activated. Application shutting down...';
            statusElement.className = 'config-status success';

            // Disable the button after successful request
            document.getElementById('setup-mode').disabled = true;

        } catch (error) {
            console.error('Failed to enter setup mode:', error);
            const statusElement = document.getElementById('setup-mode-status');
            statusElement.textContent = 'Failed to enter setup mode: ' + error.message;
            statusElement.className = 'config-status error';
            statusElement.style.display = 'block';
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
                const statusElement = document.getElementById('factory-reset-status');
                statusElement.textContent = 'Failed to perform factory reset: ' + error.message;
                statusElement.className = 'config-status error';
                statusElement.style.display = 'block';
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
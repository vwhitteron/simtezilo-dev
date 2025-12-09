// Settings page JavaScript functionality
class ConfigManager {
    constructor() {
        this.config = {};
        this.loadTimeout = null;
        this.saveTimeout = null;
        this.init();
    }

    async init() {
        await this.loadConfiguration();
        this.setupEventListeners();
        this.populateForm();
        await this.checkSetupModeAvailability();
    }

    setupEventListeners() {
        // Save button
        document.getElementById('save-config').addEventListener('click', () => {
            this.saveConfiguration();
        });

        // Load button
        document.getElementById('load-config').addEventListener('click', () => {
            this.loadConfiguration();
        });

        // Reset button
        document.getElementById('reset-config').addEventListener('click', () => {
            this.resetConfiguration();
        });

        // Setup mode button
        document.getElementById('setup-mode').addEventListener('click', () => {
            this.enterSetupMode();
        });

        // Auto-save on input change (debounced)
        const inputs = document.querySelectorAll('[data-config]');
        inputs.forEach(input => {
            input.addEventListener('change', () => {
                this.debounceAutoSave();
            });

            input.addEventListener('input', () => {
                if (input.type === 'range') {
                    this.debounceAutoSave();
                }
            });
        });
    }

    debounceAutoSave() {
        clearTimeout(this.saveTimeout);
        this.saveTimeout = setTimeout(() => {
            this.collectFormData();
            this.saveConfiguration(true); // Silent save
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
        const statusElement = document.getElementById('config-status');
        statusElement.textContent = message;
        statusElement.className = `config-status ${type}`;

        if (type !== 'info') {
            setTimeout(() => {
                statusElement.style.display = 'none';
            }, 5000);
        }
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
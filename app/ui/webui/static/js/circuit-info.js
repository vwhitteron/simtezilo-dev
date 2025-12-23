// circuit-info.js - WebSocket client for receiving circuit information updates

(function () {
    'use strict';

    let circuitSocket = null;
    let reconnectAttempts = 0;
    const maxReconnectAttempts = 10;
    const reconnectDelay = 3000; // 3 seconds

    const statusIndicator = document.getElementById('circuitStatus');
    const circuitTextElement = document.getElementById('circuitText');

    let currentName = '';
    let currentVariation = '';
    let currentCountry = '';
    let currentLength = '';
    let currentCandidates = 0;
    let hasReceivedData = false;

    // Country name to ISO code mapping for flag emojis
    const countryToISO = {
        'Japan': 'JP',
        'United States': 'US',
        'USA': 'US',
        'United Kingdom': 'GB',
        'UK': 'GB',
        'England': 'GB',
        'France': 'FR',
        'Germany': 'DE',
        'Italy': 'IT',
        'Spain': 'ES',
        'Belgium': 'BE',
        'Australia': 'AU',
        'Austria': 'AT',
        'Monaco': 'MC',
        'Canada': 'CA',
        'Brazil': 'BR',
        'Mexico': 'MX',
        'Netherlands': 'NL',
        'Portugal': 'PT',
        'Czech Republic': 'CZ',
        'South Africa': 'ZA',
        'Switzerland': 'CH',
        'Sweden': 'SE',
        'Norway': 'NO',
        'Finland': 'FI',
        'Denmark': 'DK',
        'Hungary': 'HU',
        'Poland': 'PL',
        'Russia': 'RU',
        'China': 'CN',
        'South Korea': 'KR',
        'Korea': 'KR',
        'Singapore': 'SG',
        'UAE': 'AE',
        'United Arab Emirates': 'AE',
        'Turkey': 'TR',
        'Argentina': 'AR',
        'Chile': 'CL',
        'Greece': 'GR',
        'Croatia': 'HR',
        'Slovenia': 'SI',
        'Slovakia': 'SK',
        'Romania': 'RO',
        'Bulgaria': 'BG',
        'Serbia': 'RS',
        'Ireland': 'IE',
        'New Zealand': 'NZ',
        'Malaysia': 'MY',
        'Thailand': 'TH',
        'Indonesia': 'ID',
        'Philippines': 'PH',
        'Vietnam': 'VN',
        'India': 'IN',
        'Bahrain': 'BH',
        'Qatar': 'QA',
        'Saudi Arabia': 'SA',
        'Kuwait': 'KW',
        'Oman': 'OM',
        'Israel': 'IL',
        'Egypt': 'EG',
        'Morocco': 'MA',
        'Algeria': 'DZ',
        'Tunisia': 'TN',
        'Peru': 'PE',
        'Colombia': 'CO',
        'Venezuela': 'VE',
        'Uruguay': 'UY',
        'Paraguay': 'PY',
        'Ecuador': 'EC',
        'Bolivia': 'BO'
    };

    function getCountryFlag(countryName) {
        if (!countryName) return '';

        const isoCode = countryToISO[countryName];
        if (isoCode) {
            // Convert ISO code to flag emoji using regional indicator symbols
            return String.fromCodePoint(
                ...isoCode.split('').map(char => 127397 + char.charCodeAt(0))
            );
        }

        // If no mapping found, return empty string (no flag)
        return '';
    }

    function updateConnectionStatus(connected) {
        if (statusIndicator) {
            statusIndicator.className = 'status-indicator ' +
                (connected ? 'status-connected' : 'status-disconnected');
        }
    }

    function updateCircuitInfo(data) {
        // Mark that we've received data from the WebSocket
        hasReceivedData = true;

        // Update stored values
        if (data.name !== undefined) {
            currentName = data.name;
        }
        if (data.variation !== undefined) {
            currentVariation = data.variation;
        }
        if (data.country !== undefined) {
            currentCountry = data.country;
        }
        if (data.length !== undefined) {
            currentLength = data.length;
        }
        if (data.candidates !== undefined) {
            currentCandidates = parseInt(data.candidates) || 0;
        }

        // Update display
        if (circuitTextElement) {
            // Show "Analyzing..." if circuit not matched yet (no name/variation) OR length is zero
            const lengthValue = parseFloat(currentLength) || 0;
            if ((currentName === '' && currentVariation === '') || lengthValue === 0) {
                // We're receiving data but circuit not matched yet - show "Analyzing..."
                const waitingTranslation = document.querySelector('[data-i18n="runmode.home.circuit.waiting"]');
                const analyzingText = waitingTranslation ? waitingTranslation.textContent : 'Analyzing...';

                // Only show candidate count if there are multiple candidates (ambiguity)
                if (currentCandidates > 1) {
                    circuitTextElement.innerHTML = `<span class="vehicle-placeholder">${analyzingText} (${currentCandidates} matched)</span>`;
                } else {
                    circuitTextElement.innerHTML = `<span class="vehicle-placeholder">${analyzingText}</span>`;
                }
            } else {
                const flag = getCountryFlag(currentCountry);
                const circuitFullText = `${currentVariation} ${flag} (${currentLength} km)`.trim();
                circuitTextElement.innerHTML = `<span class="vehicle-value">${circuitFullText}</span>`;
            }
        }

        // Dispatch event for overlay manager
        window.dispatchEvent(new CustomEvent('circuitDataUpdate', {
            detail: {
                name: currentName,
                variation: currentVariation,
                country: currentCountry,
                length: currentLength,
                candidates: currentCandidates
            }
        }));
    }

    function connectCircuitWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/circuit`;

        try {
            circuitSocket = new WebSocket(wsUrl);

            circuitSocket.onopen = function () {
                console.log('Circuit WebSocket connected');
                updateConnectionStatus(true);
                reconnectAttempts = 0;
                window.dispatchEvent(new CustomEvent('circuitConnectionChange', {
                    detail: { connected: true }
                }));
            };

            circuitSocket.onmessage = function (event) {
                try {
                    const circuitData = JSON.parse(event.data);
                    updateCircuitInfo(circuitData);
                } catch (error) {
                    console.error('Error parsing circuit data:', error);
                }
            };

            circuitSocket.onerror = function (error) {
                console.error('Circuit WebSocket error:', error);
                updateConnectionStatus(false);
                window.dispatchEvent(new CustomEvent('circuitConnectionChange', {
                    detail: { connected: false }
                }));
            };

            circuitSocket.onclose = function () {
                console.log('Circuit WebSocket disconnected');
                updateConnectionStatus(false);

                // Attempt to reconnect
                if (reconnectAttempts < maxReconnectAttempts) {
                    reconnectAttempts++;
                    console.log(`Attempting to reconnect circuit WebSocket (${reconnectAttempts}/${maxReconnectAttempts})...`);
                    setTimeout(connectCircuitWebSocket, reconnectDelay);
                } else {
                    console.error('Max reconnection attempts reached for circuit WebSocket');
                }
            };
        } catch (error) {
            console.error('Failed to create circuit WebSocket:', error);
            updateConnectionStatus(false);
        }
    }

    // Initialize connection when page loads
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', connectCircuitWebSocket);
    } else {
        connectCircuitWebSocket();
    }
})();

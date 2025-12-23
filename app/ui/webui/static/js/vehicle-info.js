// vehicle-info.js - WebSocket client for receiving vehicle information updates

(function () {
    'use strict';

    let vehicleSocket = null;
    let reconnectAttempts = 0;
    const maxReconnectAttempts = 10;
    const reconnectDelay = 3000; // 3 seconds

    const statusIndicator = document.getElementById('vehicleStatus');
    const vehicleTextElement = document.getElementById('vehicleText');

    let currentManufacturer = '';
    let currentModel = '';
    let currentCarID = 0;

    function updateConnectionStatus(connected) {
        if (statusIndicator) {
            statusIndicator.className = 'status-indicator ' +
                (connected ? 'status-connected' : 'status-disconnected');
        }
    }

    function updateVehicleInfo(data) {
        // Update stored values
        if (data.manufacturer !== undefined) {
            currentManufacturer = data.manufacturer;
        }
        if (data.model !== undefined) {
            currentModel = data.model;
        }
        if (data.carID !== undefined) {
            currentCarID = data.carID;
        }

        // Update display
        if (vehicleTextElement) {
            if (currentManufacturer === '' && currentModel === '') {
                vehicleTextElement.innerHTML = '<span class="vehicle-placeholder" data-i18n="runmode.home.vehicle.waiting">Waiting for data...</span>';
            } else {
                const vehicleFullText = `${currentManufacturer} ${currentModel}`.trim();
                vehicleTextElement.className = 'vehicle-value';

                if (currentCarID && currentCarID > 0) {
                    vehicleTextElement.innerHTML = `<a href="https://www.gran-turismo.com/au/gt7/carlist/id/car${currentCarID}" target="_blank" style="color: #fff; text-decoration: underline;">${vehicleFullText}</a>`;
                } else {
                    vehicleTextElement.textContent = vehicleFullText;
                }
            }
        }

        if (data.manufacturer || data.model) {
            console.log('Vehicle updated:', currentManufacturer || '(cleared)', currentModel || '(cleared)');
        }

        // Dispatch game state update if present
        if (data.gamestate !== undefined) {
            window.dispatchEvent(new CustomEvent('gameStateUpdate', {
                detail: {
                    state: data.gamestate
                }
            }));
        }

        // Dispatch event for overlay manager
        window.dispatchEvent(new CustomEvent('vehicleDataUpdate', {
            detail: {
                manufacturer: currentManufacturer,
                model: currentModel,
                carID: currentCarID
            }
        }));
    }

    function connectVehicleWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/vehicle`;

        try {
            vehicleSocket = new WebSocket(wsUrl);

            vehicleSocket.onopen = function () {
                console.log('Vehicle WebSocket connected');
                updateConnectionStatus(true);
                reconnectAttempts = 0;
                window.dispatchEvent(new CustomEvent('vehicleConnectionChange', {
                    detail: { connected: true }
                }));
            };

            vehicleSocket.onmessage = function (event) {
                try {
                    const vehicleData = JSON.parse(event.data);
                    updateVehicleInfo(vehicleData);
                } catch (error) {
                    console.error('Error parsing vehicle data:', error);
                }
            };

            vehicleSocket.onerror = function (error) {
                console.error('Vehicle WebSocket error:', error);
                updateConnectionStatus(false);
                window.dispatchEvent(new CustomEvent('vehicleConnectionChange', {
                    detail: { connected: false }
                }));
            };

            vehicleSocket.onclose = function () {
                console.log('Vehicle WebSocket disconnected');
                updateConnectionStatus(false);

                // Attempt to reconnect
                if (reconnectAttempts < maxReconnectAttempts) {
                    reconnectAttempts++;
                    console.log(`Attempting to reconnect (${reconnectAttempts}/${maxReconnectAttempts})...`);
                    setTimeout(connectVehicleWebSocket, reconnectDelay);
                } else {
                    console.error('Max reconnection attempts reached');
                }
            };
        } catch (error) {
            console.error('Failed to create vehicle WebSocket:', error);
            updateConnectionStatus(false);
        }
    }

    // Initialize connection when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', connectVehicleWebSocket);
    } else {
        connectVehicleWebSocket();
    }

    // Clean up on page unload
    window.addEventListener('beforeunload', function () {
        if (vehicleSocket) {
            vehicleSocket.close();
        }
    });
})();

// Configuration constants
const CONFIG = {
    FIFO_CAPACITY: 100000,  // Increased to retain more historical data for panning
    RECONNECT_DELAY: 1000,
    MAX_RECONNECT_DELAY: 30000,
    WEBSOCKET_URL: `ws://${location.host}/ws`
};

// Function to update window size display with appropriate units
function updateWindowSizeDisplay(windowSize) {
    const displayElement = document.getElementById('window-size-display');
    if (displayElement && !displayElement.dataset.userEditing) {
        // Window size is in sequence IDs, data arrives at 60Hz (60 packets per second)
        // So windowSize / 60 = seconds
        const windowSizeInSeconds = windowSize / 60;

        let displayText;
        if (windowSizeInSeconds < 1) {
            displayText = `${(windowSizeInSeconds * 1000).toFixed(0)}ms`;
        } else if (windowSizeInSeconds < 60) {
            displayText = `${windowSizeInSeconds.toFixed(1)}s`;
        } else if (windowSizeInSeconds < 3600) {
            const minutes = Math.floor(windowSizeInSeconds / 60);
            const seconds = Math.floor(windowSizeInSeconds % 60);
            displayText = `${minutes}m ${seconds}s`;
        } else {
            const hours = Math.floor(windowSizeInSeconds / 3600);
            const minutes = Math.floor((windowSizeInSeconds % 3600) / 60);
            displayText = `${hours}h ${minutes}m`;
        }
        displayElement.value = displayText;
    }
}

// Function to parse user input with units and return window size in sequence IDs
function parseWindowSizeInput(input) {
    const trimmed = input.trim().toLowerCase();

    // Parse patterns like "10s", "2m", "500ms", "1h", "2m 30s"
    const patterns = [
        { regex: /^(\d+(?:\.\d+)?)\s*ms$/, multiplier: 0.001 },
        { regex: /^(\d+(?:\.\d+)?)\s*s$/, multiplier: 1 },
        { regex: /^(\d+(?:\.\d+)?)\s*m$/, multiplier: 60 },
        { regex: /^(\d+(?:\.\d+)?)\s*h$/, multiplier: 3600 },
        { regex: /^(\d+)\s*m\s+(\d+)\s*s$/, isComplex: true },
        { regex: /^(\d+)\s*h\s+(\d+)\s*m$/, isComplex: true }
    ];

    for (const pattern of patterns) {
        const match = trimmed.match(pattern.regex);
        if (match) {
            let seconds;
            if (pattern.isComplex) {
                if (trimmed.includes('h')) {
                    seconds = parseInt(match[1]) * 3600 + parseInt(match[2]) * 60;
                } else {
                    seconds = parseInt(match[1]) * 60 + parseInt(match[2]);
                }
            } else {
                seconds = parseFloat(match[1]) * pattern.multiplier;
            }
            // Convert seconds to sequence IDs (60 per second)
            return seconds * 60;
        }
    }

    return null;
}

// Setup window size input handler
function setupWindowSizeInput(charts, chartFollowModes) {
    const displayElement = document.getElementById('window-size-display');
    if (!displayElement) return;

    displayElement.addEventListener('focus', () => {
        displayElement.dataset.userEditing = 'true';
    });

    displayElement.addEventListener('blur', () => {
        delete displayElement.dataset.userEditing;
    });

    displayElement.addEventListener('keydown', (event) => {
        if (event.key === 'Enter') {
            const windowSize = parseWindowSizeInput(displayElement.value);
            if (windowSize !== null && windowSize > 0) {
                // Apply the new window size to all charts
                charts.forEach((chart, index) => {
                    const xAxis = chart.xAxis;
                    const currentRange = xAxis.visibleRange;
                    const currentCenter = (currentRange.min + currentRange.max) / 2;

                    let newMin = currentCenter - windowSize / 2;
                    let newMax = currentCenter + windowSize / 2;

                    if (newMin < 0) {
                        newMin = 0;
                        newMax = windowSize;
                    }

                    xAxis.visibleRange = new SciChart.NumberRange(newMin, newMax);

                    // Enable follow mode for this chart (like double-click behavior)
                    const chartId = Object.keys(chartFollowModes)[index];
                    if (chartId) {
                        chartFollowModes[chartId] = true;
                    }
                });

                displayElement.blur();
                updateWindowSizeDisplay(windowSize);
            } else {
                // Invalid input - revert to current window size
                const xAxis = charts[0].xAxis;
                const currentRange = xAxis.visibleRange;
                const rangeSize = currentRange.max - currentRange.min;
                updateWindowSizeDisplay(rangeSize);
                displayElement.blur();
            }
        }
    });
}

// Connection state management
let connectionState = {
    isConnected: false,
    reconnectAttempts: 0,
    reconnectDelay: CONFIG.RECONNECT_DELAY
};

// UI status update functions
function updateConnectionStatus(status, message) {
    // Use the unified nav-status-indicator
    if (typeof window.showNavbarStatus === 'function') {
        switch (status) {
            case 'connected':
                window.showNavbarStatus('success');
                break;
            case 'connecting':
                window.showNavbarStatus('saving');
                break;
            case 'disconnected':
            case 'error':
                window.showNavbarStatus('error');
                break;
        }
    }
}

// Manual reconnect function
function forceReconnect() {
    console.log('Manual reconnect requested');
    connectionState.reconnectAttempts = 0;
    connectionState.reconnectDelay = CONFIG.RECONNECT_DELAY;

    if (globalWebSocket) {
        globalWebSocket.close();
    }

    setTimeout(() => {
        globalWebSocket = createWebSocketConnection();
        if (globalHandleWebSocketMessage) {
            globalWebSocket.addEventListener('message', globalHandleWebSocketMessage);
        }
    }, 100);
}

// Chart configurations for different pages
const CHART_CONFIGURATIONS = {
    // Telemetry page - driver-focused charts
    'telemetry': [
        { id: 'rpm-speed', containerId: 'scichart-root-1', enabled: true },
        { id: 'throttle-brake', containerId: 'scichart-root-2', enabled: true },
        { id: 'tyre-temperature', containerId: 'scichart-root-3', enabled: true },
        { id: 'fuel-range', containerId: 'scichart-root-4', enabled: true }
    ],

    // Dev page - developer-focused charts
    'dev': [
        { id: 'translational-acceleration', containerId: 'scichart-root-1', enabled: true },
        { id: 'rotational-acceleration', containerId: 'scichart-root-2', enabled: true },
        { id: 'jerk', containerId: 'scichart-root-3', enabled: true },
        { id: 'snap', containerId: 'scichart-root-4', enabled: true },
        { id: 'channel-output-left', containerId: 'scichart-root-5', enabled: true },
        { id: 'channel-output-right', containerId: 'scichart-root-6', enabled: true },
        { id: 'compute-time', containerId: 'scichart-root-7', enabled: true },
        { id: 'audio-health', containerId: 'scichart-root-8', enabled: true },
        { id: 'haptic-latency', containerId: 'scichart-root-9', enabled: true },
        { id: 'seq-gap', containerId: 'scichart-root-10', enabled: true }
    ],

    // Default/fallback - all charts enabled
    'default': [
        { id: 'rpm-speed', containerId: 'scichart-root-1', enabled: true },
        { id: 'throttle-brake', containerId: 'scichart-root-2', enabled: true },
        { id: 'tyre-temperature', containerId: 'scichart-root-3', enabled: true },
        { id: 'fuel-range', containerId: 'scichart-root-4', enabled: true },
        { id: 'translational-acceleration', containerId: 'scichart-root-5', enabled: true },
        { id: 'rotational-acceleration', containerId: 'scichart-root-6', enabled: true },
        { id: 'jerk', containerId: 'scichart-root-7', enabled: true },
        { id: 'snap', containerId: 'scichart-root-8', enabled: true },
        { id: 'compute-time', containerId: 'scichart-root-9', enabled: true }
    ]
};

// Function to get chart configuration from script tag data attribute
function getChartRegistry() {
    const currentScript = document.currentScript ||
        document.querySelector('script[src*="scichart.js"]') ||
        document.querySelector('script[data-chart-config]');

    if (currentScript) {
        const configType = currentScript.getAttribute('data-chart-config');
        if (configType && CHART_CONFIGURATIONS[configType]) {
            console.log(`Loading chart configuration: ${configType}`);
            return CHART_CONFIGURATIONS[configType];
        }
    }

    console.log('No chart configuration specified, using default');
    return CHART_CONFIGURATIONS.default;
}

const CHART_REGISTRY = getChartRegistry();

// Global variables for WebSocket management
let globalWebSocket = null;
let globalCharts = {};
let globalAllDataSeries = {};
let globalHandleWebSocketMessage = null;

// Generate or retrieve client session ID
function getClientSessionId() {
    let sessionId = sessionStorage.getItem('ws-session-id');
    if (!sessionId) {
        sessionId = 'session-' + Math.random().toString(36).substr(2, 9) + '-' + Date.now();
        sessionStorage.setItem('ws-session-id', sessionId);
    }
    return sessionId;
}

// WebSocket connection management
function createWebSocketConnection() {
    if (globalWebSocket) {
        globalWebSocket.onclose = null; // Prevent reconnection logic
        globalWebSocket.onerror = null;
        globalWebSocket.close(1000, 'Creating new connection');
        globalWebSocket = null;
    }

    updateConnectionStatus('connecting');

    // Add session ID to WebSocket URL
    const sessionId = getClientSessionId();
    const wsUrl = `${CONFIG.WEBSOCKET_URL}?session=${encodeURIComponent(sessionId)}`;
    const ws = new WebSocket(wsUrl);

    // Handle connection timeouts
    const connectionTimeout = setTimeout(() => {
        if (ws.readyState === WebSocket.CONNECTING) {
            console.warn('WebSocket connection timeout');
            ws.close();
        }
    }, 5000);

    ws.onopen = (event) => {
        clearTimeout(connectionTimeout);
        console.log('WebSocket connected');
        connectionState.isConnected = true;
        connectionState.reconnectAttempts = 0;
        connectionState.reconnectDelay = CONFIG.RECONNECT_DELAY;
        updateConnectionStatus('connected');

        // Subscribe to telemetry data when connection opens
        ws.send(JSON.stringify({
            type: 'subscribe',
            subscriptions: {
                telemetry: true,
                vehicle: true,
                gameState: true,
                circuit: true,
                race: true
            }
        }));
        console.log('Sent telemetry subscription request');

        if (globalHandleWebSocketMessage) {
            ws.addEventListener('message', globalHandleWebSocketMessage);
        }
    };

    ws.onclose = (event) => {
        connectionState.isConnected = false;
        const reason = event.reason || 'Unknown reason';
        console.log(`WebSocket connection closed: ${reason}. Attempting to reconnect...`);

        connectionState.reconnectAttempts++;

        // Exponential backoff with jitter
        const backoffDelay = Math.min(
            connectionState.reconnectDelay * Math.pow(1.5, connectionState.reconnectAttempts - 1),
            CONFIG.MAX_RECONNECT_DELAY
        );
        const jitteredDelay = backoffDelay + (Math.random() * 1000);

        updateConnectionStatus('connecting', 'Reconnecting...');

        setTimeout(() => {
            if (!connectionState.isConnected) {
                globalWebSocket = createWebSocketConnection();
            }
        }, jitteredDelay);
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        connectionState.isConnected = false;
        updateConnectionStatus('error', 'Connection failed');
    };

    return ws;
}

async function initSciChart() {
    // We'll initialize WebSocket connection after creating charts
    let ws;

    // Configure SciChart v4 - no need for data file configuration in v4
    // For community license, no action needed. For commercial, use:
    // SciChart.SciChartSurface.setRuntimeLicenseKey("YOUR_LICENSE_KEY");

    // Configure WASM URL if needed (usually auto-detected)
    SciChart.SciChartSurface.configure({
        wasmUrl: `https://cdn.jsdelivr.net/npm/scichart@${SciChart.libraryVersion}/_wasm/scichart2d.wasm`
    });


    // Chart creation helper functions
    const createAxisWithOptions = (wasmContext, options = {}) => {
        return new SciChart.NumericAxis(wasmContext, options);
    };

    const createDataSeries = (wasmContext, dataSeriesName = '') => {
        return new SciChart.XyDataSeries(wasmContext, {
            dataSeriesName: dataSeriesName,
            fifoCapacity: CONFIG.FIFO_CAPACITY,
            isSorted: true,
            containsNaN: false
        });
    };

    // SciChart v4 routes pointer/wheel input through an overlay stacked above the
    // 2D canvas, so DOM events bubble to the chart root rather than to the sibling
    // domCanvas2D. Attach custom listeners here so they actually receive events.
    const getEventElement = (surface) => surface.domChartRoot || surface.domCanvas2D;

    // Adjust the x-axis time window based on a horizontal scroll delta.
    // Shared by addZoomModifiers and addHorizontalZoomModifiers.
    const applyTimeWindowZoom = (xAxis, deltaX) => {
        const currentRange = xAxis.visibleRange;
        const rangeSize = currentRange.max - currentRange.min;

        // Scale the step size to the current window so control stays consistent.
        const windowSizeInSeconds = rangeSize / 60;
        let zoomFactor;
        if (windowSizeInSeconds < 10) {
            zoomFactor = deltaX > 0 ? 1.05 : 0.95;
        } else if (windowSizeInSeconds < 60) {
            zoomFactor = deltaX > 0 ? 1.03 : 0.97;
        } else {
            zoomFactor = deltaX > 0 ? 1.02 : 0.98;
        }
        const newRangeSize = rangeSize * zoomFactor;

        // While live-scrolling, keep the right edge pinned to the latest data and
        // resize only the left edge, so changing the window size doesn't drop out
        // of follow mode. Apply to every chart (they share one time window) and
        // guard the range change so it isn't treated as a manual pan.
        const following = Object.values(chartFollowModes).some(Boolean);
        if (following) {
            isUpdatingRange = true;
            Object.values(charts).forEach(chart => {
                if (chart && chart.xAxis) {
                    const max = chart.xAxis.visibleRange.max;
                    chart.xAxis.visibleRange = new SciChart.NumberRange(
                        Math.max(0, max - newRangeSize),
                        max
                    );
                }
            });
            isUpdatingRange = false;
            updateWindowSizeDisplay(newRangeSize);
            return;
        }

        // Paused/zoomed into history: resize around the centre so the user can
        // explore both directions. The visibleRangeChanged handler syncs the
        // other charts to match.
        const center = (currentRange.min + currentRange.max) / 2;
        let newMin = center - newRangeSize / 2;
        let newMax = center + newRangeSize / 2;

        if (newMin < 0) {
            newMin = 0;
            newMax = newRangeSize;
        }

        xAxis.visibleRange = new SciChart.NumberRange(newMin, newMax);
        updateWindowSizeDisplay(newRangeSize);
    };

    // Remembers the original autoRange of any Y axis we switch off, keyed by axis
    // instance, so it can be restored on double-click (return to live).
    const yAxisAutoFitDefaults = new WeakMap();

    // Disable continuous auto-ranging on auto-fitting Y axes so a manual vertical
    // zoom is not immediately overwritten when the next frame refits the axis.
    const disableYAxisAutoFit = (surface) => {
        surface.yAxes.asArray().forEach(axis => {
            if (!yAxisAutoFitDefaults.has(axis)) {
                yAxisAutoFitDefaults.set(axis, axis.autoRange);
            }
            if (axis.autoRange === SciChart.EAutoRange.Always) {
                axis.autoRange = SciChart.EAutoRange.Never;
            }
        });
    };

    // Restore the original autoRange so the Y axis re-fits to live data again.
    const restoreYAxisAutoFit = (surface) => {
        surface.yAxes.asArray().forEach(axis => {
            if (yAxisAutoFitDefaults.has(axis)) {
                axis.autoRange = yAxisAutoFitDefaults.get(axis);
            }
        });
    };

    const addZoomModifiers = (surface) => {
        // Custom horizontal-scroll handler adjusts the time window. Vertical wheel
        // zoom is handled by the native MouseWheelZoomModifier below, so only
        // consume the event for horizontal scroll and let vertical events fall through.
        getEventElement(surface).addEventListener('wheel', (event) => {
            if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
                event.preventDefault();
                const xAxis = surface.xAxes.get(0);
                if (xAxis) {
                    applyTimeWindowZoom(xAxis, event.deltaX);
                }
            } else {
                // Vertical wheel: let the MouseWheelZoomModifier do the zoom, but
                // first disable auto-fit so the new Y range persists.
                disableYAxisAutoFit(surface);
            }
        }, { passive: false });

        // Mouse wheel zoom modifier - zoom Y-axis only (vertical scale)
        surface.chartModifiers.add(new SciChart.MouseWheelZoomModifier({
            xyDirection: SciChart.EXyDirection.YDirection
        }));

        // Zoom pan modifier - pan by dragging with left mouse button
        surface.chartModifiers.add(new SciChart.ZoomPanModifier({
            xyDirection: SciChart.EXyDirection.XyDirection
        }));

        // Pinch zoom for touch devices
        surface.chartModifiers.add(new SciChart.PinchZoomModifier());

        // Add rollover modifier to show values at cursor position (Y values only)
        surface.chartModifiers.add(new SciChart.RolloverModifier({
            showTooltip: true,
            showRolloverLine: true,
            rolloverLineStroke: "#51cf66",
            tooltipContainerBackground: "rgba(30, 30, 30, 0.95)",
            tooltipTextStroke: "#ffffff",
            showAxisLabels: false,
            tooltipDataTemplate: (seriesInfo) => {
                const seriesName = seriesInfo.seriesName ||
                    seriesInfo.renderableSeries?.dataSeries?.dataSeriesName ||
                    'Value';
                const yValue = seriesInfo.yValue !== undefined ? seriesInfo.yValue.toFixed(2) : 'N/A';
                return [`${seriesName}: ${yValue}`];
            }
        }));
    };

    const addHorizontalZoomModifiers = (surface) => {
        // Custom wheel handler for horizontal scroll (time window) only. Only
        // consume horizontal scroll so vertical wheel events fall through normally.
        getEventElement(surface).addEventListener('wheel', (event) => {
            if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
                event.preventDefault();
                const xAxis = surface.xAxes.get(0);
                if (xAxis) {
                    applyTimeWindowZoom(xAxis, event.deltaX);
                }
            }
        }, { passive: false });

        // Zoom pan modifier - horizontal pan only
        surface.chartModifiers.add(new SciChart.ZoomPanModifier({
            xyDirection: SciChart.EXyDirection.XDirection
        }));

        // Pinch zoom for touch devices - horizontal only
        surface.chartModifiers.add(new SciChart.PinchZoomModifier({
            xyDirection: SciChart.EXyDirection.XDirection
        }));

        // Add rollover modifier to show values at cursor position
        surface.chartModifiers.add(new SciChart.RolloverModifier({
            showTooltip: true,
            showRolloverLine: true,
            rolloverLineStroke: "#51cf66",
            tooltipContainerBackground: "rgba(30, 30, 30, 0.95)",
            tooltipTextStroke: "#ffffff",
            showAxisLabels: false,
            tooltipDataTemplate: (seriesInfo) => {
                const seriesName = seriesInfo.seriesName ||
                    seriesInfo.renderableSeries?.dataSeries?.dataSeriesName ||
                    'Value';
                const yValue = seriesInfo.yValue !== undefined ? seriesInfo.yValue.toFixed(2) : 'N/A';
                return [`${seriesName}: ${yValue}`];
            }
        }));
    };

    const createStandardChart = async (containerId) => {
        const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

        const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
        const yAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Always });

        sciChartSurface.xAxes.add(xAxis);
        sciChartSurface.yAxes.add(yAxis);

        return { sciChartSurface, wasmContext };
    };

    // Modular chart definitions
    const CHART_DEFINITIONS = {
        'rpm-speed': {
            title: 'RPM / Speed',
            titleKey: 'runmode.telemetry.chart.rpmspeed',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxisRPM = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_RPM",
                    axisAlignment: SciChart.EAxisAlignment.Left,
                    visibleRange: new SciChart.NumberRange(0, 10000),
                    labelPrecision: 0,
                    labelStyle: { color: "#50C7E0" }
                });
                const yAxisSpeed = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_SPEED",
                    axisAlignment: SciChart.EAxisAlignment.Right,
                    visibleRange: new SciChart.NumberRange(0, 350),
                    labelPrecision: 0,
                    labelStyle: { color: "#C750E0" }
                });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxisRPM, yAxisSpeed);

                const rpmSeries = createDataSeries(wasmContext, "RPM");
                const speedSeries = createDataSeries(wasmContext, "Speed (km/h)");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: rpmSeries,
                        dataSeriesName: "RPM",
                        yAxisId: "ID_Y_AXIS_RPM",
                        strokeThickness: 3,
                        stroke: "#50C7E0"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: speedSeries,
                        dataSeriesName: "Speed (km/h)",
                        yAxisId: "ID_Y_AXIS_SPEED",
                        strokeThickness: 3,
                        stroke: "#C750E0"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        rpm: rpmSeries,
                        speed: speedSeries
                    },
                    dataFields: ['rpm', 'speed']
                };
            }
        },

        'throttle-brake': {
            title: 'Throttle / Brake',
            titleKey: 'runmode.telemetry.chart.throttlebrake',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxis = createAxisWithOptions(wasmContext, { visibleRange: new SciChart.NumberRange(0, 110) });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxis);

                const throttleInputSeries = createDataSeries(wasmContext, "Throttle In");
                const throttleOutputSeries = createDataSeries(wasmContext, "Throttle Out");
                const brakeInputSeries = createDataSeries(wasmContext, "Brake In");
                const brakeOutputSeries = createDataSeries(wasmContext, "Brake Out");

                const seriesConfigs = [
                    { dataSeries: throttleInputSeries, dataSeriesName: "Throttle In", strokeThickness: 3, stroke: "#00F000" },
                    { dataSeries: throttleOutputSeries, dataSeriesName: "Throttle Out", strokeThickness: 2, stroke: "#6EADFF" },
                    { dataSeries: brakeInputSeries, dataSeriesName: "Brake In", strokeThickness: 3, stroke: "#F00000" },
                    { dataSeries: brakeOutputSeries, dataSeriesName: "Brake Out", strokeThickness: 2, stroke: "#FF8A7D" }
                ];

                seriesConfigs.forEach(config => {
                    sciChartSurface.renderableSeries.add(
                        new SciChart.FastLineRenderableSeries(wasmContext, config)
                    );
                });

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        throttleInput: throttleInputSeries,
                        throttleOutput: throttleOutputSeries,
                        brakeInput: brakeInputSeries,
                        brakeOutput: brakeOutputSeries
                    },
                    dataFields: ['throttleInput', 'throttleOutput', 'brakeInput', 'brakeOutput']
                };
            }
        },

        'tyre-temperature': {
            title: 'Tyre Temperature',
            titleKey: 'runmode.telemetry.chart.tyretemperature',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxis = createAxisWithOptions(wasmContext, {
                    autoRange: SciChart.EAutoRange.Never,
                    visibleRange: new SciChart.NumberRange(60, 90)
                }); sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxis);

                const tyreColors = {
                    FL: "#7072fdff", // Front Left - Light Blue
                    FR: "#fa6a6aff", // Front Right - Light Red
                    RL: "#0043fcff", // Rear Left - Dark Blue
                    RR: "#ff0000ff"  // Rear Right - Red
                };

                const tyreSeries = {};
                Object.entries(tyreColors).forEach(([position, color]) => {
                    const series = createDataSeries(wasmContext, `${position}`);
                    tyreSeries[position] = series;

                    sciChartSurface.renderableSeries.add(
                        new SciChart.FastLineRenderableSeries(wasmContext, {
                            dataSeries: series,
                            dataSeriesName: `${position}`,
                            strokeThickness: 3,
                            stroke: color
                        })
                    );
                });

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        tyreTempFL: tyreSeries.FL,
                        tyreTempFR: tyreSeries.FR,
                        tyreTempRL: tyreSeries.RL,
                        tyreTempRR: tyreSeries.RR
                    },
                    dataFields: ['tyreTempFL', 'tyreTempFR', 'tyreTempRL', 'tyreTempRR']
                };
            }
        },

        'fuel-range': {
            title: 'Fuel Range/Rate',
            titleKey: 'runmode.telemetry.chart.fuelrange',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxisRate = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_RATE",
                    axisAlignment: SciChart.EAxisAlignment.Left,
                    autoRange: SciChart.EAutoRange.Always,
                    labelPrecision: 2,
                    labelStyle: { color: "#f9b73dff" }
                });
                const yAxisRange = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_RANGE",
                    axisAlignment: SciChart.EAxisAlignment.Right,
                    autoRange: SciChart.EAutoRange.Always,
                    labelPrecision: 1,
                    labelStyle: { color: "#5072e0ff" }
                });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxisRate, yAxisRange);

                const fuelRateSeries = createDataSeries(wasmContext, "Fuel Usage (%/km)");
                const fuelRangeSeries = createDataSeries(wasmContext, "Range (km)");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: fuelRateSeries,
                        dataSeriesName: "Fuel Usage (%/km)",
                        yAxisId: "ID_Y_AXIS_RATE",
                        strokeThickness: 3,
                        stroke: "#f9b73dff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: fuelRangeSeries,
                        dataSeriesName: "Range (km)",
                        yAxisId: "ID_Y_AXIS_RANGE",
                        strokeThickness: 3,
                        stroke: "#5072e0ff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        fuelRate: fuelRateSeries,
                        fuelRange: fuelRangeSeries
                    },
                    dataFields: ['fuelUsagePerKm', 'fuelRangeKm']
                };
            }
        },

        'jerk': {
            title: '6DOF Jerk',
            titleKey: 'runmode.telemetry.chart.jerk',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId);

                addZoomModifiers(sciChartSurface);

                const translationalJerk = createDataSeries(wasmContext, "Trans. Jerk");
                const translationalJerkCalc = createDataSeries(wasmContext, "Trans. Jerk (Calc)");
                const rotationalJerk = createDataSeries(wasmContext, "Rot. Jerk");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: translationalJerk,
                        dataSeriesName: "Trans. Jerk",
                        strokeThickness: 1,
                        stroke: "#949494ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: translationalJerkCalc,
                        dataSeriesName: "Trans. Jerk (Calc)",
                        strokeThickness: 1,
                        stroke: "#50C7E0"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: rotationalJerk,
                        dataSeriesName: "Rot. Jerk",
                        strokeThickness: 2,
                        stroke: "#C750E0"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        translationalJerk: translationalJerk,
                        translationalJerkCalc: translationalJerkCalc,
                        rotationalJerk: rotationalJerk
                    },
                    dataFields: ['SixDOFTranslationalJerk', 'SixDOFTranslationalJerkCalc', 'SixDOFRotationalJerk']
                };
            }
        },

        'snap': {
            title: '6DOF Snap',
            titleKey: 'runmode.telemetry.chart.snap',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId);

                addZoomModifiers(sciChartSurface);

                const translationalSnapCalc = createDataSeries(wasmContext, "Trans. Snap (Calc)");
                const rotationalSnap = createDataSeries(wasmContext, "Rot. Snap");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: translationalSnapCalc,
                        dataSeriesName: "Trans. Snap (Calc)",
                        strokeThickness: 1,
                        stroke: "#50C7E0ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: rotationalSnap,
                        dataSeriesName: "Rot. Snap",
                        strokeThickness: 2,
                        stroke: "#c850e0ff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        translationalSnapCalc: translationalSnapCalc,
                        rotationalSnap: rotationalSnap
                    },
                    dataFields: ['SixDOFTranslationalSnapCalc', 'SixDOFRotationalSnap']
                };
            }
        },

        'translational-acceleration': {
            title: '6DOF Translational Acceleration',
            titleKey: 'runmode.telemetry.chart.translationalaccel',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId);

                addZoomModifiers(sciChartSurface);

                const translationalAccelerationX = createDataSeries(wasmContext, "Surge");
                const translationalAccelerationY = createDataSeries(wasmContext, "Sway");
                const translationalAccelerationZ = createDataSeries(wasmContext, "Heave");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: translationalAccelerationX,
                        dataSeriesName: "Surge",
                        strokeThickness: 1,
                        stroke: "#e05050ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: translationalAccelerationY,
                        dataSeriesName: "Sway",
                        strokeThickness: 2,
                        stroke: "#50e06aff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: translationalAccelerationZ,
                        dataSeriesName: "Heave",
                        strokeThickness: 3,
                        stroke: "#5052e0ff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        translationalAccelerationX: translationalAccelerationX,
                        translationalAccelerationY: translationalAccelerationY,
                        translationalAccelerationZ: translationalAccelerationZ
                    },
                    dataFields: ['SixDOFTranslationalAccelX', 'SixDOFTranslationalAccelY', 'SixDOFTranslationalAccelZ']
                };
            }
        },

        'rotational-acceleration': {
            title: '6DOF Rotational Acceleration',
            titleKey: 'runmode.telemetry.chart.rotationalaccel',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId);

                addZoomModifiers(sciChartSurface);

                const rotationalAccelerationX = createDataSeries(wasmContext, "Pitch");
                const rotationalAccelerationY = createDataSeries(wasmContext, "Yaw");
                const rotationalAccelerationZ = createDataSeries(wasmContext, "Roll");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: rotationalAccelerationX,
                        dataSeriesName: "Pitch",
                        strokeThickness: 1,
                        stroke: "#e05050ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: rotationalAccelerationY,
                        dataSeriesName: "Yaw",
                        strokeThickness: 2,
                        stroke: "#50e06aff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: rotationalAccelerationZ,
                        dataSeriesName: "Roll ",
                        strokeThickness: 3,
                        stroke: "#5052e0ff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        rotationalAccelerationX: rotationalAccelerationX,
                        rotationalAccelerationY: rotationalAccelerationY,
                        rotationalAccelerationZ: rotationalAccelerationZ
                    },
                    dataFields: ['SixDOFRotationalAccelX', 'SixDOFRotationalAccelY', 'SixDOFRotationalAccelZ']
                };
            }
        },

        'compute-time': {
            title: 'Compute Time (µs)',
            titleKey: 'runmode.telemetry.chart.computetime',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId);

                addZoomModifiers(sciChartSurface);

                const series = createDataSeries(wasmContext, "Compute Time (µs)");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastMountainRenderableSeries(wasmContext, {
                        dataSeries: series,
                        dataSeriesName: "Compute Time (µs)",
                        strokeThickness: 1,
                        stroke: "#42cb52ff",
                        fillLinearGradient: new SciChart.GradientParams(new SciChart.Point(0, 0), new SciChart.Point(0, 1), [
                            { color: "#42cb5299", offset: 0 },
                            { color: "#42cb521e", offset: 1 },
                        ]),
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        computeTime: series
                    },
                    dataFields: ['computeTime']
                };
            }
        },

        'channel-output-left': {
            title: 'Channel 0 Output',
            titleKey: 'runmode.telemetry.chart.channeloutputleft',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addHorizontalZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxisAmplitude = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_AMPLITUDE",
                    axisAlignment: SciChart.EAxisAlignment.Left,
                    visibleRange: new SciChart.NumberRange(0, 1),
                    labelPrecision: 3,
                    labelStyle: { color: "#fcdd5fff" }
                });
                const yAxisFrequency = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_FREQUENCY",
                    axisAlignment: SciChart.EAxisAlignment.Right,
                    visibleRange: new SciChart.NumberRange(0, 60),
                    labelPrecision: 0,
                    labelStyle: { color: "#38b0faff" }
                });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxisAmplitude, yAxisFrequency);

                const amplitudeSeries = createDataSeries(wasmContext, "Amplitude (L)");
                const frequencySeries = createDataSeries(wasmContext, "Frequency (L)");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastMountainRenderableSeries(wasmContext, {
                        dataSeries: amplitudeSeries,
                        dataSeriesName: "Amplitude (L)",
                        yAxisId: "ID_Y_AXIS_AMPLITUDE",
                        strokeThickness: 1,
                        stroke: "#fcca5fdf",
                        fillLinearGradient: new SciChart.GradientParams(new SciChart.Point(0, 0), new SciChart.Point(0, 1), [
                            { color: "#fcca5f99", offset: 0 },
                            { color: "#fcca5f1b", offset: 1 },
                        ]),
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: frequencySeries,
                        dataSeriesName: "Frequency (L)",
                        yAxisId: "ID_Y_AXIS_FREQUENCY",
                        strokeThickness: 2,
                        stroke: "#38b0faff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        channelAmplitudeL: amplitudeSeries,
                        channelFrequencyL: frequencySeries
                    },
                    // Telemetry now sends per-channel arrays (synthChannelAmplitude /
                    // synthChannelFrequency); index into channel 0 here (the ":N" suffix
                    // is a synthetic field name understood by the frame ingestion code).
                    dataFields: ['synthChannelAmplitude:0', 'synthChannelFrequency:0']
                };
            }
        },

        'audio-health': {
            title: 'Audio Pipeline Health',
            titleKey: 'runmode.telemetry.chart.audiohealth',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addHorizontalZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxisFill = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_FILL",
                    axisAlignment: SciChart.EAxisAlignment.Left,
                    visibleRange: new SciChart.NumberRange(0, 1),
                    labelPrecision: 2,
                    labelStyle: { color: "#50e06aff" }
                });
                const yAxisEvents = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_EVENTS",
                    axisAlignment: SciChart.EAxisAlignment.Right,
                    visibleRange: new SciChart.NumberRange(0, 10),
                    labelPrecision: 0,
                    labelStyle: { color: "#e05050ff" }
                });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxisFill, yAxisEvents);

                const asyncFillSeries = createDataSeries(wasmContext, "Async Buffer Fill");
                const engineFillSeries = createDataSeries(wasmContext, "Engine Fill");
                const chassis0FillSeries = createDataSeries(wasmContext, "Chassis 0 Fill");
                const underrunsSeries = createDataSeries(wasmContext, "Underruns");
                const producerWaitsSeries = createDataSeries(wasmContext, "Producer Waits");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: asyncFillSeries,
                        dataSeriesName: "Async Buffer Fill",
                        yAxisId: "ID_Y_AXIS_FILL",
                        strokeThickness: 2,
                        stroke: "#50e06aff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: engineFillSeries,
                        dataSeriesName: "Engine Fill",
                        yAxisId: "ID_Y_AXIS_FILL",
                        strokeThickness: 2,
                        stroke: "#50c7e0ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: chassis0FillSeries,
                        dataSeriesName: "Chassis 0 Fill",
                        yAxisId: "ID_Y_AXIS_FILL",
                        strokeThickness: 2,
                        stroke: "#c750e0ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: underrunsSeries,
                        dataSeriesName: "Underruns",
                        yAxisId: "ID_Y_AXIS_EVENTS",
                        strokeThickness: 2,
                        stroke: "#e05050ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: producerWaitsSeries,
                        dataSeriesName: "Producer Waits",
                        yAxisId: "ID_Y_AXIS_EVENTS",
                        strokeThickness: 2,
                        stroke: "#e0c750ff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        asyncBufferFill: asyncFillSeries,
                        mixerEngineFill: engineFillSeries,
                        mixerChassis0Fill: chassis0FillSeries,
                        asyncUnderruns: underrunsSeries,
                        asyncProducerWaits: producerWaitsSeries
                    },
                    // mixerChassisFill is now a per-channel array; chart channel 0's fill only.
                    dataFields: ['asyncBufferFill', 'mixerEngineFill', 'mixerChassisFill:0', 'asyncUnderruns', 'asyncProducerWaits']
                };
            }
        },

        'haptic-latency': {
            title: 'Haptic Latency & Drift (ms)',
            titleKey: 'runmode.telemetry.chart.hapticlatency',
            create: async (containerId) => {
                // These four metrics share a millisecond scale, so a single
                // auto-ranging Y axis keeps engine/chassis/ring buffer latency
                // and the cumulative drift (signed) together. Telemetry cadence
                // jitter lives on the Sequence ID Gaps chart instead.
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId);

                addHorizontalZoomModifiers(sciChartSurface);

                const engineLatencySeries = createDataSeries(wasmContext, "Engine Latency (ms)");
                const chassisLatencySeries = createDataSeries(wasmContext, "Chassis Latency (ms)");
                const ringLatencySeries = createDataSeries(wasmContext, "Ring Latency (ms)");
                const driftSeries = createDataSeries(wasmContext, "Drift (ms)");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: engineLatencySeries,
                        dataSeriesName: "Engine Latency (ms)",
                        strokeThickness: 2,
                        stroke: "#50c7e0ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: chassisLatencySeries,
                        dataSeriesName: "Chassis Latency (ms)",
                        strokeThickness: 2,
                        stroke: "#c750e0ff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: ringLatencySeries,
                        dataSeriesName: "Ring Latency (ms)",
                        strokeThickness: 2,
                        stroke: "#50e06aff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: driftSeries,
                        dataSeriesName: "Drift (ms)",
                        strokeThickness: 2,
                        stroke: "#e05050ff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        engineLatencyMs: engineLatencySeries,
                        chassisLatencyMs: chassisLatencySeries,
                        ringLatencyMs: ringLatencySeries,
                        driftMs: driftSeries
                    },
                    dataFields: ['engineLatencyMs', 'chassisLatencyMs', 'ringLatencyMs', 'driftMs']
                };
            }
        },

        'seq-gap': {
            title: 'Sequence ID Gaps',
            titleKey: 'runmode.telemetry.chart.seqgap',
            create: async (containerId) => {
                // Two telemetry-packet health metrics on a shared time axis:
                // dropped-packet count (left, integer) and cadence jitter in ms
                // (right, auto-ranged). Jitter is a smoothed absolute deviation
                // from the nominal 60 fps cadence.
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxisGap = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_GAP",
                    axisAlignment: SciChart.EAxisAlignment.Left,
                    autoRange: SciChart.EAutoRange.Always,
                    labelPrecision: 0,
                    labelStyle: { color: "#e05050ff" }
                });
                const yAxisJitter = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_JITTER",
                    axisAlignment: SciChart.EAxisAlignment.Right,
                    autoRange: SciChart.EAutoRange.Always,
                    labelPrecision: 2,
                    labelStyle: { color: "#e0c750ff" }
                });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxisGap, yAxisJitter);

                const seqGapSeries = createDataSeries(wasmContext, "Dropped Packets");
                const seqJitterSeries = createDataSeries(wasmContext, "Jitter (ms)");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastMountainRenderableSeries(wasmContext, {
                        dataSeries: seqGapSeries,
                        dataSeriesName: "Dropped Packets",
                        yAxisId: "ID_Y_AXIS_GAP",
                        strokeThickness: 1,
                        stroke: "#e05050ff",
                        fillLinearGradient: new SciChart.GradientParams(new SciChart.Point(0, 0), new SciChart.Point(0, 1), [
                            { color: "#e0505099", offset: 0 },
                            { color: "#e050501e", offset: 1 },
                        ]),
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: seqJitterSeries,
                        dataSeriesName: "Jitter (ms)",
                        yAxisId: "ID_Y_AXIS_JITTER",
                        strokeThickness: 2,
                        stroke: "#e0c750ff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        seqGap: seqGapSeries,
                        seqJitterMs: seqJitterSeries
                    },
                    dataFields: ['seqGap', 'seqJitterMs']
                };
            }
        },

        'channel-output-right': {
            title: 'Channel 1 Output',
            titleKey: 'runmode.telemetry.chart.channeloutputright',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.createSingle(containerId);

                addHorizontalZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Never });
                const yAxisAmplitude = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_AMPLITUDE",
                    axisAlignment: SciChart.EAxisAlignment.Left,
                    visibleRange: new SciChart.NumberRange(0, 1),
                    labelPrecision: 3,
                    labelStyle: { color: "#fcdd5fff" }
                });
                const yAxisFrequency = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_FREQUENCY",
                    axisAlignment: SciChart.EAxisAlignment.Right,
                    visibleRange: new SciChart.NumberRange(0, 60),
                    labelPrecision: 0,
                    labelStyle: { color: "#38b0faff" }
                });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxisAmplitude, yAxisFrequency);

                const amplitudeSeries = createDataSeries(wasmContext, "Amplitude (R)");
                const frequencySeries = createDataSeries(wasmContext, "Frequency (R)");

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastMountainRenderableSeries(wasmContext, {
                        dataSeries: amplitudeSeries,
                        dataSeriesName: "Amplitude (R)",
                        yAxisId: "ID_Y_AXIS_AMPLITUDE",
                        strokeThickness: 1,
                        stroke: "#fcca5fdf",
                        fillLinearGradient: new SciChart.GradientParams(new SciChart.Point(0, 0), new SciChart.Point(0, 1), [
                            { color: "#fcca5f99", offset: 0 },
                            { color: "#fcca5f1b", offset: 1 },
                        ]),
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: frequencySeries,
                        dataSeriesName: "Frequency (R)",
                        yAxisId: "ID_Y_AXIS_FREQUENCY",
                        strokeThickness: 2,
                        stroke: "#38b0faff"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        channelAmplitudeR: amplitudeSeries,
                        channelFrequencyR: frequencySeries
                    },
                    // See channel-output-left: index into channel 1 of the arrays.
                    dataFields: ['synthChannelAmplitude:1', 'synthChannelFrequency:1']
                };
            }
        }
    };

    // Chart factory system
    const createCharts = async () => {
        const charts = {};
        const allDataSeries = {};

        // Create charts based on registry
        for (const chartConfig of CHART_REGISTRY) {
            if (!chartConfig.enabled) {
                console.log(`Chart ${chartConfig.id} is disabled, skipping...`);
                continue;
            }

            const chartDefinition = CHART_DEFINITIONS[chartConfig.id];
            if (!chartDefinition) {
                console.warn(`Chart definition not found for: ${chartConfig.id}`);
                continue;
            }

            try {
                console.log(`Creating chart: ${chartConfig.id}`);
                const chartInstance = await chartDefinition.create(chartConfig.containerId);

                charts[chartConfig.id] = chartInstance;

                Object.assign(allDataSeries, chartInstance.dataSeries);

                console.log(`Successfully created chart: ${chartConfig.id}`);
            } catch (error) {
                console.error(`Failed to create chart ${chartConfig.id}:`, error);
            }
        }

        return { charts, allDataSeries };
    };

    const { charts, allDataSeries } = await createCharts();

    // Initialize follow mode for all charts and attach pan detection
    const chartFollowModes = {};
    let isUpdatingRange = false; // Flag to track when we're programmatically updating ranges
    let isDoubleClickTriggered = false;

    // Setup window size input handler (after chartFollowModes is initialized)
    setupWindowSizeInput(Object.values(charts), chartFollowModes);

    Object.entries(charts).forEach(([chartId, chart]) => {
        chartFollowModes[chartId] = true;

        // Listen for single click to disable follow mode
        getEventElement(chart.surface).addEventListener('click', (event) => {
            Object.keys(charts).forEach(otherChartId => {
                chartFollowModes[otherChartId] = false;
            });
        });

        // Listen for double-click to switch back to following latest data
        getEventElement(chart.surface).addEventListener('dblclick', (event) => {
            event.preventDefault();
            event.stopPropagation();

            // Guard against the programmatic pan below (and the live-update pan on
            // the next frame) being treated as a user-initiated range change, which
            // would immediately cancel follow mode again. The visibleRangeChanged
            // subscription checks this flag.
            isDoubleClickTriggered = true;

            // Re-enable follow mode for all charts on double-click (preserves window size)
            Object.keys(charts).forEach(otherChartId => {
                chartFollowModes[otherChartId] = true;
            });

            // Immediately pan all charts to show the latest data point and
            // re-enable Y auto-fit that a prior vertical zoom may have disabled.
            isUpdatingRange = true;
            const latestSeq = lastSeq;
            Object.entries(charts).forEach(([otherChartId, otherChart]) => {
                if (otherChart && otherChart.xAxis) {
                    const currentRange = otherChart.xAxis.visibleRange;
                    const windowSize = currentRange.max - currentRange.min;
                    otherChart.xAxis.visibleRange = new SciChart.NumberRange(
                        latestSeq - windowSize,
                        latestSeq
                    );
                }
                if (otherChart && otherChart.surface) {
                    restoreYAxisAutoFit(otherChart.surface);
                }
            });
            isUpdatingRange = false;

            // Release the guard after any deferred visibleRangeChanged has settled.
            setTimeout(() => {
                isDoubleClickTriggered = false;
            }, 100);
        });

        // Listen for when user manually changes the visible range
        chart.xAxis.visibleRangeChanged.subscribe((args) => {
            // Only disable follow mode if this is a user action (not our programmatic update)
            if (!isUpdatingRange && !isDoubleClickTriggered) {
                chartFollowModes[chartId] = false;

                // Synchronize all other charts to the same X-axis range
                isUpdatingRange = true; // Prevent recursion
                Object.entries(charts).forEach(([otherChartId, otherChart]) => {
                    if (otherChartId !== chartId && otherChart && otherChart.xAxis) {
                        chartFollowModes[otherChartId] = false;
                        otherChart.xAxis.visibleRange = new SciChart.NumberRange(
                            args.visibleRange.min,
                            args.visibleRange.max
                        );
                    }
                });
                isUpdatingRange = false;
            }
        });
    });

    // WebSocket message handling
    let lastTimeOfDay = 0;
    let lastSeq = 0;

    const handleWebSocketMessage = (event) => {
        const receivedData = JSON.parse(event.data);

        // Check if this is a unified WebSocket message with type envelope
        let dataFrames;
        if (receivedData.type === 'telemetry') {
            // New unified format: extract data from envelope
            dataFrames = Array.isArray(receivedData.data) ? receivedData.data : [receivedData.data];
        } else if (receivedData.type) {
            // Other message types (vehicle, circuit, race, gameState) - ignore for charts
            return;
        } else {
            // Legacy format: direct data (array or single frame)
            dataFrames = Array.isArray(receivedData) ? receivedData : [receivedData];
        }

        // Process each frame in the batch
        dataFrames.forEach(data => {
            // Clear all data series if sequence number has reset (indicates new session)
            if (data.seq < lastSeq) {
                Object.values(allDataSeries).forEach(series => series.clear());
                // Reset follow modes on new session
                Object.keys(charts).forEach(chartId => {
                    chartFollowModes[chartId] = true;
                });
            }

            const currentTime = data.seq;

            const dataFieldMappings = {};

            // Build the mapping dynamically based on enabled charts
            Object.values(charts).forEach(chart => {
                chart.dataFields.forEach(fieldName => {
                    const seriesKey = Object.keys(chart.dataSeries).find(key => {
                        // Map data field names to series keys
                        const fieldMappings = {
                            'fuelUsagePerKm': 'fuelRate',
                            'fuelRangeKm': 'fuelRange',
                            'SixDOFTranslationalJerkCalc': 'translationalJerkCalc',
                            'SixDOFTranslationalJerk': 'translationalJerk',
                            'SixDOFTranslationalSnapCalc': 'translationalSnapCalc',
                            'SixDOFTranslationalSnap': 'translationalSnap',
                            'SixDOFTranslationalAccelX': 'translationalAccelerationX',
                            'SixDOFTranslationalAccelY': 'translationalAccelerationY',
                            'SixDOFTranslationalAccelZ': 'translationalAccelerationZ',
                            'SixDOFRotationalJerk': 'rotationalJerk',
                            'SixDOFRotationalSnap': 'rotationalSnap',
                            "SixDOFRotationalAccelX": 'rotationalAccelerationX',
                            "SixDOFRotationalAccelY": 'rotationalAccelerationY',
                            "SixDOFRotationalAccelZ": 'rotationalAccelerationZ',
                            'synthChannelAmplitude:0': 'channelAmplitudeL',
                            'synthChannelFrequency:0': 'channelFrequencyL',
                            'synthChannelAmplitude:1': 'channelAmplitudeR',
                            'synthChannelFrequency:1': 'channelFrequencyR',
                            'mixerChassisFill:0': 'mixerChassis0Fill'
                        };
                        return fieldMappings[fieldName] === key || fieldName === key;
                    });

                    if (seriesKey) {
                        // Synthetic "arrayField:index" field names index into a telemetry
                        // array field (e.g. synthChannelAmplitude, synthChannelFrequency,
                        // mixerChassisFill), which replaced the old flat per-channel
                        // (...L/...R/...0) fields.
                        const separatorIndex = fieldName.indexOf(':');
                        let value;
                        if (separatorIndex !== -1) {
                            const arrayField = fieldName.slice(0, separatorIndex);
                            const channelIndex = parseInt(fieldName.slice(separatorIndex + 1), 10);
                            value = Array.isArray(data[arrayField]) ? data[arrayField][channelIndex] : undefined;
                        } else {
                            value = data[fieldName];
                        }

                        if (value !== undefined) {
                            dataFieldMappings[seriesKey] = value;
                        }
                    }
                });
            });

            Object.entries(dataFieldMappings).forEach(([seriesKey, value]) => {
                if (allDataSeries[seriesKey] && value !== undefined) {
                    allDataSeries[seriesKey].appendRange([currentTime], [value]);
                }
            });

            lastTimeOfDay = data.timeOfDay;
            lastSeq = data.seq;
        });

        // Update visible range for smooth scrolling on all charts (if following live data)
        // Do this once after processing all frames in the batch
        const lastFrame = dataFrames[dataFrames.length - 1];
        const currentTime = lastFrame.seq;

        isUpdatingRange = true; // Set flag before updating ranges
        Object.entries(charts).forEach(([chartId, chart]) => {
            if (chart && chart.xAxis) {
                // Initialize visible range on first data if not set
                if (chart.xAxis.visibleRange.max === 0 || chart.xAxis.visibleRange.min === 0) {
                    const initialWindowSize = 200;
                    chart.xAxis.visibleRange = new SciChart.NumberRange(
                        Math.max(0, currentTime - initialWindowSize),
                        currentTime
                    );
                    // Update window size display on initialization
                    updateWindowSizeDisplay(initialWindowSize);
                    return;
                }

                // Only auto-scroll if in follow mode
                if (chartFollowModes[chartId]) {
                    // Preserve the current window size
                    const currentRange = chart.xAxis.visibleRange;
                    const windowSize = currentRange.max - currentRange.min;

                    // Update to show the latest data while keeping the same window size
                    chart.xAxis.visibleRange = new SciChart.NumberRange(
                        currentTime - windowSize,
                        currentTime
                    );

                    // Update window size display periodically (throttle to first chart only)
                    if (chartId === Object.keys(charts)[0]) {
                        updateWindowSizeDisplay(windowSize);
                    }
                }
            }
        });
        isUpdatingRange = false; // Clear flag after updating ranges
    };

    // Store references globally for reconnection handling
    globalCharts = charts;
    globalAllDataSeries = allDataSeries;
    globalHandleWebSocketMessage = handleWebSocketMessage;

    globalWebSocket = createWebSocketConnection();
    globalWebSocket.addEventListener('message', handleWebSocketMessage);

    // Cleanup on page unload to prevent orphaned connections
    const cleanup = () => {
        console.log('Page unloading, cleaning up resources...');

        // Close WebSocket connection immediately and synchronously
        if (globalWebSocket) {
            try {
                // Unsubscribe from telemetry before closing
                if (globalWebSocket.readyState === WebSocket.OPEN) {
                    globalWebSocket.send(JSON.stringify({
                        type: 'subscribe',
                        subscriptions: {
                            telemetry: false
                        }
                    }));
                }

                globalWebSocket.onclose = null; // Prevent reconnection attempts
                globalWebSocket.onerror = null;
                globalWebSocket.onmessage = null;
                globalWebSocket.close(1000, 'Page unload');
                globalWebSocket = null;
            } catch (e) {
                console.error('Error closing WebSocket:', e);
            }
        }

        // Delete all charts to free resources
        Object.values(charts).forEach(chart => {
            if (chart && chart.surface) {
                try {
                    chart.surface.delete();
                } catch (e) {
                    console.error('Error deleting chart:', e);
                }
            }
        });

        globalCharts = {};
        globalAllDataSeries = {};
        globalHandleWebSocketMessage = null;
    };

    let isPageUnloading = false;

    // Use visibility change to detect tab closing/navigation - more reliable than beforeunload
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'hidden' && globalWebSocket) {
            isPageUnloading = true;
            cleanup();
        }
    });

    // Also use pagehide as backup
    window.addEventListener('pagehide', () => {
        if (!isPageUnloading) {
            isPageUnloading = true;
            cleanup();
        }
    });

    // Use unload as last resort (least reliable but covers some edge cases)
    window.addEventListener('unload', () => {
        if (!isPageUnloading) {
            cleanup();
        }
    });
}

// Initialize the application - wait for i18n to be loaded first
let isInitialized = false;

async function initializeWithi18n() {
    if (isInitialized) {
        console.log('SciChart already initialized, skipping...');
        return;
    }

    // Check if i18n is already loaded
    if (window.i18nLoaded) {
        console.log('i18n already loaded, initializing SciChart...');
        isInitialized = true;
        await initSciChart();
    } else {
        console.log('Waiting for i18n to load before initializing SciChart...');
        window.addEventListener('i18nLoaded', async () => {
            if (!isInitialized) {
                console.log('i18n loaded, now initializing SciChart...');
                isInitialized = true;
                await initSciChart();
            }
        }, { once: true });
    }
}

initializeWithi18n().catch(error => {
    console.error('Failed to initialize application:', error);
});
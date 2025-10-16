// Configuration constants
const CONFIG = {
    FIFO_CAPACITY: 200,
    RECONNECT_DELAY: 1000,
    WEBSOCKET_URL: `ws://${location.host}/ws`
};

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
        { id: 'translational-jerk', containerId: 'scichart-root-1', enabled: true },
        { id: 'rotational-jerk', containerId: 'scichart-root-2', enabled: true },
        { id: 'translational-snap', containerId: 'scichart-root-3', enabled: true },
        { id: 'rotational-snap', containerId: 'scichart-root-4', enabled: true },
        { id: 'synthesizer-output', containerId: 'scichart-root-5', enabled: true },
        { id: 'compute-time', containerId: 'scichart-root-6', enabled: true }
    ],

    // Default/fallback - all charts enabled
    'default': [
        { id: 'rpm-speed', containerId: 'scichart-root-1', enabled: true },
        { id: 'throttle-brake', containerId: 'scichart-root-2', enabled: true },
        { id: 'tyre-temperature', containerId: 'scichart-root-3', enabled: true },
        { id: 'fuel-range', containerId: 'scichart-root-4', enabled: true },
        { id: 'translational-jerk', containerId: 'scichart-root-5', enabled: true },
        { id: 'rotational-jerk', containerId: 'scichart-root-6', enabled: true },
        { id: 'translational-snap', containerId: 'scichart-root-7', enabled: true },
        { id: 'rotational-snap', containerId: 'scichart-root-8', enabled: true },
        { id: 'synthesizer-output', containerId: 'scichart-root-9', enabled: true },
        { id: 'compute-time', containerId: 'scichart-root-10', enabled: true }
    ]
};

// Function to get chart configuration from script tag data attribute
function getChartRegistry() {
    // Find the current script tag
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

// Get the appropriate chart registry based on script tag parameter
const CHART_REGISTRY = getChartRegistry();

// WebSocket connection management
function createWebSocketConnection() {
    const ws = new WebSocket(CONFIG.WEBSOCKET_URL);

    ws.onclose = (event) => {
        console.log('WebSocket connection closed. Reconnecting in 1 second...', event.reason);
        setTimeout(() => {
            // Note: In a production app, you might want to implement exponential backoff
            createWebSocketConnection();
        }, CONFIG.RECONNECT_DELAY);
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
    };

    return ws;
}

async function initSciChart() {
    // Initialize WebSocket connection
    let ws = createWebSocketConnection();

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

    const createDataSeries = (wasmContext) => {
        return new SciChart.XyDataSeries(wasmContext, {
            fifoCapacity: CONFIG.FIFO_CAPACITY,
            isSorted: true,
            containsNaN: false
        });
    };

    const addZoomModifiers = (surface) => {
        surface.chartModifiers.add(new SciChart.ZoomExtentsModifier({ isAnimated: false }));
        surface.chartModifiers.add(new SciChart.RubberBandXyZoomModifier());
    };

    const createStandardChart = async (containerId, title) => {
        const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.create(containerId, {
            title,
            titleStyle: { fontSize: "16" }
        });

        const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Always });
        const yAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Always });

        sciChartSurface.xAxes.add(xAxis);
        sciChartSurface.yAxes.add(yAxis);

        return { sciChartSurface, wasmContext };
    };

    // Modular chart definitions
    const CHART_DEFINITIONS = {
        'rpm-speed': {
            title: 'RPM / Speed',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.create(containerId, {
                    title: 'RPM / Speed',
                    titleStyle: { fontSize: "16" }
                });

                addZoomModifiers(sciChartSurface);

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Always });
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

                const rpmSeries = createDataSeries(wasmContext);
                const speedSeries = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: rpmSeries,
                        yAxisId: "ID_Y_AXIS_RPM",
                        strokeThickness: 3,
                        stroke: "#50C7E0"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: speedSeries,
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
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.create(containerId, {
                    title: 'Throttle / Brake',
                    titleStyle: { fontSize: "16" }
                });

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Always });
                const yAxis = createAxisWithOptions(wasmContext, { visibleRange: new SciChart.NumberRange(0, 110) });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxis);

                const throttleInputSeries = createDataSeries(wasmContext);
                const throttleOutputSeries = createDataSeries(wasmContext);
                const brakeInputSeries = createDataSeries(wasmContext);
                const brakeOutputSeries = createDataSeries(wasmContext);

                const seriesConfigs = [
                    { dataSeries: throttleInputSeries, strokeThickness: 3, stroke: "#00F000" },
                    { dataSeries: throttleOutputSeries, strokeThickness: 2, stroke: "#6EADFF" },
                    { dataSeries: brakeInputSeries, strokeThickness: 3, stroke: "#F00000" },
                    { dataSeries: brakeOutputSeries, strokeThickness: 2, stroke: "#FF8A7D" }
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
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId, 'Tyre Temperature');

                const tyreColors = {
                    FL: "#7072fdff", // Front Left - Light Blue
                    FR: "#fa6a6aff", // Front Right - Light Red
                    RL: "#0043fcff", // Rear Left - Dark Blue
                    RR: "#ff0000ff"  // Rear Right - Red
                };

                const tyreSeries = {};
                Object.entries(tyreColors).forEach(([position, color]) => {
                    const series = createDataSeries(wasmContext);
                    tyreSeries[position] = series;

                    sciChartSurface.renderableSeries.add(
                        new SciChart.FastLineRenderableSeries(wasmContext, {
                            dataSeries: series,
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
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.create(containerId, {
                    title: 'Fuel Range/Rate',
                    titleStyle: { fontSize: "16" }
                });

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Always });
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

                const fuelRateSeries = createDataSeries(wasmContext);
                const fuelRangeSeries = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: fuelRateSeries,
                        yAxisId: "ID_Y_AXIS_RATE",
                        strokeThickness: 2,
                        stroke: "#f9b73dff"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: fuelRangeSeries,
                        yAxisId: "ID_Y_AXIS_RANGE",
                        strokeThickness: 2,
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

        'translational-jerk': {
            title: '6DOF Translational Jerk',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId, '6DOF Translational Jerk');

                const calcSeries = createDataSeries(wasmContext);
                const actualSeries = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: calcSeries,
                        strokeThickness: 1,
                        stroke: "#50C7E0"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: actualSeries,
                        strokeThickness: 2,
                        stroke: "#C750E0"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        translationalJerkCalc: calcSeries,
                        translationalJerk: actualSeries
                    },
                    dataFields: ['SixDOFTranslationalJerkCalc', 'SixDOFTranslationalJerk']
                };
            }
        },

        'rotational-jerk': {
            title: '6DOF Rotational Jerk',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId, '6DOF Rotational Jerk');

                const series = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: series,
                        strokeThickness: 2,
                        stroke: "#C750E0"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        rotationalJerk: series
                    },
                    dataFields: ['SixDOFRotationalJerk']
                };
            }
        },

        'translational-snap': {
            title: '6DOF Translational Snap',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId, '6DOF Translational Snap');

                const calcSeries = createDataSeries(wasmContext);
                const actualSeries = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: calcSeries,
                        strokeThickness: 1,
                        stroke: "#50C7E0"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: actualSeries,
                        strokeThickness: 2,
                        stroke: "#C750E0"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        translationalSnapCalc: calcSeries,
                        translationalSnap: actualSeries
                    },
                    dataFields: ['SixDOFTranslationalSnapCalc', 'SixDOFTranslationalSnap']
                };
            }
        },

        'rotational-snap': {
            title: '6DOF Rotational Snap',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId, '6DOF Rotational Snap');

                const series = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: series,
                        strokeThickness: 2,
                        stroke: "#C750E0"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis: sciChartSurface.xAxes.get(0),
                    dataSeries: {
                        rotationalSnap: series
                    },
                    dataFields: ['SixDOFRotationalSnap']
                };
            }
        },

        'synthesizer-output': {
            title: 'Synthesizer Outputs',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await SciChart.SciChartSurface.create(containerId, {
                    title: 'Synthesizer Outputs',
                    titleStyle: { fontSize: "16" }
                });

                const xAxis = createAxisWithOptions(wasmContext, { autoRange: SciChart.EAutoRange.Always });
                const yAxisAmplitude = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_AMPLITUDE",
                    axisAlignment: SciChart.EAxisAlignment.Left,
                    visibleRange: new SciChart.NumberRange(0, 1),
                    labelPrecision: 3,
                    labelStyle: { color: "#50C7E0" }
                });
                const yAxisFrequency = createAxisWithOptions(wasmContext, {
                    id: "ID_Y_AXIS_FREQUENCY",
                    axisAlignment: SciChart.EAxisAlignment.Right,
                    visibleRange: new SciChart.NumberRange(0, 60),
                    labelPrecision: 0,
                    labelStyle: { color: "#C750E0" }
                });

                sciChartSurface.xAxes.add(xAxis);
                sciChartSurface.yAxes.add(yAxisAmplitude, yAxisFrequency);

                const amplitudeSeries = createDataSeries(wasmContext);
                const frequencySeries = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: amplitudeSeries,
                        yAxisId: "ID_Y_AXIS_AMPLITUDE",
                        strokeThickness: 3,
                        stroke: "#50C7E0"
                    }),
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: frequencySeries,
                        yAxisId: "ID_Y_AXIS_FREQUENCY",
                        strokeThickness: 3,
                        stroke: "#C750E0"
                    })
                );

                return {
                    surface: sciChartSurface,
                    xAxis,
                    dataSeries: {
                        synthAmplitude: amplitudeSeries,
                        synthFrequency: frequencySeries
                    },
                    dataFields: ['synthOutputAmplitude', 'synthOutputFrequency']
                };
            }
        },

        'compute-time': {
            title: 'Compute Time (µs)',
            create: async (containerId) => {
                const { sciChartSurface, wasmContext } = await createStandardChart(containerId, 'Compute Time (µs)');

                const series = createDataSeries(wasmContext);

                sciChartSurface.renderableSeries.add(
                    new SciChart.FastLineRenderableSeries(wasmContext, {
                        dataSeries: series,
                        strokeThickness: 3,
                        stroke: "#50C7E0"
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

                // Merge data series from this chart into the global collection
                Object.assign(allDataSeries, chartInstance.dataSeries);

                console.log(`Successfully created chart: ${chartConfig.id}`);
            } catch (error) {
                console.error(`Failed to create chart ${chartConfig.id}:`, error);
            }
        }

        return { charts, allDataSeries };
    };

    // Create all charts and get references
    const { charts, allDataSeries } = await createCharts();

    // WebSocket message handling
    let lastTimeOfDay = 0;
    let lastSeq = 0;

    const handleWebSocketMessage = (event) => {
        const data = JSON.parse(event.data);

        // Clear all data series if sequence number has reset (indicates new session)
        if (data.seq < lastSeq) {
            Object.values(allDataSeries).forEach(series => series.clear());
        }

        const currentTime = data.seq;

        // Create mapping between incoming data and data series
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
                        'SixDOFRotationalJerk': 'rotationalJerk',
                        'SixDOFRotationalSnap': 'rotationalSnap',
                        'synthOutputAmplitude': 'synthAmplitude',
                        'synthOutputFrequency': 'synthFrequency'
                    };
                    return fieldMappings[fieldName] === key || fieldName === key;
                });

                if (seriesKey && data[fieldName] !== undefined) {
                    dataFieldMappings[seriesKey] = data[fieldName];
                }
            });
        });

        // Append data to all active series
        Object.entries(dataFieldMappings).forEach(([seriesKey, value]) => {
            if (allDataSeries[seriesKey] && value !== undefined) {
                allDataSeries[seriesKey].appendRange([currentTime], [value]);
            }
        });

        // Update visible range for smooth scrolling on the first chart (if it exists and not zooming)
        const firstChart = Object.values(charts)[0];
        if (firstChart && firstChart.surface.zoomState !== SciChart.EZoomState.UserZooming) {
            firstChart.xAxis.visibleRange = new SciChart.NumberRange(
                currentTime - CONFIG.FIFO_CAPACITY,
                currentTime
            );
        }

        lastTimeOfDay = data.timeOfDay;
        lastSeq = data.seq;
    };

    ws.addEventListener('message', handleWebSocketMessage);
}

// Initialize the application
initSciChart().catch(error => {
    console.error('Failed to initialize SciChart:', error);
});
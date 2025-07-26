function connect() {
    return new WebSocket('ws://' + location.host + '/ws');
}

async function initSciChart() {
    // const ws = new WebSocket('ws://' + location.host + '/ws');
    var ws = connect()

    ws.onclose = function (e) {
        console.log('Socket is closed. Reconnect will be attempted in 1 second.', e.reason);
        setTimeout(function () {
            ws = connect();
        }, 1000);
    };

    const fifoCapacity = 200;

    SciChart.SciChartSurface.UseCommunityLicense()

    // In order to load data file from the CDN we need to set dataUrl
    SciChart.SciChartSurface.configure({
        dataUrl: `https://cdn.jsdelivr.net/npm/scichart@${SciChart.libraryVersion}/_wasm/scichart2d.data`,
        wasmUrl: `https://cdn.jsdelivr.net/npm/scichart@${SciChart.libraryVersion}/_wasm/scichart2d.wasm`
    });


    const createCharts = async () => {
        // RPM and speed
        const {
            sciChartSurface: sciChartSurfaceRPMSpeed,
            wasmContext: wasmContextRPMSpeed
        } = await SciChart.SciChartSurface.create("scichart-root-1", { title: "RPM / Speed", titleStyle: { fontSize: "16" } });

        sciChartSurfaceRPMSpeed.chartModifiers.add(new SciChart.ZoomExtentsModifier({ isAnimated: false }));
        sciChartSurfaceRPMSpeed.chartModifiers.add(new SciChart.RubberBandXyZoomModifier());

        // Add RPM X and Y axis
        const xAxisRPMSpeed = new SciChart.NumericAxis(wasmContextRPMSpeed, { autoRange: SciChart.EAutoRange.Always });
        const yAxisRPM = new SciChart.NumericAxis(
            wasmContextRPMSpeed,
            {
                id: "ID_Y_AXIS_1",
                axisAlignment: SciChart.EAxisAlignment.Left,
                visibleRange: new SciChart.NumberRange(0, 10000),
                labelPrecision: 0,
                labelStyle: {
                    color: "#50C7E0"
                }
            }
        );
        const yAxisSpeed = new SciChart.NumericAxis(
            wasmContextRPMSpeed,
            {
                id: "ID_Y_AXIS_2",
                axisAlignment: SciChart.EAxisAlignment.Right,
                visibleRange: new SciChart.NumberRange(0, 350),
                labelPrecision: 0,
                labelStyle: {
                    color: "#C750E0"
                }
            }
        );
        sciChartSurfaceRPMSpeed.xAxes.add(xAxisRPMSpeed);
        sciChartSurfaceRPMSpeed.yAxes.add(yAxisRPM);
        sciChartSurfaceRPMSpeed.yAxes.add(yAxisSpeed);

        // Create DataSeries for RPM
        const xyDataSeriesRPM = new SciChart.XyDataSeries(wasmContextRPMSpeed, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeriesSpeed = new SciChart.XyDataSeries(wasmContextRPMSpeed, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries for RPM and assign the dataSeries
        sciChartSurfaceRPMSpeed.renderableSeries.add(
            new SciChart.FastLineRenderableSeries(wasmContextRPMSpeed, {
                dataSeries: xyDataSeriesRPM,
                yAxisId: "ID_Y_AXIS_1",
                strokeThickness: 3,
                stroke: "#50C7E0"
            })
        );
        sciChartSurfaceRPMSpeed.renderableSeries.add(
            new SciChart.FastLineRenderableSeries(wasmContextRPMSpeed, {
                dataSeries: xyDataSeriesSpeed,
                yAxisId: "ID_Y_AXIS_2",
                strokeThickness: 3,
                stroke: "#C750E0"
            })
        );



        // Throttle/brake
        const {
            sciChartSurface: sciChartSurfaceThrottleBrake,
            wasmContext: wasmContextThrottleBrake
        } = await SciChart.SciChartSurface.create("scichart-root-2", { title: "Throttle / Brake", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisThrottleBrake = new SciChart.NumericAxis(wasmContextThrottleBrake, { autoRange: SciChart.EAutoRange.Always });
        const yAxisThrottleBrake = new SciChart.NumericAxis(wasmContextThrottleBrake, { visibleRange: new SciChart.NumberRange(0, 110) });
        sciChartSurfaceThrottleBrake.xAxes.add(xAxisThrottleBrake);
        sciChartSurfaceThrottleBrake.yAxes.add(yAxisThrottleBrake);

        // Create a DataSeries
        const xyDataSeriesThrottleInput = new SciChart.XyDataSeries(wasmContextThrottleBrake, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeriesThrottleOutput = new SciChart.XyDataSeries(wasmContextThrottleBrake, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeriesBrakeInput = new SciChart.XyDataSeries(wasmContextThrottleBrake, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeriesBrakeOutput = new SciChart.XyDataSeries(wasmContextThrottleBrake, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceThrottleBrake.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextThrottleBrake, {
            dataSeries: xyDataSeriesThrottleInput,
            strokeThickness: 3,
            stroke: "#00F000"
        }));
        sciChartSurfaceThrottleBrake.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextThrottleBrake, {
            dataSeries: xyDataSeriesThrottleOutput,
            strokeThickness: 2,
            stroke: "#6EADFF"
        }));
        sciChartSurfaceThrottleBrake.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextThrottleBrake, {
            dataSeries: xyDataSeriesBrakeInput,
            strokeThickness: 3,
            stroke: "#F00000"
        }));
        sciChartSurfaceThrottleBrake.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextThrottleBrake, {
            dataSeries: xyDataSeriesBrakeOutput,
            strokeThickness: 2,
            stroke: "#FF8A7D",
        }));



        // Transmission gear
        const {
            sciChartSurface: sciChartSurfaceGear,
            wasmContext: wasmContextGear
        } = await SciChart.SciChartSurface.create("scichart-root-3", { title: "Transmission Gear", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisGear = new SciChart.NumericAxis(wasmContextGear, { autoRange: SciChart.EAutoRange.Always });
        const yAxisGear = new SciChart.NumericAxis(wasmContextGear, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceGear.xAxes.add(xAxisGear);
        sciChartSurfaceGear.yAxes.add(yAxisGear);

        // Create a DataSeries
        const xyDataSeriesGear = new SciChart.XyDataSeries(wasmContextGear, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceGear.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextGear, {
            dataSeries: xyDataSeriesGear,
            strokeThickness: 3,
            stroke: "#50C7E0"
        }));



        // 6DOF Translational envelope surge gforce
        const {
            sciChartSurface: sciChartSurfaceGforce,
            wasmContext: wasmContextGforce
        } = await SciChart.SciChartSurface.create("scichart-root-4", { title: "Surge G-Force", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisGforce = new SciChart.NumericAxis(wasmContextGforce, { autoRange: SciChart.EAutoRange.Always });
        const yAxisGforce = new SciChart.NumericAxis(wasmContextGforce, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceGforce.xAxes.add(xAxisGforce);
        sciChartSurfaceGforce.yAxes.add(yAxisGforce);

        // Create a DataSeries
        const xyDataSeriesSurgeGforce = new SciChart.XyDataSeries(wasmContextGforce, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeriesSurgeGforceCalc = new SciChart.XyDataSeries(wasmContextGforce, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceGforce.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextGforce, {
            dataSeries: xyDataSeriesSurgeGforceCalc,
            strokeThickness: 2,
            stroke: "#50C7E0"
        }));
        sciChartSurfaceGforce.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextGforce, {
            dataSeries: xyDataSeriesSurgeGforce,
            strokeThickness: 2,
            stroke: "#C750E0"
        }));



        // 6DOF translational envelope jerk
        const {
            sciChartSurface: sciChartSurfaceTranslationalJerk,
            wasmContext: wasmContextTranslationalJerk
        } = await SciChart.SciChartSurface.create("scichart-root-5", { title: "6DOF Translational Jerk", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisTranslationalJerk = new SciChart.NumericAxis(wasmContextTranslationalJerk, { autoRange: SciChart.EAutoRange.Always });
        const yAxisTranslationalJerk = new SciChart.NumericAxis(wasmContextTranslationalJerk, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceTranslationalJerk.xAxes.add(xAxisTranslationalJerk);
        sciChartSurfaceTranslationalJerk.yAxes.add(yAxisTranslationalJerk);

        // Create a DataSeries
        const xyDataSeries6DOFTranslationalJerkCalc = new SciChart.XyDataSeries(wasmContextTranslationalJerk, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeries6DOFTranslationalJerk = new SciChart.XyDataSeries(wasmContextTranslationalJerk, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceTranslationalJerk.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextTranslationalJerk, {
            dataSeries: xyDataSeries6DOFTranslationalJerkCalc,
            strokeThickness: 1,
            stroke: "#50C7E0"
        }));
        sciChartSurfaceTranslationalJerk.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextTranslationalJerk, {
            dataSeries: xyDataSeries6DOFTranslationalJerk,
            strokeThickness: 2,
            stroke: "#C750E0"
        }));



        // 6DOF rotational envelope jerk
        const {
            sciChartSurface: sciChartSurfaceRotationalJerk,
            wasmContext: wasmContextRotationalJerk
        } = await SciChart.SciChartSurface.create("scichart-root-6", { title: "6DOF Rotational Jerk", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisRotationalJerk = new SciChart.NumericAxis(wasmContextRotationalJerk, { autoRange: SciChart.EAutoRange.Always });
        const yAxisRotationalJerk = new SciChart.NumericAxis(wasmContextRotationalJerk, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceRotationalJerk.xAxes.add(xAxisRotationalJerk);
        sciChartSurfaceRotationalJerk.yAxes.add(yAxisRotationalJerk);

        // Create a DataSeries
        const xyDataSeries6DOFRotationalJerk = new SciChart.XyDataSeries(wasmContextRotationalJerk, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceRotationalJerk.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextRotationalJerk, {
            dataSeries: xyDataSeries6DOFRotationalJerk,
            strokeThickness: 2,
            stroke: "#C750E0"
        }));



        // 6DOF translational envelope snap
        const {
            sciChartSurface: sciChartSurfaceTranslationalSnap,
            wasmContext: wasmContextTranslationalSnap
        } = await SciChart.SciChartSurface.create("scichart-root-7", { title: "6DOF Translational Snap", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisTranslationalSnap = new SciChart.NumericAxis(wasmContextTranslationalSnap, { autoRange: SciChart.EAutoRange.Always });
        const yAxisTranslationalSnap = new SciChart.NumericAxis(wasmContextTranslationalSnap, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceTranslationalSnap.xAxes.add(xAxisTranslationalSnap);
        sciChartSurfaceTranslationalSnap.yAxes.add(yAxisTranslationalSnap);

        // Create a DataSeries
        const xyDataSeries6DOFTranslationalSnapCalc = new SciChart.XyDataSeries(wasmContextTranslationalSnap, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeries6DOFTranslationalSnap = new SciChart.XyDataSeries(wasmContextTranslationalSnap, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceTranslationalSnap.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextTranslationalSnap, {
            dataSeries: xyDataSeries6DOFTranslationalSnapCalc,
            strokeThickness: 1,
            stroke: "#50C7E0"
        }));
        sciChartSurfaceTranslationalSnap.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextTranslationalSnap, {
            dataSeries: xyDataSeries6DOFTranslationalSnap,
            strokeThickness: 2,
            stroke: "#C750E0"
        }));



        // 6DOF rotational envelope snap
        const {
            sciChartSurface: sciChartSurfaceRotationalSnap,
            wasmContext: wasmContextRotationalSnap
        } = await SciChart.SciChartSurface.create("scichart-root-8", { title: "6DOF Rotational Snap", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisRotationalSnap = new SciChart.NumericAxis(wasmContextRotationalSnap, { autoRange: SciChart.EAutoRange.Always });
        const yAxisRotationalSnap = new SciChart.NumericAxis(wasmContextRotationalSnap, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceRotationalSnap.xAxes.add(xAxisRotationalSnap);
        sciChartSurfaceRotationalSnap.yAxes.add(yAxisRotationalSnap);

        // Create a DataSeries
        const xyDataSeries6DOFRotationalSnap = new SciChart.XyDataSeries(wasmContextRotationalSnap, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceRotationalSnap.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextRotationalSnap, {
            dataSeries: xyDataSeries6DOFRotationalSnap,
            strokeThickness: 2,
            stroke: "#C750E0"
        }));



        // Synthesizer output ampliture and frequency
        const {
            sciChartSurface: sciChartSurfaceSynthOutput,
            wasmContext: wasmContextSynthOutput
        } = await SciChart.SciChartSurface.create("scichart-root-9", { title: "Synthesizer Outputs", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisSynthOutput = new SciChart.NumericAxis(wasmContextSynthOutput, { autoRange: SciChart.EAutoRange.Always });
        const yAxisSynthOutputAmplitude = new SciChart.NumericAxis(
            wasmContextSynthOutput,
            {
                id: "ID_Y_AXIS_1",
                axisAlignment: SciChart.EAxisAlignment.Left,
                visibleRange: new SciChart.NumberRange(0, 1),
                labelPrecision: 3,
                labelStyle: {
                    color: "#50C7E0"
                }
            }
        );
        const yAxisSynthOutputFrequency = new SciChart.NumericAxis(
            wasmContextSynthOutput,
            {
                id: "ID_Y_AXIS_2",
                axisAlignment: SciChart.EAxisAlignment.Right,
                visibleRange: new SciChart.NumberRange(0, 60),
                labelPrecision: 0,
                labelStyle: {
                    color: "#C750E0"
                }
            }
        );
        sciChartSurfaceSynthOutput.xAxes.add(xAxisSynthOutput);
        sciChartSurfaceSynthOutput.yAxes.add(yAxisSynthOutputAmplitude);
        sciChartSurfaceSynthOutput.yAxes.add(yAxisSynthOutputFrequency);

        // Create a DataSeries
        const xyDataSeriesSynthOutputAmplitude = new SciChart.XyDataSeries(wasmContextSynthOutput, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeriesSynthOutputFrequency = new SciChart.XyDataSeries(wasmContextSynthOutput, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceSynthOutput.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextSynthOutput, {
            dataSeries: xyDataSeriesSynthOutputAmplitude,
            yAxisId: "ID_Y_AXIS_1",
            strokeThickness: 3,
            stroke: "#50C7E0"
        }));
        sciChartSurfaceSynthOutput.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextSynthOutput, {
            dataSeries: xyDataSeriesSynthOutputFrequency,
            yAxisId: "ID_Y_AXIS_2",
            strokeThickness: 3,
            stroke: "#C750E0"
        }));



        // Compute time microseconds
        const {
            sciChartSurface: sciChartSurfaceComputeTime,
            wasmContext: wasmContextComputeTime
        } = await SciChart.SciChartSurface.create("scichart-root-10", { title: "Compute Time (µs)", titleStyle: { fontSize: "16" } });

        // Add an X and a Y Axis
        const xAxisComputeTime = new SciChart.NumericAxis(wasmContextComputeTime, { autoRange: SciChart.EAutoRange.Always });
        const yAxisComputeTime = new SciChart.NumericAxis(wasmContextComputeTime, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceComputeTime.xAxes.add(xAxisComputeTime);
        sciChartSurfaceComputeTime.yAxes.add(yAxisComputeTime);

        // Create a DataSeries
        const xyDataSeriesComputeTime = new SciChart.XyDataSeries(wasmContextComputeTime, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceComputeTime.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextComputeTime, {
            dataSeries: xyDataSeriesComputeTime,
            strokeThickness: 3,
            stroke: "#50C7E0"
        }));



        lastTimeOfDay = 0;
        lastSeq = 0;
        ws.addEventListener('message', (event) => {
            const data = JSON.parse(event.data);

            // if (data.timeOfDay < lastTimeOfDay) {
            if (data.seq < lastSeq) {
                xyDataSeriesRPM.clear();
                xyDataSeriesSpeed.clear();
                xyDataSeriesThrottleInput.clear();
                xyDataSeriesThrottleOutput.clear();
                xyDataSeriesBrakeInput.clear();
                xyDataSeriesBrakeOutput.clear();
                xyDataSeriesGear.clear();
                xyDataSeriesComputeTime.clear();
                xyDataSeriesSurgeGforce.clear();
                xyDataSeriesSurgeGforceCalc.clear();
                xyDataSeries6DOFTranslationalJerkCalc.clear();
                xyDataSeries6DOFTranslationalJerk.clear();
                xyDataSeries6DOFTranslationalSnapCalc.clear();
                xyDataSeries6DOFTranslationalSnap.clear();
                xyDataSeries6DOFRotationalJerk.Clear();
                xyDataSeries6DOFRotationalSnap.Clear();
                xyDataSeriesSynthOutputAmplitude.Clear();
                xyDataSeriesSynthOutputFrequency.Clear();
            }

            // i = data.timeOfDay;
            i = data.seq;
            xyDataSeriesRPM.appendRange([i], [data.rpm]);
            xyDataSeriesSpeed.appendRange([i], [data.speed]);
            xyDataSeriesThrottleInput.appendRange([i], [data.throttleInput]);
            xyDataSeriesThrottleOutput.appendRange([i], [data.throttleOutput]);
            xyDataSeriesBrakeInput.appendRange([i], [data.brakeInput]);
            xyDataSeriesBrakeOutput.appendRange([i], [data.brakeOutput]);
            xyDataSeriesGear.appendRange([i], [data.gear]);
            xyDataSeriesComputeTime.appendRange([i], [data.computeTime]);
            xyDataSeriesSurgeGforce.appendRange([i], [data.surgeGforce]);
            xyDataSeriesSurgeGforceCalc.appendRange([i], [data.surgeGforceCalc]);
            xyDataSeries6DOFTranslationalJerk.appendRange([i], [data.SixDOFTranslationalJerk]);
            xyDataSeries6DOFTranslationalSnap.appendRange([i], [data.SixDOFTranslationalSnap]);
            xyDataSeries6DOFTranslationalJerkCalc.appendRange([i], [data.SixDOFTranslationalJerkCalc]);
            xyDataSeries6DOFTranslationalSnapCalc.appendRange([i], [data.SixDOFTranslationalSnapCalc]);
            xyDataSeries6DOFRotationalJerk.appendRange([i], [data.SixDOFRotationalJerk]);
            xyDataSeries6DOFRotationalSnap.appendRange([i], [data.SixDOFRotationalSnap]);
            xyDataSeriesSynthOutputAmplitude.appendRange([i], [data.synthOutputAmplitude]);
            xyDataSeriesSynthOutputFrequency.appendRange([i], [data.synthOutputFrequency]);


            if (sciChartSurfaceRPMSpeed.zoomState !== SciChart.EZoomState.UserZooming) {
                xAxisRPMSpeed.visibleRange = new SciChart.NumberRange(i - fifoCapacity, i);
            }

            lastTimeOfDay = data.timeOfDay;
            lastSeq = data.seq;

        });
    }


    createCharts();
}

initSciChart();
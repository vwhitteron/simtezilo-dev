async function initSciChart() {
    const socket = new WebSocket('ws://' + location.host + '/ws');
    const fifoCapacity = 200;

    SciChart.SciChartSurface.UseCommunityLicense()

    // In order to load data file from the CDN we need to set dataUrl
    SciChart.SciChartSurface.configure({
        dataUrl: `https://cdn.jsdelivr.net/npm/scichart@${SciChart.libraryVersion}/_wasm/scichart2d.data`,
        wasmUrl: `https://cdn.jsdelivr.net/npm/scichart@${SciChart.libraryVersion}/_wasm/scichart2d.wasm`
    });


    const createCharts = async () => {
        // RPM
        const {
            sciChartSurface: sciChartSurfaceRPM,
            wasmContext: wasmContextRPM
        } = await SciChart.SciChartSurface.create("scichart-root-1", { title: "RPM", titleStyle: { fontSize: "16" } });

        sciChartSurfaceRPM.chartModifiers.add(new SciChart.ZoomExtentsModifier({ isAnimated: false }));
        sciChartSurfaceRPM.chartModifiers.add(new SciChart.RubberBandXyZoomModifier());

        // Add RPM X and Y axis
        const xAxisRPM = new SciChart.NumericAxis(wasmContextRPM, { autoRange: SciChart.EAutoRange.Always });
        const yAxisRPM = new SciChart.NumericAxis(wasmContextRPM, { visibleRange: new SciChart.NumberRange(0, 10000) });
        sciChartSurfaceRPM.xAxes.add(xAxisRPM);
        sciChartSurfaceRPM.yAxes.add(yAxisRPM);

        // Create DataSeries for RPM
        const xyDataSeriesRPM = new SciChart.XyDataSeries(wasmContextRPM, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries for RPM and assign the dataSeries
        sciChartSurfaceRPM.renderableSeries.add(
            new SciChart.FastLineRenderableSeries(wasmContextRPM, {
                dataSeries: xyDataSeriesRPM,
                strokeThickness: 3,
                stroke: "#50C7E0"
            }
            ));


        // Speed
        const {
            sciChartSurface: sciChartSurfaceSpeed,
            wasmContext: wasmContextSpeed
        } = await SciChart.SciChartSurface.create("scichart-root-2", { title: "Speed", titleStyle: { fontSize: "16" } });

        //   Add Speed X and Y axis
        const xAxisSpeed = new SciChart.NumericAxis(wasmContextSpeed, { autoRange: SciChart.EAutoRange.Always });
        const yAxisSpeed = new SciChart.NumericAxis(wasmContextSpeed, { visibleRange: new SciChart.NumberRange(0, 350) });
        sciChartSurfaceSpeed.xAxes.add(xAxisSpeed);
        sciChartSurfaceSpeed.yAxes.add(yAxisSpeed);

        // Create DataSeries for Speed
        const xyDataSeriesSpeed = new SciChart.XyDataSeries(wasmContextSpeed, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries for Speed and assign the dataSeries
        sciChartSurfaceSpeed.renderableSeries.add(
            new SciChart.FastLineRenderableSeries(wasmContextSpeed, {
                dataSeries: xyDataSeriesSpeed,
                strokeThickness: 3,
                stroke: "#50C7E0"
            }
            ));


        // Throttle/brake
        const {
            sciChartSurface: sciChartSurfaceThrottleBrake,
            wasmContext: wasmContextThrottleBrake
        } = await SciChart.SciChartSurface.create("scichart-root-3", { title: "Throttle / Brake", titleStyle: { fontSize: "16" } });


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
        } = await SciChart.SciChartSurface.create("scichart-root-4", { title: "Transmission Gear", titleStyle: { fontSize: "16" } });
        6

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


        // Copmute time microseconds
        const {
            sciChartSurface: sciChartSurfaceComputeTime,
            wasmContext: wasmContextComputeTime
        } = await SciChart.SciChartSurface.create("scichart-root-5", { title: "Compute Time (µs)", titleStyle: { fontSize: "16" } });
        6

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


        // Longitudinal G-Force
        const {
            sciChartSurface: sciChartSurfaceGforce,
            wasmContext: wasmContextGforce
        } = await SciChart.SciChartSurface.create("scichart-root-6", { title: "Surge G-Force", titleStyle: { fontSize: "16" } });
        6

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
        const xyDataSeriesSurgeCalcGforce = new SciChart.XyDataSeries(wasmContextGforce, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceGforce.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextGforce, {
            dataSeries: xyDataSeriesSurgeGforce,
            strokeThickness: 2,
            stroke: "#50C7E0"
        }));
        sciChartSurfaceGforce.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextGforce, {
            dataSeries: xyDataSeriesSurgeCalcGforce,
            strokeThickness: 2,
            stroke: "#C750E0"
        }));


        // Jerk
        const {
            sciChartSurface: sciChartSurfaceJerk,
            wasmContext: wasmContextJerk
        } = await SciChart.SciChartSurface.create("scichart-root-7", { title: "6DOF Translatioanl Jerk", titleStyle: { fontSize: "16" } });


        // Add an X and a Y Axis
        const xAxisJerk = new SciChart.NumericAxis(wasmContextJerk, { autoRange: SciChart.EAutoRange.Always });
        const yAxisJerk = new SciChart.NumericAxis(wasmContextJerk, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceJerk.xAxes.add(xAxisJerk);
        sciChartSurfaceJerk.yAxes.add(yAxisJerk);

        // Create a DataSeries
        const xyDataSeries6DOFTranslationalCalcJerk = new SciChart.XyDataSeries(wasmContextJerk, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeries6DOFTranslationalJerk = new SciChart.XyDataSeries(wasmContextJerk, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });


        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceJerk.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextJerk, {
            dataSeries: xyDataSeries6DOFTranslationalCalcJerk,
            strokeThickness: 1,
            stroke: "#50C7E0"
        }));
        sciChartSurfaceJerk.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextJerk, {
            dataSeries: xyDataSeries6DOFTranslationalJerk,
            strokeThickness: 1,
            stroke: "#C750E0"
        }));



        // Snap
        const {
            sciChartSurface: sciChartSurfaceSnap,
            wasmContext: wasmContextSnap
        } = await SciChart.SciChartSurface.create("scichart-root-8", { title: "6DOF Translational Snap", titleStyle: { fontSize: "16" } });


        // Add an X and a Y Axis
        const xAxisSnap = new SciChart.NumericAxis(wasmContextSnap, { autoRange: SciChart.EAutoRange.Always });
        const yAxisSnap = new SciChart.NumericAxis(wasmContextSnap, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceSnap.xAxes.add(xAxisSnap);
        sciChartSurfaceSnap.yAxes.add(yAxisSnap);

        // Create a DataSeries
        const xyDataSeries6DOFTranslationalCalcSnap = new SciChart.XyDataSeries(wasmContextSnap, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeries6DOFTranslationalSnap = new SciChart.XyDataSeries(wasmContextSnap, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceSnap.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextSnap, {
            dataSeries: xyDataSeries6DOFTranslationalCalcSnap,
            strokeThickness: 1,
            stroke: "#50C7E0"
        }));
        sciChartSurfaceSnap.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextSnap, {
            dataSeries: xyDataSeries6DOFTranslationalSnap,
            strokeThickness: 1,
            stroke: "#C750E0"
        }));



        lastTimeOfDay = 0;
        lastSeq = 0;
        socket.addEventListener('message', (event) => {
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
                xyDataSeriesSurgeCalcGforce.clear();
                xyDataSeries6DOFTranslationalCalcJerk.clear();
                xyDataSeries6DOFTranslationalJerk.clear();
                xyDataSeries6DOFTranslationalCalcSnap.clear();
                xyDataSeries6DOFTranslationalSnap.clear();
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
            xyDataSeriesSurgeCalcGforce.appendRange([i], [data.surgeCalcGforce]);
            xyDataSeries6DOFTranslationalJerk.appendRange([i], [data.SixDOFTranslationalJerk]);
            xyDataSeries6DOFTranslationalSnap.appendRange([i], [data.SixDOFTranslationalSnap]);
            xyDataSeries6DOFTranslationalCalcJerk.appendRange([i], [data.SixDOFTranslationalCalcJerk]);
            xyDataSeries6DOFTranslationalCalcSnap.appendRange([i], [data.SixDOFTranslationalCalcSnap]);

            if (sciChartSurfaceRPM.zoomState !== SciChart.EZoomState.UserZooming) {
                xAxisRPM.visibleRange = new SciChart.NumberRange(i - fifoCapacity, i);
            }

            lastTimeOfDay = data.timeOfDay;
            lastSeq = data.seq;

        });
    }


    createCharts();
}

initSciChart();
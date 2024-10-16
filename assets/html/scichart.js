async function initSciChart() {
    const socket = new WebSocket('ws://' + location.host + '/ws');
    const fifoCapacity = 400;

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
        } = await SciChart.SciChartSurface.create("scichart-root-1");

        sciChartSurfaceRPM.chartModifiers.add(new SciChart.ZoomExtentsModifier({isAnimated: false}));
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
        } = await SciChart.SciChartSurface.create("scichart-root-2");

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
        } = await SciChart.SciChartSurface.create("scichart-root-3");


        // Add an X and a Y Axis
        const xAxisThrottleBrake = new SciChart.NumericAxis(wasmContextThrottleBrake, { autoRange: SciChart.EAutoRange.Always });
        const yAxisThrottleBrake = new SciChart.NumericAxis(wasmContextThrottleBrake, { visibleRange: new SciChart.NumberRange(0, 110) });
        sciChartSurfaceThrottleBrake.xAxes.add(xAxisThrottleBrake);
        sciChartSurfaceThrottleBrake.yAxes.add(yAxisThrottleBrake);

        // Create a DataSeries
        const xyDataSeriesThrottle = new SciChart.XyDataSeries(wasmContextThrottleBrake, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });
        const xyDataSeriesBrake = new SciChart.XyDataSeries(wasmContextThrottleBrake, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceThrottleBrake.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextThrottleBrake, {
            dataSeries: xyDataSeriesThrottle,
            strokeThickness: 3,
            stroke: "#00F000"
        }));
        sciChartSurfaceThrottleBrake.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextThrottleBrake, {
            dataSeries: xyDataSeriesBrake,
            strokeThickness: 3,
            stroke: "#F00000"
        }));


        // Output audio
        const {
            sciChartSurface: sciChartSurfaceGforce,
            wasmContext: wasmContextGforce
        } = await SciChart.SciChartSurface.create("scichart-root-4");


        // Add an X and a Y Axis
        const xAxisGforce = new SciChart.NumericAxis(wasmContextGforce, { autoRange: SciChart.EAutoRange.Always });
        const yAxisGforce = new SciChart.NumericAxis(wasmContextGforce, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceGforce.xAxes.add(xAxisGforce);
        sciChartSurfaceGforce.yAxes.add(yAxisGforce);

        // Create a DataSeries
        const xyDataSeriesGforce = new SciChart.XyDataSeries(wasmContextGforce, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceGforce.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextGforce, {
            dataSeries: xyDataSeriesGforce,
            strokeThickness: 3,
            stroke: "#50C7E0"
        }));


        // Jerk
        const {
            sciChartSurface: sciChartSurfaceJerk,
            wasmContext: wasmContextJerk
        } = await SciChart.SciChartSurface.create("scichart-root-5");


        // Add an X and a Y Axis
        const xAxisJerk = new SciChart.NumericAxis(wasmContextJerk, { autoRange: SciChart.EAutoRange.Always });
        const yAxisJerk = new SciChart.NumericAxis(wasmContextJerk, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceJerk.xAxes.add(xAxisJerk);
        sciChartSurfaceJerk.yAxes.add(yAxisJerk);

        // Create a DataSeries
        const xyDataSeriesJerk = new SciChart.XyDataSeries(wasmContextJerk, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceJerk.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextJerk, {
            dataSeries: xyDataSeriesJerk,
            strokeThickness: 3,
            stroke: "#50C7E0"
        }));
        
        
        
        // Snap
        const {
            sciChartSurface: sciChartSurfaceSnap,
            wasmContext: wasmContextSnap
        } = await SciChart.SciChartSurface.create("scichart-root-6");


        // Add an X and a Y Axis
        const xAxisSnap = new SciChart.NumericAxis(wasmContextSnap, { autoRange: SciChart.EAutoRange.Always });
        const yAxisSnap = new SciChart.NumericAxis(wasmContextSnap, { autoRange: SciChart.EAutoRange.Always });
        sciChartSurfaceSnap.xAxes.add(xAxisSnap);
        sciChartSurfaceSnap.yAxes.add(yAxisSnap);

        // Create a DataSeries
        const xyDataSeriesSnap = new SciChart.XyDataSeries(wasmContextSnap, {
            fifoCapacity: fifoCapacity,
            isSorted: true,
            containsNaN: false
        });

        // Create a renderableSeries and assign the dataSeries
        sciChartSurfaceSnap.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContextSnap, {
            dataSeries: xyDataSeriesSnap,
            strokeThickness: 3,
            stroke: "#50C7E0"
        }));



        lastTimeOfDay = 0;
        // i = 1;
        socket.addEventListener('message', (event) => {
            const data = JSON.parse(event.data);

            if (data.timeOfDay < lastTimeOfDay) {
                xyDataSeriesRPM.clear();
                xyDataSeriesSpeed.clear();
                xyDataSeriesJerk.clear();
                xyDataSeriesThrottle.clear();
                xyDataSeriesBrake.clear();
                xyDataSeriesGforce.clear();
                xyDataSeriesSnap.clear();
            }

            i = data.timeOfDay;
            xyDataSeriesRPM.appendRange([i], [data.rpm]);
            xyDataSeriesSpeed.appendRange([i], [data.speed]);
            xyDataSeriesThrottle.appendRange([i], [data.throttle]);
            xyDataSeriesBrake.appendRange([i], [data.brake]);
            xyDataSeriesGforce.appendRange([i], [data.output]);
            xyDataSeriesJerk.appendRange([i], [data.jerk]);
            xyDataSeriesSnap.appendRange([i], [data.snap]);

            if (sciChartSurfaceRPM.zoomState !== SciChart.EZoomState.UserZooming) {
                xAxisRPM.visibleRange = new SciChart.NumberRange(i - fifoCapacity, i);
            }

            lastTimeOfDay = data.timeOfDay;

            // i++;
        });
    }


    createCharts();
}

initSciChart();
import * as SciChart from 'scichart';

const fifoCapacity = 300;

async function initSciChart2() {
    const socket = new WebSocket('ws://localhost:8080/ws');

    SciChart.SciChartSurface.UseCommunityLicense()

    // In order to load data file from the CDN we need to set dataUrl
    SciChart.SciChartSurface.configure({
        dataUrl: `https://cdn.jsdelivr.net/npm/scichart@${SciChart.libraryVersion}/_wasm/scichart2d.data`,
        wasmUrl: `https://cdn.jsdelivr.net/npm/scichart@${SciChart.libraryVersion}/_wasm/scichart2d.wasm`
    });

}

async function createThrottleBrakeChart(socket: WebSocket) {
    // Create a SciChartSurface inside the div with id 'scichart-root'
    const { sciChartSurface: sciChartSurfaceThrottle, wasmContext } = await SciChart.SciChartSurface.create("scichart-root-4");


    // Add an X and a Y Axis
    const xAxis = new SciChart.NumericAxis(wasmContext, { autoRange: SciChart.EAutoRange.Always });
    const yAxis = new SciChart.NumericAxis(wasmContext, { autoRange: SciChart.EAutoRange.Always });
    sciChartSurfaceThrottle.xAxes.add(xAxis);
    sciChartSurfaceThrottle.yAxes.add(yAxis);

    // Create a DataSeries
    const xyDataSeriesThrottle = new SciChart.XyDataSeries(wasmContext, {
        fifoCapacity: fifoCapacity,
        isSorted: true
    });
    const xyDataSeriesBrake = new SciChart.XyDataSeries(wasmContext, {
        fifoCapacity: fifoCapacity,
        isSorted: true
    });

    // Create a renderableSeries and assign the dataSeries
    sciChartSurfaceThrottle.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContext, {
        dataSeries: xyDataSeriesThrottle,
        strokeThickness: 3,
        stroke: "#00F000"
    }));
    sciChartSurfaceThrottle.renderableSeries.add(new SciChart.FastLineRenderableSeries(wasmContext, {
        dataSeries: xyDataSeriesBrake,
        strokeThickness: 3,
        stroke: "#F00000"
    }));

    var lastTimeOfDay = 0;
    socket.addEventListener('message', (event) => {
        const data = JSON.parse(event.data);

        if (data.timeOfDay < lastTimeOfDay) {
            lastTimeOfDay = data.timeOfDay;
            xyDataSeriesBrake.removeRange(0, fifoCapacity - 1);
            // xyDataSeriesThrottle.clear();
            // xyDataSeriesBrake.clear();
        }

        xyDataSeriesThrottle.appendRange([data.timeOfDay], [data.throttle]);
        xyDataSeriesBrake.appendRange([data.timeOfDay], [data.brake]);
    });
}

initSciChart2();
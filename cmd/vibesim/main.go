package main

import (
	"fmt"
	"log"
	"net/http"
)

// StartWebFiringServer starts the web server for the firing order graph.
func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, htmlPage)
	})
	log.Println("Starting server on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

const htmlPage = `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>4-Stroke Engine Firing Order Visualization</title>
	<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
	<style>
		body { font-family: sans-serif; margin: 20px; }
		#piston-chart-container { width: 100%; height: 400px; margin-top: 20px; }
	</style>
</head>
<body>
	<h2>4-Stroke Engine Firing Order</h2>
	<div style="margin-bottom: 1em;">
		<label for="cylSlider">Cylinders Count: </label>
		<input type="range" id="cylSlider" min="1" max="16" step="1" value="6" list="cylList">
		<datalist id="cylList">
			<option value="1">
			<option value="2">
			<option value="3">
			<option value="4">
			<option value="5">
			<option value="6">
			<option value="8">
			<option value="10">
			<option value="12">
			<option value="16">
		</datalist>
		<span id="cylCount">6</span>
	</div>
	<div style="margin-bottom: 1em;">
		<label for="orderSelect">Firing Order: </label>
		<select id="orderSelect"></select>
	</div>
	<div style="margin-bottom: 1em;">
		<label for="angleSlider">Cylinder Bank Angle (°): </label>
		<input type="range" id="angleSlider" min="0" max="180" step="1" value="60" style="width: 400px;">
		<span id="angleValue">60</span>
	</div>
	<div style="margin-bottom: 1em;">
		<label for="crankSlider">Crank Plane Angle (°): </label>
		<input type="range" id="crankSlider" min="0" max="180" step="1" value="120" style="width: 400px;">
		<span id="crankValue">120</span>
	</div>
	<div id="firingInfo" style="margin-bottom: 1em; padding: 10px; background-color: #f0f0f0; border-radius: 5px;">
		<strong>Firing Intervals:</strong> <span id="intervalInfo"></span>
	</div>
	<h3 style="margin-top: 20px;">Piston Position & Firing Events</h3>
	<div id="piston-chart-container">
		<canvas id="pistonChart"></canvas>
	</div>
	<script>
	const allowedCyls = [1,2,3,4,5,6,8,10,12,16];
	const colorList = [
		"#e41a1c", "#377eb8", "#4daf4a", "#ff7f00",
		"#984ea3", "#a65628", "#f781bf", "#999999",
		"#dede00", "#00ced1", "#8b0000", "#4682b4",
		"#228b22", "#ffa500", "#6a3d9a", "#b15928"
	];

	// Typical firing orders by cylinder count
	const firingOrders = {
		1: [[1]],
		2: [[1,2],[2,1]],
		3: [[1,2,3],[1,3,2]],
		4: [[1,3,4,2], [1,2,4,3]],
		5: [[1,2,4,5,3]],
		6: [[1,5,3,6,2,4],[1,2,4,6,5,3]],
		8: [[1,5,4,2,6,3,7,8],[1,8,7,2,6,5,4,3],[1,3,7,2,6,5,4,8]],
		10: [[1,6,5,10,2,7,3,8,4,9]],
		12: [[1,7,5,11,3,9,6,12,2,8,4,10]],
		16: [[1,5,3,7,2,6,4,8,9,13,11,15,10,14,12,16]]
	};

	function getFiringOrder(n, idx) {
		const arrs = firingOrders[n];
		if (!arrs) {
			let arr = [];
			for (let i=1; i<=n; i++) arr.push(i);
			return arr;
		}
		return arrs[idx || 0];
	}

	function populateOrderSelect(numCylinders) {
		const select = document.getElementById('orderSelect');
		select.innerHTML = '';
		const orders = firingOrders[numCylinders] || [[...Array(numCylinders).keys()].map(i=>i+1)];
		orders.forEach((order, i) => {
			const opt = document.createElement('option');
			opt.value = i;
			opt.text = order.join('-');
			select.appendChild(opt);
		});
	}

	function drawChart(numCylinders, vAngle, orderIdx, crankAngle) {
		const firingOrder = getFiringOrder(numCylinders, orderIdx);
		const colors = colorList;
		const events = [];
		const fullCrank = 720;
		
		// Pure mathematical calculation of firing events
		// For a 4-stroke engine, each cylinder fires once per 720° at TDC compression stroke
		
		// Step 1: Calculate all physical TDC positions for each cylinder
		// Each cylinder has 2 TDC events per 720° (compression and exhaust at 0° and 360° offset)
		const allTDCPositions = []; // Array of {cyl, angle, id}
		
		for (let cyl = 1; cyl <= numCylinders; cyl++) {
			// Which crank throw this cylinder is on (paired: 1&2 on throw 0, 3&4 on throw 1, etc.)
			const throwNumber = Math.floor((cyl - 1) / 2);
			
			// Crank throw angle position
			const throwAngle = (throwNumber * crankAngle) % 360;
			
			// Bank offset (split V-angle symmetrically)
			const bankOffset = (cyl % 2 === 0) ? (vAngle / 2) : -(vAngle / 2);
			
			// Physical TDC angle (within 0-360°)
			const physicalTDC = (throwAngle + bankOffset + 360) % 360;
			
			// Each cylinder has 2 TDC events per 720° cycle
			allTDCPositions.push({
				cyl: cyl,
				angle: physicalTDC,
				id: cyl + '-A'
			});
			allTDCPositions.push({
				cyl: cyl,
				angle: physicalTDC + 360,
				id: cyl + '-B'
			});
		}
		
		// Step 2: Sort all TDC positions by angle
		allTDCPositions.sort((a, b) => a.angle - b.angle);
		
		// Step 3: Assign TDCs to firing order
		// Each cylinder in firing order gets its next unused TDC in sequence
		const usedTDCs = new Set();
		const rawEvents = [];
		let currentAngle = 0;
		
		for (let i = 0; i < firingOrder.length; i++) {
			const cyl = firingOrder[i];
			
			// Find the next available TDC for this cylinder after currentAngle
			let selectedTDC = null;
			
			// First try: find TDC after current angle
			for (const tdc of allTDCPositions) {
				if (tdc.cyl === cyl && !usedTDCs.has(tdc.id) && tdc.angle >= currentAngle) {
					selectedTDC = tdc;
					break;
				}
			}
			
			// If not found, wrap around and take the first unused one
			if (!selectedTDC) {
				for (const tdc of allTDCPositions) {
					if (tdc.cyl === cyl && !usedTDCs.has(tdc.id)) {
						selectedTDC = tdc;
						break;
					}
				}
			}
			
			if (selectedTDC) {
				usedTDCs.add(selectedTDC.id);
				rawEvents.push({
					cyl: cyl,
					angle: selectedTDC.angle
				});
				currentAngle = selectedTDC.angle + 0.1; // Move slightly past this angle
			}
		}
		
		// Step 4: Center the events at 360°
		const angles = rawEvents.map(e => e.angle);
		const minAngle = Math.min(...angles);
		const maxAngle = Math.max(...angles);
		const centerOffset = 360 - (minAngle + maxAngle) / 2;
		
		// Step 5: Create final events
		for (const raw of rawEvents) {
			const centeredAngle = (raw.angle + centerOffset + 720) % 720;
			const y = (raw.cyl % 2 === 0) ? 0.9 : 1.1;
			
			events.push({
				x: centeredAngle,
				y: y,
				cylinder: raw.cyl,
				color: colors[(raw.cyl - 1) % colors.length],
				orderLabel: 'Cylinder ' + raw.cyl + ' @ ' + centeredAngle.toFixed(1) + '°'
			});
		}
		
		// Sort events by firing angle to calculate intervals
		events.sort((a, b) => a.x - b.x);
		
		// Calculate firing intervals
		const intervals = [];
		for (let i = 0; i < events.length; i++) {
			const current = events[i].x;
			const next = i < events.length - 1 ? events[i + 1].x : events[0].x + fullCrank;
			const interval = next - current;
			intervals.push(interval);
		}
		
		// Check if intervals are even
		const avgInterval = intervals.reduce((a, b) => a + b, 0) / intervals.length;
		const maxDeviation = Math.max(...intervals.map(i => Math.abs(i - avgInterval)));
		const isEven = maxDeviation < 1;
		const intervalInfo = document.getElementById('intervalInfo');
		if (intervalInfo) {
			const uniqueIntervals = [...new Set(intervals.map(i => i.toFixed(1)))];
			if (isEven) {
				intervalInfo.innerHTML = '<span style="color: green;">✓ Even spacing: ' + uniqueIntervals[0] + '° between all firing events</span>';
			} else {
				intervalInfo.innerHTML = '<span style="color: orange;">⚠ Uneven spacing: ' + intervals.map(i => i.toFixed(1) + '°').join(', ') + '</span>';
			}
		}
		
		// Create firing sequence line data
		// Shows timing between firing events with spikes to each cylinder's bank
		const lineData = [];
		// Add starting point at center
		lineData.push({ x: 0, y: 1.0 });
		// Add points for each firing event
		events.forEach((event, idx) => {
			// Point just before firing (at center)
			if (event.x > 0.5) {
				lineData.push({ x: event.x - 0.5, y: 1.0 });
			}
			// Spike to firing cylinder's bank
			lineData.push({ x: event.x, y: event.y });
			// Return to center after firing
			lineData.push({ x: event.x + 0.5, y: 1.0 });
		});
		// Add ending point at center
		lineData.push({ x: 720, y: 1.0 });
		
		const ctx = document.getElementById('firingChart').getContext('2d');
		if (window.firingChartObj) window.firingChartObj.destroy();
		if (events.length === 0) {
			ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
			ctx.font = '20px sans-serif';
			ctx.fillText('No firing events to display', 100, 200);
			return;
		}
		
		window.firingChartObj = new Chart(ctx, {
			type: 'scatter',
			data: {
				datasets: [
					{
						label: 'Firing Sequence',
						data: lineData,
						borderColor: '#666666',
						backgroundColor: 'transparent',
						borderWidth: 2,
						pointRadius: 0,
						showLine: true,
						type: 'line',
						order: 2
					},
					{
						label: 'Cylinders',
						data: events,
						backgroundColor: events.map(e => e.color),
						pointRadius: 8,
						showLine: false,
						order: 1
					}
				],
			},
			options: {
				responsive: true,
				maintainAspectRatio: false,
				animation: { y: { duration: 0 } },
				plugins: {
					title: {
						display: true,
						text: 'Cylinder Firing Order'
					},
					legend: {
						display: false
					},
					tooltip: {
						callbacks: {
							label: function(context) {
								return context.raw.orderLabel;
							}
						}
					}
				},
				scales: {
					x: {
						type: 'linear',
						title: { display: true, text: 'Crank Angle (°)' },
						min: 0,
						max: 720,
						ticks: { stepSize: 90 }
					},
					y: {
						ticks: {
							callback: function(value) {
								if (value === 1.1) return 'Odd';
								if (value === 0.9) return 'Even';
								return '';
							},
							display: true
						},
						min: 0.8,
						max: 1.2,
						display: true,
						title: { display: true, text: 'Bank' }
					}
				}
			}
		});
	}

	function drawPistonChart(numCylinders, vAngle, crankAngle, orderIdx) {
		const firingOrder = getFiringOrder(numCylinders, orderIdx);
		const colors = colorList;
		const pistonDatasets = [];
		const allCylinderData = {}; // Store all cylinder positions by angle for summing
		
		// Calculate firing events (same logic as drawChart)
		const allTDCPositions = [];
		for (let cyl = 1; cyl <= numCylinders; cyl++) {
			const throwNumber = Math.floor((cyl - 1) / 2);
			const throwAngle = (throwNumber * crankAngle) % 360;
			const bankOffset = (cyl % 2 === 0) ? (vAngle / 2) : -(vAngle / 2);
			
			// For 4-stroke: cylinders on the same throw are 360° out of phase
			// Odd cylinders (1,3,5...) fire on first stroke, even (2,4,6...) on second
			const cyclePhase = (cyl % 2 === 0) ? 360 : 0;
			
			const physicalTDC = (throwAngle + bankOffset + cyclePhase + 720) % 720;
			
			// Each cylinder has one compression TDC per 720° cycle
			allTDCPositions.push({
				cyl: cyl,
				angle: physicalTDC,
				id: cyl + '-compression'
			});
		}
		
		allTDCPositions.sort((a, b) => a.angle - b.angle);
		
		// Build firing events for 4-stroke engine using pure geometry
		// Each cylinder fires once per 720° at its compression TDC
		// Firing order determines the sequence, not the timing
		// The actual firing angles come from engine geometry
		
		const rawEvents = [];
		
		for (const cyl of firingOrder) {
			// Find this cylinder's compression TDC
			const tdc = allTDCPositions.find(t => t.cyl === cyl);
			if (tdc) {
				rawEvents.push({
					cyl: cyl,
					angle: tdc.angle
				});
			}
		}
		
		// Sort firing events by angle for display
		rawEvents.sort((a, b) => a.angle - b.angle);
		
		// Generate piston position data for each cylinder
		for (let cyl = 1; cyl <= numCylinders; cyl++) {
			// Which crank throw this cylinder is on
			const throwNumber = Math.floor((cyl - 1) / 2);
			
			// Crank throw angle position
			const throwAngle = (throwNumber * crankAngle) % 360;
			
			// Bank offset (split V-angle symmetrically)
			const bankOffset = (cyl % 2 === 0) ? (vAngle / 2) : -(vAngle / 2);
			
			// Phase offset for this cylinder (degrees)
			const phaseOffset = (throwAngle + bankOffset + 360) % 360;
			
			// Generate piston position curve
			const pistonData = [];
			for (let angle = 0; angle <= 720; angle += 2) {
				// Piston position formula: cos(angle + phase)
				// This gives position from +1 (TDC) to -1 (BDC)
				const adjustedAngle = (angle + phaseOffset) * Math.PI / 180;
				const position = Math.cos(adjustedAngle);
				pistonData.push({ x: angle, y: position });
				
				// Store for summing
				if (!allCylinderData[angle]) allCylinderData[angle] = [];
				allCylinderData[angle].push(position);
			}
			
			// Use lighter colors for individual cylinders (add transparency)
			const baseColor = colors[(cyl - 1) % colors.length];
			const lightColor = baseColor + '40'; // Add alpha for transparency
			
			pistonDatasets.push({
				label: 'Cyl ' + cyl,
				data: pistonData,
				borderColor: lightColor,
				backgroundColor: 'transparent',
				borderWidth: 1.5,
				pointRadius: 0,
				showLine: true,
				tension: 0.4,
				yAxisID: 'y'
			});
		}
		
		// Calculate sum of all cylinders
		const sumData = [];
		for (let angle = 0; angle <= 720; angle += 2) {
			const sum = allCylinderData[angle].reduce((a, b) => a + b, 0);
			// Round to 3 decimal places to avoid floating point precision artifacts
			const roundedSum = Math.round(sum * 1000) / 1000;
			sumData.push({ x: angle, y: roundedSum });
		}
		
		// Add sum line as first dataset (so it appears on top in legend)
		pistonDatasets.unshift({
			label: 'Total (Sum)',
			data: sumData,
			borderColor: '#000000',
			backgroundColor: 'transparent',
			borderWidth: 3,
			pointRadius: 0,
			showLine: true,
			tension: 0.4,
			yAxisID: 'y1'
		});
		
		// Add firing events as scatter points at TDC
		const firingEvents = rawEvents.map(raw => ({
			x: raw.angle,
			y: 1,
			cyl: raw.cyl
		}));
		
		const firingEventColors = firingEvents.map(e => colors[(e.cyl - 1) % colors.length]);
		console.log('Firing events:', firingEvents.map(e => 'Cyl' + e.cyl + '@' + e.x.toFixed(0) + '°').join(', '));
		console.log('Firing event colors:', firingEvents.map((e, i) => 'Cyl' + e.cyl + ':' + firingEventColors[i]).join(', '));
		pistonDatasets.push({
			label: 'Firing Events',
			data: firingEvents.map(e => ({
				x: e.x,
				y: 1
			})),
			type: 'scatter',
			backgroundColor: firingEventColors,
			pointRadius: 8,
			pointStyle: 'circle',
			showLine: false,
			yAxisID: 'y'
		});
		
		const pistonCtx = document.getElementById('pistonChart').getContext('2d');
		if (window.pistonChartObj) window.pistonChartObj.destroy();
		
		window.pistonChartObj = new Chart(pistonCtx, {
			type: 'line',
			data: {
				datasets: pistonDatasets
			},
			options: {
				responsive: true,
				maintainAspectRatio: false,
				animation: { duration: 0 },
				plugins: {
					title: {
						display: true,
						text: 'Piston Position Throughout Crank Rotation'
					},
					legend: {
						display: true,
						position: 'right'
					}
				},
				scales: {
					x: {
						type: 'linear',
						title: { display: true, text: 'Crank Angle (°)' },
						min: 0,
						max: 720,
						ticks: { stepSize: 90 }
					},
					y: {
						type: 'linear',
						position: 'left',
						title: { display: true, text: 'Piston Position' },
						min: -1.15,
						max: 1.15,
						ticks: {
							callback: function(value) {
								if (value === 1) return 'TDC';
								if (value === -1) return 'BDC';
								return value.toFixed(1);
							}
						}
					},
					y1: {
						type: 'linear',
						position: 'right',
						title: { display: true, text: 'Total Sum' },
						grid: {
							drawOnChartArea: false
						},
						ticks: {
							callback: function(value) {
								return value.toFixed(3);
							}
						}
					}
				}
			}
		});
	}

	function getAllowedCrankAngles(cylCount) {
		let allowed = [];
		const idealSpacing = 720 / cylCount;
		if (idealSpacing <= 180) allowed.push(idealSpacing);
		const commonAngles = [60, 72, 90, 120, 180];
		commonAngles.forEach(angle => {
			if (angle <= 180 && !allowed.includes(angle)) {
				allowed.push(angle);
			}
		});
		if (!allowed.includes(0)) allowed.unshift(0);
		allowed = [...new Set(allowed)].sort((a, b) => a - b);
		return allowed;
	}

	function updateCrankSliderOptions(cylCount) {
		const allowed = getAllowedCrankAngles(cylCount);
		const crankSlider = document.getElementById('crankSlider');
		const crankValue = document.getElementById('crankValue');
		crankSlider.min = Math.min(...allowed);
		crankSlider.max = Math.max(...allowed);
		crankSlider.step = 1;
		let snapped = allowed.reduce((prev, curr) => Math.abs(curr-currentCrank) < Math.abs(prev-currentCrank) ? curr : prev);
		currentCrank = snapped;
		crankSlider.value = snapped;
		crankValue.textContent = snapped;
	}

	let currentCrank;

	window.onload = function() {
		const slider = document.getElementById('cylSlider');
		const cylCount = document.getElementById('cylCount');
		const angleSlider = document.getElementById('angleSlider');
		const angleValue = document.getElementById('angleValue');
		const crankSlider = document.getElementById('crankSlider');
		const crankValue = document.getElementById('crankValue');
		const orderSelect = document.getElementById('orderSelect');

		let currentCyls = 6;
		let currentAngle = 60;
		let currentOrderIdx = 0;
		currentCrank = 120;
		populateOrderSelect(currentCyls);
		updateCrankSliderOptions(currentCyls);
		drawPistonChart(currentCyls, currentAngle, currentCrank, currentOrderIdx);

		slider.addEventListener('input', function() {
			let val = parseInt(slider.value);
			let closest = allowedCyls.reduce((prev, curr) => Math.abs(curr-val) < Math.abs(prev-val) ? curr : prev);
			slider.value = closest;
			cylCount.textContent = closest;
			currentCyls = closest;
			updateCrankSliderOptions(currentCyls);
			populateOrderSelect(currentCyls);
			currentOrderIdx = 0;
			drawPistonChart(currentCyls, currentAngle, currentCrank, currentOrderIdx);
		});

		angleSlider.addEventListener('input', function() {
			let val = parseInt(angleSlider.value);
			angleValue.textContent = val;
			currentAngle = val;
			drawPistonChart(currentCyls, currentAngle, currentCrank, currentOrderIdx);
		});

		crankSlider.addEventListener('input', function() {
			let val = parseInt(crankSlider.value);
			const allowed = getAllowedCrankAngles(currentCyls);
			let snapped = allowed.reduce((prev, curr) => Math.abs(curr-val) < Math.abs(prev-val) ? curr : prev);
			crankSlider.value = snapped;
			crankValue.textContent = snapped;
			currentCrank = snapped;
			drawPistonChart(currentCyls, currentAngle, currentCrank, currentOrderIdx);
		});

		orderSelect.addEventListener('change', function() {
			currentOrderIdx = parseInt(orderSelect.value);
			drawPistonChart(currentCyls, currentAngle, currentCrank, currentOrderIdx);
		});
	};
	</script>
</body>
</html>
`

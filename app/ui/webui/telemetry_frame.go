package webui

// TelemetryFrame is one 60 Hz telemetry chart sample. It replaces the
// per-frame map[string]float32 the app layer used to build and send over the
// telemetry feed channel, which allocated a new map on every frame at 60 Hz.
// The json tags reproduce the original map keys verbatim so the WebSocket
// wire format is unchanged for clients.
//
//nolint:tagliatelle // tags are frozen to the legacy wire format the web UI consumes
type TelemetryFrame struct {
	ComputeTime                 float32   `json:"computeTime"`
	Seq                         float32   `json:"seq"`
	SeqGap                      float32   `json:"seqGap"`
	TimeOfDay                   float32   `json:"timeOfDay"`
	ThrottleInput               float32   `json:"throttleInput"`
	ThrottleOutput              float32   `json:"throttleOutput"`
	BrakeInput                  float32   `json:"brakeInput"`
	BrakeOutput                 float32   `json:"brakeOutput"`
	RPM                         float32   `json:"rpm"`
	Speed                       float32   `json:"speed"`
	Gear                        float32   `json:"gear"`
	FuelUsagePerKm              float32   `json:"fuelUsagePerKm"`
	FuelRangeKm                 float32   `json:"fuelRangeKm"`
	FuelRangeLaps               float32   `json:"fuelRangeLaps"`
	TyreTempFL                  float32   `json:"tyreTempFL"`
	TyreTempFR                  float32   `json:"tyreTempFR"`
	TyreTempRL                  float32   `json:"tyreTempRL"`
	TyreTempRR                  float32   `json:"tyreTempRR"`
	SurgeGforce                 float32   `json:"surgeGforce"`
	SurgeGforceCalc             float32   `json:"surgeGforceCalc"`
	SixDOFTranslationalJerk     float32   `json:"SixDOFTranslationalJerk"`
	SixDOFTranslationalSnap     float32   `json:"SixDOFTranslationalSnap"`
	SixDOFTranslationalJerkCalc float32   `json:"SixDOFTranslationalJerkCalc"`
	SixDOFTranslationalSnapCalc float32   `json:"SixDOFTranslationalSnapCalc"`
	SixDOFTranslationalAccelX   float32   `json:"SixDOFTranslationalAccelX"`
	SixDOFTranslationalAccelY   float32   `json:"SixDOFTranslationalAccelY"`
	SixDOFTranslationalAccelZ   float32   `json:"SixDOFTranslationalAccelZ"`
	SixDOFRotationalJerk        float32   `json:"SixDOFRotationalJerk"`
	SixDOFRotationalSnap        float32   `json:"SixDOFRotationalSnap"`
	SixDOFRotationalAccelX      float32   `json:"SixDOFRotationalAccelX"`
	SixDOFRotationalAccelY      float32   `json:"SixDOFRotationalAccelY"`
	SixDOFRotationalAccelZ      float32   `json:"SixDOFRotationalAccelZ"`
	SynthChannelAmplitude       []float32 `json:"synthChannelAmplitude"`
	SynthChannelFrequency       []float32 `json:"synthChannelFrequency"`
	EngineVibrationEnabled      float32   `json:"engineVibrationEnabled"`
	// Audio pipeline health metrics
	AsyncBufferFill    float32   `json:"asyncBufferFill"`
	AsyncUnderruns     float32   `json:"asyncUnderruns"`
	AsyncProducerWaits float32   `json:"asyncProducerWaits"`
	MixerEngineFill    float32   `json:"mixerEngineFill"`
	MixerChassisFill   []float32 `json:"mixerChassisFill"`
	// Haptic latency/drift monitor (milliseconds)
	EngineLatencyMs  float32 `json:"engineLatencyMs"`
	ChassisLatencyMs float32 `json:"chassisLatencyMs"`
	RingLatencyMs    float32 `json:"ringLatencyMs"`
	DriftMs          float32 `json:"driftMs"`
	SeqJitterMs      float32 `json:"seqJitterMs"`
}

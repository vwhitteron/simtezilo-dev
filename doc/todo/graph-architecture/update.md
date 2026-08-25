## devices
* wired
  * audio (haptics, speech, tone)
  * display
  * buttons
* wireless
  * Bluetooth
    * audio (speech, tone only)
    * fan controller

## Inputs
* telemetry
  * can accept data from any telemetry source (automotive sim racing, aerospace simulation, possibly even real telemetry)
* user
  * buttons

## Processing
* modular functions that can be connected together
* modules can support variable number of inputs and outputs
  * example: increment: 1 input, 1 output
  * example: sum has 2 inputs, 1 output
  * example: pinknoise has 0-1 input (optional seed) and 1 output
* modules can branch on output side
  * to separate module pipelines to avoid recomputing the same data
  * back into an exisitng module pipeline when modules take multiple inputs
* final output can be to multiple output devices

## Outputs
* audio devices
  * resampling contained within the output devices, not as a module on the pipeline
* event devices
  * display
    * pre-defined screen layouts
      * output targets screen elements
  * audio
    * tones
    * speech
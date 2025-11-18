package app

import "time"

const frameRate = 60 // 60 Hz

const pitRadioFrameRate = 1      // 1 Hz
const displayFrameRate = 15      // 15 Hz
const engineHapticFrameRate = 30 // 30 Hz
const hapticFrameRate = 120      // 120 Hz
const telemetryFrameRate = 60    // 60 Hz

// TODO: should these be user configurable?
const tyreConditionStablisationTime = 5 * time.Second
const tyreInterNotifyGap = 30 * time.Second

// TODO: find a better place for this.
const snapMultiplier = 160

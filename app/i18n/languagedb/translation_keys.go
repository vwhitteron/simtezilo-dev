package languagedb

import (
	"fmt"
	"strings"
)

// Key provides a type-safe way to reference translation keys.
type Key string

const (
	AppName        Key = "app.name"
	AppDescription Key = "app.description"
	AppVersion     Key = "app.version"

	UIError    Key = "ui.error"
	UISuccess  Key = "ui.success"
	UIQuit     Key = "ui.quit"
	UIStarting Key = "ui.starting"
	UIStopping Key = "ui.stopping"
	UILoading  Key = "ui.loading"
	UIWaiting  Key = "ui.waiting"
	UIReady    Key = "ui.ready"
	UISettings Key = "ui.settings"

	UIMenuVol        Key = "ui.menu.vol"
	UIMenuCVol       Key = "ui.menu.cvol"
	UIMenuTVol       Key = "ui.menu.tvol"
	UIMenuEVol       Key = "ui.menu.evol"
	UIMenuEPrimary   Key = "ui.menu.eprimary"
	UIMenuESecondary Key = "ui.menu.esecondary"
	UIMenuEPVol      Key = "ui.menu.epvol"
	UIMenuEPScale    Key = "ui.menu.epscale"
	UIMenuVCurve     Key = "ui.menu.vcurve"
	UIMenuVSat       Key = "ui.menu.vsat"
	UIMenuFCurve     Key = "ui.menu.fcurve"
	UIMenuFSat       Key = "ui.menu.fsat"
	UIMenuFMin       Key = "ui.menu.fmin"
	UIMenuFMax       Key = "ui.menu.fmax"
	UIMenuTCurve     Key = "ui.menu.tcurve"
	UIMenuTSat       Key = "ui.menu.tsat"
	UIMenuMix        Key = "ui.menu.mix"
	UIMenuLang       Key = "ui.menu.lang"
	UIMenuInfo       Key = "ui.menu.info"
	UIMenuSetupmode  Key = "ui.menu.setupmode"

	RadioOnline            Key = "radio.online"
	RadioLapRecordFmt      Key = "radio.laprecordfmt"
	RadioSlowerLapFmt      Key = "radio.slowerlapfmt"
	RadioFuelRangeFmt      Key = "radio.fuelrangefmt"
	RadioFuelPreWarnFmt    Key = "radio.fuelprewarnfmt"
	RadioBoxThisLap        Key = "radio.boxthislap"
	RadioFuelCritical      Key = "radio.fuelcritical"
	RadioFuelCriticalBox   Key = "radio.fuelcriticalbox"
	RadioOutOfFuelLastLap  Key = "radio.outoffuellastlap"
	RadioOutOfFuelBox      Key = "radio.outoffuel"
	RadioLapsRemainingFmt  Key = "radio.lapsremainingfmt"
	RadioRaceProgressFmt   Key = "radio.raceprogressfmt"
	RadioRaceFinish        Key = "radio.racefinish"
	RadioFinalLap          Key = "radio.finallap"
	RadioCircuitUpdatedFmt Key = "radio.circuitupdatedfmt"
	RadioTyresUnderTemp    Key = "radio.tyresundertemp"
	RadioTyresOptimalTemp  Key = "radio.tyresoptimaltemp"
	RadioTyresOverTemp     Key = "radio.tyresovertemp"
	RadioFront             Key = "radio.front"
	RadioRear              Key = "radio.rear"
	RadioFrontLeft         Key = "radio.frontleft"
	RadioFrontRight        Key = "radio.frontright"
	RadioRearLeft          Key = "radio.rearleft"
	RadioRearRight         Key = "radio.rearright"

	// Setup Mode - Network Configuration.
	SetupmodeNetworkTitle                Key = "setupmode.network.title"
	SetupmodeNetworkNetworkselection     Key = "setupmode.network.networkselection"
	SetupmodeNetworkModeAutomatic        Key = "setupmode.network.mode.automatic"
	SetupmodeNetworkModeManual           Key = "setupmode.network.mode.manual"
	SetupmodeNetworkRescan               Key = "setupmode.network.rescan"
	SetupmodeNetworkScanning             Key = "setupmode.network.scanning"
	SetupmodeNetworkSelectnetwork        Key = "setupmode.network.selectnetwork"
	SetupmodeNetworkLoadingnetworks      Key = "setupmode.network.loadingnetworks"
	SetupmodeNetworkScanningfornetworks  Key = "setupmode.network.scanningfornetworks"
	SetupmodeNetworkErrorloadingnetworks Key = "setupmode.network.errorloadingnetworks"
	SetupmodeNetworkManualssid           Key = "setupmode.network.manualssid"
	SetupmodeNetworkEnterssid            Key = "setupmode.network.enterssid"
	SetupmodeNetworkSecuritytype         Key = "setupmode.network.securitytype"
	SetupmodeNetworkPassword             Key = "setupmode.network.password"
	SetupmodeNetworkEnterpassword        Key = "setupmode.network.enterpassword"
	SetupmodeNetworkShowpassword         Key = "setupmode.network.showpassword"
	SetupmodeNetworkHidepassword         Key = "setupmode.network.hidepassword"
	SetupmodeNetworkIpconfiguration      Key = "setupmode.network.ipconfiguration"
	SetupmodeNetworkIpmodeDhcp           Key = "setupmode.network.ipmode.dhcp"
	SetupmodeNetworkIpmodeStatic         Key = "setupmode.network.ipmode.static"
	SetupmodeNetworkIpaddress            Key = "setupmode.network.ipaddress"
	SetupmodeNetworkIpaddressexample     Key = "setupmode.network.ipaddressexample"
	SetupmodeNetworkPrefix               Key = "setupmode.network.prefix"
	SetupmodeNetworkPrefixexample        Key = "setupmode.network.prefixexample"
	SetupmodeNetworkGateway              Key = "setupmode.network.gateway"
	SetupmodeNetworkGatewayexample       Key = "setupmode.network.gatewayexample"
	SetupmodeNetworkDns                  Key = "setupmode.network.dns"
	SetupmodeNetworkDnsexample           Key = "setupmode.network.dnsexample"
	SetupmodeNetworkReturntorunmode      Key = "setupmode.network.returntorunmode"

	// Setup Mode - Validation.
	SetupmodeValidationRequired          Key = "setupmode.validation.required"
	SetupmodeValidationInvalidip         Key = "setupmode.validation.invalidip"
	SetupmodeValidationInvaliddns        Key = "setupmode.validation.invaliddns"
	SetupmodeValidationPasswordrequired  Key = "setupmode.validation.passwordrequired"
	SetupmodeValidationPasswordminlength Key = "setupmode.validation.passwordminlength"
	SetupmodeValidationPasswordmaxlength Key = "setupmode.validation.passwordmaxlength"
	SetupmodeValidationSsidrequired      Key = "setupmode.validation.ssidrequired"

	// Setup Mode - Confirmation.
	SetupmodeConfirmationSavetitle          Key = "setupmode.confirmation.savetitle"
	SetupmodeConfirmationSavemessage        Key = "setupmode.confirmation.savemessage"
	SetupmodeConfirmationSaveinstructions   Key = "setupmode.confirmation.saveinstructions"
	SetupmodeConfirmationReturntitle        Key = "setupmode.confirmation.returntitle"
	SetupmodeConfirmationReturnmessage      Key = "setupmode.confirmation.returnmessage"
	SetupmodeConfirmationReturninstructions Key = "setupmode.confirmation.returninstructions"

	// Setup Mode - Result.
	SetupmodeResultSuccess      Key = "setupmode.result.success"
	SetupmodeResultError        Key = "setupmode.result.error"
	SetupmodeResultMoreinfo     Key = "setupmode.result.moreinfo"
	SetupmodeResultReturning    Key = "setupmode.result.returning"
	SetupmodeResultNetworkerror Key = "setupmode.result.networkerror"

	// Run Mode - Navigation.
	RunmodeNavTelemetry Key = "runmode.nav.telemetry"
	RunmodeNavRace      Key = "runmode.nav.race"
	RunmodeNavDeveloper Key = "runmode.nav.developer"
	RunmodeNavSettings  Key = "runmode.nav.settings"
	RunmodeNavLogs      Key = "runmode.nav.logs"
	RunmodeNavInfo      Key = "runmode.nav.info"

	// Run Mode - Info.
	RunmodeInfoVersion        Key = "runmode.info.version"
	RunmodeInfoBuilddate      Key = "runmode.info.buildDate"
	RunmodeInfoTargetplatform Key = "runmode.info.targetPlatform"
	RunmodeInfoHardware       Key = "runmode.info.hardware"

	// Run Mode - Home Vehicle.
	RunmodeHomeVehicleWaiting Key = "runmode.home.vehicle.waiting"
	RunmodeHomeVehicleUnknown Key = "runmode.home.vehicle.unknown"

	// Run Mode - Home Circuit.
	RunmodeHomeCircuitWaiting Key = "runmode.home.circuit.waiting"
	RunmodeHomeCircuitUnknown Key = "runmode.home.circuit.unknown"

	// Run Mode - Home Race.
	RunmodeHomeRaceTitle            Key = "runmode.home.race.title"
	RunmodeHomeRaceTimeofday        Key = "runmode.home.race.timeofday"
	RunmodeHomeRaceLap              Key = "runmode.home.race.lap"
	RunmodeHomeRacePosition         Key = "runmode.home.race.position"
	RunmodeHomeRaceWaiting          Key = "runmode.home.race.waiting"
	RunmodeHomeRaceLapeventLap      Key = "runmode.home.race.lapevent.lap"
	RunmodeHomeRaceLapeventLaptime  Key = "runmode.home.race.lapevent.laptime"
	RunmodeHomeRaceLapeventDelta    Key = "runmode.home.race.lapevent.delta"
	RunmodeHomeRaceLapeventPosition Key = "runmode.home.race.lapevent.position"

	// Run Mode - Home Game State.
	RunmodeHomeGamestateConnecting Key = "runmode.home.gamestate.connecting"
	RunmodeHomeGamestateMainmenu   Key = "runmode.home.gamestate.mainmenu"
	RunmodeHomeGamestateRacemenu   Key = "runmode.home.gamestate.racemenu"
	RunmodeHomeGamestateOncircuit  Key = "runmode.home.gamestate.oncircuit"
	RunmodeHomeGamestateReplay     Key = "runmode.home.gamestate.replay"
	RunmodeHomeGamestatePaused     Key = "runmode.home.gamestate.paused"
	RunmodeHomeGamestateUnknown    Key = "runmode.home.gamestate.unknown"

	// Run Mode - Logs.
	RunmodeLogsShowing       Key = "runmode.logs.showing"
	RunmodeLogsPerpage       Key = "runmode.logs.perpage"
	RunmodeLogsFilterlevels  Key = "runmode.logs.filterlevels"
	RunmodeLogsTitle         Key = "runmode.logs.title"
	RunmodeLogsStatsTotal    Key = "runmode.logs.stats.total"
	RunmodeLogsStatsErrors   Key = "runmode.logs.stats.errors"
	RunmodeLogsStatsWarnings Key = "runmode.logs.stats.warnings"
	RunmodeLogsStatsInfo     Key = "runmode.logs.stats.info"
	RunmodeLogsFilterAll     Key = "runmode.logs.filter.all"
	RunmodeLogsFilterError   Key = "runmode.logs.filter.error"
	RunmodeLogsFilterWarning Key = "runmode.logs.filter.warning"
	RunmodeLogsFilterInfo    Key = "runmode.logs.filter.info"
	RunmodeLogsFilterDebug   Key = "runmode.logs.filter.debug"
	RunmodeLogsClear         Key = "runmode.logs.clear"

	// Run Mode - Settings App.
	RunmodeSettingsAppTitle                         Key = "runmode.settings.app.title"
	RunmodeSettingsAppGeneralTitle                  Key = "runmode.settings.app.general.title"
	RunmodeSettingsAppGeneralLanguage               Key = "runmode.settings.app.general.language"
	RunmodeSettingsAppGeneralAccent                 Key = "runmode.settings.app.general.accent"
	RunmodeSettingsAppGeneralAccentTooltip          Key = "runmode.settings.app.general.accent.tooltip"
	RunmodeSettingsAppGeneralLoglevel               Key = "runmode.settings.app.general.loglevel"
	RunmodeSettingsAppGeneralBasedir                Key = "runmode.settings.app.general.basedir"
	RunmodeSettingsAppGeneralVehicledbfile          Key = "runmode.settings.app.general.vehicledbfile"
	RunmodeSettingsAppGeneralTelemetrySource        Key = "runmode.settings.app.general.telemetry.source"
	RunmodeSettingsAppGeneralTelemetrySourceTooltip Key = "runmode.settings.app.general.telemetry.source.tooltip"
	RunmodeSettingsAppGeneralTelemetryAuto          Key = "runmode.settings.app.general.telemetry.auto"
	RunmodeSettingsAppGeneralTelemetryDemo          Key = "runmode.settings.app.general.telemetry.demo"

	// Run Mode - Settings App Config.
	RunmodeSettingsAppConfigTitle  Key = "runmode.settings.app.config.title"
	RunmodeSettingsAppConfigExport Key = "runmode.settings.app.config.export"
	RunmodeSettingsAppConfigImport Key = "runmode.settings.app.config.import"
	RunmodeSettingsAppConfigReset  Key = "runmode.settings.app.config.reset"

	// Run Mode - Settings App Control.
	RunmodeSettingsAppControlTitle        Key = "runmode.settings.app.control.title"
	RunmodeSettingsAppControlCacheddata   Key = "runmode.settings.app.control.cacheddata"
	RunmodeSettingsAppControlCacheItem    Key = "runmode.settings.app.control.cacheitem"
	RunmodeSettingsAppControlCacheItems   Key = "runmode.settings.app.control.cacheitems"
	RunmodeSettingsAppControlClearcache   Key = "runmode.settings.app.control.clearcache"
	RunmodeSettingsAppControlRestart      Key = "runmode.settings.app.control.restart"
	RunmodeSettingsAppControlSetupmode    Key = "runmode.settings.app.control.setupmode"
	RunmodeSettingsAppControlFactoryreset Key = "runmode.settings.app.control.factoryreset"

	// Run Mode - Settings Haptics.
	RunmodeSettingsHapticsTitle          Key = "runmode.settings.haptics.title"
	RunmodeSettingsHapticsGeneralTitle   Key = "runmode.settings.haptics.general.title"
	RunmodeSettingsHapticsOutputmode     Key = "runmode.settings.haptics.outputmode"
	RunmodeSettingsHapticsModeLive       Key = "runmode.settings.haptics.mode.live"
	RunmodeSettingsHapticsModeLivereplay Key = "runmode.settings.haptics.mode.livereplay"

	// Run Mode - Settings Haptics Chassis.
	RunmodeSettingsHapticsChassisTitle                    Key = "runmode.settings.haptics.chassis.title"
	RunmodeSettingsHapticsChassisJerkcurve                Key = "runmode.settings.haptics.chassis.jerkcurve"
	RunmodeSettingsHapticsChassisJerkcurveTooltip         Key = "runmode.settings.haptics.chassis.jerkcurve.tooltip"
	RunmodeSettingsHapticsChassisJerkmax                  Key = "runmode.settings.haptics.chassis.jerkmax"
	RunmodeSettingsHapticsChassisJerkmaxTooltip           Key = "runmode.settings.haptics.chassis.jerkmax.tooltip"
	RunmodeSettingsHapticsChassisSnapcurve                Key = "runmode.settings.haptics.chassis.snapcurve"
	RunmodeSettingsHapticsChassisSnapcurveTooltip         Key = "runmode.settings.haptics.chassis.snapcurve.tooltip"
	RunmodeSettingsHapticsChassisSnapmax                  Key = "runmode.settings.haptics.chassis.snapmax"
	RunmodeSettingsHapticsChassisSnapmaxTooltip           Key = "runmode.settings.haptics.chassis.snapmax.tooltip"
	RunmodeSettingsHapticsChassisPulsemaxamplitude        Key = "runmode.settings.haptics.chassis.pulsemaxamplitude"
	RunmodeSettingsHapticsChassisPulsemaxamplitudeTooltip Key = "runmode.settings.haptics.chassis.pulsemaxamplitude.tooltip"
	RunmodeSettingsHapticsChassisPulseminfreq             Key = "runmode.settings.haptics.chassis.pulseminfreq"
	RunmodeSettingsHapticsChassisPulseminfreqTooltip      Key = "runmode.settings.haptics.chassis.pulseminfreq.tooltip"
	RunmodeSettingsHapticsChassisPulsemaxfreq             Key = "runmode.settings.haptics.chassis.pulsemaxfreq"
	RunmodeSettingsHapticsChassisPulsemaxfreqTooltip      Key = "runmode.settings.haptics.chassis.pulsemaxfreq.tooltip"

	// Run Mode - Settings Haptics Transmission.
	RunmodeSettingsHapticsTransmissionTitle                   Key = "runmode.settings.haptics.transmission.title"
	RunmodeSettingsHapticsTransmissionFeedbackstrength        Key = "runmode.settings.haptics.transmission.feedbackstrength"
	RunmodeSettingsHapticsTransmissionFeedbackstrengthTooltip Key = "runmode.settings.haptics.transmission.feedbackstrength.tooltip"
	RunmodeSettingsHapticsTransmissionFeedbackstrengthFixed   Key = "runmode.settings.haptics.transmission.feedbackstrength.fixed"
	RunmodeSettingsHapticsTransmissionFeedbackstrengthdynamic Key = "runmode.settings.haptics.transmission.feedbackstrengthdynamic"
	RunmodeSettingsHapticsTransmissionCurve                   Key = "runmode.settings.haptics.transmission.curve"
	RunmodeSettingsHapticsTransmissionCurveTooltip            Key = "runmode.settings.haptics.transmission.curve.tooltip"
	RunmodeSettingsHapticsTransmissionGforcemax               Key = "runmode.settings.haptics.transmission.gforcemax"
	RunmodeSettingsHapticsTransmissionGforcemaxTooltip        Key = "runmode.settings.haptics.transmission.gforcemax.tooltip"

	// Run Mode - Settings Haptics Advanced.
	RunmodeSettingsHapticsAdvancedTitle Key = "runmode.settings.haptics.advanced.title"

	// Run Mode - Settings Haptics Engine Profiles.
	RunmodeSettingsHapticsEngineprofilesTitle                   Key = "runmode.settings.haptics.engineprofiles.title"
	RunmodeSettingsHapticsEngineprofilesPrimarybalance          Key = "runmode.settings.haptics.engineprofiles.primarybalance"
	RunmodeSettingsHapticsEngineprofilesPrimarybalanceTooltip   Key = "runmode.settings.haptics.engineprofiles.primarybalance.tooltip"
	RunmodeSettingsHapticsEngineprofilesSecondarybalance        Key = "runmode.settings.haptics.engineprofiles.secondarybalance"
	RunmodeSettingsHapticsEngineprofilesSecondarybalanceTooltip Key = "runmode.settings.haptics.engineprofiles.secondarybalance.tooltip"
	RunmodeSettingsHapticsEngineprofilesGain                    Key = "runmode.settings.haptics.engineprofiles.gain"
	RunmodeSettingsHapticsEngineprofilesGainTooltip             Key = "runmode.settings.haptics.engineprofiles.gain.tooltip"
	RunmodeSettingsHapticsEngineprofilesPulsescale              Key = "runmode.settings.haptics.engineprofiles.pulsescale"
	RunmodeSettingsHapticsEngineprofilesPulsescaleTooltip       Key = "runmode.settings.haptics.engineprofiles.pulsescale.tooltip"

	// Run Mode - Settings Pit Radio.
	RunmodeSettingsPitradioTitle           Key = "runmode.settings.pitradio.title"
	RunmodeSettingsPitradioGeneralTitle    Key = "runmode.settings.pitradio.general.title"
	RunmodeSettingsPitradioEnable          Key = "runmode.settings.pitradio.enable"
	RunmodeSettingsPitradioOutput          Key = "runmode.settings.pitradio.output"
	RunmodeSettingsPitradioMessageinterval Key = "runmode.settings.pitradio.messageinterval"

	// Run Mode - Settings Pit Radio Discord.
	RunmodeSettingsPitradioDiscordTitle                     Key = "runmode.settings.pitradio.discord.title"
	RunmodeSettingsPitradioDiscordBottoken                  Key = "runmode.settings.pitradio.discord.bottoken"
	RunmodeSettingsPitradioDiscordBottokenPlaceholder       Key = "runmode.settings.pitradio.discord.bottoken.placeholder"
	RunmodeSettingsPitradioDiscordGuildid                   Key = "runmode.settings.pitradio.discord.guildid"
	RunmodeSettingsPitradioDiscordGuildidPlaceholder        Key = "runmode.settings.pitradio.discord.guildid.placeholder"
	RunmodeSettingsPitradioDiscordChannelid                 Key = "runmode.settings.pitradio.discord.channelid"
	RunmodeSettingsPitradioDiscordChannelidPlaceholder      Key = "runmode.settings.pitradio.discord.channelid.placeholder"
	RunmodeSettingsPitradioDiscordVoicechannelid            Key = "runmode.settings.pitradio.discord.voicechannelid"
	RunmodeSettingsPitradioDiscordVoicechannelidPlaceholder Key = "runmode.settings.pitradio.discord.voicechannelid.placeholder"

	// Run Mode - Settings Pit Radio Notifications.
	RunmodeSettingsPitradioNotificationsTitle Key = "runmode.settings.pitradio.notifications.title"

	// Run Mode - Settings Pit Radio Notifications Lap Times.
	RunmodeSettingsPitradioNotificationsLaptimesTitle    Key = "runmode.settings.pitradio.notifications.laptimes.title"
	RunmodeSettingsPitradioNotificationsLaptimesEnable   Key = "runmode.settings.pitradio.notifications.laptimes.enable"
	RunmodeSettingsPitradioNotificationsLaptimesMaxdelta Key = "runmode.settings.pitradio.notifications.laptimes.maxdelta"

	// Run Mode - Settings Pit Radio Notifications Circuit Matching.
	RunmodeSettingsPitradioNotificationsCircuitmatchingEnable Key = "runmode.settings.pitradio.notifications.circuitmatching.enable"

	// Run Mode - Settings Pit Radio Notifications Race Laps.
	RunmodeSettingsPitradioNotificationsRacelapsTitle     Key = "runmode.settings.pitradio.notifications.racelaps.title"
	RunmodeSettingsPitradioNotificationsRacelapsEnable    Key = "runmode.settings.pitradio.notifications.racelaps.enable"
	RunmodeSettingsPitradioNotificationsRacelapsInterval  Key = "runmode.settings.pitradio.notifications.racelaps.interval"
	RunmodeSettingsPitradioNotificationsRacelapsCountdown Key = "runmode.settings.pitradio.notifications.racelaps.countdown"

	// Run Mode - Settings Pit Radio Notifications Race Progress.
	RunmodeSettingsPitradioNotificationsRaceprogressEnable   Key = "runmode.settings.pitradio.notifications.raceprogress.enable"
	RunmodeSettingsPitradioNotificationsRaceprogressMinlaps  Key = "runmode.settings.pitradio.notifications.raceprogress.minlaps"
	RunmodeSettingsPitradioNotificationsRaceprogressInterval Key = "runmode.settings.pitradio.notifications.raceprogress.interval"

	// Run Mode - Settings Pit Radio Notifications Fuel.
	RunmodeSettingsPitradioNotificationsFuelTitle              Key = "runmode.settings.pitradio.notifications.fuel.title"
	RunmodeSettingsPitradioNotificationsFuelEnable             Key = "runmode.settings.pitradio.notifications.fuel.enable"
	RunmodeSettingsPitradioNotificationsFuelPrewarnlaps        Key = "runmode.settings.pitradio.notifications.fuel.prewarnlaps"
	RunmodeSettingsPitradioNotificationsFuelStrategylaps       Key = "runmode.settings.pitradio.notifications.fuel.strategylaps"
	RunmodeSettingsPitradioNotificationsFuelSafetymarginlaps   Key = "runmode.settings.pitradio.notifications.fuel.safetymarginlaps"
	RunmodeSettingsPitradioNotificationsFuelSafetymarginmeters Key = "runmode.settings.pitradio.notifications.fuel.safetymarginmeters"

	// Run Mode - Settings Pit Radio Notifications Tyres.
	RunmodeSettingsPitradioNotificationsTyresTitle       Key = "runmode.settings.pitradio.notifications.tyres.title"
	RunmodeSettingsPitradioNotificationsTyresEnable      Key = "runmode.settings.pitradio.notifications.tyres.enable"
	RunmodeSettingsPitradioNotificationsTyresTempoptimal Key = "runmode.settings.pitradio.notifications.tyres.tempoptimal"
	RunmodeSettingsPitradioNotificationsTyresTempwindow  Key = "runmode.settings.pitradio.notifications.tyres.tempwindow"
	RunmodeSettingsPitradioNotificationsTyresTempmargin  Key = "runmode.settings.pitradio.notifications.tyres.tempmargin"

	// Run Mode - Settings Synth.
	RunmodeSettingsSynthTitle Key = "runmode.settings.synth.title"

	// Run Mode - Settings Synth Sample Rates.
	RunmodeSettingsSynthSampleratesTitle           Key = "runmode.settings.synth.samplerates.title"
	RunmodeSettingsSynthSampleratesInternal        Key = "runmode.settings.synth.samplerates.internal"
	RunmodeSettingsSynthSampleratesInternalTooltip Key = "runmode.settings.synth.samplerates.internal.tooltip"
	RunmodeSettingsSynthSampleratesOutput          Key = "runmode.settings.synth.samplerates.output"
	RunmodeSettingsSynthSampleratesOutputTooltip   Key = "runmode.settings.synth.samplerates.output.tooltip"

	// Run Mode - Settings Synth Gain Controls.
	RunmodeSettingsSynthGaincontrolsTitle                        Key = "runmode.settings.synth.gaincontrols.title"
	RunmodeSettingsSynthGaincontrolsMaster                       Key = "runmode.settings.synth.gaincontrols.master"
	RunmodeSettingsSynthGaincontrolsMasterTooltip                Key = "runmode.settings.synth.gaincontrols.master.tooltip"
	RunmodeSettingsSynthGaincontrolsChassis                      Key = "runmode.settings.synth.gaincontrols.chassis"
	RunmodeSettingsSynthGaincontrolsChassisTooltip               Key = "runmode.settings.synth.gaincontrols.chassis.tooltip"
	RunmodeSettingsSynthGaincontrolsTransmission                 Key = "runmode.settings.synth.gaincontrols.transmission"
	RunmodeSettingsSynthGaincontrolsTransmissionTooltip          Key = "runmode.settings.synth.gaincontrols.transmission.tooltip"
	RunmodeSettingsSynthGaincontrolsTransmissionminrace          Key = "runmode.settings.synth.gaincontrols.transmissionminrace"
	RunmodeSettingsSynthGaincontrolsTransmissionminraceTooltip   Key = "runmode.settings.synth.gaincontrols.transmissionminrace.tooltip"
	RunmodeSettingsSynthGaincontrolsTransmissionminstreet        Key = "runmode.settings.synth.gaincontrols.transmissionminstreet"
	RunmodeSettingsSynthGaincontrolsTransmissionminstreetTooltip Key = "runmode.settings.synth.gaincontrols.transmissionminstreet.tooltip"
	RunmodeSettingsSynthGaincontrolsEngine                       Key = "runmode.settings.synth.gaincontrols.engine"
	RunmodeSettingsSynthGaincontrolsEngineTooltip                Key = "runmode.settings.synth.gaincontrols.engine.tooltip"
	RunmodeSettingsSynthGaincontrolsIncrement                    Key = "runmode.settings.synth.gaincontrols.increment"
	RunmodeSettingsSynthGaincontrolsIncrementTooltip             Key = "runmode.settings.synth.gaincontrols.increment.tooltip"

	// Run Mode - Settings System.
	RunmodeSettingsSystemTitle Key = "runmode.settings.system.title"

	// Run Mode - Settings System Hardware.
	RunmodeSettingsSystemHardwareTitle              Key = "runmode.settings.system.hardware.title"
	RunmodeSettingsSystemHardwareModel              Key = "runmode.settings.system.hardware.model"
	RunmodeSettingsSystemHardwareDisplayorientation Key = "runmode.settings.system.hardware.displayorientation"

	// Run Mode - Settings System Calibration.
	RunmodeSettingsSystemCalibrationTitle            Key = "runmode.settings.system.calibration.title"
	RunmodeSettingsSystemCalibrationEnabled          Key = "runmode.settings.system.calibration.enabled"
	RunmodeSettingsSystemCalibrationEnabledTooltip   Key = "runmode.settings.system.calibration.enabled.tooltip"
	RunmodeSettingsSystemCalibrationFrequency        Key = "runmode.settings.system.calibration.frequency"
	RunmodeSettingsSystemCalibrationFrequencyTooltip Key = "runmode.settings.system.calibration.frequency.tooltip"
	RunmodeSettingsSystemCalibrationVolume           Key = "runmode.settings.system.calibration.volume"
	RunmodeSettingsSystemCalibrationVolumeTooltip    Key = "runmode.settings.system.calibration.volume.tooltip"
	RunmodeSettingsSystemCalibrationChannel          Key = "runmode.settings.system.calibration.channel"
	RunmodeSettingsSystemCalibrationChannelTooltip   Key = "runmode.settings.system.calibration.channel.tooltip"
	RunmodeSettingsSystemCalibrationChannelBoth      Key = "runmode.settings.system.calibration.channel.both"
	RunmodeSettingsSystemCalibrationChannelLeft      Key = "runmode.settings.system.calibration.channel.left"
	RunmodeSettingsSystemCalibrationChannelRight     Key = "runmode.settings.system.calibration.channel.right"
	RunmodeSettingsSystemCalibrationHint             Key = "runmode.settings.system.calibration.hint"

	// Run Mode - Settings System Access.
	RunmodeSettingsSystemSSHAccessTitle            Key = "runmode.settings.system.sshaccess.title"
	RunmodeSettingsSystemSSHAccessEnabled          Key = "runmode.settings.system.sshaccess.enabled"
	RunmodeSettingsSystemSSHAccessEnabledTooltip   Key = "runmode.settings.system.sshaccess.enabled.tooltip"
	RunmodeSettingsSystemSSHAccessPublickey        Key = "runmode.settings.system.sshaccess.publickey"
	RunmodeSettingsSystemSSHAccessPublickeyTooltip Key = "runmode.settings.system.sshaccess.publickey.tooltip"
	RunmodeSettingsSystemSSHAccessProvision        Key = "runmode.settings.system.sshaccess.provision"
	RunmodeSettingsSystemSSHAccessHint             Key = "runmode.settings.system.sshaccess.hint"

	// Run Mode - Settings System Advanced.
	RunmodeSettingsSystemAdvancedTitle Key = "runmode.settings.system.advanced.title"

	// Run Mode - Settings System Equalizer.
	RunmodeSettingsSystemEqualizerTitle     Key = "runmode.settings.system.equalizer.title"
	RunmodeSettingsSystemEqualizerTooltip   Key = "runmode.settings.system.equalizer.tooltip"
	RunmodeSettingsSystemEqualizerEnabled   Key = "runmode.settings.system.equalizer.enabled"
	RunmodeSettingsSystemEqualizerBand      Key = "runmode.settings.system.equalizer.band"
	RunmodeSettingsSystemEqualizerFrequency Key = "runmode.settings.system.equalizer.frequency"
	RunmodeSettingsSystemEqualizerGain      Key = "runmode.settings.system.equalizer.gain"
	RunmodeSettingsSystemEqualizerQfactor   Key = "runmode.settings.system.equalizer.qfactor"
	RunmodeSettingsSystemEqualizerReset     Key = "runmode.settings.system.equalizer.reset"

	// Run Mode - Telemetry Charts.
	RunmodeTelemetryChartRpmspeed           Key = "runmode.telemetry.chart.rpmspeed"
	RunmodeTelemetryChartThrottlebrake      Key = "runmode.telemetry.chart.throttlebrake"
	RunmodeTelemetryChartTyretemperature    Key = "runmode.telemetry.chart.tyretemperature"
	RunmodeTelemetryChartFuelrange          Key = "runmode.telemetry.chart.fuelrange"
	RunmodeTelemetryChartJerk               Key = "runmode.telemetry.chart.jerk"
	RunmodeTelemetryChartSnap               Key = "runmode.telemetry.chart.snap"
	RunmodeTelemetryChartTranslationalaccel Key = "runmode.telemetry.chart.translationalaccel"
	RunmodeTelemetryChartRotationalaccel    Key = "runmode.telemetry.chart.rotationalaccel"
	RunmodeTelemetryChartSynthoutput        Key = "runmode.telemetry.chart.synthoutput"
	RunmodeTelemetryChartComputetime        Key = "runmode.telemetry.chart.computetime"
	RunmodeTelemetryGraphwindow             Key = "runmode.telemetry.graphwindow"
	RunmodeTelemetryAdjusthint              Key = "runmode.telemetry.adjusthint"
	RunmodeTelemetryReconnect               Key = "runmode.telemetry.reconnect"

	// Run Mode - Settings Restart.
	RunmodeSettingsRestartRequired          Key = "runmode.settings.restart.required"
	RunmodeSettingsRestartOverlayRestarting Key = "runmode.settings.restart.overlay.restarting"
	RunmodeSettingsRestartOverlayTitle      Key = "runmode.settings.restart.overlay.title"
	RunmodeSettingsRestartOverlayPleasewait Key = "runmode.settings.restart.overlay.pleasewait"

	// Run Mode - Settings Confirm.
	RunmodeSettingsConfirmReset     Key = "runmode.settings.confirm.reset"
	RunmodeSettingsConfirmRestart   Key = "runmode.settings.confirm.restart"
	RunmodeSettingsConfirmSetupmode Key = "runmode.settings.confirm.setupmode"

	// Run Mode - Settings Error.
	RunmodeSettingsErrorExportfailed       Key = "runmode.settings.error.exportfailed"
	RunmodeSettingsErrorImportfailed       Key = "runmode.settings.error.importfailed"
	RunmodeSettingsErrorRestartfailed      Key = "runmode.settings.error.restartfailed"
	RunmodeSettingsErrorReconnectfailed    Key = "runmode.settings.error.reconnectfailed"
	RunmodeSettingsErrorSetupmodefailed    Key = "runmode.settings.error.setupmodefailed"
	RunmodeSettingsErrorFactoryresetfailed Key = "runmode.settings.error.factoryresetfailed"
	RunmodeSettingsErrorSaveprofilefailed  Key = "runmode.settings.error.saveprofilefailed"
	RunmodeSettingsErrorLoadconfigfailed   Key = "runmode.settings.error.loadconfigfailed"
	RunmodeSettingsErrorSshkeyrequired     Key = "runmode.settings.error.sshkeyrequired"

	// Run Mode - Settings Success.
	RunmodeSettingsSuccessSshprovisioned Key = "runmode.settings.success.sshprovisioned"

	// Run Mode - Settings Status.
	RunmodeSettingsStatusLoading       Key = "runmode.settings.status.loading"
	RunmodeSettingsStatusSelectprofile Key = "runmode.settings.status.selectprofile"

	// Run Mode - Settings Modal.
	RunmodeSettingsModalWarning           Key = "runmode.settings.modal.warning"
	RunmodeSettingsModalAllappsettings    Key = "runmode.settings.modal.allappsettings"
	RunmodeSettingsModalAllnetworkconfigs Key = "runmode.settings.modal.allnetworkconfigs"
	RunmodeSettingsModalAllsaveddata      Key = "runmode.settings.modal.allsaveddata"
	RunmodeSettingsModalCannotundo        Key = "runmode.settings.modal.cannotundo"
	RunmodeSettingsModalConfirmtext       Key = "runmode.settings.modal.confirmtext"
	RunmodeSettingsModalPlaceholder       Key = "runmode.settings.modal.placeholder"
	RunmodeSettingsModalCancel            Key = "runmode.settings.modal.cancel"
	RunmodeSettingsModalReset             Key = "runmode.settings.modal.reset"
)

// String returns the string representation of the translation key.
func (k Key) String() string {
	return string(k)
}

// ToLower returns a new translation key with all characters converted to lowercase.
func (k Key) ToLower() Key {
	return Key(strings.ToLower(string(k)))
}

// StringToKey converts a string to a Key type.
// It returns an error if the string does not correspond to a valid Key.
func StringToKey(str string) (Key, error) {
	key := Key(str)

	if key == "" {
		return "", fmt.Errorf("invalid translation key: %q", str)
	}

	return key, nil
}

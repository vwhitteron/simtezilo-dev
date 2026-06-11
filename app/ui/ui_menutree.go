package ui

import "github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"

// The menu is declared as a nested literal of the constructors below and
// finalised by buildMenu, which derives parent pointers, injects Return items,
// and inherits visibility context. Editing the menu means editing the literal in
// newMenuTree: move a line to move a node, add or remove a line to add or remove
// one — the wiring is derived, not maintained by hand.

// newMenuTree declares the full menu hierarchy and returns its finalised root.
func newMenuTree() *MenuNode {
	root := branch(menuNodeRoot,
		branch(languagedb.UIMenuLive,
			liveLeaf(languagedb.UIMenuLiveView),
			liveLeaf(languagedb.UIMenuLiveDashboard),
		),
		branch(languagedb.UIMenuSettings,
			branch(languagedb.UIMenuApp,
				leaf(languagedb.UIMenuAppLanguage),
				leaf(languagedb.UIMenuAppLoglevel),
				leaf(languagedb.UIMenuAppTelemetrySource),
				leaf(languagedb.UIMenuAppDevtools),
			),
			branch(languagedb.UIMenuSystem,
				leaf(languagedb.UIMenuSystemDisplayOrientation),
				leaf(languagedb.UIMenuSystemSetupmode),
			),
			branch(languagedb.UIMenuSynth,
				branch(languagedb.UIMenuSynthSampleRates,
					leaf(languagedb.UIMenuSynthInternalSampleRate),
					leaf(languagedb.UIMenuSynthOutputSampleRate),
				),
				branch(languagedb.UIMenuSynthMute,
					leaf(languagedb.UIMenuSynthMuteMaster),
					leaf(languagedb.UIMenuSynthMuteLeft),
					leaf(languagedb.UIMenuSynthMuteRight),
					leaf(languagedb.UIMenuSynthMuteChassis),
					leaf(languagedb.UIMenuSynthMuteEngine),
					leaf(languagedb.UIMenuSynthMuteTransmission),
				),
				branch(languagedb.UIMenuSynthGainControls,
					leaf(languagedb.UIMenuSynthMasterGain),
					leaf(languagedb.UIMenuSynthLeftGain),
					leaf(languagedb.UIMenuSynthRightGain),
					leaf(languagedb.UIMenuSynthChassisGain),
					leaf(languagedb.UIMenuSynthEngineGain),
					leaf(languagedb.UIMenuSynthTransmissionGain),
					leaf(languagedb.UIMenuSynthTransmissionGainMinRace),
					leaf(languagedb.UIMenuSynthTransmissionGainMinStreet),
					leaf(languagedb.UIMenuSynthEqMode),
					leaf(languagedb.UIMenuSynthDrx),
					branch(languagedb.UIMenuSynthCalibration,
						leaf(languagedb.UIMenuSynthCalibrationEnable),
						leaf(languagedb.UIMenuSynthCalibrationChannel),
						leaf(languagedb.UIMenuSynthCalibrationFrequency),
						leaf(languagedb.UIMenuSynthCalibrationSweep),
						leaf(languagedb.UIMenuSynthCalibrationSweepRange),
					),
				),
			),
			branch(languagedb.UIMenuHaptics,
				leaf(languagedb.UIMenuHapticsOutputMode),
				branch(languagedb.UIMenuHapticsChassis,
					leaf(languagedb.UIMenuHapticsJerkCurve),
					leaf(languagedb.UIMenuHapticsJerkMax),
					leaf(languagedb.UIMenuHapticsSnapCurve),
					leaf(languagedb.UIMenuHapticsSnapMax),
					leaf(languagedb.UIMenuHapticsPulseMaxAmplitude),
					leaf(languagedb.UIMenuHapticsPulseMinFreq),
					leaf(languagedb.UIMenuHapticsPulseMaxFreq),
				),
				branch(languagedb.UIMenuHapticsTransmission,
					leaf(languagedb.UIMenuHapticsTransmissionFFBStrength),
					leaf(languagedb.UIMenuHapticsTransmissionCurve),
					leaf(languagedb.UIMenuHapticsTransmissionGforceMax),
				),
				branch(languagedb.UIMenuHapticsEngineProfile,
					leaf(languagedb.UIMenuHapticsEnginePrimaryBalance),
					leaf(languagedb.UIMenuHapticsEngineSecondaryBalance),
					leaf(languagedb.UIMenuHapticsEnginePulseGain),
					leaf(languagedb.UIMenuHapticsEnginePulseScale),
				),
			),
			branch(languagedb.UIMenuPitRadio,
				leaf(languagedb.UIMenuPitRadioEnable),
				branch(languagedb.UIMenuPitRadioNotifications,
					branch(languagedb.UIMenuPitRadioLapTimes,
						leaf(languagedb.UIMenuPitRadioLapTimesEnable),
						leaf(languagedb.UIMenuPitRadioLapTimesMaxDelta),
					),
					branch(languagedb.UIMenuPitRadioRaceLaps,
						leaf(languagedb.UIMenuPitRadioRaceLapsEnable),
						leaf(languagedb.UIMenuPitRadioRaceLapsCountdown),
						leaf(languagedb.UIMenuPitRadioRaceLapsInterval),
					),
					branch(languagedb.UIMenuPitRadioRaceProgress,
						leaf(languagedb.UIMenuPitRadioRaceProgressEnable),
						leaf(languagedb.UIMenuPitRadioRaceProgressMinLaps),
						leaf(languagedb.UIMenuPitRadioRaceProgressInterval),
					),
				),
				branch(languagedb.UIMenuPitRadioFuel,
					leaf(languagedb.UIMenuPitRadioFuelEnable),
					leaf(languagedb.UIMenuPitRadioFuelPreWarn),
					leaf(languagedb.UIMenuPitRadioFuelStrategy),
					leaf(languagedb.UIMenuPitRadioFuelSafetyLaps),
					leaf(languagedb.UIMenuPitRadioFuelSafetyMetres),
				),
				branch(languagedb.UIMenuPitRadioTyre,
					leaf(languagedb.UIMenuPitRadioTyreEnable),
					leaf(languagedb.UIMenuPitRadioTyreTempOptimal),
					leaf(languagedb.UIMenuPitRadioTyreTempWindow),
					leaf(languagedb.UIMenuPitRadioTyreTempMargin),
				),
			),
		),
		infoBranch(languagedb.UIMenuInfo,
			leaf(languagedb.UIMenuInfoVersion),
			leaf(languagedb.UIMenuInfoCommitHash),
			leaf(languagedb.UIMenuInfoBuildTime),
			leaf(languagedb.UIMenuInfoPlatform),
			leaf(languagedb.UIMenuInfoIPAddress),
		),
		devtools(branch(languagedb.UIMenuDevtools,
			leaf(languagedb.UIMenuDevtoolsRecord),
		)),
	)

	buildMenu(root)

	return root
}

// branch builds a submenu. buildMenu injects a Return child unless noReturn is set.
func branch(name languagedb.Key, children ...*MenuNode) *MenuNode {
	return &MenuNode{name: name, nodeType: NodeTypeBranch, kind: KindSetting, children: children}
}

// leaf builds an editable setting leaf.
func leaf(name languagedb.Key) *MenuNode {
	return &MenuNode{name: name, nodeType: NodeTypeLeaf, kind: KindSetting}
}

// liveLeaf builds a live-view leaf.
func liveLeaf(name languagedb.Key) *MenuNode {
	return &MenuNode{name: name, nodeType: NodeTypeLeaf, kind: KindLive}
}

// infoBranch builds a branch of read-only info pages. Its leaf children are
// tagged KindInfo and it has no Return item (info leaves exit on up/down).
func infoBranch(name languagedb.Key, children ...*MenuNode) *MenuNode {
	n := branch(name, children...)
	n.noReturn = true

	for _, c := range n.children {
		if c.nodeType == NodeTypeLeaf {
			c.kind = KindInfo
		}
	}

	return n
}

// devtools marks a node visible only when dev tools are enabled. buildMenu
// inherits the context to its descendants (including the injected Return).
func devtools(n *MenuNode) *MenuNode {
	n.context = PageContextDevTools

	return n
}

// buildMenu finalises a declared tree: it injects a Return child into every
// non-root branch (except those marked noReturn), sets parent pointers, and
// inherits the visibility context from each node to its children.
func buildMenu(root *MenuNode) {
	if root.context == "" {
		root.context = PageContextAlways
	}

	resolveMenu(root, true)
}

func resolveMenu(n *MenuNode, isRoot bool) {
	if n.nodeType == NodeTypeBranch && !isRoot && !n.noReturn {
		// The Return item is itself a branch but is terminal: noReturn stops it
		// from recursively gaining its own Return child.
		ret := &MenuNode{name: languagedb.UIMenuReturn, nodeType: NodeTypeBranch, context: n.context, noReturn: true}
		n.children = append([]*MenuNode{ret}, n.children...)
	}

	for _, c := range n.children {
		if c.context == "" {
			c.context = n.context
		}

		c.parent = n

		resolveMenu(c, false)
	}
}

// find returns the first node with the given name in depth-first order, or nil.
func find(n *MenuNode, name languagedb.Key) *MenuNode {
	if n.name == name {
		return n
	}

	for _, c := range n.children {
		if got := find(c, name); got != nil {
			return got
		}
	}

	return nil
}

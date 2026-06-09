package ui

import "github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"

// The menu is declared as a nested literal of the constructors below and
// finalised by MenuNode.build, which derives parent pointers, injects Return items,
// and inherits visibility context. Editing the menu means editing the literal in
// newMenuTree: move a line to move a node, add or remove a line to add or remove
// one — the wiring is derived, not maintained by hand.

// newMenuTree declares the full menu hierarchy and returns its finalised root.
func newMenuTree() *MenuNode {
	menu := branch(menuNodeRoot,
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
				leaf(languagedb.UIMenuAppExperimental),
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
			// The fan/wind simulator is experimental, so the whole submenu is gated
			// behind the experimental features flag.
			branch(languagedb.UIMenuFan,
				leaf(languagedb.UIMenuFanEnable),
				leaf(languagedb.UIMenuFanManualSpeed),
				leaf(languagedb.UIMenuFanMode),
				leaf(languagedb.UIMenuFanWindSimMaxSpeed),
				leaf(languagedb.UIMenuFanCommandTimeout),
			).setVisibility(PageContextExperimental),
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
		branch(languagedb.UIMenuInfo,
			infoLeaf(languagedb.UIMenuInfoVersion),
			infoLeaf(languagedb.UIMenuInfoCommitHash),
			infoLeaf(languagedb.UIMenuInfoBuildTime),
			infoLeaf(languagedb.UIMenuInfoPlatform),
			infoLeaf(languagedb.UIMenuInfoIPAddress),
		),
		branch(languagedb.UIMenuDevtools,
			leaf(languagedb.UIMenuDevtoolsRecord),
		).setVisibility(PageContextDevTools),
	)

	menu.build()

	return menu
}

// branch builds a submenu. build injects a Return child unless noReturn is set.
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

// infoLeaf builds a read-only info leaf. It renders with LayoutInfo and, being
// read-only, ignores up/down value adjustment; exit is via the branch's Return.
func infoLeaf(name languagedb.Key) *MenuNode {
	return &MenuNode{name: name, nodeType: NodeTypeLeaf, kind: KindInfo}
}

// buildSubtree recursively finalises a subtree: it prepends a Return child to each
// non-root branch, then links every child to its parent and inherits its context.
// isRoot suppresses the Return injection at the top of the tree.
func buildSubtree(node *MenuNode, isRoot bool) {
	if node.nodeType == NodeTypeBranch && !isRoot && !node.noReturn {
		// The Return item is itself a branch but is terminal: noReturn stops it
		// from recursively gaining its own Return child.
		ret := &MenuNode{name: languagedb.UIMenuReturn, nodeType: NodeTypeBranch, context: node.context, noReturn: true}
		node.children = append([]*MenuNode{ret}, node.children...)
	}

	for _, child := range node.children {
		if child.context == "" {
			child.context = node.context
		}

		child.parent = node

		buildSubtree(child, false)
	}
}

// build finalises a declared tree: it injects a Return child into every
// non-root branch (except those marked noReturn), sets parent pointers, and
// inherits the visibility context from each node to its children.
func (node *MenuNode) build() {
	if node.context == "" {
		node.context = PageContextAlways
	}

	buildSubtree(node, true)
}

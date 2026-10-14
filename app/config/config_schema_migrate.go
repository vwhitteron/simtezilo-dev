package config

import (
	"encoding/json"
	"fmt"
	"math"
)

// Schema versions. legacySchemaVersion is the last version that stored the five
// inverted haptics settings as integers in the old direction. invertedSchemaVersion
// is the last version that stored the surfaceRumble level and coarseness in
// internal units, rather than as 0 to 100 settings.
const (
	legacySchemaVersion   = "1.0.0"
	invertedSchemaVersion = "1.1.0"
	currentSchemaVersion  = "1.2.0"
)

// legacyHapticsJerkCenterMin and legacyHapticsJerkCenterMax bound the
// pre-inversion jerkCenter field, which stored a plain jerk in m/s^3
// directly. They are used only by migrateJerkMax, which still operates on
// pre-migration (schema 1.0.0 and earlier) values before migrateInvertedSettings
// converts them to the new setting.
const (
	legacyHapticsJerkCenterMin = 1
	legacyHapticsJerkCenterMax = 20000
)

// legacyHapticsSnapCenterMin and legacyHapticsSnapCenterMax bound the
// pre-inversion snapCenter field, in units of hapticsSnapCenterUnitMs4 m/s^4.
// They are used only by migrateSnapMax, which still operates on pre-migration
// (schema 1.0.0 and earlier) values before migrateInvertedSettings converts them
// to the new setting.
const (
	legacyHapticsSnapCenterMin = 5
	legacyHapticsSnapCenterMax = 995
)

// legacyDefaultJerkCompression, legacyDefaultJerkCenter,
// legacyDefaultSnapCompression, legacyDefaultSnapCenter and
// legacyDefaultDynamicTransmissionJerkCompression are the pre-1.1.0 shipped
// defaults for the five settings migrateInvertedSettings inverts. Each maps
// exactly onto its current-direction default (see defaultConfig) through the
// v = clamp(round((1000 - o) / 10)) formula, so baseConfigFor can lay them
// under a legacy file without migrateInvertedSettings double-inverting a key
// the file happens to omit.
const (
	legacyDefaultJerkCompression                    = 255
	legacyDefaultJerkCenter                         = 624
	legacyDefaultSnapCompression                    = 410
	legacyDefaultSnapCenter                         = 353
	legacyDefaultDynamicTransmissionJerkCompression = 750
)

// legacyDefaultSurfaceRumble returns the pre-1.2.0 shipped surfaceRumble defaults,
// in the internal units those versions stored: level as a loudness from 0 to 2 and
// coarseness as a low-pass cutoff multiplier from 0.1 to 2. baseConfigFor lays
// them under a pre-1.2.0 file, so a surface the file omits keeps the sound it had
// and is converted once. The current defaults (see defaultConfig) are tuned on
// the new scale, so they can differ slightly from these values once converted.
func legacyDefaultSurfaceRumble() map[string]SurfaceRumble {
	return map[string]SurfaceRumble{
		"tarmac":   {Level: 0.40, Coarseness: 1.00},
		"concrete": {Level: 0.50, Coarseness: 0.95},
		"grass":    {Level: 0.65, Coarseness: 0.85},
		"dirt":     {Level: 0.85, Coarseness: 0.80},
		"sand":     {Level: 0.30, Coarseness: 1.20},
		"snow":     {Level: 0.60, Coarseness: 0.90},
	}
}

// baseConfigFor returns the defaults a config file at schemaVersion should be
// unmarshalled over. A file with no schemaVersion is one every current build
// stamps on write, so its absence means the file predates schema versioning
// and, like an explicit "1.0.0", is legacy: the five settings inverted by
// migrateInvertedSettings are backfilled with their pre-1.1.0 defaults (rather
// than the new-direction defaults defaultConfig already holds) and
// SchemaVersion is forced to legacySchemaVersion, so a key the file omits
// keeps its legacy value instead of being inverted a second time, and
// migrateInvertedSettings still runs even though the file never named a
// version.
//
// Any file below currentSchemaVersion, including a 1.1.0 one, also gets the
// pre-1.2.0 surfaceRumble defaults, for the same reason: a surface the file
// omits is converted by migrateSurfaceRumbleScale from its old default instead
// of keeping the new default and being converted twice. Unmarshalling a file's
// surfaceRumble map over this one merges by surface name.
func baseConfigFor(schemaVersion string) *viperConfig {
	base := defaultConfig()

	if schemaVersion != "" && schemaVersion != legacySchemaVersion && schemaVersion != invertedSchemaVersion {
		return base
	}

	base.Haptics.SurfaceRumble = legacyDefaultSurfaceRumble()

	if schemaVersion == invertedSchemaVersion {
		base.SchemaVersion = invertedSchemaVersion

		return base
	}

	base.SchemaVersion = legacySchemaVersion
	base.Haptics.JerkCompression = legacyDefaultJerkCompression
	base.Haptics.JerkCenter = legacyDefaultJerkCenter
	base.Haptics.SnapCompression = legacyDefaultSnapCompression
	base.Haptics.SnapCenter = legacyDefaultSnapCenter
	base.Haptics.DynamicTransmissionJerkCompression = legacyDefaultDynamicTransmissionJerkCompression

	return base
}

// schemaVersionFromJSON reads the schemaVersion header field from raw config
// JSON without unmarshalling the rest of the document, so callers can pick
// the right base config (see baseConfigFor) before doing the full unmarshal.
func schemaVersionFromJSON(jsonData []byte) (string, error) {
	var header struct {
		SchemaVersion string `json:"schemaVersion"`
	}

	err := json.Unmarshal(jsonData, &header)
	if err != nil {
		return "", fmt.Errorf("reading schema version: %w", err)
	}

	return header.SchemaVersion, nil
}

// migrateSchema brings a config loaded from an older schema up to the current one. It
// is the single entry point for every config migration, so finalise and the
// import check run the same steps in the same order.
//
// Caller must hold c.mu.
func (c *Config) migrateSchema() {
	// Fold any surviving legacy key names into their renamed field before the
	// jerkMax/snapMax migrations, which read the renamed fields.
	c.migrateLegacyKeys()

	// Fold any deprecated jerkMax and snapMax value into its center. Both work
	// on the old-style values, so they run before the settings are inverted.
	c.migrateJerkMax()
	c.migrateSnapMax()

	// Invert and rescale the five haptics settings from a pre-1.1.0 schema.
	c.migrateInvertedSettings()

	// Rescale the surface rumble entries from a pre-1.2.0 schema. This runs after
	// the inversion, which stamps 1.1.0, so a 1.0.0 config passes through both.
	c.migrateSurfaceRumbleScale()
}

// migrateLegacyKeys copies any surviving key renamed since the v0.8.0 public
// release onto its renamed field and then clears it, so omitempty drops the
// old key from the file on the next write and the migration runs at most
// once. JerkCurve and SnapCurve are pointers so a legitimate zero value is
// still distinguishable from an absent legacy key.
//
// Caller must hold c.mu.
func (c *Config) migrateLegacyKeys() {
	haptics := c.viper.Haptics

	if haptics.JerkCurve != nil {
		haptics.JerkCompression = float64(*haptics.JerkCurve)
		haptics.JerkCurve = nil
	}

	if haptics.SnapCurve != nil {
		haptics.SnapCompression = float64(*haptics.SnapCurve)
		haptics.SnapCurve = nil
	}

	if haptics.DynamicTransmissionJerkCurve != nil {
		haptics.DynamicTransmissionJerkCompression = float64(*haptics.DynamicTransmissionJerkCurve)
		haptics.DynamicTransmissionJerkCurve = nil
	}
}

// migrateJerkMax converts a surviving jerkMax value into the equivalent center and
// then clears it, so the conversion runs at most once and omitempty drops the key
// on the next write.
//
// jerkMax named the full-scale jerk directly, so the center that reproduces the
// same curve is the jerk at which that curve has fallen to the fixed bias:
//
//	center = jerkMax * 10^(hapticsJerkBiasDB/(20*exponent))
//
// This runs before migrateInvertedSettings, so JerkCompression and JerkCenter
// still hold their pre-1.1.0 (old-integer) values here: JerkCompression in
// thousandths and JerkCenter as a plain m/s^3 figure.
//
// Caller must hold c.mu.
func (c *Config) migrateJerkMax() {
	if c.viper.Haptics.JerkMax <= 0 {
		return
	}

	exponent := c.viper.Haptics.JerkCompression / 1000.0
	if exponent <= 0 {
		c.viper.Haptics.JerkMax = 0

		return
	}

	// jerkMax counted in hundreds of m/s^3; the center is a plain m/s^3 figure.
	jerkMax := 100 * float64(c.viper.Haptics.JerkMax)
	biasFrac := hapticsJerkBiasDB / 20
	center := jerkMax * math.Pow(10, biasFrac/exponent)

	c.viper.Haptics.JerkCenter = min(float64(legacyHapticsJerkCenterMax), max(float64(legacyHapticsJerkCenterMin), math.Round(center)))
	c.viper.Haptics.JerkMax = 0
}

// migrateSnapMax converts a surviving snapMax value into the equivalent center
// and then clears it, so the conversion runs at most once and omitempty drops
// the key on the next write.
//
// snapMax named the full-scale snap directly, so the center that reproduces the
// same scale is:
//
//	center = 1000 * snapMax * (hapticsSnapBiasHz / (pulseMaxHz - pulseMinHz))^(1/exponent)
//
// converted from m/s^4 into the setting's hapticsSnapCenterUnitMs4 units.
//
// This runs before migrateInvertedSettings, so SnapCompression and SnapCenter
// still hold their pre-1.1.0 (old-integer) values here: SnapCompression in
// thousandths and SnapCenter in units of hapticsSnapCenterUnitMs4 m/s^4.
//
// Caller must hold c.mu.
func (c *Config) migrateSnapMax() {
	if c.viper.Haptics.SnapMax <= 0 {
		return
	}

	exponent := c.viper.Haptics.SnapCompression / 1000.0
	hzRange := c.viper.Haptics.PulseMaxFrequencyHz - c.viper.Haptics.PulseMinFrequencyHz

	if exponent <= 0 || hzRange <= 0 {
		c.viper.Haptics.SnapMax = 0

		return
	}

	// snapMax counted in thousands of m/s^4; the center is a plain m/s^4 figure.
	snapMax := 1000 * float64(c.viper.Haptics.SnapMax)
	centerMs4 := snapMax * math.Pow(hapticsSnapBiasHz/hzRange, 1/exponent)

	c.viper.Haptics.SnapCenter = min(float64(legacyHapticsSnapCenterMax), max(float64(legacyHapticsSnapCenterMin), math.Round(centerMs4/hapticsSnapCenterUnitMs4)))
	c.viper.Haptics.SnapMax = 0
}

// migrateInvertedSettings converts the five haptics settings (jerkCompression,
// jerkCenter, snapCompression, snapCenter, dynamicTransmissionJerkCompression)
// from their pre-1.1.0 direction to the current one: a setting value v in
// [hapticsSettingMin, hapticsSettingMax] where higher v means more
// compression (or a lower center), replacing the old value o via
//
//	v = clamp((1000 - o) / 10, hapticsSettingMin, hapticsSettingMax)
//
// This runs once, gated on SchemaVersion: a config at "1.0.0" is converted and
// then stamped invertedSchemaVersion ("1.1.0"); a config already at "1.1.0" or later is left
// untouched. Every loader lays a legacy file over baseConfigFor's
// old-direction defaults and stamps its SchemaVersion to legacySchemaVersion
// before this runs, so "" is not a version this method should see in
// practice; the check is kept only as a defensive fallback for a
// viperConfig built by hand rather than by one of those loaders.
// dynamicTransmissionJerkCompression is a special case: a value of zero means
// "unset, use the shipped default" both before and after inversion, so it is
// left at zero rather than converted.
//
// Caller must hold c.mu.
func (c *Config) migrateInvertedSettings() {
	if c.viper.SchemaVersion != "" && c.viper.SchemaVersion != legacySchemaVersion {
		return
	}

	invert := func(o float64) float64 {
		return clampSetting(roundSetting((1000 - o) / 10))
	}

	c.viper.Haptics.JerkCompression = invert(c.viper.Haptics.JerkCompression)
	c.viper.Haptics.JerkCenter = invert(c.viper.Haptics.JerkCenter)
	c.viper.Haptics.SnapCompression = invert(c.viper.Haptics.SnapCompression)
	c.viper.Haptics.SnapCenter = invert(c.viper.Haptics.SnapCenter)

	if c.viper.Haptics.DynamicTransmissionJerkCompression > 0 {
		c.viper.Haptics.DynamicTransmissionJerkCompression = invert(c.viper.Haptics.DynamicTransmissionJerkCompression)
	}

	c.viper.SchemaVersion = invertedSchemaVersion
}

// migrateSurfaceRumbleScale converts every surfaceRumble entry from the internal
// units of schema 1.1.0 and earlier to the 0 to 100 settings: level from a
// loudness of 0 to 2, and coarseness from a low-pass cutoff multiplier of 0.1 to
// 2 where lower was coarser, via
//
//	level      = clamp(round2(oldLevel * 50))
//	coarseness = clamp(round2((2 - oldCoarseness) / 1.9 * 100))
//
// so a higher coarseness now means a coarser grain. It runs once, gated on
// SchemaVersion: a config at "", "1.0.0" or "1.1.0" is converted and stamped
// currentSchemaVersion; a config already at "1.2.0" or later is left untouched.
// migrateInvertedSettings has stamped a 1.0.0 config "1.1.0" by the time this
// runs.
//
// Caller must hold c.mu.
func (c *Config) migrateSurfaceRumbleScale() {
	switch c.viper.SchemaVersion {
	case "", legacySchemaVersion, invertedSchemaVersion:
	default:
		return
	}

	for surface, rumble := range c.viper.Haptics.SurfaceRumble {
		c.viper.Haptics.SurfaceRumble[surface] = SurfaceRumble{
			Level:      roundSurfaceRumble(rumble.Level * surfaceRumbleLevelDivisor),
			Coarseness: roundSurfaceRumble((surfaceRumbleCutoffMax - rumble.Coarseness) / surfaceRumbleCutoffSpan * surfaceRumbleMax),
		}
	}

	c.viper.SchemaVersion = currentSchemaVersion
}

// upgradeLegacyJSON returns jsonData as loading it would leave it. A config at
// legacySchemaVersion or invertedSchemaVersion is laid over baseConfigFor's
// pre-current defaults and
// put through migrateSchema, as finalise does, so an imported backup
// validates against the current schema. Any other config, including one with
// no schemaVersion, is returned unchanged so the schema still reports what is
// wrong with it: unlike the loaders in config.go, which must treat a missing
// version as legacy because every file the app writes stamps one, a file
// handed to ValidateConfig with no version at all is invalid input, not a
// legacy config, and reporting the missing field is more useful than
// silently upgrading it.
func upgradeLegacyJSON(jsonData []byte) ([]byte, error) {
	schemaVersion, err := schemaVersionFromJSON(jsonData)
	if err != nil {
		return nil, err
	}

	if schemaVersion != legacySchemaVersion && schemaVersion != invertedSchemaVersion {
		return jsonData, nil
	}

	legacy := &Config{viper: baseConfigFor(schemaVersion)}

	err = json.Unmarshal(jsonData, legacy.viper)
	if err != nil {
		return nil, fmt.Errorf("reading legacy config: %w", err)
	}

	legacy.migrateSchema()

	upgraded, err := json.Marshal(legacy.viper)
	if err != nil {
		return nil, fmt.Errorf("writing upgraded config: %w", err)
	}

	return upgraded, nil
}

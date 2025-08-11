package en

const (
	Code             = "en"
	Name             = "English"
	RegularFont      = "LeagueGothic-Regular.ttf"
	RegularFontScale = 1.0
	ItalicFont       = "LeagueGothic-Italic.ttf"
	ItalicFontScale  = 1.0
	ValueFont        = "LeagueGothic-Regular.ttf"
	ValueFontScale   = 1.0
)

var Translations = map[string]string{
	"app.name":        "Simtezilo",
	"app.description": "Sim Racing Haptics Synthesizer",
	"app.version":     "version",

	"ui.error":    "error",
	"ui.success":  "success",
	"ui.quit":     "goodbye",
	"ui.starting": "starting",
	"ui.stopping": "stopping",
	"ui.loading":  "loading",
	"ui.waiting":  "waiting",
	"ui.ready":    "ready",
	"ui.settings": "settings",

	"ui.menu.vol":    "Master Gain",
	"ui.menu.cvol":   "Chassis Vol",
	"ui.menu.tvol":   "Trans Vol",
	"ui.menu.vcurve": "FFB Curve",
	"ui.menu.vsat":   "FFB Saturation",
	"ui.menu.fcurve": "Freq Curve",
	"ui.menu.fsat":   "Freq Saturation",
	"ui.menu.fmin":   "Freq Min",
	"ui.menu.fmax":   "Freq Max",
	"ui.menu.tcurve": "Trans Curve",
	"ui.menu.tsat":   "Trans Saturation",
	"ui.menu.mix":    "Mix Algo",
	"ui.menu.lang":   "Language",
}

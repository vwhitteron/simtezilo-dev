package jp

const (
	Code             = "jp"
	Name             = "日本語"
	RegularFont      = "LINESeedJP_TTF_Bd.ttf"
	RegularFontScale = 0.8
	ItalicFont       = "LINESeedJP_TTF_Bd.ttf"
	ItalicFontScale  = 0.8
	ValueFont        = "LeagueGothic-Regular.ttf"
	ValueFontScale   = 1.0
)

var Translations = map[string]string{
	"app.name":        "シムテジロ",
	"app.description": "シンセサイザーのエミュレーション",
	"app.version":     "バージョン",

	"ui.error":    "エラー",
	"ui.success":  "成功",
	"ui.quit":     "終了",
	"ui.starting": "開始",
	"ui.stopping": "停止",
	"ui.loading":  "読み込み中",
	"ui.waiting":  "待機中",
	"ui.ready":    "準備完了",
	"ui.settings": "設定",

	"ui.menu.vol":    "音量",
	"ui.menu.cvol":   "シャーシ音量",
	"ui.menu.tvol":   "ギア音量",
	"ui.menu.evol":   "エンジン音量",
	"ui.menu.vcurve": "FFB曲線",
	"ui.menu.vsat":   "FFB飽和",
	"ui.menu.fcurve": "周波数曲線",
	"ui.menu.fsat":   "周波数飽和",
	"ui.menu.fmin":   "周波数最小",
	"ui.menu.fmax":   "周波数最大",
	"ui.menu.tcurve": "ギア曲線",
	"ui.menu.tsat":   "ギア飽和",
	"ui.menu.mix":    "ミキサーアルゴ",
	"ui.menu.lang":   "言語",
}

// //nolint:gosmopolitan // contains many string literals containing Japanese characters
package jp

import "github.com/vwhitteron/simtezilo-dev/app/i18n/translations"

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

var Translations = map[translations.Key]string{
	translations.AppName:        "シムテジロ",
	translations.AppDescription: "シンセサイザーのエミュレーション",
	translations.AppVersion:     "バージョン",

	translations.UIError:    "エラー",
	translations.UISuccess:  "成功",
	translations.UIQuit:     "終了",
	translations.UIStarting: "開始",
	translations.UIStopping: "停止",
	translations.UILoading:  "読み込み中",
	translations.UIWaiting:  "待機中",
	translations.UIReady:    "準備完了",
	translations.UISettings: "設定",

	translations.UIMenuVol:        "音量",
	translations.UIMenuCVol:       "シャーシ音量",
	translations.UIMenuTVol:       "ギア音量",
	translations.UIMenuEVol:       "エンジン音量",
	translations.UIMenuEPrimary:   "エンジンバランス1",
	translations.UIMenuESecondary: "エンジンバランス2",
	translations.UIMenuEPVol:      "エンジン音量",
	translations.UIMenuEPScale:    "エンジンパルススケール",
	translations.UIMenuVCurve:     "FFB曲線",
	translations.UIMenuVSat:       "FFB飽和",
	translations.UIMenuFCurve:     "周波数曲線",
	translations.UIMenuFSat:       "周波数飽和",
	translations.UIMenuFMin:       "周波数最小",
	translations.UIMenuFMax:       "周波数最大",
	translations.UIMenuTCurve:     "ギア曲線",
	translations.UIMenuTSat:       "ギア飽和",
	translations.UIMenuMix:        "ミキサーアルゴ",
	translations.UIMenuLang:       "言語",

	translations.RadioOnline:           "無線チェック",
	translations.RadioLapRecord:        "最速ラップ",
	translations.RadioFuelRangeFmt:     "燃料範囲は%d周。残り%d周",
	translations.RadioFuelPreWarnFmt:   "%d周で燃料補給",
	translations.RadioBoxForFuel:       "ボックスこのラップを燃料用に",
	translations.RadioFuelCritical:     "燃料危機的、マップ6",
	translations.RadioFuelCriticalBox:  "燃料危機的、マップ6 ボックスボックスボックス",
	translations.RadioOutOfFuelLastLap: "燃料切れで予備燃料に切り替えて、そのままフィニッシュ",
	translations.RadioOutOfFuelBox:     "燃料切れ、予備に切り替え、すぐにボックス",
	translations.RadioLapsRemainingFmt: "%dラップ残り",
	translations.RadioFinalLap:         "最終ラップ",
	translations.RadioRaceProgressFmt:  "レース進行状況 %d%%",
	translations.RadioRaceFinish:       "レース終了",
}

package ui

type HIDInputEvent int

const (
	HIDInputNone HIDInputEvent = iota
	HIDInputUp
	HIDInputDown
	HIDInputLeft
	HIDInputRight
	HIDInputPageUp
	HIDInputPageDown
	HIDInputHome
	HIDInputEnd
	HIDInputEnter
	HIDInputTab
	HIDInputEscape
	HIDInputPower
)

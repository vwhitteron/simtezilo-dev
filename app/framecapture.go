package app

import (
	"image"

	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

// frameTapDisplay wraps a hardware.Display and forwards a copy of every canvas
// written to the display to an optional sink. It exists so the web UI can mirror
// the rendered hardware screen without touching any of the render code paths.
type frameTapDisplay struct {
	hardware.Display

	sink func(*image.RGBA)
}

// Write forwards the canvas to the sink before handing it to the real display.
// The sink runs synchronously, so it must stay cheap (hash + clone only).
func (t *frameTapDisplay) Write(content *display.Content) error {
	if t.sink != nil && content != nil && content.Canvas != nil {
		t.sink(content.Canvas)
	}

	return t.Display.Write(content)
}

// wrapDisplayFrameTap returns the display wrapped so that every rendered frame is
// mirrored to the web UI screen feed.
//

func (a *App) wrapDisplayFrameTap(d hardware.Display) hardware.Display {
	return &frameTapDisplay{Display: d, sink: a.captureScreenFrame}
}

// captureScreenFrame mirrors a rendered display frame to the web UI. It is called
// from both the display tick and the HID event goroutine, so it must be safe to
// call concurrently and must never lose the most recent frame: the menu screens
// are rendered by a single write at a moment when the live-view stream may have
// the feed saturated, and once in the menu no further frames are produced. The
// feed therefore keeps only the latest frame (older pending frames are discarded),
// guaranteeing the final rendered screen always reaches the browser. De-duplication
// of identical frames happens in the web UI broadcaster.
func (a *App) captureScreenFrame(canvas *image.RGBA) {
	if a.screenFrameFeed == nil {
		return
	}

	// The render code allocates a fresh canvas per frame, but the display device
	// retains the reference (e.g. for orientation redraws), so clone before the
	// frame is handed off to be encoded on another goroutine.
	clone := image.NewRGBA(canvas.Bounds())
	copy(clone.Pix, canvas.Pix)

	// The ST7789 panel ignores the alpha channel and reads RGB directly onto an
	// opaque black panel. The render code draws text (menus, gear view) with
	// near-zero alpha, which only composites correctly against black; encoding the
	// canvas straight to PNG would leave that text transparent and invisible in the
	// browser. Force every pixel opaque to mirror the panel's appearance. (The
	// dashboard live view uses fully opaque colours, which is why it shows up
	// regardless.)
	for i := 3; i < len(clone.Pix); i += 4 {
		clone.Pix[i] = 0xff
	}

	// Latest-wins: enqueue the new frame, discarding any pending frame if the feed
	// is full so the most recent screen is never dropped.
	for {
		select {
		case a.screenFrameFeed <- clone:
			return
		default:
			select {
			case <-a.screenFrameFeed:
				// Discarded a stale pending frame; retry the send.
			default:
				// Feed drained by the consumer in the meantime; retry the send.
			}
		}
	}
}

// sendHIDInput injects a HID input event from the web UI hardware view, emulating
// a press of the corresponding physical button. The key names match the on-screen
// buttons and their keyboard shortcuts: up, down, left, right, enter, and the
// auxiliary buttons 1, 2 and 3. It returns false for an unknown key or when the
// event queue is full.
func (a *App) sendHIDInput(key string) bool {
	event, known := hidKeyAction(key)
	if !known {
		return false
	}

	if a.hidEvents == nil {
		return false
	}

	select {
	case a.hidEvents <- event:
		return true
	default:
		return false
	}
}

// hidKeyMap maps a web UI key name to its HID input event.
// Button 1 → Escape, Button 2 → None, Button 3 → Power.
var hidKeyMap = map[string]ui.HIDInputEvent{ //nolint:gochecknoglobals // unavoidable global to avoid allocations on hidKeyAction() calls
	"up":    ui.HIDInputUp,
	"down":  ui.HIDInputDown,
	"left":  ui.HIDInputLeft,
	"right": ui.HIDInputRight,
	"enter": ui.HIDInputEnter,
	"1":     ui.HIDInputEscape,
	"2":     ui.HIDInputNone,
	"3":     ui.HIDInputPower,
}

// hidKeyAction returns the HID input event for a web UI key name.
// Returns (event, true) on a known key, or (0, false) otherwise.
func hidKeyAction(key string) (ui.HIDInputEvent, bool) {
	event, known := hidKeyMap[key]

	return event, known
}

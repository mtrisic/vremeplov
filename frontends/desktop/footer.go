package main

// The footer: a clickable button bar above the status line, mirroring
// the TUI's footer chrome. Every button doubles as an F-key; clicks
// and keys share one dispatch (chromeAction). Labels and colors follow
// the machine state (pause/resume, record red while armed, quit turns
// into "sure?" awaiting confirmation).

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/mtrisic/vremeplov/core"
)

const (
	btnH          = 26
	btnGap        = 8
	btnPadX       = 9
	footPadTop    = 8
	footPadBottom = 4

	// rewindHold*: holding F4 (or the back button) repeats the 2 s
	// rewind — after half a second, four times a second.
	rewindHoldDelay  = 25
	rewindHoldRepeat = 12
)

// Button groups keep the TUI footer's color language.
var (
	clrRun    = color.RGBA{0x2E, 0x5C, 0x9E, 0xFF} // blue: pause/reset
	clrTime   = color.RGBA{0x8A, 0x4D, 0x9E, 0xFF} // magenta: rewind
	clrTape   = color.RGBA{0x9E, 0x7A, 0x1F, 0xFF} // yellow: tape
	clrTapeOn = color.RGBA{0xB0, 0x30, 0x30, 0xFF} // red: recording live
	clrState  = color.RGBA{0x55, 0x58, 0x5E, 0xFF} // gray: save states
	clrView   = color.RGBA{0x2E, 0x7D, 0x4F, 0xFF} // green: view/sound
	clrShot   = color.RGBA{0x2E, 0x7D, 0x8A, 0xFF} // cyan: screenshot
	clrQuit   = color.RGBA{0xA8, 0x2A, 0x2A, 0xFF} // red: quit
)

type footSpec struct {
	id   string
	text string // key hint + label, e.g. "F2 pause"
	clr  color.RGBA
}

type footHit struct {
	id             string
	x0, y0, x1, y1 int
}

// chromeKeys maps the F-keys onto the shared chrome actions.
var chromeKeys = map[ebiten.Key]string{
	ebiten.KeyF1:  "sound",
	ebiten.KeyF2:  "pause",
	ebiten.KeyF3:  "reset",
	ebiten.KeyF4:  "back",
	ebiten.KeyF5:  "save",
	ebiten.KeyF6:  "load",
	ebiten.KeyF7:  "reload",
	ebiten.KeyF8:  "rec",
	ebiten.KeyF9:  "mon",
	ebiten.KeyF10: "full",
	ebiten.KeyF11: "fullscr",
	ebiten.KeyF12: "shot",
}

// footerSpecs is the current button bar, labels tracking state.
func (g *Game) footerSpecs() []footSpec {
	sound, pause, rec, view, mon := "mute", "pause", "record", "full", "monitor"
	recClr := clrTape
	if !g.m.AudioEnabled() {
		sound = "sound"
	}
	if g.mon.open {
		mon = "close"
	}
	if g.paused {
		pause = "resume"
	}
	if g.m.TapeRecording() {
		rec, recClr = "stop rec", clrTapeOn
	}
	if g.fullFrame {
		view = "active"
	}
	quit := "quit"
	if g.confirmQuit {
		quit = "sure?"
	}
	return []footSpec{
		{"pause", "F2 " + pause, clrRun},
		{"reset", "F3 reset", clrRun},
		{"back", "F4 back 2s", clrTime},
		{"save", "F5 save", clrState},
		{"load", "F6 load", clrState},
		{"reload", "F7 reload", clrTape},
		{"rec", "F8 " + rec, recClr},
		{"mon", "F9 " + mon, clrShot},
		{"sound", "F1 " + sound, clrView},
		{"full", "F10 " + view, clrView},
		{"fullscr", "F11 fullscr", clrView},
		{"shot", "F12 shot", clrShot},
		{"quit", quit, clrQuit},
	}
}

// footerLayout flows chips of the given widths into rows that fit
// winW, wrapping like the TUI footer on narrow terminals. Positions
// are relative to the footer block's top-left; height spans the rows.
func footerLayout(widths []int, winW int) (pos [][2]int, height int) {
	x, y := btnPadX, 0
	for _, w := range widths {
		if x > btnPadX && x+w > winW-btnPadX {
			x, y = btnPadX, y+btnH+btnGap
		}
		pos = append(pos, [2]int{x, y})
		x += w + btnGap
	}
	return pos, y + btnH
}

// chromeHeight is the vertical space the footer block and status line
// take at the current window width — the screen scales into the rest.
func (g *Game) chromeHeight(winW int) int {
	specs := g.footerSpecs()
	widths := make([]int, len(specs))
	for i, s := range specs {
		widths[i] = textWidth(s.text) + 2*btnPadX
	}
	_, rows := footerLayout(widths, winW)
	return footPadTop + rows + footPadBottom + statusH
}

// drawFooter renders the bar and records this frame's hit rectangles
// for mouse dispatch.
func (g *Game) drawFooter(dst *ebiten.Image) {
	specs := g.footerSpecs()
	widths := make([]int, len(specs))
	for i, s := range specs {
		widths[i] = textWidth(s.text) + 2*btnPadX
	}
	winW, winH := dst.Bounds().Dx(), dst.Bounds().Dy()
	pos, rows := footerLayout(widths, winW)
	yTop := winH - statusH - footPadBottom - rows
	g.footHits = g.footHits[:0]
	for i, s := range specs {
		x, y := pos[i][0], yTop+pos[i][1]
		vector.DrawFilledRect(dst, float32(x), float32(y), float32(widths[i]), btnH, s.clr, false)
		drawText(dst, s.text, x+btnPadX, y+(btnH-fontSize)/2-2, color.White)
		g.footHits = append(g.footHits, footHit{s.id, x, y, x + widths[i], y + btnH})
	}
}

func hitAt(hits []footHit, x, y int) (string, bool) {
	for _, h := range hits {
		if x >= h.x0 && x < h.x1 && y >= h.y0 && y < h.y1 {
			return h.id, true
		}
	}
	return "", false
}

// handleChrome runs the frontend controls: window-close interception,
// F-keys, footer clicks, the rewind hold, and the quit confirmation.
func (g *Game) handleChrome() {
	if ebiten.IsWindowBeingClosed() {
		g.chromeAction("quit")
		return
	}
	for key, id := range chromeKeys {
		if inpututil.IsKeyJustPressed(key) {
			g.chromeAction(id)
			return
		}
	}
	// Holding F4 keeps rewinding.
	if d := inpututil.KeyPressDuration(ebiten.KeyF4); d > rewindHoldDelay && d%rewindHoldRepeat == 0 {
		g.chromeAction("back")
		return
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()
		if id, ok := hitAt(g.footHits, x, y); ok {
			g.chromeAction(id)
			return
		}
		if g.confirmQuit {
			g.cancelQuit()
		}
		return
	}
	// Holding the back button keeps rewinding too.
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()
		if id, _ := hitAt(g.footHits, x, y); id == "back" {
			g.backHold++
			if g.backHold > rewindHoldDelay && g.backHold%rewindHoldRepeat == 0 {
				g.chromeAction("back")
			}
			return
		}
	}
	g.backHold = 0
	if g.confirmQuit {
		if inpututil.IsKeyJustPressed(ebiten.KeyY) {
			g.quit = true
			return
		}
		if len(inpututil.AppendJustPressedKeys(nil)) > 0 {
			g.cancelQuit()
		}
	}
}

func (g *Game) cancelQuit() {
	g.confirmQuit = false
	g.status = "quit cancelled"
}

// chromeAction is the single dispatch for clicks and F-keys. Any
// action except quit clears a pending quit confirmation.
func (g *Game) chromeAction(id string) {
	if g.confirmQuit && id != "quit" {
		g.confirmQuit = false
	}
	switch id {
	case "sound":
		g.toggleSound()
	case "pause":
		g.setPaused(!g.paused)
	case "reset":
		g.m.Reset()
		g.dropAudio()
		g.setPaused(false)
		g.status = "reset"
	case "back":
		if err := g.m.Rewind(100 * core.TstatesPerFrame); err != nil {
			g.status = "rewind: " + err.Error()
		} else {
			// Holder bookkeeping belongs to the abandoned timeline.
			g.keys.releaseAll()
			g.dropAudio()
			g.status = fmt.Sprintf("rewound to t=%.1fs", float64(g.m.Tstates())/core.CPUClockHz)
		}
	case "save":
		g.saveSnapshot()
	case "load":
		g.loadSnapshot()
	case "reload":
		g.reloadTape()
	case "rec":
		g.toggleRecording()
	case "mon":
		g.toggleMonitor()
	case "full":
		g.fullFrame = !g.fullFrame
		g.screen = nil // recreate at the new geometry
	case "fullscr":
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	case "shot":
		g.screenshot()
	case "quit":
		if g.confirmQuit {
			g.quit = true
		} else {
			g.confirmQuit = true
			g.status = "quit? Y or the sure? button confirms"
		}
	}
}

// saveSnapshot writes the machine state to vremeplov-snap-<frame>.gob —
// the same gob files headless, the TUI, and the web demo exchange.
func (g *Game) saveSnapshot() {
	name := fmt.Sprintf("vremeplov-snap-%d.gob", g.m.FrameSeq())
	f, err := os.Create(name)
	if err == nil {
		_, err = g.m.Snapshot().WriteTo(f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		g.status = "snapshot: " + err.Error()
		return
	}
	g.status = "snapshot saved to " + name
}

// loadSnapshot restores the newest vremeplov-snap-*.gob around.
func (g *Game) loadSnapshot() {
	name, err := newestSnapshotFile()
	if err != nil {
		g.status = "snapshot: " + err.Error()
		return
	}
	data, err := os.ReadFile(name)
	if err != nil {
		g.status = "snapshot: " + err.Error()
		return
	}
	g.applyFile(name, data)
}

// newestSnapshotFile picks the vremeplov-snap-*.gob with the highest
// frame number (numeric, not lexicographic).
func newestSnapshotFile() (string, error) {
	names, _ := filepath.Glob("vremeplov-snap-*.gob")
	best, bestN := "", -1
	for _, n := range names {
		var v int
		if _, err := fmt.Sscanf(filepath.Base(n), "vremeplov-snap-%d.gob", &v); err == nil && v > bestN {
			best, bestN = n, v
		}
	}
	if best == "" {
		return "", fmt.Errorf("no vremeplov-snap-*.gob in the current directory (F5 saves one)")
	}
	return best, nil
}

// screenshot writes the current view (active area, or the full frame
// when toggled) as a deterministic grayscale PNG.
func (g *Game) screenshot() {
	data, err := g.m.FramePNG(!g.fullFrame)
	name := fmt.Sprintf("vremeplov-shot-%d.png", g.m.FrameSeq())
	if err == nil {
		err = os.WriteFile(name, data, 0o644)
	}
	if err != nil {
		g.status = "screenshot: " + err.Error()
		return
	}
	g.status = "screen saved to " + name
}

package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/monitor"
)

// Default window: the active area at 3× (768×624) plus the status
// line, with headroom for the footer buttons and monitor panel that
// dock around it.
const (
	defaultWinW = 1160
	defaultWinH = 700

	// statusH is the reserved strip under the screen for the status
	// line (the footer button bar stacks above it when it lands).
	statusH = 26
)

// Game is the Ebiten app: it owns the machine and drives one emulated
// frame per Update tick (SetTPS pins ticks at 50 Hz, Ebiten's clock
// supplies the catch-up policy the TUI and wasm frontends hand-roll).
type Game struct {
	m    *core.Machine
	sess *monitor.Session

	pix    []byte        // full-frame luminance scratch
	rgba   []byte        // RGBA staging for the current view
	screen *ebiten.Image // machine screen texture, recreated on view toggle

	fullFrame bool // render all 384×320 scanned pixels, not the active crop
	paused    bool
	status    string

	keys     *keyHolder
	aud      *audioPipe
	player   *audio.Player
	clip     clipper
	lastTape []byte // current tape image, for F7 reload

	confirmQuit bool // window close / quit button awaits confirmation
	quit        bool
	footHits    []footHit // last Draw's button rectangles
	backHold    int       // ticks the mouse has held the back button
	mon         monitorUI
}

func newGame(m *core.Machine) *Game {
	return &Game{
		m:      m,
		sess:   monitor.New(m),
		pix:    make([]byte, core.FrameWidth*core.FrameHeight),
		status: "READY — drop a .gtp or .wav on the window to load it",
		keys:   newKeyHolder(m),
		clip:   systemClipboard{},
	}
}

func (g *Game) Update() error {
	g.handleChrome()
	if g.quit {
		return ebiten.Termination
	}
	switch {
	case g.confirmQuit:
		// The confirmation swallows machine input; releases still land
		// so no key sticks down.
		g.handleMachineKeyReleases()
	case g.mon.open:
		// The monitor REPL owns the keyboard; drops still load tapes.
		g.handleMachineKeyReleases()
		g.handleMonitorInput()
		g.handleDrops()
	default:
		g.handleDrops()
		g.handleMachineKeys()
	}
	if !g.paused {
		g.runFrame()
		g.pumpAudio()
	}
	return nil
}

// runFrame executes exactly one video frame, stopping early on a
// breakpoint or watchpoint like the other live frontends.
func (g *Game) runFrame() {
	t := g.m.Tstates()
	boundary := t - t%core.TstatesPerFrame + core.TstatesPerFrame
	s := g.m.RunDebug(boundary - t)
	if s.Reason != core.StopBudget {
		g.paused = true
		g.mon.open = true
		g.logStop(s)
	}
}

func (g *Game) setPaused(p bool) {
	g.paused = p
	if p {
		g.status = "paused"
	} else {
		g.status = "running"
	}
}

func (g *Game) Draw(dst *ebiten.Image) {
	g.blitScreen(dst)
	g.drawMonitor(dst)
	g.drawFooter(dst)
	g.drawStatus(dst)
}

// blitScreen uploads the current machine frame and draws it centered
// at the largest integer scale that fits above the status strip.
func (g *Game) blitScreen(dst *ebiten.Image) {
	w, h := viewSize(g.fullFrame)
	if g.screen == nil {
		g.screen = ebiten.NewImage(w, h)
		g.rgba = make([]byte, w*h*4)
	}
	g.m.Frame(g.pix)
	viewRGBA(g.pix, g.rgba, g.fullFrame)
	g.screen.WritePixels(g.rgba)

	winW, winH := dst.Bounds().Dx(), dst.Bounds().Dy()
	scale, x, y := layoutScreen(winW, winH, w, h, g.monPanelWidth(), g.chromeHeight(winW))
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(scale), float64(scale))
	op.GeoM.Translate(float64(x), float64(y))
	dst.DrawImage(g.screen, op)
}

func (g *Game) drawStatus(dst *ebiten.Image) {
	winH := dst.Bounds().Dy()
	flags := ""
	if g.paused {
		flags = "[PAUSED] "
	}
	drawText(dst, flags+g.status, 8, winH-statusH+6, color.Gray{0xAA})
}

// Layout keeps the logical resolution equal to the window size; the
// game does its own integer scaling of the machine screen.
func (g *Game) Layout(w, h int) (int, int) { return w, h }

// viewSize is the rendered rectangle of the frame: the active area, or
// every scanned pixel in full-frame mode.
func viewSize(full bool) (w, h int) {
	if full {
		return core.FrameWidth, core.FrameHeight
	}
	return core.ActiveW, core.ActiveH
}

// viewRGBA converts the machine's luminance frame into RGBA bytes for
// the current view. dst must be viewSize(full) w*h*4 bytes; pix a full
// core.FrameWidth×FrameHeight frame.
func viewRGBA(pix, dst []byte, full bool) {
	w, h := viewSize(full)
	x0, y0 := core.ActiveX, core.ActiveY
	if full {
		x0, y0 = 0, 0
	}
	i := 0
	for y := 0; y < h; y++ {
		row := (y0 + y) * core.FrameWidth
		for x := 0; x < w; x++ {
			v := pix[row+x0+x] * 0xFF
			dst[i+0] = v
			dst[i+1] = v
			dst[i+2] = v
			dst[i+3] = 0xFF
			i += 4
		}
	}
}

// layoutScreen places an imgW×imgH texture in a winW×winH window with
// a panelW-wide right dock and footH-tall bottom strip reserved:
// largest integer scale that fits (min 1), centered in the remainder.
func layoutScreen(winW, winH, imgW, imgH, panelW, footH int) (scale, x, y int) {
	availW, availH := winW-panelW, winH-footH
	scale = min(availW/imgW, availH/imgH)
	if scale < 1 {
		scale = 1
	}
	x = (availW - imgW*scale) / 2
	y = (availH - imgH*scale) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return scale, x, y
}

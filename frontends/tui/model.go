package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
	"github.com/mtrisic/vremeplov/core/monitor"
)

// framePeriod is the real-time budget of one emulated frame (50 Hz).
const framePeriod = 20 * time.Millisecond

// holdFrames is how long a terminal keypress holds its matrix key: long
// enough to survive the ROM's 256-read debounce, short enough that
// terminal auto-repeat drives the ROM's own key repeat naturally.
const holdFrames = 3

// maxCatchUpFrames caps frame-skip when the terminal can't keep up:
// emulation slows down rather than spiraling.
const maxCatchUpFrames = 5

type tickMsg time.Time

type model struct {
	m *core.Machine

	width, height int
	renderer      rendererMode
	manualMode    bool // renderer chosen via chrome, don't auto-switch
	fullFrame     bool
	paused        bool
	sticky        bool
	prefix        bool // Ctrl+X pressed, next key is a chrome command
	confirmQuit   bool // quit requested, waiting for y/n
	status        string
	mon           monitorUI
	sess          *monitor.Session

	// holds maps pressed matrix keys to the machine T-state at which
	// they auto-release (sticky mode uses MaxUint64).
	holds    map[core.Key]uint64
	lastTick time.Time

	// footHits is the clickable footer-button map of the last View
	// render, in view coordinates.
	footHits []footHit

	// clipOut receives OSC 52 clipboard escapes (the terminal's stdout;
	// swapped for a buffer in tests).
	clipOut io.Writer

	pix []byte // scratch frame buffer
}

func newModel(m *core.Machine) *model {
	// Rewind history: a snapshot every second, two minutes deep
	// (~30 MB; ~15 MB per minute). run() re-arms this per --rewind.
	m.EnableHistory(50*core.TstatesPerFrame, 120)
	return &model{
		m:        m,
		sess:     monitor.New(m),
		renderer: rendererBraille,
		holds:    make(map[core.Key]uint64),
		pix:      make([]byte, core.FrameWidth*core.FrameHeight),
		clipOut:  os.Stdout,
		status:   "^X + key, or click a button",
	}
}

func (mo *model) pc() uint16 { return mo.m.CPU().State().PC }

func (mo *model) Init() tea.Cmd {
	mo.lastTick = time.Now()
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(framePeriod, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (mo *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		mo.advance(time.Time(msg))
		return mo, tick()
	case tea.WindowSizeMsg:
		mo.width, mo.height = msg.Width, msg.Height
		if !mo.manualMode {
			w, h := mo.viewSize()
			mo.renderer = pickRenderer(mo.width, mo.height-mo.footerRows(mo.width, mo.height), w, h)
		}
		return mo, nil
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			for _, h := range mo.footHits {
				if msg.Y >= h.y0 && msg.Y <= h.y1 && msg.X >= h.x0 && msg.X < h.x1 {
					mo.prefix = false
					return mo.chromeAction(h.key)
				}
			}
		}
		return mo, nil
	case tea.KeyMsg:
		return mo.handleKey(msg)
	}
	return mo, nil
}

// advance runs the machine to catch up with wall time and expires holds.
func (mo *model) advance(now time.Time) {
	frames := 1
	if elapsed := now.Sub(mo.lastTick); elapsed > framePeriod {
		frames = int(math.Round(float64(elapsed) / float64(framePeriod)))
		if frames > maxCatchUpFrames {
			frames = maxCatchUpFrames
		}
	}
	mo.lastTick = now
	if mo.paused {
		return
	}
	for i := 0; i < frames; i++ {
		// RunDebug (frame-aligned like RunFrame) so breakpoints and
		// watchpoints stop the machine even with the monitor closed.
		boundary := mo.m.Tstates() - mo.m.Tstates()%core.TstatesPerFrame + core.TstatesPerFrame
		if s := mo.m.RunDebug(boundary - mo.m.Tstates()); s.Reason != core.StopBudget {
			mo.paused = true
			mo.sess.Paused = true
			mo.mon.open = true
			mo.logStop(s)
			break
		}
	}
	for k, releaseAt := range mo.holds {
		if mo.m.Tstates() >= releaseAt {
			mo.m.ReleaseKey(k)
			delete(mo.holds, k)
		}
	}
}

func (mo *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if mo.confirmQuit {
		mo.confirmQuit = false
		if s := msg.String(); s == "y" || s == "Y" {
			return mo, tea.Quit
		}
		mo.status = "quit cancelled"
		return mo, nil
	}
	if msg.Paste {
		mo.prefix = false
		mo.pasteText(string(msg.Runes))
		return mo, nil
	}
	if mo.prefix {
		mo.prefix = false
		return mo.chromeCommand(msg)
	}
	if msg.Type == tea.KeyCtrlX {
		mo.prefix = true
		mo.status = "^X: [q]uit [p]ause [r]eset [b]ack-2s [w]rite-snap [l]oad-snap [c]shot [y]copy [d]ump [s]ticky [v]renderer [f]ull [m]onitor [t]ape-rec"
		return mo, nil
	}
	if mo.mon.open {
		mo.handleMonitorKey(msg)
		return mo, nil
	}
	press, ok := mapKeyMsg(msg)
	if !ok {
		return mo, nil
	}
	mo.pressWithHold(press)
	return mo, nil
}

// pressWithHold pushes a matrix combination and schedules its release.
// In sticky mode the previous held keys are released first and the new
// ones stay down until the next press.
func (mo *model) pressWithHold(p matrixPress) {
	releaseAt := mo.m.Tstates() + holdFrames*core.TstatesPerFrame
	if mo.sticky {
		for k := range mo.holds {
			mo.m.ReleaseKey(k)
			delete(mo.holds, k)
		}
		releaseAt = math.MaxUint64
	}
	if p.shift {
		mo.m.PressKey(core.KeyShift)
		mo.holds[core.KeyShift] = releaseAt
	}
	mo.m.PressKey(p.key)
	mo.holds[p.key] = releaseAt
}

func (mo *model) chromeCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return mo.chromeAction(msg.String())
}

// chromeAction executes one chrome command — from a ^X-prefixed key or
// a footer-button click.
func (mo *model) chromeAction(key string) (tea.Model, tea.Cmd) {
	mo.status = "^X + key, or click a button"
	if key != "q" {
		mo.confirmQuit = false
	}
	switch key {
	case "q":
		// Quitting drops the machine (and any unsaved game), so a stray
		// click or keypress asks first.
		if mo.confirmQuit {
			return mo, tea.Quit
		}
		mo.confirmQuit = true
		mo.status = "quit? y confirms — any other key (or another button) cancels"
	case "p":
		mo.paused = !mo.paused
		mo.sess.Paused = mo.paused
		if mo.paused {
			mo.status = "paused (^X p to resume)"
		}
	case "r":
		mo.m.Reset()
		mo.status = "machine reset"
	case "d":
		name := fmt.Sprintf("vremeplov-mem-%d.bin", mo.m.FrameSeq())
		if err := os.WriteFile(name, mo.m.DumpMemory(0, 0x10000), 0o644); err != nil {
			mo.status = "dump failed: " + err.Error()
		} else {
			mo.status = "memory dumped to " + name
		}
	case "s":
		mo.sticky = !mo.sticky
		if !mo.sticky {
			for k := range mo.holds {
				mo.m.ReleaseKey(k)
				delete(mo.holds, k)
			}
			mo.status = "sticky keys off"
		} else {
			mo.status = "sticky keys on (game mode)"
		}
	case "v":
		mo.renderer = (mo.renderer + 1) % rendererCount
		mo.manualMode = true
		mo.status = "renderer: " + mo.renderer.String()
	case "f":
		mo.fullFrame = !mo.fullFrame
		if mo.fullFrame {
			mo.status = "full frame (borders visible)"
		} else {
			mo.status = "active area"
		}
	case "w":
		mo.saveSnapshot()
	case "l":
		mo.loadSnapshot()
	case "c":
		name := fmt.Sprintf("vremeplov-shot-%d.png", mo.m.FrameSeq())
		data, err := mo.m.FramePNG(!mo.fullFrame)
		if err == nil {
			err = os.WriteFile(name, data, 0o644)
		}
		if err != nil {
			mo.status = "screenshot: " + err.Error()
		} else {
			mo.status = "screenshot saved to " + name
		}
	case "y":
		mo.copyScreen()
	case "b":
		if err := mo.m.Rewind(100 * core.TstatesPerFrame); err != nil {
			mo.status = "rewind: " + err.Error()
		} else {
			mo.status = fmt.Sprintf("rewound to t=%.1fs", float64(mo.m.Tstates())/core.CPUClockHz)
		}
	case "t":
		if !mo.m.TapeRecording() {
			mo.m.StartTapeRecording()
			mo.status = "recording tape (^X t to stop; SAVE in BASIC)"
			return mo, nil
		}
		streams := mo.m.StopTapeRecording()
		if len(streams) == 0 {
			mo.status = "recording stopped — no tape output captured"
			return mo, nil
		}
		name := fmt.Sprintf("vremeplov-tape-%d", mo.m.FrameSeq())
		img, err := gtp.Build(name, streams...)
		if err == nil {
			err = os.WriteFile(name+".gtp", img, 0o644)
		}
		if err == nil {
			wav := core.CompileTapeBlocks(streams...).EncodeWAV(44100)
			err = os.WriteFile(name+".wav", wav, 0o644)
		}
		if err != nil {
			mo.status = "recording failed: " + err.Error()
		} else {
			mo.status = fmt.Sprintf("%d tape block(s) written to %s.{gtp,wav}", len(streams), name)
		}
	case "m":
		mo.mon.open = !mo.mon.open
		if mo.mon.open {
			mo.paused = true
			mo.sess.Paused = true
			mo.status = "monitor (keys go to the REPL; ^X m closes)"
			mo.logPC()
		} else {
			mo.status = "monitor closed (^X p to resume)"
		}
	}
	return mo, nil
}

// viewSize is the pixel viewport: active area or full frame.
func (mo *model) viewSize() (int, int) {
	if mo.fullFrame {
		return core.FrameWidth, core.FrameHeight
	}
	return core.ActiveW, core.ActiveH
}

func (mo *model) View() string {
	mo.m.Frame(mo.pix)

	// Footer first: its height decides the content budget (button rows
	// collapse when they would not leave a full text screen of content;
	// they grow borders when they cost no content rows).
	btnLines, hits := mo.footerLayout(mo.width, mo.height)
	budget := mo.height - 1 - len(btnLines) // <= 0 while size is unknown

	var lines []string
	hint := ""
	if mo.mon.open {
		lines = mo.splitView(budget)
	} else {
		x0, y0 := core.ActiveX, core.ActiveY
		w, h := mo.viewSize()
		if mo.fullFrame {
			x0, y0 = 0, 0
		}
		switch mo.renderer {
		case rendererText:
			lines = mo.m.ScreenText()
		case rendererScaled:
			cols, rows := mo.width, budget
			if cols <= 0 || rows <= 0 { // no WindowSizeMsg yet
				cols, rows = textCols*4, textRows*4
			}
			lines = renderScaled(mo.pix, core.FrameWidth, x0, y0, w, h, cols, rows)
		default:
			lines = renderView(mo.pix, core.FrameWidth, x0, y0, w, h, mo.renderer)
		}

		// Bubbletea discards the TOP of a view taller than the terminal;
		// the Galaksija prompt lives at the top, so crop the bottom
		// ourselves and say so.
		if mo.height > 0 && len(lines) > budget {
			hint = fmt.Sprintf(" · cropped %d/%d rows", budget, len(lines))
			lines = lines[:budget]
		}
	}

	var flags []string
	if mo.paused {
		flags = append(flags, "PAUSED")
	}
	if mo.sticky {
		flags = append(flags, "STICKY")
	}
	if mo.m.TapeRecording() {
		flags = append(flags, "REC")
	}
	flagStr := ""
	if len(flags) > 0 {
		flagStr = " [" + strings.Join(flags, " ") + "]"
	}
	out := append(lines, fmt.Sprintf("vremeplov · %s%s%s · %s", mo.renderer, hint, flagStr, mo.status))

	// Buttons below the status line; remember where they landed for
	// mouse hit-testing.
	mo.footHits = mo.footHits[:0]
	for _, h := range hits {
		h.y0 += len(out)
		h.y1 += len(out)
		mo.footHits = append(mo.footHits, h)
	}
	out = append(out, btnLines...)
	return strings.Join(out, "\n")
}

// splitView composes the monitor layout: Galaksija screen left, monitor
// panel right — or the monitor alone when the terminal is too narrow.
// rows is the content budget left over by the footer.
func (mo *model) splitView(rows int) []string {
	cols := mo.width
	if cols <= 0 || rows <= 0 { // no WindowSizeMsg yet
		cols, rows = textCols*4, textRows*4
	}
	if cols < monMinSplit {
		lines := mo.monitorLines(rows)
		if len(lines) > rows {
			lines = lines[:rows]
		}
		return lines
	}

	leftCols := cols - monPanelW - 1
	left := mo.screenLines(leftCols, rows)
	right := mo.monitorLines(rows)
	n := max(len(left), len(right))
	if n > rows {
		n = rows
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if lr := []rune(l); len(lr) > leftCols {
			l = string(lr[:leftCols])
		} else {
			l += strings.Repeat(" ", leftCols-len(lr))
		}
		out[i] = l + " " + r
	}
	return out
}

// screenLines renders the Galaksija screen into a cols×rows region,
// auto-picking the renderer for that size (the split pane is narrower
// than the terminal, so the user's full-screen choice may not fit).
func (mo *model) screenLines(cols, rows int) []string {
	x0, y0 := core.ActiveX, core.ActiveY
	w, h := mo.viewSize()
	if mo.fullFrame {
		x0, y0 = 0, 0
	}
	switch r := pickRenderer(cols, rows, w, h); r {
	case rendererText:
		return mo.m.ScreenText()
	case rendererScaled:
		return renderScaled(mo.pix, core.FrameWidth, x0, y0, w, h, cols, rows)
	default:
		return renderView(mo.pix, core.FrameWidth, x0, y0, w, h, r)
	}
}

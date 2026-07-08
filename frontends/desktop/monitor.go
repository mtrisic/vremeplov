package main

// The monitor is a machine-language debugger panel: registers, a live
// disassembly window at PC, watch expressions, and a command REPL.
// The engine (commands, formatting) is core/monitor, shared with the
// TUI and web frontends; this file owns the Ebiten presentation. F9
// toggles it; while open, typing goes to the REPL (machine presses
// are swallowed, releases still land). Opening pauses the machine;
// `c` resumes with the panel visible, and a breakpoint or watchpoint
// hit pauses and reopens it.

import (
	"image/color"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/monitor"
)

const (
	// monPanelChars is the panel's column width, matching the TUI.
	monPanelChars = 42
	// monDisasmRows is the size of the disassembly window at PC.
	monDisasmRows = 8
	// monLogMax bounds the REPL scrollback.
	monLogMax = 200
	// monPad is the panel's inner padding in pixels.
	monPad = 8
	// monLineH is the panel's text line pitch.
	monLineH = 18
)

var (
	clrMonBack  = color.RGBA{0x14, 0x14, 0x18, 0xFF}
	clrMonText  = color.RGBA{0xD8, 0xD8, 0xD8, 0xFF}
	clrMonInput = color.RGBA{0x8F, 0xE0, 0x8F, 0xFF}
)

// monitorUI is the frontend-side monitor state: panel visibility, the
// REPL input line, history, and scrollback. The editing methods are
// Ebiten-free so they test headless.
type monitorUI struct {
	open    bool
	input   []rune
	hist    []string
	histPos int // len(hist) while editing a fresh line
	log     []string
}

func (mn *monitorUI) append(lines ...string) {
	mn.log = append(mn.log, lines...)
	if len(mn.log) > monLogMax {
		mn.log = mn.log[len(mn.log)-monLogMax:]
	}
}

func (mn *monitorUI) typeRunes(rs []rune) {
	for _, r := range rs {
		if r >= ' ' {
			mn.input = append(mn.input, r)
		}
	}
}

func (mn *monitorUI) backspace() {
	if len(mn.input) > 0 {
		mn.input = mn.input[:len(mn.input)-1]
	}
}

func (mn *monitorUI) clearLine() { mn.input = mn.input[:0] }

func (mn *monitorUI) histUp() {
	if mn.histPos > 0 {
		mn.histPos--
		mn.input = []rune(mn.hist[mn.histPos])
	}
}

func (mn *monitorUI) histDown() {
	if mn.histPos < len(mn.hist) {
		mn.histPos++
		if mn.histPos == len(mn.hist) {
			mn.input = mn.input[:0]
		} else {
			mn.input = []rune(mn.hist[mn.histPos])
		}
	}
}

// submit takes the current line (recording history), or "" for an
// empty input.
func (mn *monitorUI) submit() string {
	line := strings.TrimSpace(string(mn.input))
	mn.input = mn.input[:0]
	if line == "" {
		return ""
	}
	mn.hist = append(mn.hist, line)
	mn.histPos = len(mn.hist)
	return line
}

// execMonitor runs a REPL line through the shared session with the
// TUI's pause handshake: the session sees the frontend's pause state,
// and commands like c/s move it.
func (g *Game) execMonitor(line string) {
	g.mon.append("> " + line)
	g.sess.Paused = g.paused
	g.mon.append(g.sess.Exec(line)...)
	g.paused = g.sess.Paused
}

// logPC logs the instruction the machine is paused at.
func (g *Game) logPC() {
	g.mon.append(g.sess.PCLine())
}

// logStop reports a breakpoint/watchpoint stop in the log and status.
func (g *Game) logStop(s core.Stop) {
	if msg := monitor.FormatStop(s); msg != "" {
		g.status = msg
		g.mon.append(msg)
	}
	g.logPC()
}

// toggleMonitor is the F9 chrome action (the TUI's ^X m semantics:
// opening pauses; closing leaves the machine paused for F2).
func (g *Game) toggleMonitor() {
	g.mon.open = !g.mon.open
	if g.mon.open {
		g.paused = true
		g.sess.Paused = true
		g.status = "monitor (keys go to the REPL; F9 closes)"
		g.logPC()
	} else {
		g.status = "monitor closed (F2 resumes)"
	}
}

// handleMonitorInput edits the REPL line from Ebiten's typed-rune
// stream and the editing keys. Chrome F-keys are dispatched before
// this, so F9 still closes the panel.
func (g *Game) handleMonitorInput() {
	mn := &g.mon
	mn.typeRunes(ebiten.AppendInputChars(nil))
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControlLeft) || ebiten.IsKeyPressed(ebiten.KeyControlRight)
	switch {
	case ctrl && inpututil.IsKeyJustPressed(ebiten.KeyU):
		mn.clearLine()
	case keyRepeats(ebiten.KeyBackspace):
		mn.backspace()
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowUp):
		mn.histUp()
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowDown):
		mn.histDown()
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter):
		if line := mn.submit(); line != "" {
			g.execMonitor(line)
		}
	}
}

// keyRepeats fires on the press and then at a typematic cadence while
// the key is held.
func keyRepeats(k ebiten.Key) bool {
	d := inpututil.KeyPressDuration(k)
	return d == 1 || (d > 25 && d%3 == 0)
}

// monitorLines composes the panel text, monPanelChars wide, at most
// rows lines: registers, disassembly at PC, watches, log tail, input
// line — the TUI's exact layout.
func (g *Game) monitorLines(rows int) []string {
	sep := func(title string) string {
		return "── " + title + " " + strings.Repeat("─", max(0, monPanelChars-len(title)-4))
	}

	lines := g.sess.RegisterLines()
	lines = append(lines, sep("disasm"))
	lines = append(lines, g.sess.DisasmLines(monDisasmRows)...)
	if w := g.sess.WatchLines(); len(w) > 0 {
		lines = append(lines, sep("watches"))
		lines = append(lines, w...)
	}

	lines = append(lines, sep("log"))
	input := "> " + string(g.mon.input) + "_"
	tail := rows - len(lines) - 1 // reserve the input line
	if tail < 1 {
		tail = 1
	}
	log := g.mon.log
	if len(log) > tail {
		log = log[len(log)-tail:]
	}
	lines = append(lines, log...)
	lines = append(lines, input)

	// Clamp to the panel width.
	for i, l := range lines {
		if r := []rune(l); len(r) > monPanelChars {
			lines[i] = string(r[:monPanelChars])
		}
	}
	if len(lines) > rows && rows > 0 {
		// Keep the top (registers/disasm) and the input line.
		lines = append(lines[:rows-1:rows-1], lines[len(lines)-1])
	}
	return lines
}

// monPanelWidth is the docked panel's pixel width — 0 when closed.
func (g *Game) monPanelWidth() int {
	if !g.mon.open {
		return 0
	}
	return monPanelChars*textWidth("0") + 2*monPad
}

// drawMonitor renders the right-docked panel above the footer.
func (g *Game) drawMonitor(dst *ebiten.Image) {
	if !g.mon.open {
		return
	}
	winW, winH := dst.Bounds().Dx(), dst.Bounds().Dy()
	panelW := g.monPanelWidth()
	panelH := winH - g.chromeHeight(winW)
	x0 := winW - panelW
	vector.DrawFilledRect(dst, float32(x0), 0, float32(panelW), float32(panelH), clrMonBack, false)

	rows := (panelH - 2*monPad) / monLineH
	lines := g.monitorLines(rows)
	for i, l := range lines {
		clr := color.Color(clrMonText)
		if i == len(lines)-1 {
			clr = clrMonInput
		}
		drawText(dst, l, x0+monPad, monPad+i*monLineH, clr)
	}
}

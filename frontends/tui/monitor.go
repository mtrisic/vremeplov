package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/monitor"
)

// The monitor is a machine-language debugger panel: registers, a live
// disassembly window at PC, watch expressions, and a command REPL. The
// engine (commands, formatting) is core/monitor, shared with the web
// frontend; this file owns the terminal presentation. Toggled with ^X m;
// while open, all typing goes to the REPL (close it to type into the
// Galaksija). Opening pauses the machine; `c` resumes with the panel
// still visible, and a breakpoint or watchpoint hit pauses and reopens
// it.

const (
	// monPanelW is the monitor panel's column width.
	monPanelW = 42
	// monMinSplit is the minimum terminal width for the side-by-side
	// layout; below it the monitor takes the whole terminal.
	monMinSplit = 76
	// monDisasmRows is the size of the disassembly window at PC.
	monDisasmRows = 8
	// monLogMax bounds the REPL scrollback.
	monLogMax = 200
)

// monitorUI is the terminal-side monitor state: panel visibility, the
// REPL input line, history, and scrollback.
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

// handleMonitorKey edits the REPL input line. The chrome prefix (^X) is
// handled before this, so ^X m still closes the panel.
func (mo *model) handleMonitorKey(msg tea.KeyMsg) {
	mn := &mo.mon
	switch msg.Type {
	case tea.KeyRunes:
		mn.input = append(mn.input, msg.Runes...)
	case tea.KeySpace:
		mn.input = append(mn.input, ' ')
	case tea.KeyBackspace:
		if len(mn.input) > 0 {
			mn.input = mn.input[:len(mn.input)-1]
		}
	case tea.KeyCtrlU:
		mn.input = mn.input[:0]
	case tea.KeyUp:
		if mn.histPos > 0 {
			mn.histPos--
			mn.input = []rune(mn.hist[mn.histPos])
		}
	case tea.KeyDown:
		if mn.histPos < len(mn.hist) {
			mn.histPos++
			if mn.histPos == len(mn.hist) {
				mn.input = mn.input[:0]
			} else {
				mn.input = []rune(mn.hist[mn.histPos])
			}
		}
	case tea.KeyEnter:
		line := strings.TrimSpace(string(mn.input))
		mn.input = mn.input[:0]
		if line == "" {
			return
		}
		mn.hist = append(mn.hist, line)
		mn.histPos = len(mn.hist)
		mn.append("> " + line)
		mo.sess.Paused = mo.paused
		mn.append(mo.sess.Exec(line)...)
		mo.paused = mo.sess.Paused
	}
}

// logPC logs the instruction the machine is paused at.
func (mo *model) logPC() {
	mo.mon.append(mo.sess.PCLine())
}

// logStop reports a breakpoint/watchpoint stop in the log and status.
func (mo *model) logStop(s core.Stop) {
	if msg := monitor.FormatStop(s); msg != "" {
		mo.status = msg
		mo.mon.append(msg)
	}
	mo.logPC()
}

// monitorLines renders the monitor panel, monPanelW wide, at most rows
// lines: registers, disassembly at PC, watches, log tail, input line.
func (mo *model) monitorLines(rows int) []string {
	sep := func(title string) string {
		return "── " + title + " " + strings.Repeat("─", max(0, monPanelW-len(title)-4))
	}

	lines := mo.sess.RegisterLines()
	lines = append(lines, sep("disasm"))
	lines = append(lines, mo.sess.DisasmLines(monDisasmRows)...)
	if w := mo.sess.WatchLines(); len(w) > 0 {
		lines = append(lines, sep("watches"))
		lines = append(lines, w...)
	}

	lines = append(lines, sep("log"))
	input := "> " + string(mo.mon.input)
	tail := rows - len(lines) - 1 // reserve the input line
	if tail < 1 {
		tail = 1
	}
	log := mo.mon.log
	if len(log) > tail {
		log = log[len(log)-tail:]
	}
	lines = append(lines, log...)
	lines = append(lines, input)

	// Clamp and pad to the panel width.
	for i, l := range lines {
		r := []rune(l)
		if len(r) > monPanelW {
			r = r[:monPanelW]
		}
		lines[i] = string(r) + strings.Repeat(" ", monPanelW-len(r))
	}
	if len(lines) > rows && rows > 0 {
		// Keep the top (registers/disasm) and the input line.
		lines = append(lines[:rows-1:rows-1], lines[len(lines)-1])
	}
	return lines
}

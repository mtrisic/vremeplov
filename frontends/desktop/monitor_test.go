package main

import (
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core"
)

func TestMonitorUIEditing(t *testing.T) {
	var mn monitorUI
	mn.typeRunes([]rune("help"))
	mn.backspace()
	mn.typeRunes([]rune{'p', 0x07}) // control runes never land
	if got := string(mn.input); got != "help" {
		t.Fatalf("input = %q, want help", got)
	}
	if line := mn.submit(); line != "help" {
		t.Fatalf("submit = %q", line)
	}
	if len(mn.input) != 0 {
		t.Fatal("input not cleared by submit")
	}
	if line := mn.submit(); line != "" {
		t.Fatalf("empty submit = %q", line)
	}

	mn.typeRunes([]rune("junk"))
	mn.clearLine()
	if len(mn.input) != 0 {
		t.Fatal("Ctrl-U did not clear the line")
	}
}

func TestMonitorUIHistory(t *testing.T) {
	var mn monitorUI
	for _, cmd := range []string{"first", "second"} {
		mn.typeRunes([]rune(cmd))
		mn.submit()
	}
	mn.histUp()
	if got := string(mn.input); got != "second" {
		t.Fatalf("histUp = %q", got)
	}
	mn.histUp()
	if got := string(mn.input); got != "first" {
		t.Fatalf("histUp x2 = %q", got)
	}
	mn.histUp() // clamps at the oldest
	if got := string(mn.input); got != "first" {
		t.Fatalf("histUp clamp = %q", got)
	}
	mn.histDown()
	mn.histDown()
	if len(mn.input) != 0 {
		t.Fatalf("histDown past the end should clear, got %q", string(mn.input))
	}
}

func TestMonitorUILogCap(t *testing.T) {
	var mn monitorUI
	for i := 0; i < monLogMax+50; i++ {
		mn.append("line")
	}
	if len(mn.log) != monLogMax {
		t.Fatalf("log length = %d, want %d", len(mn.log), monLogMax)
	}
}

// TestMonitorPanelComposition: registers on top, separators, the log
// tail, and the input line last — clamped to the panel width.
func TestMonitorPanelComposition(t *testing.T) {
	g := testGame(t)
	g.m.RunTstates(5 * core.TstatesPerFrame)
	g.toggleMonitor()
	if !g.paused || !g.mon.open {
		t.Fatal("opening the monitor should pause")
	}
	g.mon.typeRunes([]rune("hel"))

	lines := g.monitorLines(30)
	if len(lines) > 30 {
		t.Fatalf("%d lines for 30 rows", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "PC ") {
		t.Fatal("no register line in the panel")
	}
	if !strings.Contains(joined, "── disasm") || !strings.Contains(joined, "── log") {
		t.Fatalf("missing separators:\n%s", joined)
	}
	if got := lines[len(lines)-1]; got != "> hel_" {
		t.Fatalf("input line = %q", got)
	}
	for i, l := range lines {
		if len([]rune(l)) > monPanelChars {
			t.Fatalf("line %d wider than the panel: %q", i, l)
		}
	}

	// Tiny panel: the top and the input line survive.
	lines = g.monitorLines(6)
	if len(lines) != 6 {
		t.Fatalf("%d lines for 6 rows", len(lines))
	}
	if got := lines[5]; got != "> hel_" {
		t.Fatalf("clamped input line = %q", got)
	}
}

// TestMonitorExecHandshake: commands run through the shared session
// and move the frontend's pause state (c resumes, s steps paused).
func TestMonitorExecHandshake(t *testing.T) {
	g := testGame(t)
	g.m.RunTstates(5 * core.TstatesPerFrame)
	g.toggleMonitor()

	before := g.m.Tstates()
	g.execMonitor("s 3")
	if g.m.Tstates() <= before {
		t.Fatal("s 3 did not step the machine")
	}
	if !g.paused {
		t.Fatal("stepping should stay paused")
	}
	g.execMonitor("c")
	if g.paused {
		t.Fatal("c should resume")
	}
	if !strings.Contains(strings.Join(g.mon.log, "\n"), "> s 3") {
		t.Fatal("commands not echoed to the log")
	}

	// Closing leaves the machine paused for F2, like the TUI.
	g.paused = true
	g.toggleMonitor()
	if g.mon.open || !g.paused {
		t.Fatalf("after close: open=%v paused=%v", g.mon.open, g.paused)
	}
}

// TestBreakpointOpensMonitor: a RunDebug stop pauses and pops the
// panel with the stop logged.
func TestBreakpointOpensMonitor(t *testing.T) {
	g := testGame(t)
	g.m.RunTstates(100 * core.TstatesPerFrame) // boot to READY
	g.m.AddBreakpoint(g.m.CPU().State().PC)    // the idle loop revisits it

	g.runFrame()
	if !g.paused || !g.mon.open {
		t.Fatalf("after stop: paused=%v open=%v", g.paused, g.mon.open)
	}
	joined := strings.Join(g.mon.log, "\n")
	if !strings.Contains(joined, "break at") {
		t.Fatalf("stop not logged:\n%s", joined)
	}
}

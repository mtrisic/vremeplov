package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func openMonitor(mo *model) {
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('m'))
}

// typeCmd types a REPL line and presses Enter.
func typeCmd(mo *model, line string) {
	for _, r := range line {
		mo.Update(runeMsg(r))
	}
	mo.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

func lastLog(mo *model) string {
	if len(mo.mon.log) == 0 {
		return ""
	}
	return mo.mon.log[len(mo.mon.log)-1]
}

func TestMonitorToggleAndPause(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)

	openMonitor(mo)
	if !mo.mon.open {
		t.Fatal("monitor did not open")
	}
	if !mo.paused {
		t.Fatal("opening the monitor must pause the machine")
	}
	view := mo.View()
	if !strings.Contains(view, "PC ") || !strings.Contains(view, "▶") {
		t.Fatalf("monitor view missing registers/disasm marker:\n%s", view)
	}

	openMonitor(mo) // ^X m again closes
	if mo.mon.open {
		t.Fatal("monitor did not close")
	}
}

func TestMonitorKeysDoNotReachMachine(t *testing.T) {
	mo := newModel(testMachine(t))
	openMonitor(mo)
	mo.Update(runeMsg('a'))
	if got := mo.m.MemRead(0x2001); got&1 == 0 {
		t.Fatal("typing into the REPL pressed a matrix key")
	}
	if string(mo.mon.input) != "a" {
		t.Fatalf("input = %q, want %q", string(mo.mon.input), "a")
	}
}

func TestMonitorBreakpointFlow(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)

	openMonitor(mo)
	typeCmd(mo, "b 0038")
	if bps := mo.m.Breakpoints(); len(bps) != 1 || bps[0] != 0x0038 {
		t.Fatalf("Breakpoints() = %v, want [0x0038]", bps)
	}

	typeCmd(mo, "c")
	if mo.paused {
		t.Fatal("c did not resume")
	}
	// Boot enables interrupts after a while; the ISR then hits the
	// breakpoint. One doTick = one frame.
	for i := 0; i < 400 && !mo.paused; i++ {
		doTick(mo)
	}
	if !mo.paused {
		t.Fatal("breakpoint at the ISR never hit")
	}
	if !mo.mon.open {
		t.Fatal("breakpoint hit must open the monitor")
	}
	if !strings.Contains(mo.status, "break at 0038") {
		t.Fatalf("status = %q, want break at 0038", mo.status)
	}
	if pc := mo.pc(); pc != 0x0038 {
		t.Fatalf("PC = 0x%04X, want 0x0038", pc)
	}

	// Stepping executes the instruction under the breakpoint.
	before := mo.m.Tstates()
	typeCmd(mo, "s")
	if mo.pc() == 0x0038 || mo.m.Tstates() == before {
		t.Fatalf("s did not advance (PC 0x%04X)", mo.pc())
	}
}

func TestMonitorPokeDumpSetAndStepOver(t *testing.T) {
	mo := newModel(testMachine(t))
	openMonitor(mo)

	// Addresses with bit 7 set survive the power-on A7 clamp untranslated.
	typeCmd(mo, "poke 2cc0 cd e0 2c") // CALL 2CE0
	typeCmd(mo, "poke 2ce0 c9")       // RET
	typeCmd(mo, "x 2cc0 3")
	if got := lastLog(mo); !strings.Contains(got, "CD E0 2C") {
		t.Fatalf("x dump = %q, want the poked bytes", got)
	}

	typeCmd(mo, "set sp 2cf0")
	typeCmd(mo, "set pc 2cc0")
	if st := mo.m.CPU().State(); st.PC != 0x2CC0 || st.SP != 0x2CF0 {
		t.Fatalf("set failed: PC=0x%04X SP=0x%04X", st.PC, st.SP)
	}

	typeCmd(mo, "n") // step over the CALL: runs CALL+RET, lands at 2CC3
	if pc := mo.pc(); pc != 0x2CC3 {
		t.Fatalf("step-over landed at 0x%04X, want 0x2CC3", pc)
	}
	if bps := mo.m.Breakpoints(); len(bps) != 0 {
		t.Fatalf("temporary breakpoint leaked: %v", bps)
	}
}

func TestMonitorDisasmCommand(t *testing.T) {
	mo := newModel(testMachine(t))
	openMonitor(mo)
	typeCmd(mo, "d 0000 2")
	// ROM A starts with DI (F3) at 0x0000.
	found := false
	for _, l := range mo.mon.log {
		if strings.Contains(l, "0000 F3") && strings.Contains(l, "DI") {
			found = true
		}
	}
	if !found {
		t.Fatalf("d 0000 did not log the reset vector:\n%s", strings.Join(mo.mon.log, "\n"))
	}
}

func TestMonitorDisplayWatch(t *testing.T) {
	mo := newModel(testMachine(t))
	openMonitor(mo)
	typeCmd(mo, "poke 2cd0 34 12")
	typeCmd(mo, "watch 2cd0 w")
	view := mo.View()
	if !strings.Contains(view, "2CD0 w 1234") {
		t.Fatalf("view missing word watch 2CD0 w 1234:\n%s", view)
	}
	typeCmd(mo, "unwatch 2cd0")
	if strings.Contains(mo.View(), "2CD0 w 1234") {
		t.Fatal("unwatch did not remove the display watch")
	}
}

func TestMonitorLayoutSplitAndNarrow(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	openMonitor(mo)

	view := strings.Split(mo.View(), "\n")
	if len(view) > 30 {
		t.Fatalf("split view has %d rows, terminal has 30", len(view))
	}
	// Side-by-side: the registers line sits to the RIGHT of the screen pane.
	if idx := strings.Index(view[0], "PC "); idx < 120-monPanelW-1 {
		t.Fatalf("registers at col %d, want right pane (>= %d):\n%s", idx, 120-monPanelW-1, view[0])
	}

	mo.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	view = strings.Split(mo.View(), "\n")
	if !strings.HasPrefix(view[0], "PC ") {
		t.Fatalf("narrow view must be monitor-only, got:\n%s", view[0])
	}
	if len(view) > 20 {
		t.Fatalf("narrow view has %d rows, terminal has 20", len(view))
	}
}

func TestMonitorHelpAndUnknown(t *testing.T) {
	mo := newModel(testMachine(t))
	openMonitor(mo)
	typeCmd(mo, "bogus")
	if !strings.Contains(lastLog(mo), "unknown command") {
		t.Fatalf("lastLog = %q", lastLog(mo))
	}
	typeCmd(mo, "help")
	joined := strings.Join(mo.mon.log, "\n")
	if !strings.Contains(joined, "step-over") || !strings.Contains(joined, "A7 clamp") {
		t.Fatal("help output incomplete")
	}
}

func TestMonitorInputEditing(t *testing.T) {
	mo := newModel(testMachine(t))
	openMonitor(mo)
	for _, r := range "xy" {
		mo.Update(runeMsg(r))
	}
	mo.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if string(mo.mon.input) != "x" {
		t.Fatalf("input = %q after backspace, want %q", string(mo.mon.input), "x")
	}
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(mo.mon.input) != 0 {
		t.Fatal("ctrl+u did not clear the input")
	}

	typeCmd(mo, "bl")
	typeCmd(mo, "wl")
	mo.Update(tea.KeyMsg{Type: tea.KeyUp})
	if string(mo.mon.input) != "wl" {
		t.Fatalf("history up = %q, want %q", string(mo.mon.input), "wl")
	}
	mo.Update(tea.KeyMsg{Type: tea.KeyUp})
	if string(mo.mon.input) != "bl" {
		t.Fatalf("history up x2 = %q, want %q", string(mo.mon.input), "bl")
	}
	mo.Update(tea.KeyMsg{Type: tea.KeyDown})
	if string(mo.mon.input) != "wl" {
		t.Fatalf("history down = %q, want %q", string(mo.mon.input), "wl")
	}
}

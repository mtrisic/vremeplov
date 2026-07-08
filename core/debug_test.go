package core

import (
	"bytes"
	"testing"
)

// isrEntry is the IM1 interrupt vector — the one address every frame's
// video ISR starts at, making it a reliable breakpoint target.
const isrEntry = 0x0038

func TestBreakpointISR(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.AddBreakpoint(isrEntry)

	// Boot enables interrupts partway through its init (well past the
	// first frames); the ISR then fires at line 56 of every frame.
	s := m.RunDebug(200 * TstatesPerFrame)
	if s.Reason != StopBreakpoint {
		t.Fatalf("Reason = %d, want StopBreakpoint", s.Reason)
	}
	if s.PC != isrEntry {
		t.Fatalf("PC = 0x%04X, want 0x%04X", s.PC, isrEntry)
	}
	if got := m.CPU().State().PC; got != isrEntry {
		t.Fatalf("CPU stopped at 0x%04X, want 0x%04X (instruction not yet executed)", got, isrEntry)
	}
}

func TestBreakpointResume(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.AddBreakpoint(isrEntry)

	first := m.RunDebug(200 * TstatesPerFrame)
	if first.Reason != StopBreakpoint {
		t.Fatalf("first Reason = %d, want StopBreakpoint", first.Reason)
	}
	tAtFirst := m.Tstates()

	// Resuming from the breakpoint must execute the instruction under it
	// and run on to the NEXT hit (the following frame's INT), not
	// re-trigger immediately.
	second := m.RunDebug(200 * TstatesPerFrame)
	if second.Reason != StopBreakpoint || second.PC != isrEntry {
		t.Fatalf("second stop = %+v, want breakpoint at 0x%04X", second, isrEntry)
	}
	if gap := m.Tstates() - tAtFirst; gap < TstatesPerFrame/2 {
		t.Fatalf("resume advanced only %d T-states; re-triggered without progress", gap)
	}
}

func TestBreakpointsSortedAndRemovable(t *testing.T) {
	m := newMachine(t, RAM6K)
	for _, pc := range []uint16{0x2C3A, 0x0038, 0x1000, 0x0038} {
		m.AddBreakpoint(pc)
	}
	want := []uint16{0x0038, 0x1000, 0x2C3A}
	got := m.Breakpoints()
	if len(got) != len(want) {
		t.Fatalf("Breakpoints() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Breakpoints() = %v, want %v", got, want)
		}
	}
	m.RemoveBreakpoint(0x1000)
	if got := m.Breakpoints(); len(got) != 2 {
		t.Fatalf("after remove: %v", got)
	}
}

func TestAddWatchValidation(t *testing.T) {
	m := newMachine(t, RAM6K)
	if err := m.AddWatch(0x3000, 0x2800, WatchRW); err == nil {
		t.Error("start > end accepted")
	}
	if err := m.AddWatch(0x2800, 0x2900, 0); err == nil {
		t.Error("zero kind accepted")
	}
	if err := m.AddWatch(0x2800, 0x2900, WatchRW+1); err == nil {
		t.Error("out-of-range kind accepted")
	}
	if err := m.AddWatch(0x2800, 0x2900, WatchWrite); err != nil {
		t.Errorf("valid watch rejected: %v", err)
	}
	m.RemoveWatch(0x2800)
	if len(m.Watches()) != 0 {
		t.Errorf("RemoveWatch left %v", m.Watches())
	}
}

func TestWatchWriteBootVideoRAM(t *testing.T) {
	m := newMachine(t, RAM6K)
	if err := m.AddWatch(0x2800, 0x29FF, WatchWrite); err != nil {
		t.Fatal(err)
	}
	s := m.RunDebug(50 * TstatesPerFrame)
	if s.Reason != StopWatch {
		t.Fatalf("Reason = %d, want StopWatch (boot writes the screen)", s.Reason)
	}
	if !s.Write {
		t.Error("Write = false, want true")
	}
	if s.Addr < 0x2800 || s.Addr > 0x29FF {
		t.Errorf("Addr = 0x%04X, outside watched range", s.Addr)
	}
}

// pokeLoop installs a program in the A7-clamp-proof region (addresses
// with bit 7 already set survive the power-on clamp untranslated) and
// points PC at it, without booting the ROM (interrupts stay disabled).
func pokeLoop(t *testing.T, m *Machine, addr uint16, prog []byte) {
	t.Helper()
	if err := m.LoadBinary(addr, prog); err != nil {
		t.Fatal(err)
	}
	st := m.CPU().State()
	st.PC = addr
	m.CPU().SetState(st)
}

func TestWatchReadFires(t *testing.T) {
	m := newMachine(t, RAM6K)
	// LD A,(2CD0); JR -5 (back to the LD), all in clamp-proof addresses.
	pokeLoop(t, m, 0x2CC0, []byte{0x3A, 0xD0, 0x2C, 0x18, 0xFB})
	if err := m.LoadBinary(0x2CD0, []byte{0xA7}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddWatch(0x2CD0, 0x2CD0, WatchRead); err != nil {
		t.Fatal(err)
	}

	s := m.RunDebug(TstatesPerFrame)
	if s.Reason != StopWatch {
		t.Fatalf("Reason = %d, want StopWatch", s.Reason)
	}
	if s.Write {
		t.Error("Write = true, want false")
	}
	if s.Addr != 0x2CD0 || s.Data != 0xA7 {
		t.Errorf("Addr/Data = 0x%04X/0x%02X, want 0x2CD0/0xA7", s.Addr, s.Data)
	}
	if s.PC != 0x2CC3 {
		t.Errorf("PC = 0x%04X, want 0x2CC3 (after the touching instruction)", s.PC)
	}
}

func TestWatchWriteFires(t *testing.T) {
	m := newMachine(t, RAM6K)
	// LD A,42; LD (2CD0),A; JR -7 (back to the top).
	pokeLoop(t, m, 0x2CC0, []byte{0x3E, 0x42, 0x32, 0xD0, 0x2C, 0x18, 0xF9})
	if err := m.AddWatch(0x2CD0, 0x2CD0, WatchWrite); err != nil {
		t.Fatal(err)
	}

	s := m.RunDebug(TstatesPerFrame)
	if s.Reason != StopWatch || !s.Write {
		t.Fatalf("stop = %+v, want write watch hit", s)
	}
	if s.Addr != 0x2CD0 || s.Data != 0x42 {
		t.Errorf("Addr/Data = 0x%04X/0x%02X, want 0x2CD0/0x42", s.Addr, s.Data)
	}
}

// TestWatchIgnoresNonCPUAccess pins the load-bearing property of the
// pins-based hook: neither the per-scanline video refresh fetch (which
// reads memory through MemRead, bypassing the bus pins) nor DumpMemory
// may trip a read watch. The CPU runs a JR self-loop whose only bus
// reads are its own opcode fetches (M1, excluded) and its displacement
// operand at 0x2CC1 — everything below 0x2C00, including the refresh
// window at 0x00xx (I=0), is watched and must stay silent.
func TestWatchIgnoresNonCPUAccess(t *testing.T) {
	m := newMachine(t, RAM6K)
	pokeLoop(t, m, 0x2CC0, []byte{0x18, 0xFE}) // JR -2: loop to self
	if err := m.AddWatch(0x0000, 0x2C00, WatchRead); err != nil {
		t.Fatal(err)
	}

	if s := m.RunDebug(2 * TstatesPerFrame); s.Reason != StopBudget {
		t.Fatalf("stop = %+v, want StopBudget (video refresh fetch tripped a watch?)", s)
	}

	m.DumpMemory(0x0000, 0x2C00)
	if m.watchHit {
		t.Error("DumpMemory latched a watch hit")
	}
}

// TestRunDebugEquivalence is the determinism anchor: a run chopped up by
// breakpoint and watch stops must be byte-identical to an uninterrupted
// run of the same length.
func TestRunDebugEquivalence(t *testing.T) {
	const frames = 12
	plain := newMachine(t, RAM6K)
	plain.RunTstates(frames * TstatesPerFrame)

	dbg := newMachine(t, RAM6K)
	dbg.AddBreakpoint(isrEntry)
	if err := dbg.AddWatch(0x2800, 0x29FF, WatchWrite); err != nil {
		t.Fatal(err)
	}
	target := uint64(frames * TstatesPerFrame)
	for dbg.Tstates() < target {
		dbg.RunDebug(target - dbg.Tstates())
	}

	if plain.Tstates() != dbg.Tstates() {
		t.Fatalf("Tstates diverged: %d vs %d", plain.Tstates(), dbg.Tstates())
	}
	if plain.CPU().State() != dbg.CPU().State() {
		t.Fatalf("CPU state diverged:\n%+v\n%+v", plain.CPU().State(), dbg.CPU().State())
	}
	if !bytes.Equal(plain.ram, dbg.ram) {
		t.Fatal("RAM diverged")
	}
	a := make([]byte, FrameWidth*FrameHeight)
	b := make([]byte, FrameWidth*FrameHeight)
	plain.Frame(a)
	dbg.Frame(b)
	if !bytes.Equal(a, b) {
		t.Fatal("frame buffers diverged")
	}
}

func TestRunDebugBudget(t *testing.T) {
	plain := newMachine(t, RAM6K)
	plain.RunTstates(3 * TstatesPerFrame)

	dbg := newMachine(t, RAM6K)
	s := dbg.RunDebug(3 * TstatesPerFrame)
	if s.Reason != StopBudget {
		t.Fatalf("Reason = %d, want StopBudget", s.Reason)
	}
	if plain.Tstates() != dbg.Tstates() {
		t.Fatalf("budget run consumed %d T-states, RunTstates consumed %d", dbg.Tstates(), plain.Tstates())
	}
	if s.PC != dbg.CPU().State().PC {
		t.Errorf("Stop.PC = 0x%04X, CPU PC = 0x%04X", s.PC, dbg.CPU().State().PC)
	}
}

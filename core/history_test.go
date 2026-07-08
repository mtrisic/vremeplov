package core

import (
	"bytes"
	"testing"

	"github.com/mtrisic/gozilog/z80"
)

func snapshotBytes(t *testing.T, s *Snapshot) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestRewindExact is the anchor: a run with every kind of journaled
// input — typed text, immediate presses, tape deck, a poke — rewound to
// a mid-run point must reproduce the machine byte-for-byte (gob-equal
// snapshots).
func TestRewindExact(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.EnableHistory(10*TstatesPerFrame, 100)

	m.RunTstates(150 * TstatesPerFrame) // boot with history on
	end, err := m.TypeText("10 PRINT 5\n")
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(end - m.Tstates() + 5*TstatesPerFrame)

	m.PressKey(KeyA)
	m.RunTstates(3 * TstatesPerFrame)
	m.ReleaseKey(KeyA)
	if err := m.LoadBinary(0x2CD0, []byte{0xA7}); err != nil {
		t.Fatal(err)
	}
	m.InsertTape(CompileTapeBlocks(nativeStream(0x2CD0, []byte{1, 2})))
	m.PlayTape()
	m.RunTstates(5 * TstatesPerFrame)

	manual := m.Snapshot()
	target := m.Tstates()
	// Advance off the target boundary before mutating again: "the state
	// at boundary T" includes every input applied at T, so the reference
	// snapshot must precede any same-T mutations.
	m.RunTstates(1)

	m.PressKey(KeyB)
	m.RunTstates(10 * TstatesPerFrame)
	m.ReleaseKey(KeyB)
	m.StopTape()
	if _, err := m.TypeText("RUN\n"); err != nil {
		t.Fatal(err)
	}
	m.Reset()
	m.RunTstates(30 * TstatesPerFrame)

	if err := m.RewindTo(target); err != nil {
		t.Fatal(err)
	}
	if got := m.Tstates(); got != target {
		t.Fatalf("rewound to T=%d, want %d", got, target)
	}
	if !bytes.Equal(snapshotBytes(t, m.Snapshot()), snapshotBytes(t, manual)) {
		t.Fatal("rewound machine differs from the reference snapshot")
	}
}

// TestStepBackWalksBoundaries records instruction boundaries going
// forward and walks them back one (and several) at a time, across
// automatic snapshot boundaries.
func TestStepBackWalksBoundaries(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.RunTstates(50 * TstatesPerFrame)
	m.EnableHistory(100, 200) // snapshot every ~15 instructions

	const steps = 40
	bounds := make([]uint64, steps)
	cpuStates := make([]z80.State, steps)
	for i := 0; i < steps; i++ {
		bounds[i] = m.Tstates()
		cpuStates[i] = m.CPU().State()
		m.StepInstruction()
	}

	for i := steps - 1; i >= steps-20; i-- {
		if err := m.StepBack(1); err != nil {
			t.Fatalf("StepBack(1) at boundary %d: %v", i, err)
		}
		if m.Tstates() != bounds[i] {
			t.Fatalf("StepBack landed at T=%d, want %d (boundary %d)", m.Tstates(), bounds[i], i)
		}
		if m.CPU().State() != cpuStates[i] {
			t.Fatalf("CPU state differs at boundary %d", i)
		}
	}

	if err := m.StepBack(5); err != nil {
		t.Fatal(err)
	}
	if want := bounds[steps-20-5]; m.Tstates() != want {
		t.Fatalf("StepBack(5) landed at T=%d, want %d", m.Tstates(), want)
	}
}

// TestRewindDiscardsFuture: after a rewind the old timeline's inputs are
// gone — running forward again diverges from the original.
func TestRewindDiscardsFuture(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.EnableHistory(10*TstatesPerFrame, 100)
	m.RunTstates(50 * TstatesPerFrame)
	target := m.Tstates()
	m.RunTstates(1)

	m.PressKey(KeyA)
	m.RunTstates(10 * TstatesPerFrame)
	if m.MemRead(0x2000+uint16(KeyA))&1 != 0 {
		t.Fatal("A not pressed on the original timeline")
	}

	if err := m.RewindTo(target); err != nil {
		t.Fatal(err)
	}
	if _, newest, _ := m.HistorySpan(); newest != m.Tstates() {
		t.Fatalf("HistorySpan newest = %d, want now %d", newest, m.Tstates())
	}
	if m.MemRead(0x2000+uint16(KeyA))&1 == 0 {
		t.Fatal("A still pressed right after rewind")
	}
	m.RunTstates(10 * TstatesPerFrame)
	if m.MemRead(0x2000+uint16(KeyA))&1 == 0 {
		t.Fatal("discarded key press reappeared on the new timeline")
	}
	// History keeps working on the new timeline.
	if err := m.RewindTo(target); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryBounds(t *testing.T) {
	m := newMachine(t, RAM6K)
	if err := m.Rewind(1); err == nil {
		t.Error("Rewind accepted with history disabled")
	}
	if err := m.StepBack(1); err == nil {
		t.Error("StepBack accepted with history disabled")
	}

	m.EnableHistory(100, 3)
	m.RunTstates(TstatesPerFrame) // far more than 3 intervals
	if len(m.histRing) > 3 {
		t.Fatalf("ring holds %d snapshots, depth is 3", len(m.histRing))
	}
	oldest, newest, ok := m.HistorySpan()
	if !ok || oldest != m.histRing[0].Tstates || newest != m.Tstates() {
		t.Fatalf("HistorySpan = (%d,%d,%v)", oldest, newest, ok)
	}
	if err := m.RewindTo(oldest - 1); err == nil {
		t.Error("rewind before recorded history accepted")
	}
	if err := m.RewindTo(newest + 1); err == nil {
		t.Error("rewind into the future accepted")
	}
	// Rewind(huge) clamps to the oldest point.
	if err := m.Rewind(1 << 60); err != nil {
		t.Fatal(err)
	}
	if m.Tstates() != oldest {
		t.Fatalf("clamped rewind landed at %d, want oldest %d", m.Tstates(), oldest)
	}

	m.HistoryRebase()
	if len(m.histRing) != 1 || len(m.histJournal) != 0 {
		t.Fatal("HistoryRebase did not reset ring/journal")
	}

	m.DisableHistory()
	if m.HistoryEnabled() {
		t.Fatal("still enabled after Disable")
	}
	if _, _, ok := m.HistorySpan(); ok {
		t.Fatal("HistorySpan ok after Disable")
	}
}

// TestRestoreRebasesHistory: a manual Restore starts a new baseline.
func TestRestoreRebasesHistory(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.EnableHistory(10*TstatesPerFrame, 100)
	m.RunTstates(30 * TstatesPerFrame)
	early := m.Snapshot()
	m.RunTstates(30 * TstatesPerFrame)

	if err := m.Restore(early); err != nil {
		t.Fatal(err)
	}
	if len(m.histRing) != 1 || m.histRing[0].Tstates != m.Tstates() {
		t.Fatal("Restore did not rebase history")
	}
	if err := m.RewindTo(early.Tstates - 1); err == nil {
		t.Fatal("pre-Restore history still reachable")
	}
}

// TestRewindRecorderAndBreakpoints: rewinding drops recorder pulses
// captured on the abandoned timeline; breakpoints survive and fire.
func TestRewindRecorderAndBreakpoints(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.EnableHistory(10*TstatesPerFrame, 100)
	m.AddBreakpoint(0x0038)
	m.StartTapeRecording()
	m.RunTstates(20 * TstatesPerFrame)
	target := m.Tstates()
	m.RunTstates(1)

	// Simulate pulses captured after the target on the old timeline.
	m.recStarts = append(m.recStarts, target-100, target+500, target+900)
	m.RunTstates(5 * TstatesPerFrame)

	if err := m.RewindTo(target); err != nil {
		t.Fatal(err)
	}
	if len(m.recStarts) != 1 || m.recStarts[0] != target-100 {
		t.Fatalf("recorder pulses after rewind: %v", m.recStarts)
	}
	if !m.TapeRecording() {
		t.Fatal("recorder disarmed by rewind")
	}
	if bps := m.Breakpoints(); len(bps) != 1 || bps[0] != 0x0038 {
		t.Fatalf("breakpoints after rewind: %v", bps)
	}
	// The breakpoint still fires on the new timeline (boot enables
	// interrupts well within the budget).
	if s := m.RunDebug(300 * TstatesPerFrame); s.Reason != StopBreakpoint {
		t.Fatalf("breakpoint did not fire after rewind (reason %d)", s.Reason)
	}
}

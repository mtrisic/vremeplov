package core

import (
	"testing"

	"github.com/mtrisic/gozilog/z80"
)

// stepUntilISR runs instructions until one ends at PC=0x0038 having
// consumed at least the 13T IM1 acceptance — i.e. an interrupt was just
// accepted. Returns the machine T-state right after acceptance.
func stepUntilISR(t *testing.T, m *Machine, limit uint64) uint64 {
	t.Helper()
	start := m.Tstates()
	for m.Tstates()-start < limit {
		consumed := m.StepInstruction()
		if consumed >= 13 && m.CPU().State().PC == 0x0038 {
			return m.Tstates()
		}
	}
	t.Fatal("no interrupt accepted within limit")
	return 0
}

// TestInterruptCadence boots ROM A far enough for EI, then checks that
// interrupt acceptances are exactly one frame apart and that the CPU
// resumes hsync-aligned: after the ack's WAIT-to-hsync stretch, the
// fixed 10 T-states of ack tail + PC push put the first ISR opcode
// fetch at frame-phase 10 mod 192.
func TestInterruptCadence(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.RunTstates(30 * TstatesPerFrame) // boot: RAM sizing, im 1, ei

	t1 := stepUntilISR(t, m, 3*TstatesPerFrame)
	if got := t1 % TstatesPerLine; got != 10 {
		t.Errorf("ISR entry at line phase %d, want 10", got)
	}
	t2 := stepUntilISR(t, m, 3*TstatesPerFrame)
	if t2-t1 != TstatesPerFrame {
		t.Errorf("ISR period %d T-states, want %d", t2-t1, TstatesPerFrame)
	}
	// The ISR must be entered at the assert line (56) — the ack WAIT
	// stretches to the *next* hsync, so first opcode is on line 57.
	if line := (t1 % TstatesPerFrame) / TstatesPerLine; line != 57 {
		t.Errorf("ISR entered on line %d, want 57", line)
	}
}

// TestScanlineLatchCadence verifies the ROM's drawn-scanline loop is
// exactly 192 T-states as observed through latch row writes: during the
// 12 drawn lines of a character row, consecutive writes of the same row
// value to the latch are 192 T apart.
func TestScanlineLatchCadence(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.RunTstates(30 * TstatesPerFrame)
	stepUntilISR(t, m, 3*TstatesPerFrame)

	// Record (tstate, value) for every latch write during one ISR pass.
	type write struct {
		t   uint64
		val byte
	}
	var writes []write
	prev := m.Latch()
	limit := m.Tstates() + TstatesPerFrame
	for m.Tstates() < limit {
		m.StepInstruction()
		if v := m.Latch(); v != prev {
			writes = append(writes, write{m.Tstates(), v})
			prev = v
		}
	}
	// The scanline loop writes the row value (rows 1..12) at the top of
	// each drawn line and a row-0 blanking value at its end. Filter to
	// the row-value writes: consecutive +1 row steps within a character
	// row must be exactly one scanline (192 T) apart.
	var rowWrites []write
	for _, w := range writes {
		if r := (w.val >> 2) & 0x0F; r >= 1 && r <= 12 {
			rowWrites = append(rowWrites, w)
		}
	}
	rowAdvances := 0
	for i := 1; i < len(rowWrites); i++ {
		r1 := (rowWrites[i-1].val >> 2) & 0x0F
		r2 := (rowWrites[i].val >> 2) & 0x0F
		if r2 == r1+1 {
			rowAdvances++
			if d := rowWrites[i].t - rowWrites[i-1].t; d != TstatesPerLine {
				t.Errorf("row %d->%d latch writes %d T apart, want exactly %d", r1, r2, d, TstatesPerLine)
			}
		}
	}
	// 16 character rows × 11 in-row advances (1->2 .. 11->12).
	if rowAdvances < 16*11 {
		t.Errorf("saw %d row-advance latch writes in one frame, want >= %d", rowAdvances, 16*11)
	}
}

// TestGlyphIndex pins the empirically derived data-line wiring
// (AGENTS.md log 9): D6 unconnected, D7 -> A6.
func TestGlyphIndex(t *testing.T) {
	cases := []struct {
		code byte
		want uint16
	}{
		{'A', 0x01},         // ASCII letter -> glyph 0x01
		{'R', 0x12},         // 'R' & 0x3F
		{' ', 0x20},         // punctuation half
		{0x01, 0x01},        // scan-code style value, same glyph
		{0xBF, 0x7F},        // block graphics: bit 7 -> A6
		{0x41 | 0x80, 0x41}, // code 0xC1: graphics half
	}
	for _, c := range cases {
		if got := glyphIndex(c.code); got != c.want {
			t.Errorf("glyphIndex(0x%02X) = 0x%02X, want 0x%02X", c.code, got, c.want)
		}
	}
}

// TestTickerContract sanity-checks that the machine sees every T-state:
// gozilog's counter and the machine counter advance in lockstep from
// power-on (waits included in both).
func TestTickerContract(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.RunTstates(TstatesPerFrame)
	if m.Tstates() != m.CPU().Tstates() {
		t.Fatalf("machine counter %d != cpu counter %d", m.Tstates(), m.CPU().Tstates())
	}
	var _ z80.Ticker = m // compile-time contract
}

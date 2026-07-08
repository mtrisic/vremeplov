package core

import (
	"bytes"
	"strings"
	"testing"
)

// captureSaveStream boots a machine, types program lines, runs SAVE, and
// captures the tape waveform the ROM writes to the latch: every pulse's
// start T-state plus the measured high/low half durations. It returns
// the pulse start times and the machine (stopped after SAVE finished).
func captureSaveStream(t *testing.T, program string) (starts []uint64, hiDurs, gaps map[uint64]int, m *Machine) {
	t.Helper()
	m = bootMachine(t)
	typeString(m, program)
	// Each typed key occupies 5 frames (3 hold + 2 gap); wait out the
	// whole listing plus margin before typing SAVE.
	m.RunTstates(uint64(5*len(program)+40) * TstatesPerFrame)
	typeString(m, "SAVE\n")

	type change struct {
		t     uint64
		latch byte
	}
	var changes []change
	prev := m.Latch()
	deadline := m.Tstates() + 3000*TstatesPerFrame
	for m.Tstates() < deadline {
		m.StepInstruction()
		if l := m.Latch(); l != prev {
			if l == 0xFC || l == 0xB8 || l == 0xBC {
				changes = append(changes, change{m.Tstates(), l})
			}
			prev = l
		}
	}
	hiDurs, gaps = map[uint64]int{}, map[uint64]int{}
	for i := 0; i+2 < len(changes); i++ {
		if changes[i].latch == 0xFC && changes[i+1].latch == 0xB8 && changes[i+2].latch == 0xBC {
			if n := len(starts); n > 0 {
				gaps[changes[i].t-starts[n-1]]++
			}
			starts = append(starts, changes[i].t)
			hiDurs[changes[i+1].t-changes[i].t]++
		}
	}
	if len(starts) == 0 {
		t.Fatal("SAVE produced no tape pulses")
	}
	return starts, hiDurs, gaps, m
}

// TestSaveNativeStream runs the ROM's own SAVE and asserts both the
// native stream layout (SPEC.md §3.6) and that the waveform matches the
// timing constants the tape deck compiles with (they were measured from
// exactly this capture; AGENTS.md log 12).
func TestSaveNativeStream(t *testing.T) {
	starts, hiDurs, gaps := func() ([]uint64, map[uint64]int, map[uint64]int) {
		s, h, g, _ := captureSaveStream(t, "10 PRINT 1\n")
		return s, h, g
	}()

	// Waveform: every pulse's high half is exactly tapePulseWidth.
	for d, n := range hiDurs {
		if d != tapePulseWidth {
			t.Errorf("pulse high half of %d T seen %d times, want %d", d, n, tapePulseWidth)
		}
	}
	// Cell timing: the dominant start-to-start gaps must be the compiled
	// constants (interbyte gaps vary by ±10 T with ROM code paths, so
	// those are checked as the dominant value in their band).
	mode := func(lo, hi uint64) (best uint64, n int) {
		for g, c := range gaps {
			if g >= lo && g < hi && c > n {
				best, n = g, c
			}
		}
		return
	}
	if g, n := mode(8000, 11000); g != tapeZeroCell || n == 0 {
		t.Errorf("zero-bit cell = %d T (×%d), want %d", g, n, tapeZeroCell)
	}
	// A "1" cell yields two gaps in the 3000–6000 band: cell start →
	// second pulse (tapeOneSplit) and second pulse → next cell
	// (tapeOneCell − tapeOneSplit). Nothing else may appear there.
	for g, n := range gaps {
		if g >= 3000 && g < 6000 && g != tapeOneSplit && g != tapeOneCell-tapeOneSplit {
			t.Errorf("unexpected 1-bit band gap %d T (×%d), want %d or %d",
				g, n, tapeOneSplit, tapeOneCell-tapeOneSplit)
		}
	}
	if g, n := mode(20000, 25000); n == 0 || g != tapeZeroCell+tapeInterbyteExtra {
		t.Errorf("interbyte gap after zero bit = %d T (×%d), want %d",
			g, n, tapeZeroCell+tapeInterbyteExtra)
	}

	// Stream layout: leader, sync, addresses, data, checksum.
	stream := decodePulseBytes(starts)
	if len(stream) < tapeLeaderBytes+6 {
		t.Fatalf("decoded only %d bytes", len(stream))
	}
	for i, b := range stream[:tapeLeaderBytes] {
		if b != 0 {
			t.Fatalf("leader byte %d = 0x%02X, want 0x00", i, b)
		}
	}
	p := stream[tapeLeaderBytes:]
	if p[0] != 0xA5 {
		t.Fatalf("sync byte 0x%02X, want 0xA5", p[0])
	}
	start := uint16(p[1]) | uint16(p[2])<<8
	end := uint16(p[3]) | uint16(p[4])<<8
	if start != 0x2C36 {
		t.Errorf("start address 0x%04X, want 0x2C36 (BASIC_START pointer)", start)
	}
	n := int(end - start)
	if len(p) < 5+n+1 {
		t.Fatalf("stream too short: %d bytes for %d data", len(p), n)
	}
	sum := byte(0)
	for _, v := range p[:5+n+1] {
		sum += v
	}
	if sum != 0xFF {
		t.Errorf("stream sums to 0x%02X, want 0xFF", sum)
	}
}

// rawRAM reads populated RAM directly, bypassing the A7 clamp: DumpMemory
// gives the CPU-eye view, which aliases 0x2Cxx to 0x2Cxx|0x80 whenever the
// machine stopped at a latch-b7=0 moment (e.g. mid-ISR), so equality
// checks on program memory must read the underlying array.
func rawRAM(m *Machine, start, end int) []byte {
	return append([]byte(nil), m.ram[start-ramBase:end-ramBase]...)
}

// TestTapeRoundtrip is the full loop: what one machine SAVEs, a second
// machine OLDs back through the compiled pulse schedule — the ROM's own
// load routine reading the deck's comparator output.
func TestTapeRoundtrip(t *testing.T) {
	starts, _, _, saver := captureSaveStream(t, "10 PRINT 123\n20 GOTO 10\n")
	stream := decodePulseBytes(starts)[tapeLeaderBytes:]
	start := uint16(stream[1]) | uint16(stream[2])<<8
	end := uint16(stream[3]) | uint16(stream[4])<<8
	want := rawRAM(saver, int(start), int(end))

	m := bootMachine(t)
	typeString(m, "OLD\n")
	m.RunTstates(20 * TstatesPerFrame) // let OLD start listening
	m.InsertTape(CompileTapeBlocks(stream))
	m.PlayTape()
	endT, ok := m.TapeEndTstate()
	if !ok {
		t.Fatal("tape not playing")
	}
	m.RunTstates(endT - m.Tstates() + 100*TstatesPerFrame)

	got := rawRAM(m, int(start), int(end))
	if !bytes.Equal(got, want) {
		t.Fatalf("loaded memory differs from saved program\nwant % X\ngot  % X", want, got)
	}
	screen := strings.Join(m.ScreenText(), "\n")
	if !strings.Contains(screen, "READY") {
		t.Fatalf("no READY after OLD:\n%s", screen)
	}
	// The loaded program must be runnable.
	typeString(m, "RUN\n")
	m.RunTstates(100 * TstatesPerFrame)
	screen = strings.Join(m.ScreenText(), "\n")
	if !strings.Contains(screen, "123") {
		t.Fatalf("loaded program did not run:\n%s", screen)
	}
}

func TestTapeScheduleActive(t *testing.T) {
	s := CompileTapeBlocks([]byte{0xA5})
	if len(s.Starts) == 0 || s.Width != tapePulseWidth {
		t.Fatalf("schedule: %d pulses, width %d", len(s.Starts), s.Width)
	}
	first := s.Starts[0]
	for rel, want := range map[uint64]bool{
		first:                     true,
		first + s.Width - 1:       true,
		first + s.Width:           false,
		first + tapeZeroCell - 10: false,
	} {
		if got := s.active(rel); got != want {
			t.Errorf("active(%d) = %v, want %v", rel, got, want)
		}
	}
	if s.active(s.Duration() + 1) {
		t.Error("active after Duration")
	}
	// Leader (96 zero bytes) then one byte with three 1-bits (0xA5):
	// 96*8 + 8 cells, 1-bits add one pulse each.
	wantPulses := (tapeLeaderBytes+1)*8 + 4
	if len(s.Starts) != wantPulses {
		t.Errorf("pulse count %d, want %d", len(s.Starts), wantPulses)
	}
}

// TestSnapshotMidTapeLoad snapshots a machine in the middle of an OLD
// tape load and verifies the restored machine finishes the load exactly
// like the original (snapshot v2 carries the deck).
func TestSnapshotMidTapeLoad(t *testing.T) {
	starts, _, _, _ := captureSaveStream(t, "10 PRINT 7\n")
	stream := decodePulseBytes(starts)[tapeLeaderBytes:]
	end := uint16(stream[3]) | uint16(stream[4])<<8

	m := bootMachine(t)
	typeString(m, "OLD\n")
	m.RunTstates(30 * TstatesPerFrame)
	m.InsertTape(CompileTapeBlocks(stream))
	m.PlayTape()
	endT, _ := m.TapeEndTstate()
	m.RunTstates((endT - m.Tstates()) / 2) // mid-load

	snap := m.Snapshot()
	if snap.Tape == nil || !snap.TapePlaying {
		t.Fatal("snapshot lost the playing tape")
	}
	m2 := newMachine(t, RAM6K)
	if err := m2.Restore(snap); err != nil {
		t.Fatal(err)
	}

	rest := endT - m.Tstates() + 100*TstatesPerFrame
	m.RunTstates(rest)
	m2.RunTstates(rest)
	a := rawRAM(m, 0x2C36, int(end))
	b := rawRAM(m2, 0x2C36, int(end))
	if !bytes.Equal(a, b) {
		t.Fatal("restored machine diverged during tape load")
	}
	if !strings.Contains(strings.Join(m2.ScreenText(), "\n"), "READY") {
		t.Fatal("restored machine did not finish OLD")
	}
}

func TestTapeDeckControls(t *testing.T) {
	m := newMachine(t, RAM6K)
	if _, ok := m.TapeEndTstate(); ok {
		t.Fatal("TapeEndTstate with no tape")
	}
	m.PlayTape() // no tape: must stay stopped
	if m.TapePlaying() {
		t.Fatal("playing with no tape inserted")
	}
	s := CompileTapeBlocks([]byte{0xA5, 0x00})
	m.InsertTape(s)
	if m.TapePlaying() {
		t.Fatal("InsertTape must not start playback")
	}
	m.PlayTape()
	if !m.TapePlaying() {
		t.Fatal("PlayTape did not start")
	}
	m.StopTape()
	if m.TapePlaying() {
		t.Fatal("StopTape did not stop")
	}
	m.InsertTape(nil) // eject
	m.PlayTape()
	if m.TapePlaying() {
		t.Fatal("playing after eject")
	}
}

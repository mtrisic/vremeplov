package core

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core/gtp"
)

// recordSave boots a machine, types the program, and records its SAVE
// through the production recorder API (RunTstates, not manual stepping —
// this exercises the MemWrite hook path).
func recordSave(t *testing.T, program string) (streams [][]byte, m *Machine) {
	t.Helper()
	m = bootMachine(t)
	typeString(m, program)
	m.RunTstates(uint64(5*len(program)+40) * TstatesPerFrame)
	m.StartTapeRecording()
	if !m.TapeRecording() {
		t.Fatal("recorder did not arm")
	}
	typeString(m, "SAVE\n")
	m.RunTstates(1000 * TstatesPerFrame)
	return m.StopTapeRecording(), m
}

func TestRecorderCapturesSave(t *testing.T) {
	streams, m := recordSave(t, "10 PRINT 123\n")
	if len(streams) != 1 {
		t.Fatalf("captured %d streams, want 1", len(streams))
	}
	if m.TapeRecording() {
		t.Fatal("recorder still armed after Stop")
	}

	b := gtp.Block{Type: gtp.BlockStandard, Payload: streams[0]}
	sec, err := b.Section()
	if err != nil {
		t.Fatalf("captured stream is not a valid standard block: %v", err)
	}
	if sec.Start != 0x2C36 {
		t.Errorf("Start = 0x%04X, want 0x2C36 (BASIC_START pointer)", sec.Start)
	}
	if want := rawRAM(m, int(sec.Start), int(sec.End)); !bytes.Equal(sec.Data, want) {
		t.Errorf("captured data differs from saver RAM\nwant % X\ngot  % X", want, sec.Data)
	}
}

// TestRecorderFullCircle is the anchor: SAVE on one machine, wrap the
// capture with gtp.Build, parse it back, play it faithfully into a fresh
// machine's OLD, and run the program.
func TestRecorderFullCircle(t *testing.T) {
	streams, saver := recordSave(t, "10 PRINT 123\n")
	if len(streams) == 0 {
		t.Fatal("nothing captured")
	}

	img, err := gtp.Build("fullcircle", streams...)
	if err != nil {
		t.Fatal(err)
	}
	f, err := gtp.Parse(img)
	if err != nil {
		t.Fatalf("recorded image does not parse: %v", err)
	}
	if f.Name != "fullcircle" {
		t.Errorf("Name = %q", f.Name)
	}
	secs, err := f.Sections()
	if err != nil {
		t.Fatal(err)
	}
	payloads := make([][]byte, 0, len(f.Blocks))
	for _, b := range f.Blocks {
		if b.Type == gtp.BlockStandard {
			payloads = append(payloads, b.Payload)
		}
	}

	m := bootMachine(t)
	typeString(m, "OLD\n")
	m.RunTstates(20 * TstatesPerFrame)
	m.InsertTape(CompileTapeBlocks(payloads...))
	m.PlayTape()
	endT, ok := m.TapeEndTstate()
	if !ok {
		t.Fatal("tape not playing")
	}
	m.RunTstates(endT - m.Tstates() + 100*TstatesPerFrame)

	want := rawRAM(saver, int(secs[0].Start), int(secs[0].End))
	if got := rawRAM(m, int(secs[0].Start), int(secs[0].End)); !bytes.Equal(got, want) {
		t.Fatalf("loaded memory differs from saved program\nwant % X\ngot  % X", want, got)
	}
	typeString(m, "RUN\n")
	m.RunTstates(100 * TstatesPerFrame)
	if screen := strings.Join(m.ScreenText(), "\n"); !strings.Contains(screen, "123") {
		t.Fatalf("recorded program did not run:\n%s", screen)
	}
}

// TestRecorderIgnoresVideo pins that ordinary latch traffic (boot, the
// video ISR's row writes, typing) cannot fabricate pulses: the tape-high
// level needs latch bit 6, which only SAVE sets.
func TestRecorderIgnoresVideo(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.StartTapeRecording()
	m.RunTstates(200 * TstatesPerFrame) // boot with the recorder armed
	typeString(m, "10 PRINT 1\n")
	m.RunTstates(100 * TstatesPerFrame)
	if streams := m.StopTapeRecording(); streams != nil {
		t.Fatalf("video/boot traffic decoded as %d tape stream(s)", len(streams))
	}
}

func TestRecorderTwoSaves(t *testing.T) {
	m := bootMachine(t)
	typeString(m, "10 PRINT 123\n")
	m.RunTstates(uint64(5*13+40) * TstatesPerFrame)
	m.StartTapeRecording()
	typeString(m, "SAVE\n")
	m.RunTstates(1000 * TstatesPerFrame)
	typeString(m, "SAVE\n")
	m.RunTstates(1000 * TstatesPerFrame)

	streams := m.StopTapeRecording()
	if len(streams) != 2 {
		t.Fatalf("captured %d streams, want 2", len(streams))
	}
	var secs []*gtp.Section
	for i, s := range streams {
		b := gtp.Block{Type: gtp.BlockStandard, Payload: s}
		sec, err := b.Section()
		if err != nil {
			t.Fatalf("stream %d invalid: %v", i, err)
		}
		secs = append(secs, sec)
	}
	if !bytes.Equal(secs[0].Data, secs[1].Data) {
		t.Error("two SAVEs of the same program captured different data")
	}
}

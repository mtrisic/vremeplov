package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core/gtp"
)

// loadGTPFixture parses one of the sample images committed under
// core/gtp/testdata (provenance: roms/PROVENANCE.md).
func loadGTPFixture(t *testing.T, name string) *gtp.File {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("gtp", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	f, err := gtp.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestGTPFastLoad pokes hackaday.gtp's decoded section straight into
// memory after boot (the --load-gtp convenience path) and RUNs it. The
// section dump includes the BASIC pointers at 0x2C36, so no further
// fixup is needed.
func TestGTPFastLoad(t *testing.T) {
	f := loadGTPFixture(t, "hackaday.gtp")
	secs, err := f.Sections()
	if err != nil {
		t.Fatal(err)
	}

	m := bootMachine(t)
	for _, sec := range secs {
		if err := m.LoadBinary(sec.Start, sec.Data); err != nil {
			t.Fatal(err)
		}
	}
	end, err := m.TypeText("RUN\n")
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(end - m.Tstates() + 300*TstatesPerFrame)
	screen := strings.Join(m.ScreenText(), "\n")
	if strings.Contains(screen, "READY") || strings.Contains(screen, "WHAT?") {
		t.Fatalf("program did not take over after RUN:\n%s", screen)
	}
	if graphics := strings.Count(screen, "#"); graphics < 32*16*3/4 {
		t.Fatalf("expected a graphics-filled screen, got %d '#' cells:\n%s", graphics, screen)
	}
}

// TestGTPFaithfulLoad plays hackaday.gtp into the ROM's OLD routine
// through the tape deck — the Phase-3 gate's faithful-playback path —
// then RUNs the loaded program.
func TestGTPFaithfulLoad(t *testing.T) {
	f := loadGTPFixture(t, "hackaday.gtp")
	secs, err := f.Sections()
	if err != nil {
		t.Fatal(err)
	}
	sec := secs[0]

	var streams [][]byte
	for _, b := range f.Blocks {
		if b.Type == gtp.BlockStandard {
			streams = append(streams, b.Payload)
		}
	}

	m := bootMachine(t)
	typeString(m, "OLD\n")
	m.RunTstates(30 * TstatesPerFrame)
	m.InsertTape(CompileTapeBlocks(streams...))
	m.PlayTape()
	endT, ok := m.TapeEndTstate()
	if !ok {
		t.Fatal("tape not playing")
	}
	m.RunTstates(endT - m.Tstates() + 100*TstatesPerFrame)

	got := rawRAM(m, int(sec.Start), int(sec.End))
	if !bytes.Equal(got, sec.Data) {
		for i := range got {
			if got[i] != sec.Data[i] {
				t.Fatalf("loaded memory differs from GTP section at 0x%04X: got 0x%02X want 0x%02X",
					int(sec.Start)+i, got[i], sec.Data[i])
			}
		}
	}
	screen := strings.Join(m.ScreenText(), "\n")
	if !strings.Contains(screen, "READY") {
		t.Fatalf("no READY after OLD:\n%s", screen)
	}

	// The demo redraws the whole screen with block-graphics codes
	// (≥0x40, shown as '#' by ScreenText): assert RUN took over the
	// display rather than stopping at a prompt or error.
	typeString(m, "RUN\n")
	m.RunTstates(300 * TstatesPerFrame)
	screen = strings.Join(m.ScreenText(), "\n")
	if strings.Contains(screen, "READY") || strings.Contains(screen, "WHAT?") {
		t.Fatalf("program did not take over after RUN:\n%s", screen)
	}
	graphics := strings.Count(screen, "#")
	if graphics < 32*16*3/4 {
		t.Fatalf("expected a graphics-filled screen, got %d '#' cells:\n%s", graphics, screen)
	}
}

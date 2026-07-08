package main

import (
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/loader"
)

func TestFrameRGBA(t *testing.T) {
	pix := make([]byte, core.FrameWidth*core.FrameHeight)
	// One bright pixel at the active area's top-left corner.
	pix[core.ActiveY*core.FrameWidth+core.ActiveX] = 1
	dst := make([]byte, viewW*viewH*4)
	frameRGBA(pix, dst)
	if dst[0] != 0xFF || dst[1] != 0xFF || dst[2] != 0xFF || dst[3] != 0xFF {
		t.Fatalf("active-corner pixel = %v, want opaque white", dst[:4])
	}
	if dst[4] != 0 || dst[7] != 0xFF {
		t.Fatalf("neighbour pixel = %v, want opaque black", dst[4:8])
	}
	for i := 8; i < len(dst); i += 4 {
		if dst[i] != 0 {
			t.Fatalf("stray bright pixel at RGBA index %d", i)
		}
	}
}

func TestKeystrokesFor(t *testing.T) {
	cases := []struct {
		key  string
		want []core.Key
	}{
		{"a", []core.Key{core.KeyA}},
		{"Z", []core.Key{core.KeyZ}},
		{"3", []core.Key{core.Key3}},
		{" ", []core.Key{core.KeySpace}},
		{"Enter", []core.Key{core.KeyEnter}},
		{"Escape", []core.Key{core.KeyBreak}},
		{"Backspace", []core.Key{core.KeyDelete}},
		{"Tab", []core.Key{core.KeyList}},
		{"ArrowUp", []core.Key{core.KeyUp}},
		{"Shift", []core.Key{core.KeyShift}},
		{";", []core.Key{core.KeySemicolon}},
		{"+", []core.Key{core.KeyShift, core.KeySemicolon}},
		{"*", []core.Key{core.KeyShift, core.KeyColon}},
		{"?", []core.Key{core.KeyShift, core.KeySlash}},
		{"-", []core.Key{core.KeyShift, core.KeyEquals}},
	}
	for _, tc := range cases {
		got, ok := keystrokesFor(tc.key)
		if !ok {
			t.Errorf("keystrokesFor(%q) not mapped", tc.key)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("keystrokesFor(%q) = %v, want %v", tc.key, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("keystrokesFor(%q) = %v, want %v", tc.key, got, tc.want)
				break
			}
		}
	}
	for _, unmapped := range []string{"F5", "Control", "Meta", "Dead", "π"} {
		if _, ok := keystrokesFor(unmapped); ok {
			t.Errorf("keystrokesFor(%q) mapped, want ignored", unmapped)
		}
	}
}

// TestSnapshotRoundtrip: snapshotBytes/restoreSnapshot bring the
// machine back to the exact saved state.
func TestSnapshotRoundtrip(t *testing.T) {
	m, err := newMachine()
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(5 * core.TstatesPerFrame)
	saved := m.Tstates()
	data, err := snapshotBytes(m)
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(7 * core.TstatesPerFrame)
	if err := restoreSnapshot(m, data); err != nil {
		t.Fatal(err)
	}
	if got := m.Tstates(); got != saved {
		t.Fatalf("restored Tstates = %d, want %d", got, saved)
	}
	if err := restoreSnapshot(m, []byte("not a snapshot")); err == nil {
		t.Fatal("garbage snapshot accepted")
	}
}

// TestPasteStyleListing pins the paste flow's core contract: a CRLF
// clipboard listing (the usual copy from a forum or file) types cleanly
// through TypeText and runs.
func TestPasteStyleListing(t *testing.T) {
	m, err := newMachine()
	if err != nil {
		t.Fatal(err)
	}
	loader.ResetToReady(m)
	end, err := m.TypeText("10 PRINT 123\r\nRUN\r\n")
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(end - m.Tstates() + 100*core.TstatesPerFrame)
	if screen := strings.Join(m.ScreenText(), "\n"); !strings.Contains(screen, "123") {
		t.Fatalf("pasted listing did not run:\n%s", screen)
	}
}

package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core"
)

// TestFooterLayoutWraps: chips flow into rows at the window edge and
// never overlap; a too-narrow window still gets one chip per row.
func TestFooterLayoutWraps(t *testing.T) {
	widths := []int{100, 100, 100}
	pos, height := footerLayout(widths, 250)
	if pos[0][1] != pos[1][1] {
		t.Fatalf("first two chips should share a row: %v", pos)
	}
	if pos[2][1] <= pos[1][1] {
		t.Fatalf("third chip should wrap: %v", pos)
	}
	if want := 2*btnH + btnGap; height != want {
		t.Fatalf("height = %d, want %d", height, want)
	}

	pos, _ = footerLayout(widths, 2000)
	if pos[0][1] != pos[2][1] {
		t.Fatalf("wide window should keep one row: %v", pos)
	}
	if pos[1][0] != btnPadX+100+btnGap {
		t.Fatalf("second chip x = %d", pos[1][0])
	}

	pos, _ = footerLayout(widths, 50) // narrower than one chip
	for i := 1; i < len(pos); i++ {
		if pos[i][1] <= pos[i-1][1] {
			t.Fatalf("chips should stack when nothing fits: %v", pos)
		}
	}
}

// TestFooterSpecsTrackState: labels follow pause/record/sound/view/
// confirm state.
func TestFooterSpecsTrackState(t *testing.T) {
	g := testGame(t)
	find := func(id string) footSpec {
		for _, s := range g.footerSpecs() {
			if s.id == id {
				return s
			}
		}
		t.Fatalf("spec %q missing", id)
		return footSpec{}
	}
	if got := find("pause").text; got != "F2 pause" {
		t.Fatalf("pause label = %q", got)
	}
	if got := find("quit").text; got != "quit" {
		t.Fatalf("quit label = %q", got)
	}
	if got := find("rec").clr; got != clrTape {
		t.Fatalf("record color = %v before arming", got)
	}
	g.setPaused(true)
	g.m.StartTapeRecording()
	g.confirmQuit = true
	g.fullFrame = true
	if got := find("pause").text; got != "F2 resume" {
		t.Fatalf("paused label = %q", got)
	}
	if s := find("rec"); s.text != "F8 stop rec" || s.clr != clrTapeOn {
		t.Fatalf("armed record = %+v", s)
	}
	if got := find("quit").text; got != "sure?" {
		t.Fatalf("confirming quit label = %q", got)
	}
	if got := find("full").text; got != "F10 active" {
		t.Fatalf("full-frame label = %q", got)
	}
}

// TestQuitConfirmation: the two-step quit — first action arms, second
// confirms, any other chrome action cancels.
func TestQuitConfirmation(t *testing.T) {
	g := testGame(t)
	g.chromeAction("quit")
	if g.quit || !g.confirmQuit {
		t.Fatalf("first quit: quit=%v confirm=%v", g.quit, g.confirmQuit)
	}
	g.chromeAction("pause")
	if g.confirmQuit {
		t.Fatal("another action should cancel the confirmation")
	}
	if !g.paused {
		t.Fatal("the cancelling action should still run")
	}
	g.chromeAction("quit")
	g.chromeAction("quit")
	if !g.quit {
		t.Fatal("second quit should confirm")
	}
}

// TestSnapshotChromeRoundtrip: F5 writes a gob, F6 restores the newest
// one to the exact T-state.
func TestSnapshotChromeRoundtrip(t *testing.T) {
	g := testGame(t)
	t.Chdir(t.TempDir())

	g.m.RunTstates(5 * core.TstatesPerFrame)
	saved := g.m.Tstates()
	g.chromeAction("save")
	if !strings.Contains(g.status, "snapshot saved") {
		t.Fatalf("save status = %q", g.status)
	}
	g.m.RunTstates(7 * core.TstatesPerFrame)
	g.chromeAction("load")
	if got := g.m.Tstates(); got != saved {
		t.Fatalf("restored Tstates = %d, want %d", got, saved)
	}
}

func TestSnapshotLoadNothingThere(t *testing.T) {
	g := testGame(t)
	t.Chdir(t.TempDir())
	g.chromeAction("load")
	if !strings.Contains(g.status, "no vremeplov-snap-") {
		t.Fatalf("status = %q", g.status)
	}
}

// TestNewestSnapshotFile: numeric ordering, not lexicographic — 100
// beats 9.
func TestNewestSnapshotFile(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, n := range []string{"vremeplov-snap-9.gob", "vremeplov-snap-100.gob"} {
		if err := os.WriteFile(n, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	name, err := newestSnapshotFile()
	if err != nil {
		t.Fatal(err)
	}
	if name != "vremeplov-snap-100.gob" {
		t.Fatalf("newest = %q", name)
	}
}

// TestScreenshotChrome: F12 writes a decodable PNG in both view
// geometries.
func TestScreenshotChrome(t *testing.T) {
	g := testGame(t)
	t.Chdir(t.TempDir())
	g.m.RunTstates(100 * core.TstatesPerFrame)

	check := func(wantW, wantH int) {
		t.Helper()
		g.chromeAction("shot")
		if !strings.Contains(g.status, "screen saved") {
			t.Fatalf("status = %q", g.status)
		}
		names, _ := filepath.Glob("vremeplov-shot-*.png")
		if len(names) == 0 {
			t.Fatal("no screenshot written")
		}
		data, err := os.ReadFile(names[len(names)-1])
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		if b.Dx() != wantW || b.Dy() != wantH {
			t.Fatalf("screenshot %d×%d, want %d×%d", b.Dx(), b.Dy(), wantW, wantH)
		}
	}
	check(core.ActiveW, core.ActiveH)
	g.fullFrame = true
	g.m.RunTstates(core.TstatesPerFrame) // a new frame seq → a new file name
	check(core.FrameWidth, core.FrameHeight)
}

// TestRewindChrome: F4 goes back two seconds on the recorded timeline.
func TestRewindChrome(t *testing.T) {
	g := testGame(t)
	g.m.EnableHistory(50*core.TstatesPerFrame, 30)
	g.m.RunTstates(400 * core.TstatesPerFrame)
	before := g.m.Tstates()
	g.chromeAction("back")
	if !strings.Contains(g.status, "rewound") {
		t.Fatalf("status = %q", g.status)
	}
	if got := g.m.Tstates(); got >= before {
		t.Fatalf("Tstates = %d, want < %d", got, before)
	}
}

func TestHitAt(t *testing.T) {
	hits := []footHit{{"pause", 10, 5, 60, 25}, {"quit", 70, 5, 110, 25}}
	if id, ok := hitAt(hits, 15, 10); !ok || id != "pause" {
		t.Fatalf("hitAt inside = (%q, %v)", id, ok)
	}
	if id, ok := hitAt(hits, 60, 10); ok { // x1 is exclusive
		t.Fatalf("hitAt boundary = (%q, %v)", id, ok)
	}
	if _, ok := hitAt(hits, 200, 200); ok {
		t.Fatal("hitAt outside matched")
	}
}

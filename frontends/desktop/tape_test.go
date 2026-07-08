package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mtrisic/vremeplov/core"
)

// repoRoot walks up to the go.work root so tests can load the
// committed ROMs and fixtures (the core-test pattern; the desktop
// module must not import roms in tests it doesn't need it for).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.work not found above test working directory")
		}
		dir = parent
	}
}

// testGame builds a Game around a real machine, without opening the
// sound device or any Ebiten runtime resource.
func testGame(t *testing.T) *Game {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), "roms", "bin", name))
		if err != nil {
			t.Fatalf("loading ROM: %v", err)
		}
		return b
	}
	m, err := core.New(core.Config{
		ROMA:    read("rom_a_v28.bin"),
		Chargen: read("chrgen_elektronika.bin"),
		RAM:     core.RAM6K,
	})
	if err != nil {
		t.Fatal(err)
	}
	return newGame(m)
}

func hackaday(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "core", "gtp", "testdata", "hackaday.gtp"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestApplyFileTape: a dropped GTP loads, runs, and is remembered for
// F7 reload.
func TestApplyFileTape(t *testing.T) {
	g := testGame(t)
	g.applyFile("hackaday.gtp", hackaday(t))
	if !strings.Contains(g.status, "loaded hackaday.bin") {
		t.Fatalf("status = %q, want loaded hackaday.bin", g.status)
	}
	if g.lastTape == nil {
		t.Fatal("tape not remembered for reload")
	}
	g.m.RunTstates(300 * core.TstatesPerFrame)
	if screen := strings.Join(g.m.ScreenText(), "\n"); strings.Contains(screen, "READY") {
		t.Fatalf("RUN did not take over:\n%s", screen)
	}
	// Reload repeats the sequence from a running program.
	g.reloadTape()
	if !strings.Contains(g.status, "loaded hackaday.bin") {
		t.Fatalf("reload status = %q", g.status)
	}
}

// TestApplyFileSnapshot: a .gob restores the exact machine state.
func TestApplyFileSnapshot(t *testing.T) {
	g := testGame(t)
	g.m.RunTstates(5 * core.TstatesPerFrame)
	saved := g.m.Tstates()
	var b bytes.Buffer
	if _, err := g.m.Snapshot().WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	g.m.RunTstates(7 * core.TstatesPerFrame)
	g.applyFile("save.GOB", b.Bytes()) // extension check is case-blind
	if got := g.m.Tstates(); got != saved {
		t.Fatalf("restored Tstates = %d, want %d", got, saved)
	}
	g.applyFile("bad.gob", []byte("not a snapshot"))
	if !strings.Contains(g.status, "snapshot:") {
		t.Fatalf("garbage snapshot status = %q", g.status)
	}
}

// TestApplyFileBadImage: a broken tape leaves the machine untouched.
func TestApplyFileBadImage(t *testing.T) {
	g := testGame(t)
	before := g.m.Tstates()
	g.applyFile("junk.gtp", []byte{0x00, 0x01})
	if !strings.Contains(g.status, "load failed") {
		t.Fatalf("status = %q, want load failed", g.status)
	}
	if g.m.Tstates() != before {
		t.Fatal("bad image disturbed the machine")
	}
	if g.lastTape != nil {
		t.Fatal("bad image remembered as the current tape")
	}
}

// TestReloadWithoutTape: F7 with nothing loaded is a status, not a crash.
func TestReloadWithoutTape(t *testing.T) {
	g := testGame(t)
	before := g.m.Tstates()
	g.reloadTape()
	if !strings.Contains(g.status, "no tape loaded") {
		t.Fatalf("status = %q", g.status)
	}
	if g.m.Tstates() != before {
		t.Fatal("reload without a tape disturbed the machine")
	}
}

// TestFirstDroppedFile: the walker finds the first regular file, even
// nested, and reports an empty drop as no file.
func TestFirstDroppedFile(t *testing.T) {
	ff := fstest.MapFS{
		"dir/game.gtp": &fstest.MapFile{Data: []byte{0xA5}},
	}
	name, data, err := firstDroppedFile(ff)
	if err != nil {
		t.Fatal(err)
	}
	if name != "game.gtp" || !bytes.Equal(data, []byte{0xA5}) {
		t.Fatalf("firstDroppedFile = (%q, % x)", name, data)
	}
	name, _, err = firstDroppedFile(fstest.MapFS{})
	if err != nil || name != "" {
		t.Fatalf("empty drop = (%q, %v), want no file", name, err)
	}
}

// TestToggleRecording: F8 captures a BASIC SAVE and writes both
// formats to the working directory.
func TestToggleRecording(t *testing.T) {
	g := testGame(t)
	t.Chdir(t.TempDir())

	g.m.RunTstates(100 * core.TstatesPerFrame) // boot to READY
	end, err := g.m.TypeText("10 PRINT 1\n")
	if err != nil {
		t.Fatal(err)
	}
	g.m.RunTstates(end - g.m.Tstates() + core.TstatesPerFrame)

	g.toggleRecording()
	if !g.m.TapeRecording() {
		t.Fatal("recorder not armed")
	}
	end, err = g.m.TypeText("SAVE\n")
	if err != nil {
		t.Fatal(err)
	}
	g.m.RunTstates(end - g.m.Tstates() + 500*core.TstatesPerFrame)
	g.toggleRecording()
	if !strings.Contains(g.status, "written to") {
		t.Fatalf("status = %q, want blocks written", g.status)
	}
	gtps, _ := filepath.Glob("vremeplov-tape-*.gtp")
	wavs, _ := filepath.Glob("vremeplov-tape-*.wav")
	if len(gtps) != 1 || len(wavs) != 1 {
		t.Fatalf("recorded files: gtp=%v wav=%v, want one of each", gtps, wavs)
	}
}

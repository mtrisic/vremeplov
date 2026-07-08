package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
)

// repoRoot walks up to the go.work root, so tests can load the
// committed ROM images without the core module importing the roms
// module (SPEC.md §4.1) — the same pattern as core's own tests.
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

func newMachine(t *testing.T) *core.Machine {
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
	return m
}

func hackadayGTP(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "core", "gtp", "testdata", "hackaday.gtp"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestResetToReady(t *testing.T) {
	m := newMachine(t)
	ResetToReady(m)
	if screen := strings.Join(m.ScreenText(), "\n"); !strings.Contains(screen, "READY") {
		t.Fatalf("no READY after ResetToReady:\n%s", screen)
	}
}

// TestLoadAndRun exercises the whole file-picker sequence — reset, boot,
// fast-load hackaday.gtp, RUN — minus the frontend. It runs the sequence
// twice to prove "Reload tape" works from a running program.
func TestLoadAndRun(t *testing.T) {
	data := hackadayGTP(t)
	m := newMachine(t)
	for round := 0; round < 2; round++ {
		name, err := LoadAndRun(m, data)
		if err != nil {
			t.Fatal(err)
		}
		if name != "hackaday.bin" {
			t.Errorf("round %d: program name %q, want hackaday.bin", round, name)
		}
		m.RunTstates(300 * core.TstatesPerFrame)
		screen := strings.Join(m.ScreenText(), "\n")
		if strings.Contains(screen, "READY") {
			t.Fatalf("round %d: RUN did not take over:\n%s", round, screen)
		}
		if strings.Count(screen, "#") < 32*16*3/4 {
			t.Fatalf("round %d: expected graphics-filled screen:\n%s", round, screen)
		}
	}
}

// TestLoadAndRunTurbo: a turbo-typed data block loads like a standard
// one (shared payload layout, SPEC §3.7).
func TestLoadAndRunTurbo(t *testing.T) {
	// 0x2CC0: bit 7 set, so the A7 clamp cannot alias the CPU-eye read
	// (AGENTS.md log 14), and the BASIC pointers stay untouched.
	sec := []byte{0xA5, 0xC0, 0x2C, 0xC3, 0x2C, 1, 2, 3}
	sum := byte(0)
	for _, v := range sec {
		sum += v
	}
	sec = append(sec, 0xFF-sum)
	img := append([]byte{byte(gtp.BlockTurbo), byte(len(sec)), 0, 0, 0}, sec...)

	m := newMachine(t)
	if _, err := LoadAndRun(m, img); err != nil {
		t.Fatal(err)
	}
	m.RunTstates(core.TstatesPerFrame)
	got := m.DumpMemory(0x2CC0, 0x2CC3)
	for i, want := range []byte{1, 2, 3} {
		if got[i] != want {
			t.Fatalf("mem[0x%04X] = %d, want %d", 0x2CC0+i, got[i], want)
		}
	}
}

func TestLoadAndRunBadImage(t *testing.T) {
	m := newMachine(t)
	before := m.Tstates()
	if _, err := LoadAndRun(m, []byte{0x00, 0x01}); err == nil {
		t.Fatal("expected parse error")
	}
	if m.Tstates() != before {
		t.Fatal("bad image disturbed the machine")
	}
}

// TestLoadAndRunWAV: the picker path for digitized audio — hackaday's
// sections re-encoded as WAV load and run exactly like the GTP.
func TestLoadAndRunWAV(t *testing.T) {
	f, err := gtp.Parse(hackadayGTP(t))
	if err != nil {
		t.Fatal(err)
	}
	var streams [][]byte
	for _, b := range f.Blocks {
		if b.Type == gtp.BlockStandard {
			streams = append(streams, b.Payload)
		}
	}
	wav := core.CompileTapeBlocks(streams...).EncodeWAV(44100)

	m := newMachine(t)
	name, err := LoadAndRun(m, wav)
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Errorf("WAV yielded program name %q, want none", name)
	}
	m.RunTstates(300 * core.TstatesPerFrame)
	screen := strings.Join(m.ScreenText(), "\n")
	if strings.Count(screen, "#") < 32*16*3/4 {
		t.Fatalf("expected graphics-filled screen:\n%s", screen)
	}
}

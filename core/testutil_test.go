package core

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot walks up from the working directory to the go.work root, so
// tests can load the committed ROM images without the core module
// importing the roms module (SPEC.md §4.1).
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

func loadROM(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "roms", "bin", name))
	if err != nil {
		t.Fatalf("loading ROM: %v", err)
	}
	return b
}

// newMachine builds the default test machine: ROM A v28 (the ROM for a
// machine without ROM B — v29 requires ROM B, AGENTS.md log 11), the
// Elektronika chargen, and the requested RAM size.
func newMachine(t *testing.T, ram RAMSize) *Machine {
	t.Helper()
	m, err := New(Config{
		ROMA:    loadROM(t, "rom_a_v28.bin"),
		Chargen: loadROM(t, "chrgen_elektronika.bin"),
		RAM:     ram,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

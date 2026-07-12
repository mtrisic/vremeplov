package main

import (
	"os"
	"path/filepath"
	"testing"
)

func loadFixtureSLD(t *testing.T) *SLD {
	t.Helper()
	s, err := LoadSLD("testdata/hello.sld", "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSLDLineToAddr(t *testing.T) {
	s := loadFixtureSLD(t)
	cases := []struct {
		line int
		addr uint16
		ok   bool
	}{
		{5, 0x8000, true},
		{6, 0x8003, true},
		{11, 0x800A, true},
		{12, 0x800D, true},
		{4, 0, false}, // blank line, no code
		{99, 0, false},
	}
	for _, tc := range cases {
		addr, ok := s.AddrFor("testdata/hello.asm", tc.line)
		if ok != tc.ok || addr != tc.addr {
			t.Errorf("AddrFor(line %d) = (0x%04X, %v), want (0x%04X, %v)",
				tc.line, addr, ok, tc.addr, tc.ok)
		}
	}
}

func TestSLDAddrToLine(t *testing.T) {
	s := loadFixtureSLD(t)
	file, line, ok := s.LineFor(0x800A)
	if !ok || line != 11 || filepath.Base(file) != "hello.asm" {
		t.Fatalf("LineFor(0x800A) = (%q, %d, %v)", file, line, ok)
	}
	if _, _, ok := s.LineFor(0x800B); ok {
		t.Fatal("mid-instruction address mapped to a line")
	}
}

func TestSLDLabels(t *testing.T) {
	s := loadFixtureSLD(t)
	for name, want := range map[string]uint16{
		"start": 0x8000, "after": 0x8003, "fill": 0x8008,
	} {
		if addr, ok := s.Label(name); !ok || addr != want {
			t.Errorf("Label(%s) = (0x%04X, %v), want 0x%04X", name, addr, ok, want)
		}
	}
	if _, ok := s.Label("nope"); ok {
		t.Error("unknown label resolved")
	}
}

func TestSLDNearestLabel(t *testing.T) {
	s := loadFixtureSLD(t)
	name, off, ok := s.NearestLabel(0x800A)
	if !ok || name != "fill" || off != 2 {
		t.Fatalf("NearestLabel(0x800A) = (%q, %d, %v), want fill+2", name, off, ok)
	}
	if _, _, ok := s.NearestLabel(0x100); ok {
		t.Fatal("label found below all labels")
	}
}

// TestSLDSuffixFallback: an editor-absolute path still resolves when
// only the basename matches the SLD's relative record.
func TestSLDSuffixFallback(t *testing.T) {
	s := loadFixtureSLD(t)
	if addr, ok := s.AddrFor("/somewhere/else/hello.asm", 5); !ok || addr != 0x8000 {
		t.Fatalf("basename fallback = (0x%04X, %v)", addr, ok)
	}
}

// TestSLDTolerance: unknown record types and junk lines are skipped,
// and a file with nothing usable errors.
func TestSLDTolerance(t *testing.T) {
	dir := t.TempDir()
	messy := filepath.Join(dir, "messy.sld")
	os.WriteFile(messy, []byte(
		"|SLD.data.version|1\n"+
			"main.asm|3||0|-1|32768|Z|device stuff\n"+
			"short|line\n"+
			"main.asm|7||0|-1|32770|T|\n"), 0o644)
	s, err := LoadSLD(messy, dir)
	if err != nil {
		t.Fatal(err)
	}
	if addr, ok := s.AddrFor(filepath.Join(dir, "main.asm"), 7); !ok || addr != 0x8002 {
		t.Fatalf("tolerant parse = (0x%04X, %v), want 0x8002", addr, ok)
	}

	empty := filepath.Join(dir, "empty.sld")
	os.WriteFile(empty, []byte("|SLD.data.version|1\n"), 0o644)
	if _, err := LoadSLD(empty, dir); err == nil {
		t.Fatal("empty SLD accepted")
	}
}

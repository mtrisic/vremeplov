package gtp

import (
	"os"
	"path/filepath"
	"testing"
)

// The four sample programs from mejs/galaksija (see roms/PROVENANCE.md).
// Expected values below were extracted by hand from the files.
var samples = []struct {
	file        string
	name        string
	start, end  uint16
	checksum    byte
	payloadLen  int
	trailingLen int // garbage bytes after the checksum
}{
	{"hackaday.gtp", "hackaday.bin", 0x2C36, 0x2E7E, 0x33, 590, 0},
	{"pumpkin.gtp", "pumpkin.bin", 0x2C36, 0x2E7E, 0x73, 590, 0},
	{"retroinfo.gtp", "retroinfo.bin", 0x2C36, 0x35C5, 0x35, 2453, 0},
	{"win11check.gtp", "win11", 0x2C36, 0x2F2D, 0x5A, 766, 1},
}

func readSample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseSamples(t *testing.T) {
	for _, tc := range samples {
		t.Run(tc.file, func(t *testing.T) {
			f, err := Parse(readSample(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if f.Name != tc.name {
				t.Errorf("Name = %q, want %q", f.Name, tc.name)
			}
			secs, err := f.Sections()
			if err != nil {
				t.Fatal(err)
			}
			if len(secs) != 1 {
				t.Fatalf("got %d sections, want 1", len(secs))
			}
			s := secs[0]
			if s.Start != tc.start || s.End != tc.end {
				t.Errorf("section [0x%04X,0x%04X), want [0x%04X,0x%04X)", s.Start, s.End, tc.start, tc.end)
			}
			if len(s.Data) != int(tc.end-tc.start) {
				t.Errorf("len(Data) = %d, want %d", len(s.Data), tc.end-tc.start)
			}
			if s.Checksum != tc.checksum {
				t.Errorf("Checksum = 0x%02X, want 0x%02X", s.Checksum, tc.checksum)
			}
			// Every sample: one name block + one standard block.
			if len(f.Blocks) != 2 || f.Blocks[0].Type != BlockName || f.Blocks[1].Type != BlockStandard {
				t.Errorf("blocks = %+v, want [name, standard]", f.Blocks)
			}
			if got := len(f.Blocks[1].Payload); got != tc.payloadLen {
				t.Errorf("standard payload = %d bytes, want %d", got, tc.payloadLen)
			}
			if got := tc.payloadLen - 5 - len(s.Data) - 1; got != tc.trailingLen {
				t.Errorf("trailing garbage = %d bytes, want %d", got, tc.trailingLen)
			}
		})
	}
}

// buildStandard assembles a valid standard-block payload for error tests.
func buildStandard(start uint16, data []byte) []byte {
	p := []byte{0xA5, byte(start), byte(start >> 8)}
	end := start + uint16(len(data))
	p = append(p, byte(end), byte(end>>8))
	p = append(p, data...)
	sum := byte(0)
	for _, v := range p {
		sum += v
	}
	return append(p, 0xFF-sum)
}

func wrap(typ BlockType, payload []byte) []byte {
	h := []byte{byte(typ), byte(len(payload)), byte(len(payload) >> 8), 0, 0}
	return append(h, payload...)
}

func TestParseSynthetic(t *testing.T) {
	payload := buildStandard(0x2C36, []byte{1, 2, 3})
	f, err := Parse(wrap(BlockStandard, payload))
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.Blocks[0].Section()
	if err != nil {
		t.Fatal(err)
	}
	if s.Start != 0x2C36 || s.End != 0x2C39 || len(s.Data) != 3 {
		t.Fatalf("section = %+v", s)
	}
}

func TestParseErrors(t *testing.T) {
	good := buildStandard(0x2C36, []byte{1, 2, 3})
	badSum := append([]byte(nil), good...)
	badSum[len(badSum)-1]++
	badSync := append([]byte(nil), good...)
	badSync[0] = 0xA4
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated header", []byte{0x00, 0x10}},
		{"truncated payload", []byte{0x00, 0x10, 0x00, 0, 0, 0xA5}},
		{"unknown type", wrap(BlockType(0x77), []byte{1})},
		{"bad checksum", wrap(BlockStandard, badSum)},
		{"bad sync", wrap(BlockStandard, badSync)},
		{"bad turbo checksum", wrap(BlockTurbo, badSum)},
		{"garbage turbo payload", wrap(BlockTurbo, []byte{0xDE, 0xAD})},
		{"end below start", wrap(BlockStandard, []byte{0xA5, 0x36, 0x2C, 0x00, 0x2C, 0x00})},
		{"short standard", wrap(BlockStandard, []byte{0xA5, 0x36})},
	}
	for _, tc := range cases {
		if _, err := Parse(tc.data); err == nil {
			t.Errorf("%s: Parse succeeded, want error", tc.name)
		}
	}
}

// TestTurboSections: turbo blocks share the standard payload layout and
// decode to sections in file order alongside standard blocks.
func TestTurboSections(t *testing.T) {
	img := wrap(BlockName, []byte("prog\x00"))
	img = append(img, wrap(BlockTurbo, buildStandard(0x2C36, []byte{1, 2}))...)
	img = append(img, wrap(BlockStandard, buildStandard(0x3000, []byte{9}))...)
	f, err := Parse(img)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "prog" {
		t.Errorf("Name = %q", f.Name)
	}
	secs, err := f.Sections()
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 || secs[0].Start != 0x2C36 || secs[1].Start != 0x3000 {
		t.Fatalf("sections = %+v", secs)
	}
	if len(secs[0].Data) != 2 || secs[0].Data[0] != 1 {
		t.Fatalf("turbo section data = %v", secs[0].Data)
	}
}

func TestBuildRoundtrip(t *testing.T) {
	s1 := buildStandard(0x2C36, []byte{1, 2, 3})
	s2 := buildStandard(0x3000, []byte{9})
	img, err := Build("demo", s1, s2)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(img)
	if err != nil {
		t.Fatalf("Build output does not parse: %v", err)
	}
	if f.Name != "demo" {
		t.Errorf("Name = %q, want %q", f.Name, "demo")
	}
	if len(f.Blocks) != 3 || f.Blocks[0].Type != BlockName {
		t.Fatalf("blocks = %d (first type 0x%02X), want name + 2 standard", len(f.Blocks), byte(f.Blocks[0].Type))
	}
	secs, err := f.Sections()
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 || secs[0].Start != 0x2C36 || secs[1].Start != 0x3000 {
		t.Fatalf("sections = %+v", secs)
	}
	if !bytesEqual(f.Blocks[1].Payload, s1) || !bytesEqual(f.Blocks[2].Payload, s2) {
		t.Error("payloads not preserved byte-for-byte")
	}
}

func TestBuildNoName(t *testing.T) {
	img, err := Build("", buildStandard(0x2C36, []byte{7}))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Blocks) != 1 || f.Blocks[0].Type != BlockStandard || f.Name != "" {
		t.Fatalf("blocks = %+v, name = %q", f.Blocks, f.Name)
	}
}

func TestBuildErrors(t *testing.T) {
	if _, err := Build("x"); err == nil {
		t.Error("no streams accepted")
	}
	if _, err := Build("x", []byte{1, 2, 3}); err == nil {
		t.Error("garbage stream accepted")
	}
	bad := buildStandard(0x2C36, []byte{1})
	bad[len(bad)-1]++ // corrupt the checksum
	if _, err := Build("x", bad); err == nil {
		t.Error("bad checksum accepted")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package main

import "testing"

// A 4×4 test pattern: X marks bright pixels.
//
//	X.X.
//	.X.X
//	X...
//	...X
var testPix = []byte{
	1, 0, 1, 0,
	0, 1, 0, 1,
	1, 0, 0, 0,
	0, 0, 0, 1,
}

func TestRenderHalfBlock(t *testing.T) {
	lines := renderView(testPix, 4, 0, 0, 4, 4, rendererHalfBlock)
	want := []string{"▀▄▀▄", "▀  ▄"}
	if len(lines) != 2 || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("got %q, want %q", lines, want)
	}
}

func TestRenderQuadrant(t *testing.T) {
	lines := renderView(testPix, 4, 0, 0, 4, 4, rendererQuadrant)
	// Cells: (TL,TR,BL,BR): (1,0,0,1)=▚ (1,0,0,1)=▚ / (1,0,0,0)=▘ (0,0,0,1)=▗
	want := []string{"▚▚", "▘▗"}
	if len(lines) != 2 || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("got %q, want %q", lines, want)
	}
}

func TestRenderBraille(t *testing.T) {
	lines := renderView(testPix, 4, 0, 0, 4, 4, rendererBraille)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	// Left cell: bright at (0,0),(1,1),(0,2) -> dots 1,5,3 = 0x01|0x10|0x04.
	// Right cell: bright at (0,0),(1,1),(1,3) -> 0x01|0x10|0x80.
	want := string([]rune{0x2800 + 0x15, 0x2800 + 0x91})
	if lines[0] != want {
		t.Fatalf("got %q, want %q", lines[0], want)
	}
}

func TestRenderOutOfBoundsIsDark(t *testing.T) {
	// Window extends past the buffer: no panic, padding is dark.
	lines := renderView(testPix, 4, 2, 2, 4, 4, rendererQuadrant)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	// Only (0,0)... source (2,2)=0,(3,2)=0,(2,3)=0,(3,3)=1 -> cell 0 = ▗
	if lines[0] != "▗ " {
		t.Fatalf("row 0 = %q, want \"▗ \"", lines[0])
	}
}

func TestPickRenderer(t *testing.T) {
	cases := []struct {
		cols, rows int
		want       rendererMode
	}{
		{300, 120, rendererHalfBlock}, // fits 256×104
		{200, 120, rendererQuadrant},  // fits 128×104
		{200, 60, rendererBraille},    // fits 128×52
		{90, 14, rendererText},        // VSCode panel: no 1:1 mode fits, text is readable
		{80, 24, rendererText},        // classic 80×24: same
		{20, 10, rendererScaled},      // narrower than the 32-char text screen
	}
	for _, c := range cases {
		if got := pickRenderer(c.cols, c.rows, 256, 208); got != c.want {
			t.Errorf("pickRenderer(%d,%d) = %s, want %s", c.cols, c.rows, got, c.want)
		}
	}
}

func TestRenderScaledFitsAndSamples(t *testing.T) {
	// 8×8 source with one bright pixel, squeezed into a 2×1-cell grid
	// (4×4 dots): every source pixel maps somewhere, so exactly one
	// dot must light up.
	pix := make([]byte, 8*8)
	pix[3*8+5] = 1
	lines := renderScaled(pix, 8, 0, 0, 8, 8, 2, 1)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	dots := 0
	for _, r := range lines[0] {
		for b := rune(1); b <= 0x80; b <<= 1 {
			if (r-0x2800)&b != 0 {
				dots++
			}
		}
	}
	if dots != 1 {
		t.Fatalf("bright dots = %d, want exactly 1 (OR-sampling)\nlines %q", dots, lines)
	}

	// All dark stays all dark.
	dark := renderScaled(make([]byte, 8*8), 8, 0, 0, 8, 8, 2, 1)
	for _, r := range dark[0] {
		if r != 0x2800 {
			t.Fatalf("dark input produced dot rune %q", r)
		}
	}
}

func TestRenderScaledNeverUpscalesOrOverflows(t *testing.T) {
	pix := make([]byte, 4*4)
	// Huge terminal: output must stay at the source's own braille size.
	lines := renderScaled(pix, 4, 0, 0, 4, 4, 500, 200)
	if len(lines) != 1 || len([]rune(lines[0])) != 2 {
		t.Fatalf("upscaled: %d lines × %d cells, want 1×2", len(lines), len([]rune(lines[0])))
	}
	// Degenerate 1×1 terminal: still one line, one cell, no panic.
	lines = renderScaled(pix, 4, 0, 0, 4, 4, 1, 1)
	if len(lines) != 1 || len([]rune(lines[0])) != 1 {
		t.Fatalf("1×1: %d lines × %d cells", len(lines), len([]rune(lines[0])))
	}
}

func TestRenderScaledPreservesAspect(t *testing.T) {
	// A 256×208 view in a wide, short grid (90×14 cells = 180×56 dots):
	// the vertical axis is the constraint (56/208), so the output must
	// be ~69 dots wide (35 cells), not stretched to 90.
	pix := make([]byte, 256*208)
	lines := renderScaled(pix, 256, 0, 0, 256, 208, 90, 14)
	if len(lines) != 14 {
		t.Fatalf("got %d lines, want 14", len(lines))
	}
	if w := len([]rune(lines[0])); w < 30 || w > 40 {
		t.Fatalf("output width %d cells, want ~35 (aspect preserved)", w)
	}
}

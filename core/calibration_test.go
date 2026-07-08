package core

import "testing"

// TestActiveRectCalibration pins the ActiveX/ActiveY constants to the
// measured position of the character grid within the full frame — the
// Phase-1 calibration step (PLAN.md §1.3), kept as a test so any change
// to the pipeline timing that moves the picture fails loudly.
//
// Method: fill video RAM with code 0xFF (block-graphics glyph 0x7F,
// whose bright pixels within the 8-pixel cell are at columns 1-3 and
// 5-7, rows 1-13 minus separators), render a frame, and take the
// bounding box of bright pixels. The leftmost bright column is
// ActiveX+1; the topmost bright line is the first drawn scanline
// (ActiveY, since glyph row 1 is bright and is drawn first).
func TestActiveRectCalibration(t *testing.T) {
	m := bootMachine(t)
	block := make([]byte, ScreenCols*ScreenRows)
	for i := range block {
		block[i] = 0xFF
	}
	if err := m.LoadBinary(videoRAMBase, block); err != nil {
		t.Fatal(err)
	}
	m.RunTstates(2 * TstatesPerFrame)

	pix := make([]byte, FrameWidth*FrameHeight)
	m.Frame(pix)
	minX, minY, maxX, maxY := FrameWidth, FrameHeight, -1, -1
	for y := 0; y < FrameHeight; y++ {
		for x := 0; x < FrameWidth; x++ {
			if pix[y*FrameWidth+x] != 0 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < 0 {
		t.Fatal("no bright pixels — video pipeline produced a black frame")
	}
	t.Logf("bright bbox: x [%d,%d] y [%d,%d]", minX, maxX, minY, maxY)

	// Glyph 0x7F: leftmost bright cell column 1, rightmost 7.
	gotX, gotY := minX-1, minY
	if gotX != ActiveX || gotY != ActiveY {
		t.Errorf("measured active origin (%d,%d) != constants (%d,%d)", gotX, gotY, ActiveX, ActiveY)
	}
	// Horizontal extent: 32 cells: last bright = ActiveX + 31*8 + 7.
	if want := gotX + 31*8 + 7; maxX != want {
		t.Errorf("rightmost bright pixel %d, want %d — char pitch broken", maxX, want)
	}
	// The full grid must fit inside the frame with the documented size.
	if gotX+ActiveW > FrameWidth || gotY+ActiveH > FrameHeight {
		t.Errorf("active rect (%d,%d,%d,%d) exceeds frame", gotX, gotY, ActiveW, ActiveH)
	}
}

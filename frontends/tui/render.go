package main

import (
	"math"
	"strings"
)

// rendererMode selects how framebuffer pixels map to terminal cells.
// All modes are monochrome (default fg on default bg), so the output is
// plain runes with no ANSI styling.
type rendererMode int

const (
	// rendererHalfBlock uses U+2580/2584/2588: 1×2 pixels per cell.
	// Active area needs 256×104 cells.
	rendererHalfBlock rendererMode = iota
	// rendererQuadrant uses the 2×2 quadrant blocks: 128×104 cells.
	rendererQuadrant
	// rendererBraille uses U+2800..28FF: 2×4 pixels, 128×52 cells.
	rendererBraille
	// rendererText prints the decoded 32×16 character screen
	// (core.ScreenText) — no pixels, but readable at any size a VSCode
	// panel offers. Graphics codes show as '#'.
	rendererText
	// rendererScaled is braille with the pixel view downsampled
	// (aspect-preserving, OR-sampled so 1-px strokes survive) to
	// whatever cell grid the terminal has. Always fits; text gets
	// squinty below the 1:1 braille size.
	rendererScaled
	rendererCount
)

func (r rendererMode) String() string {
	switch r {
	case rendererHalfBlock:
		return "half-block"
	case rendererQuadrant:
		return "quadrant"
	case rendererBraille:
		return "braille"
	case rendererText:
		return "text"
	case rendererScaled:
		return "scaled-braille"
	}
	return "?"
}

// cellSize returns the pixels per terminal cell (x, y) for the mode.
func (r rendererMode) cellSize() (int, int) {
	switch r {
	case rendererHalfBlock:
		return 1, 2
	case rendererQuadrant:
		return 2, 2
	default:
		return 2, 4
	}
}

// pickRenderer chooses the crispest mode whose cell grid for a viewW×viewH
// pixel viewport fits a cols×rows terminal (rows excludes the status line).
// When no 1:1 pixel mode fits, it prefers the readable text screen (the
// usual case: a short editor terminal panel while debugging BASIC) and
// falls back to downscaled braille only when even 32 columns are missing.
// Graphics-heavy programs on a small terminal: switch manually (^X v).
func pickRenderer(cols, rows, viewW, viewH int) rendererMode {
	for _, r := range []rendererMode{rendererHalfBlock, rendererQuadrant, rendererBraille} {
		cx, cy := r.cellSize()
		if (viewW+cx-1)/cx <= cols && (viewH+cy-1)/cy <= rows {
			return r
		}
	}
	if cols >= textCols {
		return rendererText
	}
	return rendererScaled
}

// textCols/textRows is the size of the decoded character screen.
const (
	textCols = 32
	textRows = 16
)

// quadrant runes indexed by TL<<3 | TR<<2 | BL<<1 | BR.
var quadrantRunes = [16]rune{
	' ', '▗', '▖', '▄', '▝', '▐', '▞', '▟',
	'▘', '▚', '▌', '▙', '▀', '▜', '▛', '█',
}

// Braille dot bit for (column, row) within the 2×4 cell.
var brailleBits = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// renderView converts a rectangular window of the framebuffer into
// terminal lines. pix is the full FrameWidth×FrameHeight luminance
// buffer; the window is (x0,y0,w,h) in pixels. Pixels sampled outside
// the window bounds are dark.
func renderView(pix []byte, stride, x0, y0, w, h int, mode rendererMode) []string {
	cx, cy := mode.cellSize()
	cols := (w + cx - 1) / cx
	rows := (h + cy - 1) / cy
	at := func(dx, dy int) byte {
		x, y := x0+dx, y0+dy
		if dx >= w || dy >= h || x < 0 || y < 0 || x >= stride || (y+1)*stride > len(pix) {
			return 0
		}
		return pix[y*stride+x]
	}
	lines := make([]string, rows)
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		sb.Reset()
		sb.Grow(cols * 3)
		for c := 0; c < cols; c++ {
			px, py := c*cx, r*cy
			switch mode {
			case rendererHalfBlock:
				idx := at(px, py)<<1 | at(px, py+1)
				sb.WriteRune([]rune{' ', '▄', '▀', '█'}[idx])
			case rendererQuadrant:
				idx := at(px, py)<<3 | at(px+1, py)<<2 | at(px, py+1)<<1 | at(px+1, py+1)
				sb.WriteRune(quadrantRunes[idx])
			default:
				var dots rune
				for dy := 0; dy < 4; dy++ {
					for dx := 0; dx < 2; dx++ {
						if at(px+dx, py+dy) != 0 {
							dots |= brailleBits[dy][dx]
						}
					}
				}
				sb.WriteRune(0x2800 + dots)
			}
		}
		lines[r] = sb.String()
	}
	return lines
}

// renderScaled downsamples the (x0,y0,w,h) pixel window into a braille
// grid fitting cols×rows terminal cells, preserving aspect ratio (braille
// dots are roughly square in common fonts) and never upscaling. Each
// braille dot covers a block of source pixels and lights up if ANY of
// them is bright, so single-pixel glyph strokes stay visible.
func renderScaled(pix []byte, stride, x0, y0, w, h, cols, rows int) []string {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	scale := math.Min(float64(cols*2)/float64(w), float64(rows*4)/float64(h))
	if scale > 1 {
		scale = 1
	}
	tw := max(1, int(math.Round(float64(w)*scale)))
	th := max(1, int(math.Round(float64(h)*scale)))

	// Any bright source pixel in [sx0,sx1)×[sy0,sy1) lights the dot.
	blockBright := func(dx, dy int) bool {
		sx0, sx1 := dx*w/tw, (dx+1)*w/tw
		sy0, sy1 := dy*h/th, (dy+1)*h/th
		if sx1 <= sx0 {
			sx1 = sx0 + 1
		}
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for y := sy0; y < sy1; y++ {
			row := (y0 + y) * stride
			for x := sx0; x < sx1; x++ {
				if pix[row+x0+x] != 0 {
					return true
				}
			}
		}
		return false
	}

	outCols, outRows := (tw+1)/2, (th+3)/4
	lines := make([]string, outRows)
	var sb strings.Builder
	for r := 0; r < outRows; r++ {
		sb.Reset()
		sb.Grow(outCols * 3)
		for c := 0; c < outCols; c++ {
			var dots rune
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					px, py := c*2+dx, r*4+dy
					if px < tw && py < th && blockBright(px, py) {
						dots |= brailleBits[dy][dx]
					}
				}
			}
			sb.WriteRune(0x2800 + dots)
		}
		lines[r] = sb.String()
	}
	return lines
}

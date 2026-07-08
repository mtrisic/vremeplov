package core

// Video RAM layout (SPEC.md §3.1): 32×16 character codes at 0x2800.
const (
	videoRAMBase = 0x2800
	// ScreenCols and ScreenRows are the text dimensions of the display.
	ScreenCols = 32
	ScreenRows = 16
)

// ScreenText decodes the video RAM into ScreenRows strings of ScreenCols
// characters, mapping glyph indexes back to ASCII: glyphs 0x00–0x1F are
// '@','A'–'Z','[','\',']','^','_' (ASCII 0x40+glyph), glyphs 0x20–0x3F
// are their own ASCII, and pseudo-graphic glyphs render as '#'. This is
// independent of pixel rendering and is used by tests to assert screen
// content (e.g. the READY prompt).
func (m *Machine) ScreenText() []string {
	rows := make([]string, ScreenRows)
	buf := make([]byte, ScreenCols)
	for r := 0; r < ScreenRows; r++ {
		for c := 0; c < ScreenCols; c++ {
			// Raw RAM read: the CPU-visible view depends on the momentary
			// A7 clamp, but the video circuit scans every byte each frame
			// (the ISR drives the clamp per row half).
			code := m.ram[videoRAMBase-ramBase+r*ScreenCols+c]
			g := glyphIndex(code)
			switch {
			case g < 0x20:
				buf[c] = byte(0x40 + g)
			case g < 0x40:
				buf[c] = byte(g)
			default:
				buf[c] = '#'
			}
		}
		rows[r] = string(buf)
	}
	return rows
}

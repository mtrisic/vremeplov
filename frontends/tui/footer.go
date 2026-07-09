package main

// The footer: the status line plus rows of clickable chrome "buttons",
// each showing its ^X key. Button rows wrap to the terminal width; on
// terminals too short to keep a full text screen above them they
// collapse entirely and ^X stays the only chrome path (the VSCode-panel
// fallback keeps priority over discoverability). On terminals tall
// enough that the extra rows cost no screen content, buttons grow
// rounded borders. Colors come from the 16-color ANSI palette and
// degrade to plain text on terminals without color support (lipgloss
// strips what the terminal can't do).

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type footButton struct {
	key   string // the ^X chrome key this button dispatches
	label string
	color lipgloss.Color // functional group color (ANSI palette)
}

// footHit is one rendered button's clickable region, in view
// coordinates (line indexes within the View output; bordered buttons
// span three lines).
type footHit struct {
	y0, y1, x0, x1 int
	key            string
}

// footBorderMinContent: bordered buttons (3 rows instead of 1) are used
// only when the remaining content budget still fits the full braille
// active view — i.e. when the borders cost nothing.
const footBorderMinContent = 52

// footerButtons returns the chrome buttons with state-dependent labels
// and group colors: blue = machine control, magenta = time machine,
// yellow = tape (red while recording), gray = save states, cyan =
// debugging, green = view/input toggles, red = quit.
func (mo *model) footerButtons() []footButton {
	pause := "pause"
	if mo.paused {
		pause = "resume"
	}
	rec, recColor := "record", lipgloss.Color("3")
	if mo.m.TapeRecording() {
		rec, recColor = "stop-rec", lipgloss.Color("1")
	}
	mon := "monitor"
	if mo.mon.open {
		mon = "close-mon"
	}
	sticky := "sticky"
	if mo.sticky {
		sticky = "unstick"
	}
	full := "full"
	if mo.fullFrame {
		full = "active"
	}
	quit := "quit"
	if mo.confirmQuit {
		quit = "sure?"
	}
	return []footButton{
		{"p", pause, "4"}, {"r", "reset", "4"}, {"b", "rewind", "5"},
		{"t", rec, recColor}, {"w", "save", "8"}, {"l", "load", "8"},
		{"m", mon, "6"}, {"d", "dump", "6"}, {"c", "shot", "6"},
		{"y", "copy", "6"},
		{"s", sticky, "2"}, {"v", "video", "2"}, {"f", full, "2"},
		{"q", quit, "1"},
	}
}

// keyChipText picks a readable text color for a key chip: black on the
// light backgrounds (green, yellow, cyan), white on the dark ones.
func keyChipText(bg lipgloss.Color) lipgloss.Color {
	switch bg {
	case "2", "3", "6":
		return "0"
	}
	return "15"
}

// renderButton renders one flat button: a colored [key] chip plus the
// label.
func renderButton(b footButton) string {
	chip := lipgloss.NewStyle().Bold(true).
		Background(b.color).Foreground(keyChipText(b.color)).
		Render("[" + b.key + "]")
	return chip + " " + b.label
}

// renderBorderButton renders one bordered button: three lines of equal
// width, border in the group color.
func renderBorderButton(b footButton) []string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(b.color).
		Padding(0, 1).
		Render(renderButton(b))
	return strings.Split(box, "\n")
}

// renderFooterButtons wraps the buttons into lines at most width cells
// wide and returns the lines plus each button's hit region (line
// indexes relative to the first button line).
func (mo *model) renderFooterButtons(width int, bordered bool) ([]string, []footHit) {
	if width <= 0 {
		width = textCols * 4 // no WindowSizeMsg yet; match the view default
	}
	const gap = 2
	var (
		lines []string
		hits  []footHit
		row   [][]string // current row: one line-slice per button
		col   int
	)
	flush := func() {
		if len(row) == 0 {
			return
		}
		h := len(row[0])
		for j := 0; j < h; j++ {
			var parts []string
			for _, box := range row {
				parts = append(parts, box[j])
			}
			lines = append(lines, strings.Join(parts, strings.Repeat(" ", gap)))
		}
		row, col = nil, 0
	}
	for _, btn := range mo.footerButtons() {
		var box []string
		if bordered {
			box = renderBorderButton(btn)
		} else {
			box = []string{renderButton(btn)}
		}
		w := lipgloss.Width(box[0])
		if col > 0 && col+gap+w > width {
			flush()
		}
		x0 := col
		if len(row) > 0 {
			x0 += gap
		}
		y0 := len(lines)
		hits = append(hits, footHit{
			y0: y0, y1: y0 + len(box) - 1,
			x0: x0, x1: x0 + w, key: btn.key,
		})
		row = append(row, box)
		col = x0 + w
	}
	flush()
	return lines, hits
}

// footerLayout picks the footer for the terminal size: bordered buttons
// when they cost no content rows, flat buttons normally, none at all
// when even flat rows would not leave a full text screen.
func (mo *model) footerLayout(width, height int) ([]string, []footHit) {
	flat, flatHits := mo.renderFooterButtons(width, false)
	if height > 0 && height-1-len(flat) < textRows {
		return nil, nil
	}
	if height > 0 {
		if bord, bordHits := mo.renderFooterButtons(width, true); height-1-len(bord) >= footBorderMinContent {
			return bord, bordHits
		}
	}
	return flat, flatHits
}

// footerRows is how many terminal rows the footer occupies at the given
// size: the status line plus the button rows.
func (mo *model) footerRows(width, height int) int {
	lines, _ := mo.footerLayout(width, height)
	return 1 + len(lines)
}

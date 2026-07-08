package main

import (
	"bytes"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/roms"
)

// View geometry: the active area, doubled in CSS by the page.
const (
	viewW = core.ActiveW
	viewH = core.ActiveH
)

// newMachine builds the default web machine: ROM A v28, Elektronika
// chargen, 6 KB RAM.
func newMachine() (*core.Machine, error) {
	return core.New(core.Config{
		ROMA:    roms.ROMA(),
		Chargen: roms.ChargenElektronika(),
		RAM:     core.RAM6K,
	})
}

// snapshotBytes serializes the machine state in core's gob snapshot
// format — the same files cmd/headless and the TUI read and write.
func snapshotBytes(m *core.Machine) ([]byte, error) {
	var b bytes.Buffer
	if _, err := m.Snapshot().WriteTo(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// restoreSnapshot restores a machine from snapshot data.
func restoreSnapshot(m *core.Machine, data []byte) error {
	s, err := core.ReadSnapshot(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return m.Restore(s)
}

// frameRGBA converts the machine's active-area luminance pixels into
// RGBA bytes for a canvas ImageData. dst must be viewW*viewH*4 bytes;
// pix must be a full core.FrameWidth×FrameHeight frame.
func frameRGBA(pix []byte, dst []byte) {
	i := 0
	for y := 0; y < viewH; y++ {
		row := (core.ActiveY + y) * core.FrameWidth
		for x := 0; x < viewW; x++ {
			v := pix[row+core.ActiveX+x] * 0xFF
			dst[i+0] = v
			dst[i+1] = v
			dst[i+2] = v
			dst[i+3] = 0xFF
			i += 4
		}
	}
}

// keystrokesFor maps a browser KeyboardEvent to the matrix keys to hold,
// in press order (Shift first). key is event.key (layout-aware), so the
// machine sees the character the user meant; specials map by name.
// ok=false means the event is not for the machine.
func keystrokesFor(key string) (strokes []core.Key, ok bool) {
	switch key {
	case "Enter":
		return []core.Key{core.KeyEnter}, true
	case "Escape":
		return []core.Key{core.KeyBreak}, true
	case "Backspace":
		return []core.Key{core.KeyDelete}, true
	case "Tab":
		return []core.Key{core.KeyList}, true
	case "ArrowUp":
		return []core.Key{core.KeyUp}, true
	case "ArrowDown":
		return []core.Key{core.KeyDown}, true
	case "ArrowLeft":
		return []core.Key{core.KeyLeft}, true
	case "ArrowRight":
		return []core.Key{core.KeyRight}, true
	case "Shift":
		return []core.Key{core.KeyShift}, true
	}
	runes := []rune(key)
	if len(runes) != 1 {
		return nil, false
	}
	k, shift, found := core.KeystrokeForRune(runes[0])
	if !found {
		return nil, false
	}
	if shift {
		return []core.Key{core.KeyShift, k}, true
	}
	return []core.Key{k}, true
}

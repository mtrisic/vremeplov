// Package loader turns tape-image files into running programs: the
// shared "user picked a .gtp or .wav" sequence every interactive
// frontend performs — parse, reset to READY, fast-load the sections,
// type RUN. It stays within core's purity rules (stdlib + core + gtp
// only); cmd/headless keeps its own finer-grained flag-driven paths.
package loader

import (
	"fmt"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
)

// BootFrames is how long the ROM needs from reset to a settled READY
// prompt (RAM auto-size + init; same constant the headless runner uses).
const BootFrames = 100

// ResetToReady resets the machine and bursts it to the READY prompt, so
// the user doesn't watch two seconds of boot in real time.
func ResetToReady(m *core.Machine) {
	m.Reset()
	m.RunTstates(BootFrames * core.TstatesPerFrame)
}

// ParseTapeImage decodes a picked tape file — a GTP image, or digitized
// WAV audio (recognized by the RIFF magic) — into loadable sections plus
// the program name (GTP name block only; WAVs carry none).
func ParseTapeImage(data []byte) (string, []*gtp.Section, error) {
	if len(data) >= 4 && string(data[:4]) == "RIFF" {
		sch, err := core.DecodeWAV(data)
		if err != nil {
			return "", nil, err
		}
		streams := sch.TapeStreams()
		if streams == nil {
			return "", nil, fmt.Errorf("no decodable tape data in the audio")
		}
		var secs []*gtp.Section
		for i, s := range streams {
			b := gtp.Block{Type: gtp.BlockStandard, Payload: s}
			sec, err := b.Section()
			if err != nil {
				return "", nil, fmt.Errorf("audio stream %d: %w", i, err)
			}
			secs = append(secs, sec)
		}
		return "", secs, nil
	}
	f, err := gtp.Parse(data)
	if err != nil {
		return "", nil, err
	}
	secs, err := f.Sections()
	if err != nil {
		return "", nil, err
	}
	return f.Name, secs, nil
}

// LoadAndRun brings the machine to a fresh READY prompt, fast-loads a
// .gtp image or .wav recording, and types RUN — the whole file-picker
// sequence. A parse error leaves the machine untouched. It returns the
// program name from the image (or "" if none).
func LoadAndRun(m *core.Machine, data []byte) (string, error) {
	name, secs, err := ParseTapeImage(data)
	if err != nil {
		return "", err
	}
	ResetToReady(m)
	for _, sec := range secs {
		if err := m.LoadBinary(sec.Start, sec.Data); err != nil {
			return "", fmt.Errorf("section [0x%04X,0x%04X): %w", sec.Start, sec.End, err)
		}
	}
	end, err := m.TypeText("RUN\n")
	if err != nil {
		return "", err
	}
	m.RunTstates(end - m.Tstates() + 2*core.TstatesPerFrame)
	return name, nil
}

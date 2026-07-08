package core

import "sort"

// Tape timing constants, measured cycle-exactly from ROM A v28's own SAVE
// routine (TestSaveNativeStream captures the latch writes; AGENTS.md log
// 12). The written waveform per pulse is high 662 T, low 662 T, then the
// mid rest level; on playback the input comparator holds D0 low at
// peripheral offset 0x00 for one half-pulse.
const (
	// tapePulseWidth is how long the comparator reads active (D0 low)
	// for one pulse.
	tapePulseWidth = 662
	// tapeZeroCell is the bit-cell length of a "0" (one pulse at the
	// cell start).
	tapeZeroCell = 9377
	// tapeOneSplit is the offset of the second pulse within a "1" cell;
	// tapeOneCell is that cell's total length.
	tapeOneSplit = 4705
	tapeOneCell  = 9423
	// tapeInterbyteExtra is the additional pause SAVE leaves after every
	// byte's eight cells (the ROM's per-byte bookkeeping), leader
	// included.
	tapeInterbyteExtra = 13421
	// tapeLeaderBytes is the zero-byte leader SAVE writes before the
	// 0xA5 sync byte. GTP files do not store it, so the schedule
	// compiler synthesizes it.
	tapeLeaderBytes = 96
	// tapeInterblockGap is the silence inserted between standard blocks
	// of a multi-block image (arbitrary; ≈1 s of tape hiss).
	tapeInterblockGap = CPUClockHz
)

// TapeSchedule is a compiled cassette side: comparator-active pulse start
// times relative to the moment the deck starts playing. Fields are
// exported for snapshot serialization only.
type TapeSchedule struct {
	// Starts holds each pulse's start T-state, ascending.
	Starts []uint64
	// Width is the comparator-active duration of every pulse.
	Width uint64
}

// CompileTapeBlocks compiles native tape byte streams (each starting at
// the 0xA5 sync byte, as stored in a GTP standard block) into a pulse
// schedule. Each stream gets the synthesized zero leader in front, and
// consecutive streams are separated by a silent gap. Bytes go to tape
// LSB-first: a "0" bit is one pulse per cell, a "1" bit two.
func CompileTapeBlocks(streams ...[]byte) *TapeSchedule {
	s := &TapeSchedule{Width: tapePulseWidth}
	var t uint64
	emitByte := func(b byte) {
		for j := 0; j < 8; j++ {
			s.Starts = append(s.Starts, t)
			if b>>j&1 == 1 {
				s.Starts = append(s.Starts, t+tapeOneSplit)
				t += tapeOneCell
			} else {
				t += tapeZeroCell
			}
		}
		t += tapeInterbyteExtra
	}
	for i, stream := range streams {
		if i > 0 {
			t += tapeInterblockGap
		}
		for j := 0; j < tapeLeaderBytes; j++ {
			emitByte(0)
		}
		for _, b := range stream {
			emitByte(b)
		}
	}
	return s
}

// Duration returns the schedule length in T-states (end of the last
// pulse).
func (s *TapeSchedule) Duration() uint64 {
	if len(s.Starts) == 0 {
		return 0
	}
	return s.Starts[len(s.Starts)-1] + s.Width
}

// active reports whether the comparator sees a pulse at schedule-relative
// time rel.
func (s *TapeSchedule) active(rel uint64) bool {
	i := sort.Search(len(s.Starts), func(i int) bool { return s.Starts[i] > rel })
	return i > 0 && rel-s.Starts[i-1] < s.Width
}

// InsertTape loads a compiled schedule into the deck, stopped and rewound.
// Passing nil ejects the tape.
func (m *Machine) InsertTape(s *TapeSchedule) {
	m.journal(histEvent{kind: histInsertTape, tape: s})
	m.tape = s
	m.tapePlaying = false
	m.tapeOrigin = 0
}

// PlayTape starts (or restarts, from the beginning) tape playback at the
// current machine T-state. The comparator input at offset 0x00 follows
// the schedule from here on.
func (m *Machine) PlayTape() {
	if m.tape != nil {
		m.journal(histEvent{kind: histPlayTape})
		m.tapeOrigin = m.tstates
		m.tapePlaying = true
	}
}

// StopTape stops playback.
func (m *Machine) StopTape() {
	m.journal(histEvent{kind: histStopTape})
	m.tapePlaying = false
}

// TapePlaying reports whether the deck is currently playing (true even
// after the schedule's last pulse; the tape keeps "rolling" silently).
func (m *Machine) TapePlaying() bool { return m.tapePlaying }

// TapeEndTstate returns the machine T-state at which the playing tape's
// last pulse ends, and false if no tape is playing.
func (m *Machine) TapeEndTstate() (uint64, bool) {
	if !m.tapePlaying || m.tape == nil {
		return 0, false
	}
	return m.tapeOrigin + m.tape.Duration(), true
}

// comparatorActive reports the tape comparator level for periphRead:
// true = pulse present = D0 low.
func (m *Machine) comparatorActive() bool {
	return m.tapePlaying && m.tape != nil && m.tstates >= m.tapeOrigin &&
		m.tape.active(m.tstates-m.tapeOrigin)
}

// --- Tape recording (SAVE capture) ---
//
// The recorder observes latch writes and reassembles the native byte
// streams the ROM's SAVE routine puts on tape, ready to wrap into a GTP
// image (gtp.Build) or feed straight back into CompileTapeBlocks. It is
// observational: it never affects execution and is excluded from
// snapshots (a snapshot taken mid-recording drops the capture).

const (
	// recHalfMin/Max is the accepted duration band for each pulse half
	// (nominal tapePulseWidth = 662 T, ±10 T ROM code-path jitter, with
	// generous margin). Latch writes outside the band reset the detector,
	// which is what makes video-ISR latch traffic (never a high level)
	// unable to fabricate pulses.
	recHalfMin = 600
	recHalfMax = 730
	// recStreamGap splits the recording into streams: the largest
	// legitimate intra-stream pulse gap is tapeZeroCell +
	// tapeInterbyteExtra = 22798 T, so anything beyond this is silence
	// between two SAVEs.
	recStreamGap = 40000
)

// Pulse-detector phases.
const (
	recIdle = iota
	recHigh
	recLow
)

// tapeOutLevel classifies a latch value's 3-level tape output: latch bits
// 6 and 2 both set = high, both clear = low, mixed = mid (SPEC.md §3.2).
func tapeOutLevel(latch byte) int {
	switch latch & 0x44 {
	case 0x44:
		return 2 // high
	case 0x00:
		return 0 // low
	default:
		return 1 // mid
	}
}

// StartTapeRecording arms the recorder, discarding any previous capture.
func (m *Machine) StartTapeRecording() {
	m.recording = true
	m.recPhase = recIdle
	m.recStarts = nil
}

// TapeRecording reports whether the recorder is armed.
func (m *Machine) TapeRecording() bool { return m.recording }

// StopTapeRecording disarms the recorder and returns the captured native
// byte streams, one per SAVE, each starting at the 0xA5 sync byte with
// the zero leader stripped — exactly the payload of a GTP standard block.
// It returns nil when nothing decodable was captured.
func (m *Machine) StopTapeRecording() [][]byte {
	m.recording = false
	m.recPhase = recIdle
	starts := m.recStarts
	m.recStarts = nil
	return decodeTapeStarts(starts)
}

// decodeTapeStarts splits pulse start times on silence and decodes each
// run into a native byte stream — shared by the recorder and by
// TapeSchedule.TapeStreams (WAV input).
func decodeTapeStarts(starts []uint64) [][]byte {
	var out [][]byte
	for len(starts) > 0 {
		n := 1
		for n < len(starts) && starts[n]-starts[n-1] <= recStreamGap {
			n++
		}
		if b := decodeRecordedStream(starts[:n]); b != nil {
			out = append(out, b)
		}
		starts = starts[n:]
	}
	return out
}

// recordLatch advances the pulse detector on a latch write; next is the
// incoming value, m.latch still holds the outgoing one. A pulse is the
// exact high→low→mid sequence with both halves in the accepted band; its
// recorded start is the T-state of the high edge.
func (m *Machine) recordLatch(next byte) {
	lvl := tapeOutLevel(next)
	if lvl == tapeOutLevel(m.latch) {
		return
	}
	switch {
	case lvl == 2:
		m.recPhase = recHigh
		m.recHighT = m.tstates
	case m.recPhase == recHigh && lvl == 0:
		if d := m.tstates - m.recHighT; d >= recHalfMin && d <= recHalfMax {
			m.recPhase = recLow
			m.recLowT = m.tstates
		} else {
			m.recPhase = recIdle
		}
	case m.recPhase == recLow && lvl == 1:
		if d := m.tstates - m.recLowT; d >= recHalfMin && d <= recHalfMax {
			m.recStarts = append(m.recStarts, m.recHighT)
		}
		m.recPhase = recIdle
	default:
		m.recPhase = recIdle
	}
}

// decodePulseBytes turns pulse start times into the byte stream they
// encode: a second pulse less than 6000 T after a cell start marks a "1"
// bit (nominal split 4705 T vs. a full 9377 T zero cell). LSB first;
// a trailing partial byte is dropped.
func decodePulseBytes(starts []uint64) []byte {
	var bits []int
	for i := 0; i < len(starts); i++ {
		if i+1 < len(starts) && starts[i+1]-starts[i] < 6000 {
			bits = append(bits, 1)
			i++
		} else {
			bits = append(bits, 0)
		}
	}
	var out []byte
	for i := 0; i+8 <= len(bits); i += 8 {
		b := byte(0)
		for j := 0; j < 8; j++ {
			b |= byte(bits[i+j]) << j
		}
		out = append(out, b)
	}
	return out
}

// decodeRecordedStream decodes one stream's pulses and strips the zero
// leader; nil if no 0xA5 sync byte follows (noise).
func decodeRecordedStream(starts []uint64) []byte {
	p := decodePulseBytes(starts)
	i := 0
	for i < len(p) && p[i] == 0 {
		i++
	}
	if i == len(p) || p[i] != 0xA5 {
		return nil
	}
	return p[i:]
}

package core

import "github.com/mtrisic/gozilog/z80"

// Framebuffer geometry: the full frame the beam sweeps, borders included
// (SPEC.md §3.4). Pixels are luminance bytes: 1 = bright, 0 = dark.
const (
	// FrameWidth is 384 pixels: 192 T-states per line × 2 pixels per T-state.
	FrameWidth = 384
	// FrameHeight is the 320 scanlines of one frame.
	FrameHeight = 320
)

// The standard active area within the full frame: 32×16 characters =
// 256×208 pixels. ActiveX/ActiveY were calibrated in Phase 1 against the
// ROM's boot screen with default HORIZ_POS (11) and no scroll offset —
// see TestBootReady and PLAN.md §1.3.
const (
	ActiveX = 78
	ActiveY = 59
	ActiveW = 256
	ActiveH = 208
)

// FrameSink receives each completed frame. The pix slice is borrowed: it
// is only valid until the machine executes further; copy it to keep it.
type FrameSink func(pix []byte, seq uint64)

// SetFrameSink registers a callback invoked at every vsync with the just
// completed frame. Pass nil to remove it.
func (m *Machine) SetFrameSink(sink FrameSink) { m.sink = sink }

// FrameSeq returns the number of completed frames since power-on.
func (m *Machine) FrameSeq() uint64 { return m.frameSeq }

// Frame copies the most recently completed full frame into dst (which
// must be FrameWidth*FrameHeight bytes) and returns its sequence number.
func (m *Machine) Frame(dst []byte) uint64 {
	copy(dst, m.front)
	return m.frameSeq
}

// glyphIndex maps a video-RAM character code to the chargen glyph index.
// Only 7 data-bus lines reach the chargen ROM and the unconnected one is
// D6 (verified against the ROM image; AGENTS.md log 9): A6 comes from D7,
// A5..A0 from D5..D0.
func glyphIndex(code byte) uint16 {
	return uint16(code&0x3F) | uint16(code&0x80)>>1
}

// Tick implements z80.Ticker: the whole video circuit lives here. It is
// called exactly once per T-state (waits included) and emits two pixels
// per call. See SPEC.md §4.2 for the model.
func (m *Machine) Tick(addr uint16, data int16, pins z80.Pins) int {
	t := m.tstates
	m.tstates++
	ft := t % TstatesPerFrame

	// Vsync: the tick at frame phase 0 begins a new frame; the previous
	// back buffer is complete (every frame tick writes its two pixels).
	if ft == 0 && t != 0 {
		m.back, m.front = m.front, m.back
		m.frameSeq++
		if m.sink != nil {
			m.sink(m.front, m.frameSeq)
		}
	}

	// Emit two pixels at the beam position. The shift register outputs
	// its LSB first (AGENTS.md log 10) and drains to dark (serial fill
	// is logic 1; chargen polarity: 1 = dark, 0 = bright).
	base := int(ft/TstatesPerLine)*FrameWidth + int(ft%TstatesPerLine)*2
	m.back[base] = ^m.shreg & 1
	m.shreg = m.shreg>>1 | 0x80
	m.back[base+1] = ^m.shreg & 1
	m.shreg = m.shreg>>1 | 0x80

	// Free-running interrupt generator: assert at the 56th hsync after
	// vsync; deassert on acknowledge or after the fallback pulse width.
	if ft == intAssertTstate {
		m.cpu.SetINT(true)
		m.intAsserted = true
	} else if m.intAsserted && ft >= intAssertTstate+intFallbackTstates {
		m.cpu.SetINT(false)
		m.intAsserted = false
	}

	// Refresh handling: T3 (first refresh T-state) carries the video
	// address I<<8|R — the selected chip drives the character code onto
	// the data bus; T4 (second) ends the M1 cycle, when the 74LS166
	// load pulse latches the chargen output for the CURRENT latch row.
	// (The row latched by the scanline's leading `ld (hl),d` write is
	// not yet visible at its own M1's load — that stray border char
	// therefore renders with the previous row value, exactly as on
	// hardware. SPEC.md §4.2.)
	rfsh := pins&z80.RFSH != 0
	if rfsh && !m.prevRfsh {
		m.charCode = m.MemRead(addr)
	} else if rfsh && m.prevRfsh {
		row := uint16(m.latch>>2) & 0x0F
		m.shreg = m.chargen[row<<7|glyphIndex(m.charCode)]
	}
	m.prevRfsh = rfsh

	// Watchpoints observe the bus pins of CPU data accesses (debug.go).
	if m.watches != nil {
		m.checkWatch(addr, data, pins)
	}

	// Interrupt acknowledge: hardware asserts /WAIT until the next hsync
	// edge so the ISR starts scanline-aligned. gozilog samples the wait
	// count on the M1|IORQ T-state; each inserted wait re-enters Tick
	// (advancing the beam) with the return value ignored.
	if pins == z80.M1|z80.IORQ {
		m.cpu.SetINT(false)
		m.intAsserted = false
		return int((TstatesPerLine - (t+1)%TstatesPerLine) % TstatesPerLine)
	}
	return 0
}

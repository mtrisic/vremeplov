// Package core emulates the Galaksija, the Yugoslav Z80A home computer
// designed by Voja Antonić (1983), on top of the gozilog CPU core.
//
// The Galaksija has no video chip: the CPU generates video via R-register
// refresh addressing, driven by the ROM's interrupt service routine. The
// Machine implements gozilog's per-T-state Ticker hook and reproduces that
// pipeline cycle-accurately (see SPEC.md §3.3–3.4 and §4.2).
//
// The package is deterministic and synchronous: it never reads the wall
// clock, starts no goroutines, and all external input (keys, tape) enters
// as T-state-stamped events. Frontends own the run loop and real-time
// pacing. All methods must be called from a single goroutine.
package core

import (
	"fmt"

	"github.com/mtrisic/gozilog/z80"
)

// Machine geometry and timing constants (SPEC.md §3.4).
const (
	// TstatesPerLine is the length of one scanline: 192 T-states at the
	// 3.072 MHz CPU clock (16 kHz line rate).
	TstatesPerLine = 192
	// LinesPerFrame is the number of scanlines per 50 Hz frame.
	LinesPerFrame = 320
	// TstatesPerFrame is the length of one frame: 61,440 T-states = 20 ms.
	TstatesPerFrame = TstatesPerLine * LinesPerFrame
	// CPUClockHz is the Galaksija CPU clock (half the 6.144 MHz pixel clock).
	CPUClockHz = 3_072_000
	// FramesPerSecond is the video frame rate.
	FramesPerSecond = 50

	// intAssertTstate is the frame phase (T-states after vsync start) at
	// which the divider chain asserts /INT: the 56th hsync after vsync.
	intAssertTstate = 56 * TstatesPerLine
	// intFallbackTstates is how long /INT stays asserted if never
	// acknowledged (pulse width is undocumented; AGENTS.md log entry 1).
	intFallbackTstates = 4 * TstatesPerLine
)

// Memory map boundaries (SPEC.md §3.1).
const (
	romBBase   = 0x1000
	periphBase = 0x2000
	ramBase    = 0x2800
	// onboardRAMTop is the exclusive top of the address range occupied by
	// the onboard 6116 SRAM sockets; the A7 clamp acts on this region.
	onboardRAMTop = 0x4000
)

// RAMSize selects the emulated RAM configuration.
type RAMSize int

const (
	// RAM2K, RAM4K, RAM6K are the historical configurations (one to three
	// 6116 chips at 0x2800).
	RAM2K RAMSize = 2
	RAM4K RAMSize = 4
	RAM6K RAMSize = 6
	// RAMExpanded is a non-historical flat RAM filling 0x2800–0xFFFF,
	// useful for developing larger programs. The A7 clamp still applies
	// to the onboard region 0x2800–0x3FFF only.
	RAMExpanded RAMSize = 64
)

// Config carries the immutable machine configuration. ROM images are
// copied at construction; the caller keeps ownership of the slices.
type Config struct {
	// ROMA is the 4096-byte ROM A image (required).
	ROMA []byte
	// ROMB is the optional 4096-byte ROM B image; nil leaves the socket
	// empty (reads 0xFF).
	ROMB []byte
	// Chargen is the 2048-byte character generator ROM image (required).
	Chargen []byte
	// RAM selects the RAM configuration; the zero value means RAM6K.
	RAM RAMSize
}

// Machine is a complete Galaksija: CPU, memory map, latch, cycle-accurate
// video pipeline, and keyboard matrix. It implements z80.Bus and z80.Ticker.
type Machine struct {
	cpu *z80.CPU

	romA    [0x1000]byte
	romB    [0x1000]byte
	hasROMB bool
	chargen [0x800]byte
	ram     []byte
	ramSize RAMSize
	latch   byte

	keys  [keyCount]bool
	queue []KeyEvent
	qpos  int

	// Tape deck (SPEC.md §3.6, §4.2).
	tape        *TapeSchedule
	tapeOrigin  uint64
	tapePlaying bool

	// Tape recorder (SAVE capture; tape.go) — observational, excluded
	// from snapshots.
	recording bool
	recPhase  int
	recHighT  uint64
	recLowT   uint64
	recStarts []uint64

	// Audio (audio.go) — the tape-out DAC as a pulled sample stream;
	// observational, excluded from snapshots.
	sndEnabled bool
	sndPending []sndEvent
	sndAt      uint64
	sndLevel   int8

	// Rewind history (history.go) — snapshot ring + input journal;
	// excluded from snapshots (it is infrastructure around them).
	histEnabled   bool
	histReplaying bool
	histInterval  uint64
	histDepth     int
	histNextAt    uint64
	histRing      []*Snapshot
	histJournal   []histEvent

	// Debugger state (debug.go) — observational only, excluded from
	// snapshots.
	breakpoints map[uint16]struct{}
	watches     []Watch
	watchHit    bool
	watchStop   Stop

	// tstates is the machine-owned T-state counter — the single authority
	// for video phase (SPEC.md §4.2: never use cpu.Tstates(), it is not
	// restored by SetState).
	tstates uint64

	// Video pipeline state.
	shreg       byte // 74LS166 model; bit 0 is the next (leftmost) pixel
	charCode    byte // data-bus byte captured on the first refresh T-state
	prevRfsh    bool // previous tick was a refresh T-state
	intAsserted bool
	back, front []byte // FrameWidth*FrameHeight luminance buffers
	frameSeq    uint64
	sink        FrameSink
}

// New constructs a powered-on Machine. The CPU starts at PC=0 (ROM A's
// reset vector), as after a hardware power-on.
func New(cfg Config) (*Machine, error) {
	if len(cfg.ROMA) != len(Machine{}.romA) {
		return nil, fmt.Errorf("core: ROM A must be %d bytes, got %d", len(Machine{}.romA), len(cfg.ROMA))
	}
	if len(cfg.Chargen) != len(Machine{}.chargen) {
		return nil, fmt.Errorf("core: chargen ROM must be %d bytes, got %d", len(Machine{}.chargen), len(cfg.Chargen))
	}
	if cfg.ROMB != nil && len(cfg.ROMB) != len(Machine{}.romB) {
		return nil, fmt.Errorf("core: ROM B must be %d bytes, got %d", len(Machine{}.romB), len(cfg.ROMB))
	}
	size := cfg.RAM
	if size == 0 {
		size = RAM6K
	}
	var ramLen int
	switch size {
	case RAM2K, RAM4K, RAM6K:
		ramLen = int(size) * 1024
	case RAMExpanded:
		ramLen = 0x10000 - ramBase
	default:
		return nil, fmt.Errorf("core: invalid RAM size %d", size)
	}

	m := &Machine{
		ram:     make([]byte, ramLen),
		ramSize: size,
		back:    make([]byte, FrameWidth*FrameHeight),
		front:   make([]byte, FrameWidth*FrameHeight),
	}
	copy(m.romA[:], cfg.ROMA)
	copy(m.chargen[:], cfg.Chargen)
	if cfg.ROMB != nil {
		copy(m.romB[:], cfg.ROMB)
		m.hasROMB = true
	}
	m.cpu = z80.New(m)
	return m, nil
}

// MemRead implements z80.Bus. It is side-effect free and is also used
// internally for the refresh-time video fetch.
func (m *Machine) MemRead(addr uint16) byte {
	switch {
	case addr < romBBase:
		return m.romA[addr]
	case addr < periphBase:
		if m.hasROMB {
			return m.romB[addr-romBBase]
		}
		return 0xFF
	case addr < ramBase:
		return m.periphRead(addr)
	default:
		return m.ramRead(addr)
	}
}

// MemWrite implements z80.Bus.
func (m *Machine) MemWrite(addr uint16, data byte) {
	switch {
	case addr < periphBase:
		// ROM region: writes ignored.
	case addr < ramBase:
		// Latch decode: 0010 0xxx xx11 1xxx (SPEC.md §3.1). Keyboard
		// region writes are ignored.
		if addr&0x38 == 0x38 {
			if m.recording {
				m.recordLatch(data)
			}
			if m.sndEnabled {
				m.soundLatch(data)
			}
			m.latch = data
		}
	default:
		if eff, ok := m.ramIndex(addr); ok {
			m.ram[eff] = data
		}
	}
}

// IORead implements z80.Bus. The Galaksija decodes no I/O ports; reads
// see a floating bus.
func (m *Machine) IORead(port uint16) byte { return 0xFF }

// IOWrite implements z80.Bus. No I/O ports exist; writes are ignored.
func (m *Machine) IOWrite(port uint16, data byte) {}

// periphRead handles 0x2000–0x27FF: the keyboard/comparator/latch block,
// mirrored every 0x40 bytes. Key state is on D0, active low; undefined
// data bits read as 1.
func (m *Machine) periphRead(addr uint16) byte {
	off := addr & 0x3F
	if off >= 0x38 {
		return 0xFF // write-only latch; reads undefined (AGENTS.md log 3)
	}
	if off == comparatorOffset {
		// Tape comparator: D0 low while a pulse passes the head.
		if m.comparatorActive() {
			return 0xFE
		}
		return 0xFF
	}
	if m.keys[off] {
		return 0xFE
	}
	return 0xFF
}

// ramIndex translates a CPU address to an index into m.ram, applying the
// A7 clamp: while latch bit 7 is 0, the hardware forces RAM address line
// A7 to 1 for the onboard SRAM region (all accesses, not just refresh —
// SPEC.md §3.2, AGENTS.md log 5). Returns ok=false for unpopulated
// addresses.
func (m *Machine) ramIndex(addr uint16) (int, bool) {
	if addr < onboardRAMTop && m.latch&0x80 == 0 {
		addr |= 0x80
	}
	idx := int(addr) - ramBase
	if idx < 0 || idx >= len(m.ram) {
		return 0, false
	}
	return idx, true
}

func (m *Machine) ramRead(addr uint16) byte {
	if idx, ok := m.ramIndex(addr); ok {
		return m.ram[idx]
	}
	return 0xFF
}

// CPU exposes the underlying gozilog CPU (register state via
// cpu.State()/SetState()). The Machine owns its run loop; callers should
// prefer the Machine's control methods.
func (m *Machine) CPU() *z80.CPU { return m.cpu }

// Latch returns the current video/tape latch value (write-only on real
// hardware; exposed for tests and debugging).
func (m *Machine) Latch() byte { return m.latch }

// Tstates returns the machine's T-state counter — the authority for video
// phase, preserved across Snapshot/Restore.
func (m *Machine) Tstates() uint64 { return m.tstates }

// Reset pulls the CPU /RESET line: PC to 0, interrupts off. RAM, the
// latch, and the free-running video counters are preserved, matching a
// hardware reset button.
func (m *Machine) Reset() {
	m.journal(histEvent{kind: histReset})
	m.cpu.Reset()
}

// StepInstruction applies due key events and executes one instruction
// (or accepts one pending interrupt). It returns the T-states consumed.
func (m *Machine) StepInstruction() int {
	if m.histEnabled && !m.histReplaying && m.tstates >= m.histNextAt {
		m.histSnapshot()
	}
	m.drainKeyQueue()
	return m.cpu.Step()
}

// RunTstates runs until at least n T-states have elapsed (the final
// instruction may overshoot). It returns the T-states actually consumed.
func (m *Machine) RunTstates(n uint64) uint64 {
	start := m.tstates
	target := start + n
	for m.tstates < target {
		m.StepInstruction()
	}
	return m.tstates - start
}

// RunFrame runs until the current frame completes (the machine T-state
// counter crosses the next frame boundary).
func (m *Machine) RunFrame() {
	boundary := m.tstates - m.tstates%TstatesPerFrame + TstatesPerFrame
	for m.tstates < boundary {
		m.StepInstruction()
	}
}

// LoadBinary copies data into memory starting at addr, as a debugging
// poke (it bypasses the A7 clamp and writes the underlying RAM
// directly). It fails if any byte falls outside populated RAM.
func (m *Machine) LoadBinary(addr uint16, data []byte) error {
	for i := range data {
		a := int(addr) + i
		if a < ramBase || a-ramBase >= len(m.ram) {
			return fmt.Errorf("core: LoadBinary: address 0x%04X outside populated RAM", a)
		}
	}
	m.journal(histEvent{kind: histLoadBinary, addr: addr, data: append([]byte(nil), data...)})
	for i, b := range data {
		m.ram[int(addr)+i-ramBase] = b
	}
	return nil
}

// DumpMemory returns a copy of the CPU-visible memory in [start, end)
// (ROM, peripherals, and RAM read exactly as the CPU would see them,
// except that the A7 clamp state naturally applies).
func (m *Machine) DumpMemory(start, end uint32) []byte {
	if end > 0x10000 {
		end = 0x10000
	}
	if start >= end {
		return nil
	}
	out := make([]byte, end-start)
	for a := start; a < end; a++ {
		out[a-start] = m.MemRead(uint16(a))
	}
	return out
}

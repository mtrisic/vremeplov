// Package monitor is the frontend-agnostic engine of the machine-language
// debugger shared by the TUI and web frontends: a command REPL over a
// core.Machine plus the register/disassembly/watch formatting the panels
// display. Frontends own all presentation (layout, input editing,
// scrollback) and the run loop; they mirror Session.Paused into it.
//
// REPL conventions: addresses, bytes, and lengths are hex (an optional 0x
// prefix is tolerated); repeat counts are decimal. Memory commands see the
// CPU's view — the A7 clamp applies while latch bit 7 is 0 (AGENTS.md
// log 14).
package monitor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mtrisic/gozilog/z80/disasm"
	"github.com/mtrisic/vremeplov/core"
)

// RunBudget caps synchronous run commands (n, to, frame N) so a target
// that is never reached returns to the prompt: 250 emulated frames =
// 5 emulated seconds.
const RunBudget = 250 * core.TstatesPerFrame

// dispWatch is a watch expression: a memory location displayed (byte or
// word) in the panel while stepping.
type dispWatch struct {
	addr uint16
	word bool
}

// Session is one debugger attached to a machine. Paused is the run/pause
// intent commands produce (`c` clears it, `p`/`s`/`n`/`to`/`frame`/`reset`
// set it); the owning frontend applies it to its run loop and writes back
// when the user pauses through other UI.
type Session struct {
	M      *core.Machine
	Paused bool
	disp   []dispWatch
}

// New attaches a debugger session to m.
func New(m *core.Machine) *Session {
	return &Session{M: m}
}

func (s *Session) pc() uint16 { return s.M.CPU().State().PC }

// memAt adapts the machine for disasm.Decode (CPU-eye, side-effect free).
func (s *Session) memAt(a uint16) byte { return s.M.MemRead(a) }

// Help is the REPL command summary, one line per entry.
var Help = []string{
	"c continue · s [n] step · n step-over · to A run to A",
	"bs [n] step BACK · rw [frames] rewind (time machine)",
	"frame [n] run frames · b/bd A ±breakpoint · bl list",
	"w A[-B] [r|w|rw] watchpoint · wd A del · wl list",
	"x A [len] hex dump · d [A] [n] disasm · poke A b..",
	"watch A [b|w] / unwatch A panel watch · set REG V",
	"reset · addr/byte/len are hex, counts decimal",
	"memory is the CPU's view",
	"(A7 clamp: latch b7=0 aliases 0x28xx-0x3Fxx reads)",
}

// Exec runs one REPL command and returns its output lines. Machine-running
// commands execute synchronously (emulation is far faster than real time)
// and honor breakpoints and watchpoints.
func (s *Session) Exec(line string) []string {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 0 {
		return nil
	}
	cmd, args := f[0], f[1:]
	switch cmd {
	case "help", "h", "?":
		return Help
	case "c":
		s.Paused = false
		return []string{"running"}
	case "p":
		s.Paused = true
		return []string{fmt.Sprintf("paused at %04X", s.pc())}
	case "s":
		n := 1
		if len(args) == 1 {
			if v, err := strconv.Atoi(args[0]); err == nil && v > 0 && v <= 100000 {
				n = v
			} else {
				return []string{fmt.Sprintf("bad count %q", args[0])}
			}
		}
		s.Paused = true
		for i := 0; i < n; i++ {
			s.M.StepInstruction()
		}
		return []string{s.PCLine()}
	case "bs":
		n := 1
		if len(args) == 1 {
			if v, err := strconv.Atoi(args[0]); err == nil && v > 0 && v <= 10000 {
				n = v
			} else {
				return []string{fmt.Sprintf("bad count %q", args[0])}
			}
		}
		s.Paused = true
		if err := s.M.StepBack(n); err != nil {
			return []string{err.Error()}
		}
		return []string{s.PCLine()}
	case "rw":
		frames := 100
		if len(args) == 1 {
			if v, err := strconv.Atoi(args[0]); err == nil && v > 0 && v <= 100000 {
				frames = v
			} else {
				return []string{fmt.Sprintf("bad count %q", args[0])}
			}
		}
		s.Paused = true
		if err := s.M.Rewind(uint64(frames) * core.TstatesPerFrame); err != nil {
			return []string{err.Error()}
		}
		return []string{
			fmt.Sprintf("rewound to T=%d", s.M.Tstates()),
			s.PCLine(),
		}
	case "n":
		s.Paused = true
		in := disasm.Decode(s.memAt, s.pc())
		return s.runTo(in.Addr+uint16(in.Len), "step-over")
	case "to":
		if len(args) != 1 {
			return []string{"usage: to ADDR"}
		}
		addr, err := parseHex(args[0])
		if err != nil {
			return []string{err.Error()}
		}
		s.Paused = true
		return s.runTo(addr, "run-to")
	case "frame":
		n := 1
		if len(args) == 1 {
			if v, err := strconv.Atoi(args[0]); err == nil && v > 0 && v <= 250 {
				n = v
			} else {
				return []string{fmt.Sprintf("bad count %q", args[0])}
			}
		}
		s.Paused = true
		boundary := s.M.Tstates() - s.M.Tstates()%core.TstatesPerFrame +
			uint64(n)*core.TstatesPerFrame
		if st := s.M.RunDebug(boundary - s.M.Tstates()); st.Reason != core.StopBudget {
			return []string{FormatStop(st), s.PCLine()}
		}
		return []string{s.PCLine()}
	case "b", "bd":
		if len(args) != 1 {
			return []string{fmt.Sprintf("usage: %s ADDR", cmd)}
		}
		addr, err := parseHex(args[0])
		if err != nil {
			return []string{err.Error()}
		}
		if cmd == "b" {
			s.M.AddBreakpoint(addr)
			return []string{fmt.Sprintf("breakpoint at %04X", addr)}
		}
		s.M.RemoveBreakpoint(addr)
		return []string{fmt.Sprintf("breakpoint at %04X removed", addr)}
	case "bl":
		bps := s.M.Breakpoints()
		if len(bps) == 0 {
			return []string{"no breakpoints"}
		}
		parts := make([]string, len(bps))
		for i, pc := range bps {
			parts[i] = fmt.Sprintf("%04X", pc)
		}
		return []string{"breakpoints: " + strings.Join(parts, " ")}
	case "w":
		if len(args) < 1 || len(args) > 2 {
			return []string{"usage: w ADDR[-END] [r|w|rw]"}
		}
		lo, hi := args[0], args[0]
		if a, b, found := strings.Cut(args[0], "-"); found {
			lo, hi = a, b
		}
		start, err := parseHex(lo)
		if err != nil {
			return []string{err.Error()}
		}
		end, err := parseHex(hi)
		if err != nil {
			return []string{err.Error()}
		}
		kind := core.WatchRW
		if len(args) == 2 {
			switch args[1] {
			case "r":
				kind = core.WatchRead
			case "w":
				kind = core.WatchWrite
			case "rw":
			default:
				return []string{fmt.Sprintf("bad kind %q (want r, w, or rw)", args[1])}
			}
		}
		if err := s.M.AddWatch(start, end, kind); err != nil {
			return []string{err.Error()}
		}
		return []string{fmt.Sprintf("watchpoint %04X-%04X", start, end)}
	case "wd":
		if len(args) != 1 {
			return []string{"usage: wd ADDR"}
		}
		addr, err := parseHex(args[0])
		if err != nil {
			return []string{err.Error()}
		}
		s.M.RemoveWatch(addr)
		return []string{fmt.Sprintf("watchpoints at %04X removed", addr)}
	case "wl":
		ws := s.M.Watches()
		if len(ws) == 0 {
			return []string{"no watchpoints"}
		}
		out := make([]string, len(ws))
		for i, w := range ws {
			kind := map[core.WatchKind]string{
				core.WatchRead: "r", core.WatchWrite: "w", core.WatchRW: "rw",
			}[w.Kind]
			out[i] = fmt.Sprintf("watch %04X-%04X %s", w.Start, w.End, kind)
		}
		return out
	case "x":
		if len(args) < 1 || len(args) > 2 {
			return []string{"usage: x ADDR [LEN]"}
		}
		addr, err := parseHex(args[0])
		if err != nil {
			return []string{err.Error()}
		}
		n := 64
		if len(args) == 2 {
			if v, err := strconv.ParseUint(strings.TrimPrefix(args[1], "0x"), 16, 16); err == nil && v > 0 {
				n = int(v)
			} else {
				return []string{fmt.Sprintf("bad length %q", args[1])}
			}
		}
		var out []string
		for off := 0; off < n; off += 8 {
			var hex, asc strings.Builder
			for i := 0; i < 8 && off+i < n; i++ {
				b := s.memAt(addr + uint16(off+i))
				fmt.Fprintf(&hex, "%02X ", b)
				if b >= 0x20 && b < 0x7F {
					asc.WriteByte(b)
				} else {
					asc.WriteByte('.')
				}
			}
			out = append(out, fmt.Sprintf("%04X %-24s|%s|", addr+uint16(off), hex.String(), asc.String()))
		}
		return out
	case "d":
		addr := s.pc()
		n := 8
		if len(args) >= 1 {
			a, err := parseHex(args[0])
			if err != nil {
				return []string{err.Error()}
			}
			addr = a
		}
		if len(args) == 2 {
			if v, err := strconv.Atoi(args[1]); err == nil && v > 0 && v <= 64 {
				n = v
			} else {
				return []string{fmt.Sprintf("bad count %q", args[1])}
			}
		}
		out := make([]string, n)
		for i := 0; i < n; i++ {
			in := disasm.Decode(s.memAt, addr)
			out[i] = formatInstr(in, " ")
			addr = in.Addr + uint16(in.Len)
		}
		return out
	case "poke":
		if len(args) < 2 {
			return []string{"usage: poke ADDR BYTE.."}
		}
		addr, err := parseHex(args[0])
		if err != nil {
			return []string{err.Error()}
		}
		data := make([]byte, len(args)-1)
		for i, str := range args[1:] {
			if data[i], err = parseByte(str); err != nil {
				return []string{err.Error()}
			}
		}
		if err := s.M.LoadBinary(addr, data); err != nil {
			return []string{err.Error()}
		}
		return []string{fmt.Sprintf("poked %d byte(s) at %04X", len(data), addr)}
	case "watch":
		if len(args) < 1 || len(args) > 2 {
			return []string{"usage: watch ADDR [b|w]"}
		}
		addr, err := parseHex(args[0])
		if err != nil {
			return []string{err.Error()}
		}
		word := false
		if len(args) == 2 {
			switch args[1] {
			case "b":
			case "w":
				word = true
			default:
				return []string{fmt.Sprintf("bad size %q (want b or w)", args[1])}
			}
		}
		s.disp = append(s.disp, dispWatch{addr: addr, word: word})
		return []string{fmt.Sprintf("watching %04X", addr)}
	case "unwatch":
		if len(args) != 1 {
			return []string{"usage: unwatch ADDR"}
		}
		addr, err := parseHex(args[0])
		if err != nil {
			return []string{err.Error()}
		}
		kept := s.disp[:0]
		for _, d := range s.disp {
			if d.addr != addr {
				kept = append(kept, d)
			}
		}
		s.disp = kept
		return []string{fmt.Sprintf("unwatched %04X", addr)}
	case "set":
		if len(args) != 2 {
			return []string{"usage: set REG VALUE"}
		}
		if err := s.setRegister(args[0], args[1]); err != nil {
			return []string{err.Error()}
		}
		// Register writes bypass the machine's journaled entry points;
		// rebase the rewind history on the new state.
		s.M.HistoryRebase()
		return []string{fmt.Sprintf("%s = %s", strings.ToUpper(args[0]),
			strings.ToUpper(strings.TrimPrefix(args[1], "0x")))}
	case "reset":
		s.M.Reset()
		s.Paused = true
		return []string{"machine reset (paused; c to run)"}
	default:
		return []string{fmt.Sprintf("unknown command %q (try help)", cmd)}
	}
}

// runTo runs until PC reaches target (a temporary breakpoint), another
// stop fires first, or RunBudget runs out.
func (s *Session) runTo(target uint16, what string) []string {
	temp := true
	for _, bp := range s.M.Breakpoints() {
		if bp == target {
			temp = false
		}
	}
	if temp {
		s.M.AddBreakpoint(target)
		defer s.M.RemoveBreakpoint(target)
	}
	st := s.M.RunDebug(RunBudget)
	if st.Reason == core.StopBudget {
		return []string{
			fmt.Sprintf("%s: %04X not reached (ran %d frames)", what, target,
				RunBudget/core.TstatesPerFrame),
			s.PCLine(),
		}
	}
	if st.Reason == core.StopBreakpoint && st.PC == target {
		return []string{s.PCLine()}
	}
	return []string{FormatStop(st), s.PCLine()}
}

// PCLine is the disassembly of the instruction the machine is paused at.
func (s *Session) PCLine() string {
	return formatInstr(disasm.Decode(s.memAt, s.pc()), " ")
}

// FormatStop renders a breakpoint/watchpoint stop for a status line or
// log ("" for a plain budget stop).
func FormatStop(st core.Stop) string {
	switch st.Reason {
	case core.StopBreakpoint:
		return fmt.Sprintf("break at %04X", st.PC)
	case core.StopWatch:
		dir := "read"
		if st.Write {
			dir = "write"
		}
		return fmt.Sprintf("watch %s %04X=%02X (PC %04X)", dir, st.Addr, st.Data, st.PC)
	}
	return ""
}

func (s *Session) setRegister(name, val string) error {
	v, err := parseHex(val)
	if err != nil {
		return err
	}
	st := s.M.CPU().State()
	lo := func(p *uint16) { *p = *p&0xFF00 | v&0xFF }
	hi := func(p *uint16) { *p = *p&0x00FF | v<<8 }
	switch strings.ToLower(name) {
	case "pc":
		st.PC = v
	case "sp":
		st.SP = v
	case "af":
		st.AF = v
	case "bc":
		st.BC = v
	case "de":
		st.DE = v
	case "hl":
		st.HL = v
	case "ix":
		st.IX = v
	case "iy":
		st.IY = v
	case "a":
		hi(&st.AF)
	case "f":
		lo(&st.AF)
	case "b":
		hi(&st.BC)
	case "c":
		lo(&st.BC)
	case "d":
		hi(&st.DE)
	case "e":
		lo(&st.DE)
	case "h":
		hi(&st.HL)
	case "l":
		lo(&st.HL)
	case "i":
		st.I = byte(v)
	case "r":
		st.R = byte(v)
	default:
		return fmt.Errorf("unknown register %q", name)
	}
	s.M.CPU().SetState(st)
	return nil
}

// RegisterLines renders the register panel: three lines with the pairs,
// flags, interrupt state, and the machine T-state counter.
func (s *Session) RegisterLines() []string {
	st := s.M.CPU().State()
	iff := 0
	if st.IFF1 {
		iff = 1
	}
	return []string{
		fmt.Sprintf("PC %04X SP %04X AF %04X BC %04X", st.PC, st.SP, st.AF, st.BC),
		fmt.Sprintf("DE %04X HL %04X IX %04X IY %04X", st.DE, st.HL, st.IX, st.IY),
		fmt.Sprintf("F %s IM%d IFF%d I %02X R %02X T %d",
			flagString(byte(st.AF)), st.IM, iff, st.I, st.R, s.M.Tstates()),
	}
}

// DisasmLines renders an n-line disassembly window starting at PC:
// "▶" marks PC, "●" a breakpoint, "◉" both.
func (s *Session) DisasmLines(n int) []string {
	pc := s.pc()
	bps := make(map[uint16]bool)
	for _, b := range s.M.Breakpoints() {
		bps[b] = true
	}
	out := make([]string, n)
	addr := pc
	for i := 0; i < n; i++ {
		in := disasm.Decode(s.memAt, addr)
		marker := " "
		switch {
		case in.Addr == pc && bps[in.Addr]:
			marker = "◉"
		case in.Addr == pc:
			marker = "▶"
		case bps[in.Addr]:
			marker = "●"
		}
		out[i] = formatInstr(in, marker)
		addr = in.Addr + uint16(in.Len)
	}
	return out
}

// WatchLines renders the display watches, one per line (empty when none
// are set).
func (s *Session) WatchLines() []string {
	out := make([]string, 0, len(s.disp))
	for _, d := range s.disp {
		if d.word {
			v := uint16(s.memAt(d.addr)) | uint16(s.memAt(d.addr+1))<<8
			out = append(out, fmt.Sprintf("%04X w %04X", d.addr, v))
		} else {
			out = append(out, fmt.Sprintf("%04X b %02X", d.addr, s.memAt(d.addr)))
		}
	}
	return out
}

// parseHex reads a 16-bit hex number (monitor convention: bare hex, an
// optional 0x prefix is tolerated).
func parseHex(str string) (uint16, error) {
	str = strings.TrimPrefix(strings.TrimPrefix(str, "0x"), "0X")
	v, err := strconv.ParseUint(str, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("bad hex %q", str)
	}
	return uint16(v), nil
}

func parseByte(str string) (byte, error) {
	str = strings.TrimPrefix(strings.TrimPrefix(str, "0x"), "0X")
	v, err := strconv.ParseUint(str, 16, 8)
	if err != nil {
		return 0, fmt.Errorf("bad hex byte %q", str)
	}
	return byte(v), nil
}

// formatInstr renders one disassembly line: marker, address, raw bytes,
// mnemonic.
func formatInstr(in disasm.Instr, marker string) string {
	var b strings.Builder
	for _, x := range in.Bytes[:in.Len] {
		fmt.Fprintf(&b, "%02X", x)
	}
	return fmt.Sprintf("%s%04X %-8s %s", marker, in.Addr, b.String(), in.Text)
}

// flagString renders F as SZ5H3PNC with '-' for clear bits.
func flagString(f byte) string {
	const names = "SZ5H3PNC"
	out := []byte(names)
	for i := 0; i < 8; i++ {
		if f&(0x80>>i) == 0 {
			out[i] = '-'
		}
	}
	return string(out)
}

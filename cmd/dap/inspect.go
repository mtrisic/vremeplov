package main

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/go-dap"
	"github.com/mtrisic/gozilog/z80/disasm"
)

// Variables references: two fixed scopes for the single Z80 frame.
const (
	refRegisters = 1
	refShadow    = 2
)

// onStackTrace reports the single Z80 frame: named by the nearest SLD
// label (label+0xNN) or the disassembly text, with source location
// when the SLD maps PC, and an instruction pointer reference that
// feeds VSCode's disassembly view when it doesn't.
func (s *server) onStackTrace(req *dap.StackTraceRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	var frame dap.StackFrame
	eng.do(func() {
		pc := eng.pc()
		frame = dap.StackFrame{
			Id:                          1,
			Name:                        eng.frameName(pc),
			InstructionPointerReference: fmt.Sprintf("0x%04X", pc),
		}
		if eng.sld != nil {
			if file, line, ok := eng.sld.LineFor(pc); ok {
				frame.Source = &dap.Source{Name: sourceName(file), Path: file}
				frame.Line = line
				frame.Column = 1
			}
		}
	})
	s.respond(&req.Request, &dap.StackTraceResponse{
		Body: dap.StackTraceResponseBody{
			StackFrames: []dap.StackFrame{frame},
			TotalFrames: 1,
		},
	})
}

func sourceName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// frameName names the current location (engine goroutine only).
func (e *engine) frameName(pc uint16) string {
	if e.sld != nil {
		if name, off, ok := e.sld.NearestLabel(pc); ok {
			if off == 0 {
				return name
			}
			return fmt.Sprintf("%s+0x%X", name, off)
		}
	}
	return disasm.Decode(e.memAt, pc).Text
}

func (s *server) onScopes(req *dap.ScopesRequest) {
	if s.requireEngine(&req.Request) == nil {
		return
	}
	s.respond(&req.Request, &dap.ScopesResponse{
		Body: dap.ScopesResponseBody{Scopes: []dap.Scope{
			{Name: "Registers", VariablesReference: refRegisters, PresentationHint: "registers"},
			{Name: "Shadow & system", VariablesReference: refShadow},
		}},
	})
}

// flagString renders F as the classic SZ5H3PNC letters (uppercase =
// set), matching the monitor panel's convention.
func flagString(f byte) string {
	names := "SZ5H3PNC"
	out := make([]byte, 8)
	for i := 0; i < 8; i++ {
		if f&(0x80>>i) != 0 {
			out[i] = names[i]
		} else {
			out[i] = '-'
		}
	}
	return string(out)
}

func (s *server) onVariables(req *dap.VariablesRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	var vars []dap.Variable
	eng.do(func() {
		st := eng.m.CPU().State()
		hex16 := func(name string, v uint16, memRef bool) dap.Variable {
			out := dap.Variable{Name: name, Value: fmt.Sprintf("0x%04X", v), Type: "word"}
			if memRef {
				out.MemoryReference = fmt.Sprintf("0x%04X", v)
			}
			return out
		}
		switch req.Arguments.VariablesReference {
		case refRegisters:
			vars = []dap.Variable{
				hex16("PC", st.PC, true),
				hex16("SP", st.SP, true),
				{Name: "A", Value: fmt.Sprintf("0x%02X", byte(st.AF>>8)), Type: "byte"},
				{Name: "F", Value: fmt.Sprintf("%s (0x%02X)", flagString(byte(st.AF)), byte(st.AF)), Type: "flags"},
				hex16("AF", st.AF, false),
				hex16("BC", st.BC, true),
				hex16("DE", st.DE, true),
				hex16("HL", st.HL, true),
				hex16("IX", st.IX, true),
				hex16("IY", st.IY, true),
			}
		case refShadow:
			vars = []dap.Variable{
				hex16("AF'", st.AF2, false),
				hex16("BC'", st.BC2, false),
				hex16("DE'", st.DE2, false),
				hex16("HL'", st.HL2, false),
				{Name: "I", Value: fmt.Sprintf("0x%02X", st.I), Type: "byte"},
				{Name: "R", Value: fmt.Sprintf("0x%02X", st.R), Type: "byte"},
				{Name: "IM", Value: fmt.Sprintf("%d", st.IM), Type: "byte"},
				{Name: "IFF1", Value: fmt.Sprintf("%v", st.IFF1), Type: "bool"},
				{Name: "IFF2", Value: fmt.Sprintf("%v", st.IFF2), Type: "bool"},
				{Name: "Halted", Value: fmt.Sprintf("%v", st.Halted), Type: "bool"},
				{Name: "T-states", Value: fmt.Sprintf("%d", eng.m.Tstates()), Type: "counter"},
			}
		}
	})
	if vars == nil {
		s.fail(&req.Request, "unknown variables reference %d", req.Arguments.VariablesReference)
		return
	}
	s.respond(&req.Request, &dap.VariablesResponse{
		Body: dap.VariablesResponseBody{Variables: vars},
	})
}

// onSetVariable writes a register. SetState bypasses the journal, so
// the history baseline is rebased afterwards (the monitor's `set`
// discipline).
func (s *server) onSetVariable(req *dap.SetVariableRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	val, err := strconv.ParseUint(strings.TrimSpace(req.Arguments.Value), 0, 16)
	if err != nil {
		s.fail(&req.Request, "value %q: want a 16-bit number (0x… or decimal)", req.Arguments.Value)
		return
	}
	v := uint16(val)
	var newVal string
	var setErr error
	eng.do(func() {
		st := eng.m.CPU().State()
		set16 := func(p *uint16) { *p = v }
		setHi := func(p *uint16) { *p = *p&0x00FF | v<<8 }
		setLo := func(p *uint16) { *p = *p&0xFF00 | v&0xFF }
		switch req.Arguments.Name {
		case "PC":
			set16(&st.PC)
		case "SP":
			set16(&st.SP)
		case "AF":
			set16(&st.AF)
		case "BC":
			set16(&st.BC)
		case "DE":
			set16(&st.DE)
		case "HL":
			set16(&st.HL)
		case "IX":
			set16(&st.IX)
		case "IY":
			set16(&st.IY)
		case "A":
			setHi(&st.AF)
		case "F":
			setLo(&st.AF)
		case "AF'":
			set16(&st.AF2)
		case "BC'":
			set16(&st.BC2)
		case "DE'":
			set16(&st.DE2)
		case "HL'":
			set16(&st.HL2)
		case "I":
			st.I = byte(v)
		case "R":
			st.R = byte(v)
		default:
			setErr = fmt.Errorf("register %q is not writable", req.Arguments.Name)
			return
		}
		eng.m.CPU().SetState(st)
		eng.m.HistoryRebase()

		st = eng.m.CPU().State()
		switch req.Arguments.Name {
		case "A":
			newVal = fmt.Sprintf("0x%02X", byte(st.AF>>8))
		case "F":
			newVal = fmt.Sprintf("%s (0x%02X)", flagString(byte(st.AF)), byte(st.AF))
		case "I":
			newVal = fmt.Sprintf("0x%02X", st.I)
		case "R":
			newVal = fmt.Sprintf("0x%02X", st.R)
		default:
			newVal = fmt.Sprintf("0x%04X", v)
		}
	})
	if setErr != nil {
		s.fail(&req.Request, "%v", setErr)
		return
	}
	s.respond(&req.Request, &dap.SetVariableResponse{
		Body: dap.SetVariableResponseBody{Value: newVal},
	})
}

// parseMemRef turns a DAP memory reference plus byte offset into a
// clamped [start,end) address window.
func parseMemRef(ref string, offset, count int) (start uint32, end uint32, err error) {
	base, err := strconv.ParseUint(strings.TrimSpace(ref), 0, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad memory reference %q", ref)
	}
	from := int64(base) + int64(offset)
	if from < 0 {
		from = 0
	}
	if from > 0x10000 {
		from = 0x10000
	}
	to := from + int64(count)
	if to > 0x10000 {
		to = 0x10000
	}
	return uint32(from), uint32(to), nil
}

func (s *server) onReadMemory(req *dap.ReadMemoryRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	start, end, err := parseMemRef(req.Arguments.MemoryReference, req.Arguments.Offset, req.Arguments.Count)
	if err != nil {
		s.fail(&req.Request, "%v", err)
		return
	}
	var data []byte
	eng.do(func() {
		if end > start {
			data = eng.m.DumpMemory(start, end)
		}
	})
	s.respond(&req.Request, &dap.ReadMemoryResponse{
		Body: dap.ReadMemoryResponseBody{
			Address: fmt.Sprintf("0x%04X", start),
			Data:    base64.StdEncoding.EncodeToString(data),
		},
	})
}

func (s *server) onWriteMemory(req *dap.WriteMemoryRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.Arguments.Data)
	if err != nil {
		s.fail(&req.Request, "bad base64 data: %v", err)
		return
	}
	start, _, err := parseMemRef(req.Arguments.MemoryReference, req.Arguments.Offset, len(data))
	if err != nil {
		s.fail(&req.Request, "%v", err)
		return
	}
	var writeErr error
	eng.do(func() {
		writeErr = eng.m.LoadBinary(uint16(start), data)
	})
	if writeErr != nil {
		s.fail(&req.Request, "write memory: %v", writeErr)
		return
	}
	s.respond(&req.Request, &dap.WriteMemoryResponse{
		Body: dap.WriteMemoryResponseBody{BytesWritten: len(data)},
	})
}

// onDisassemble decodes a window of instructions around a reference.
// Z80 instructions have no alignment, so a negative instructionOffset
// starts a back-scan a few bytes early and lets the decoder re-sync —
// the standard variable-length-ISA trick.
func (s *server) onDisassemble(req *dap.DisassembleRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	base, _, err := parseMemRef(req.Arguments.MemoryReference, req.Arguments.Offset, 0)
	if err != nil {
		s.fail(&req.Request, "%v", err)
		return
	}
	instrOff := req.Arguments.InstructionOffset
	count := req.Arguments.InstructionCount
	if count <= 0 {
		count = 1
	}
	var out []dap.DisassembledInstruction
	eng.do(func() {
		start := base
		if instrOff < 0 {
			back := uint32(4*(-instrOff) + 8)
			if back > start {
				start = 0
			} else {
				start -= back
			}
		}
		// Decode forward from start; collect the window beginning at
		// the instruction that covers/follows base, honoring the
		// requested instruction offset.
		var all []dap.DisassembledInstruction
		baseIdx := -1
		for addr := start; addr <= 0xFFFF; {
			in := disasm.Decode(eng.memAt, uint16(addr))
			if baseIdx < 0 && uint32(in.Addr)+uint32(in.Len) > base {
				baseIdx = len(all)
			}
			di := dap.DisassembledInstruction{
				Address:          fmt.Sprintf("0x%04X", in.Addr),
				InstructionBytes: fmt.Sprintf("% X", in.Bytes[:in.Len]),
				Instruction:      in.Text,
			}
			if eng.sld != nil {
				if file, line, ok := eng.sld.LineFor(in.Addr); ok {
					di.Location = &dap.Source{Name: sourceName(file), Path: file}
					di.Line = line
				}
			}
			all = append(all, di)
			if baseIdx >= 0 && len(all) >= baseIdx+instrOff+count {
				break
			}
			next := uint32(in.Addr) + uint32(in.Len)
			if next > 0xFFFF {
				break
			}
			addr = next
		}
		first := baseIdx + instrOff
		for i := first; i < first+count; i++ {
			if i < 0 || i >= len(all) {
				out = append(out, dap.DisassembledInstruction{
					Address:     fmt.Sprintf("0x%04X", 0),
					Instruction: "??",
				})
				continue
			}
			out = append(out, all[i])
		}
	})
	s.respond(&req.Request, &dap.DisassembleResponse{
		Body: dap.DisassembleResponseBody{Instructions: out},
	})
}

// onEvaluate bridges the debug console to the shared monitor REPL —
// the full command set (x, d, poke, w, b, s, bs, rw, set, help…). If
// a command moved the machine (step, rewind, reset), a synthetic
// stopped event resyncs the editor.
func (s *server) onEvaluate(req *dap.EvaluateRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	if req.Arguments.Context != "repl" && req.Arguments.Context != "" {
		s.fail(&req.Request, "only debug-console evaluation is supported (try the monitor REPL: help)")
		return
	}
	line := strings.TrimSpace(req.Arguments.Expression)
	var result string
	eng.do(func() {
		if line == "c" {
			eng.resume()
			result = "running"
			return
		}
		beforeT, beforePC := eng.m.Tstates(), eng.pc()
		eng.mon.Paused = !eng.running
		lines := eng.mon.Exec(line)
		result = strings.Join(lines, "\n")
		if eng.running && eng.mon.Paused {
			// The REPL asked to pause (p).
			eng.running = false
		}
		if eng.m.Tstates() != beforeT || eng.pc() != beforePC {
			eng.running = false
			eng.recordStop()
			eng.onStopped("pause", "moved by console command", nil)
		}
	})
	s.respond(&req.Request, &dap.EvaluateResponse{
		Body: dap.EvaluateResponseBody{Result: result},
	})
}

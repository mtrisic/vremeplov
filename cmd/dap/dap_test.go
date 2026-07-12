package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-dap"
)

func base64decode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

func TestInitializeCapabilities(t *testing.T) {
	c := startClient(t)
	c.send(&dap.InitializeRequest{Request: newRequest("initialize")})
	resp := c.resp("initialize").(*dap.InitializeResponse)
	caps := resp.Body
	for name, got := range map[string]bool{
		"configurationDone":      caps.SupportsConfigurationDoneRequest,
		"instructionBreakpoints": caps.SupportsInstructionBreakpoints,
		"disassemble":            caps.SupportsDisassembleRequest,
		"readMemory":             caps.SupportsReadMemoryRequest,
		"writeMemory":            caps.SupportsWriteMemoryRequest,
		"stepBack":               caps.SupportsStepBack,
		"setVariable":            caps.SupportsSetVariable,
		"restart":                caps.SupportsRestartRequest,
		"steppingGranularity":    caps.SupportsSteppingGranularity,
	} {
		if !got {
			t.Errorf("capability %s not advertised", name)
		}
	}
	if caps.SupportsDataBreakpoints {
		t.Error("data breakpoints advertised but deliberately unsupported in v1")
	}
}

func TestLaunchValidation(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing program", map[string]any{}},
		{"bin without org", map[string]any{"program": "testdata/hello.bin"}},
		{"bad rom", map[string]any{"program": "testdata/hello.bin", "org": "0x8000", "rom": "v99"}},
		{"org in ROM range", map[string]any{"program": "testdata/hello.bin", "org": "0x0100"}},
		{"entry label without sld", map[string]any{"program": "testdata/hello.bin", "org": "0x8000", "entry": "start"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := startClient(t)
			c.initialize()
			raw, _ := json.Marshal(tc.args)
			c.send(&dap.LaunchRequest{Request: newRequest("launch"), Arguments: raw})
			c.respErr("launch")
		})
	}
}

func TestLaunchStopOnEntry(t *testing.T) {
	c := launchHello(t)
	f := c.frame()
	if f.Name != "start" {
		t.Errorf("frame name = %q, want start", f.Name)
	}
	if f.InstructionPointerReference != "0x8000" {
		t.Errorf("PC ref = %q, want 0x8000", f.InstructionPointerReference)
	}
	if f.Source == nil || f.Source.Name != "hello.asm" || f.Line != 5 {
		t.Errorf("source = %+v line %d, want hello.asm:5", f.Source, f.Line)
	}
}

func TestLaunchEntryLabel(t *testing.T) {
	c := startClient(t)
	c.initialize()
	c.launch(map[string]any{
		"program": "testdata/hello.bin",
		"org":     "0x8000",
		"entry":   "fill",
		"sld":     "testdata/hello.sld",
	})
	c.configurationDone()
	c.stopped()
	if pc := c.pc(); pc != 0x8008 {
		t.Fatalf("entry-by-label PC = 0x%04X, want 0x8008", pc)
	}
}

func TestContinuePause(t *testing.T) {
	c := launchHello(t)
	c.send(&dap.ContinueRequest{Request: newRequest("continue"),
		Arguments: dap.ContinueArguments{ThreadId: threadID}})
	c.resp("continue")
	c.event("continued")
	// The run loop paces real time (20 ms frame slices); give it a few
	// slices before pausing so the program demonstrably ran.
	time.Sleep(100 * time.Millisecond)

	c.send(&dap.PauseRequest{Request: newRequest("pause"),
		Arguments: dap.PauseArguments{ThreadId: threadID}})
	c.resp("pause")
	if body := c.stopped(); body.Reason != "pause" {
		t.Fatalf("stop reason = %q, want pause", body.Reason)
	}
	// The busy loop lives at after (0x8003) / JR (0x8004).
	if pc := c.pc(); pc != 0x8003 && pc != 0x8004 {
		t.Fatalf("paused PC = 0x%04X, want inside the busy loop", pc)
	}
}

func setSourceBPs(c *client, path string, lines ...int) []dap.Breakpoint {
	c.t.Helper()
	bps := make([]dap.SourceBreakpoint, len(lines))
	for i, l := range lines {
		bps[i] = dap.SourceBreakpoint{Line: l}
	}
	c.send(&dap.SetBreakpointsRequest{
		Request: newRequest("setBreakpoints"),
		Arguments: dap.SetBreakpointsArguments{
			Source:      dap.Source{Path: path},
			Breakpoints: bps,
		},
	})
	resp := c.resp("setBreakpoints").(*dap.SetBreakpointsResponse)
	return resp.Body.Breakpoints
}

func TestSourceBreakpoint(t *testing.T) {
	c := launchHello(t)
	got := setSourceBPs(c, "testdata/hello.asm", 11, 4)
	if len(got) != 2 {
		t.Fatalf("breakpoints = %d, want 2", len(got))
	}
	if !got[0].Verified || got[0].Message != "0x800A" {
		t.Fatalf("line 11 bp = %+v, want verified at 0x800A", got[0])
	}
	if got[1].Verified {
		t.Fatalf("line 4 bp unexpectedly verified (no code there)")
	}

	c.send(&dap.ContinueRequest{Request: newRequest("continue"),
		Arguments: dap.ContinueArguments{ThreadId: threadID}})
	c.resp("continue")
	c.event("continued")

	body := c.stopped()
	if body.Reason != "breakpoint" {
		t.Fatalf("stop reason = %q, want breakpoint", body.Reason)
	}
	if len(body.HitBreakpointIds) != 1 || body.HitBreakpointIds[0] != got[0].Id {
		t.Fatalf("hit ids = %v, want [%d]", body.HitBreakpointIds, got[0].Id)
	}
	f := c.frame()
	if f.Line != 11 || f.Name != "fill+0x2" {
		t.Fatalf("frame = %q line %d, want fill+0x2 line 11", f.Name, f.Line)
	}
}

func TestInstructionBreakpoint(t *testing.T) {
	c := launchHello(t)
	c.send(&dap.SetInstructionBreakpointsRequest{
		Request: newRequest("setInstructionBreakpoints"),
		Arguments: dap.SetInstructionBreakpointsArguments{
			Breakpoints: []dap.InstructionBreakpoint{{InstructionReference: "0x800D"}},
		},
	})
	resp := c.resp("setInstructionBreakpoints").(*dap.SetInstructionBreakpointsResponse)
	if len(resp.Body.Breakpoints) != 1 || !resp.Body.Breakpoints[0].Verified {
		t.Fatalf("instruction bp = %+v", resp.Body.Breakpoints)
	}
	c.send(&dap.ContinueRequest{Request: newRequest("continue"),
		Arguments: dap.ContinueArguments{ThreadId: threadID}})
	c.resp("continue")
	c.event("continued")
	c.stopped()
	if pc := c.pc(); pc != 0x800D {
		t.Fatalf("PC = 0x%04X, want 0x800D", pc)
	}
}

func TestStepping(t *testing.T) {
	c := launchHello(t) // stopped at 0x8000: CALL fill

	// next over the CALL lands on the instruction after it.
	c.send(&dap.NextRequest{Request: newRequest("next"),
		Arguments: dap.NextArguments{ThreadId: threadID, Granularity: "instruction"}})
	c.resp("next")
	c.event("continued") // async run-to
	c.stopped()
	if pc := c.pc(); pc != 0x8003 {
		t.Fatalf("after next over CALL: PC = 0x%04X, want 0x8003", pc)
	}

	// next over INC A is a plain step.
	c.send(&dap.NextRequest{Request: newRequest("next"),
		Arguments: dap.NextArguments{ThreadId: threadID, Granularity: "instruction"}})
	c.resp("next")
	c.stopped()
	if pc := c.pc(); pc != 0x8004 {
		t.Fatalf("after next over INC A: PC = 0x%04X, want 0x8004", pc)
	}

	// next over the taken JR must STEP (not run away): JR after → 0x8003.
	c.send(&dap.NextRequest{Request: newRequest("next"),
		Arguments: dap.NextArguments{ThreadId: threadID, Granularity: "instruction"}})
	c.resp("next")
	c.stopped()
	if pc := c.pc(); pc != 0x8003 {
		t.Fatalf("after next over JR: PC = 0x%04X, want 0x8003 (taken jump)", pc)
	}
}

func TestStepInStepOut(t *testing.T) {
	c := launchHello(t)
	c.send(&dap.StepInRequest{Request: newRequest("stepIn"),
		Arguments: dap.StepInArguments{ThreadId: threadID, Granularity: "instruction"}})
	c.resp("stepIn")
	c.stopped()
	if pc := c.pc(); pc != 0x8008 {
		t.Fatalf("stepIn: PC = 0x%04X, want 0x8008 (inside fill)", pc)
	}
	c.send(&dap.StepOutRequest{Request: newRequest("stepOut"),
		Arguments: dap.StepOutArguments{ThreadId: threadID}})
	c.resp("stepOut")
	c.event("continued")
	c.stopped()
	if pc := c.pc(); pc != 0x8003 {
		t.Fatalf("stepOut: PC = 0x%04X, want 0x8003", pc)
	}
}

func TestLineStepping(t *testing.T) {
	c := launchHello(t) // at line 5 (CALL fill)
	// Line-granularity next: the whole CALL line completes as one step
	// → line 6.
	c.send(&dap.NextRequest{Request: newRequest("next"),
		Arguments: dap.NextArguments{ThreadId: threadID}})
	c.resp("next")
	c.stopped()
	if f := c.frame(); f.Line != 6 {
		t.Fatalf("line next: line = %d, want 6", f.Line)
	}
}

func TestVariablesAndSetVariable(t *testing.T) {
	c := launchHello(t)

	c.send(&dap.ScopesRequest{Request: newRequest("scopes"),
		Arguments: dap.ScopesArguments{FrameId: 1}})
	scopes := c.resp("scopes").(*dap.ScopesResponse).Body.Scopes
	if len(scopes) != 2 || scopes[0].Name != "Registers" {
		t.Fatalf("scopes = %+v", scopes)
	}

	vars := c.variables(refRegisters)
	if vars["PC"] != "0x8000" {
		t.Fatalf("PC var = %q, want 0x8000", vars["PC"])
	}
	if _, ok := vars["F"]; !ok {
		t.Fatal("no F flags variable")
	}

	// Move the PC by hand — the machine must honor it on the next step.
	c.send(&dap.SetVariableRequest{Request: newRequest("setVariable"),
		Arguments: dap.SetVariableArguments{
			VariablesReference: refRegisters, Name: "PC", Value: "0x8008",
		}})
	if got := c.resp("setVariable").(*dap.SetVariableResponse).Body.Value; got != "0x8008" {
		t.Fatalf("setVariable value = %q", got)
	}
	if pc := c.pc(); pc != 0x8008 {
		t.Fatalf("PC after setVariable = 0x%04X", pc)
	}
}

func (c *client) variables(ref int) map[string]string {
	c.t.Helper()
	c.send(&dap.VariablesRequest{Request: newRequest("variables"),
		Arguments: dap.VariablesArguments{VariablesReference: ref}})
	resp := c.resp("variables").(*dap.VariablesResponse)
	out := map[string]string{}
	for _, v := range resp.Body.Variables {
		out[v.Name] = v.Value
	}
	return out
}

func TestMemoryReadWrite(t *testing.T) {
	c := launchHello(t)
	// Before the program runs, 0x9000 is empty.
	if got := c.readMem(0x9000, 1); got[0] != 0x00 {
		t.Fatalf("mem[0x9000] = %02X before run", got[0])
	}
	// Write through DAP, read back.
	c.send(&dap.WriteMemoryRequest{Request: newRequest("writeMemory"),
		Arguments: dap.WriteMemoryArguments{
			MemoryReference: "0x9000",
			Data:            base64.StdEncoding.EncodeToString([]byte{0xAB}),
		}})
	c.resp("writeMemory")
	if got := c.readMem(0x9000, 1); got[0] != 0xAB {
		t.Fatalf("mem[0x9000] = %02X after write, want AB", got[0])
	}
	// Writing into ROM must fail.
	c.send(&dap.WriteMemoryRequest{Request: newRequest("writeMemory"),
		Arguments: dap.WriteMemoryArguments{
			MemoryReference: "0x0100",
			Data:            base64.StdEncoding.EncodeToString([]byte{1}),
		}})
	c.respErr("writeMemory")
}

func TestDisassemble(t *testing.T) {
	c := launchHello(t)
	c.send(&dap.DisassembleRequest{Request: newRequest("disassemble"),
		Arguments: dap.DisassembleArguments{
			MemoryReference:  "0x8000",
			InstructionCount: 3,
		}})
	resp := c.resp("disassemble").(*dap.DisassembleResponse)
	ins := resp.Body.Instructions
	if len(ins) != 3 {
		t.Fatalf("instructions = %d, want 3", len(ins))
	}
	if !strings.HasPrefix(ins[0].Instruction, "CALL") || ins[0].Address != "0x8000" {
		t.Fatalf("ins[0] = %+v, want CALL at 0x8000", ins[0])
	}
	if !strings.HasPrefix(ins[1].Instruction, "INC") {
		t.Fatalf("ins[1] = %+v, want INC A", ins[1])
	}
	if ins[0].Location == nil || ins[0].Line != 5 {
		t.Fatalf("ins[0] source = %+v line %d, want hello.asm:5", ins[0].Location, ins[0].Line)
	}
}

func TestStepBackRestoresState(t *testing.T) {
	c := launchHello(t)
	// Run to line 12 (RET) — the store to 0x9000 has happened.
	bps := setSourceBPs(c, "testdata/hello.asm", 12)
	if !bps[0].Verified {
		t.Fatal("line 12 bp unverified")
	}
	c.send(&dap.ContinueRequest{Request: newRequest("continue"),
		Arguments: dap.ContinueArguments{ThreadId: threadID}})
	c.resp("continue")
	c.event("continued")
	c.stopped()
	if got := c.readMem(0x9000, 1); got[0] != 0x2A {
		t.Fatalf("mem[0x9000] = %02X at RET, want 2A", got[0])
	}
	// One instruction back: the store is un-done.
	c.send(&dap.StepBackRequest{Request: newRequest("stepBack"),
		Arguments: dap.StepBackArguments{ThreadId: threadID, Granularity: "instruction"}})
	c.resp("stepBack")
	c.stopped()
	if pc := c.pc(); pc != 0x800A {
		t.Fatalf("stepBack PC = 0x%04X, want 0x800A", pc)
	}
	if got := c.readMem(0x9000, 1); got[0] != 0x00 {
		t.Fatalf("mem[0x9000] = %02X after stepBack, want 00 (write undone)", got[0])
	}
}

func TestReverseContinue(t *testing.T) {
	c := launchHello(t)
	entryPC := c.pc()
	// Step a couple of times to create later stops.
	for i := 0; i < 2; i++ {
		c.send(&dap.StepInRequest{Request: newRequest("stepIn"),
			Arguments: dap.StepInArguments{ThreadId: threadID, Granularity: "instruction"}})
		c.resp("stepIn")
		c.stopped()
	}
	c.send(&dap.ReverseContinueRequest{Request: newRequest("reverseContinue"),
		Arguments: dap.ReverseContinueArguments{ThreadId: threadID}})
	c.resp("reverseContinue")
	c.stopped()
	// Bounces back to the previous stop (the first stepIn's landing).
	if pc := c.pc(); pc != 0x8008 {
		t.Fatalf("reverseContinue PC = 0x%04X, want 0x8008", pc)
	}
	_ = entryPC
}

func TestStepBackWithoutHistory(t *testing.T) {
	c := startClient(t)
	c.initialize()
	c.launch(map[string]any{
		"program": "testdata/hello.bin",
		"org":     "0x8000",
		"history": 0,
	})
	c.configurationDone()
	c.stopped()
	c.send(&dap.StepBackRequest{Request: newRequest("stepBack"),
		Arguments: dap.StepBackArguments{ThreadId: threadID}})
	resp := c.respErr("stepBack")
	if !strings.Contains(resp.GetResponse().Message, "history") {
		t.Fatalf("error = %q, want history mention", resp.GetResponse().Message)
	}
}

func TestEvaluateREPL(t *testing.T) {
	c := launchHello(t)
	c.send(&dap.EvaluateRequest{Request: newRequest("evaluate"),
		Arguments: dap.EvaluateArguments{Expression: "x 8000 3", Context: "repl"}})
	resp := c.resp("evaluate").(*dap.EvaluateResponse)
	if !strings.Contains(resp.Body.Result, "CD 08 80") {
		t.Fatalf("evaluate x = %q, want the CALL bytes", resp.Body.Result)
	}
	// A console step moves the machine → synthetic stopped event.
	c.send(&dap.EvaluateRequest{Request: newRequest("evaluate"),
		Arguments: dap.EvaluateArguments{Expression: "s", Context: "repl"}})
	c.resp("evaluate")
	if body := c.stopped(); body.Description != "moved by console command" {
		t.Fatalf("console step stop = %+v", body)
	}
}

func TestRestartKeepsBreakpoints(t *testing.T) {
	c := launchHello(t)
	setSourceBPs(c, "testdata/hello.asm", 11)
	c.send(&dap.RestartRequest{Request: newRequest("restart")})
	c.resp("restart")
	if body := c.stopped(); body.Reason != "entry" {
		t.Fatalf("restart stop = %q, want entry", body.Reason)
	}
	c.send(&dap.ContinueRequest{Request: newRequest("continue"),
		Arguments: dap.ContinueArguments{ThreadId: threadID}})
	c.resp("continue")
	c.event("continued")
	if body := c.stopped(); body.Reason != "breakpoint" {
		t.Fatalf("post-restart stop = %q, want breakpoint (bp survived)", body.Reason)
	}
}

func TestGTPLaunch(t *testing.T) {
	c := startClient(t)
	c.initialize()
	c.launch(map[string]any{
		"program": "../../core/gtp/testdata/hackaday.gtp",
		"history": 0,
	})
	c.configurationDone()
	// No stopOnEntry: the machine runs (RUN types and executes). Pause
	// and confirm the program took over.
	c.send(&dap.PauseRequest{Request: newRequest("pause"),
		Arguments: dap.PauseArguments{ThreadId: threadID}})
	c.resp("pause")
	c.stopped()
	c.send(&dap.EvaluateRequest{Request: newRequest("evaluate"),
		Arguments: dap.EvaluateArguments{Expression: "x 2c3a 2", Context: "repl"}})
	resp := c.resp("evaluate").(*dap.EvaluateResponse)
	if resp.Body.Result == "" {
		t.Fatal("evaluate returned nothing")
	}
}

func TestScreenView(t *testing.T) {
	c := startClient(t)
	c.initialize()
	c.launch(map[string]any{
		"program": "testdata/hello.bin",
		"org":     "0x8000",
		"history": 0,
		"screen":  "127.0.0.1:0",
	})
	// The screen URL is announced as an output event.
	var url string
	for i := 0; i < 10; i++ {
		ev := c.event("output").(*dap.OutputEvent)
		if strings.Contains(ev.Body.Output, "screen view:") {
			url = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ev.Body.Output), "screen view:"))
			break
		}
	}
	if url == "" {
		t.Fatal("no screen view URL announced")
	}
	c.configurationDone()
	c.stopped()

	resp, err := http.Get(url + "screen.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 16 {
		t.Fatalf("screen.txt rows = %d, want 16", len(lines))
	}
	png, err := http.Get(url + "screen.png")
	if err != nil {
		t.Fatal(err)
	}
	defer png.Body.Close()
	sig := make([]byte, 4)
	io.ReadFull(png.Body, sig)
	if fmt.Sprintf("%X", sig) != "89504E47" {
		t.Fatalf("screen.png signature = %X", sig)
	}
}

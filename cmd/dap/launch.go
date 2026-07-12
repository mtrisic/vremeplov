package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/go-dap"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/loader"
	"github.com/mtrisic/vremeplov/roms"
)

// launchArgs is the launch request's arguments — the adapter's
// configuration schema (mirrored by editors/vscode/package.json and
// the README's Helix template).
type launchArgs struct {
	// Program is a .gtp/.wav tape image, or a raw .bin (anything else).
	Program string `json:"program"`
	// Org is the load address for a raw .bin (required for .bin).
	Org hexArg `json:"org"`
	// Entry is where execution starts: for .bin the initial PC
	// (default org); for tapes an entry breakpoint address. With an
	// SLD file it may be a label name.
	Entry entryArg `json:"entry"`
	// SLD is a sjasmplus --sld source map enabling source-level
	// debugging; SourceRoot resolves its relative paths (default: the
	// SLD file's directory).
	SLD        string `json:"sld"`
	SourceRoot string `json:"sourceRoot"`
	// RAM: 2|4|6|expanded. Default: expanded for .bin, 6 for tapes.
	RAM string `json:"ram"`
	// ROM: v28 (default) or v29 (pulls the embedded ROM B).
	ROM string `json:"rom"`
	// StopOnEntry: default true for .bin, false for tapes.
	StopOnEntry *bool `json:"stopOnEntry"`
	// History is the rewind window in seconds for reverse debugging
	// (default 30; 0 disables stepBack/reverseContinue).
	History *int `json:"history"`
	// Screen serves the live screen view on this HTTP address.
	Screen string `json:"screen"`
}

// hexArg accepts a JSON number or a string like "0x8000" / "32768".
type hexArg struct {
	set bool
	val uint16
}

func (h *hexArg) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseUint(s, 0, 16)
	if err != nil {
		return fmt.Errorf("not a 16-bit address: %s", s)
	}
	h.set, h.val = true, uint16(v)
	return nil
}

// entryArg is a hex address or (with SLD) a label name.
type entryArg struct {
	raw string
}

func (a *entryArg) UnmarshalJSON(b []byte) error {
	a.raw = strings.Trim(string(b), `"`)
	if a.raw == "null" {
		a.raw = ""
	}
	return nil
}

// resolve turns the entry into an address, consulting SLD labels.
func (a entryArg) resolve(sld *SLD) (uint16, bool, error) {
	if a.raw == "" {
		return 0, false, nil
	}
	if v, err := strconv.ParseUint(a.raw, 0, 16); err == nil {
		return uint16(v), true, nil
	}
	if sld != nil {
		if addr, ok := sld.Label(a.raw); ok {
			return addr, true, nil
		}
		return 0, false, fmt.Errorf("entry %q: no such label in the SLD file", a.raw)
	}
	return 0, false, fmt.Errorf("entry %q is not an address and no \"sld\" file is configured for label lookup", a.raw)
}

// isTape reports whether the program loads through the tape path.
func (a launchArgs) isTape() bool {
	ext := strings.ToLower(filepath.Ext(a.Program))
	return ext == ".gtp" || ext == ".wav"
}

func (a launchArgs) stopOnEntry() bool {
	if a.StopOnEntry != nil {
		return *a.StopOnEntry
	}
	return !a.isTape() // .bin stops at entry by default
}

func (a launchArgs) historySeconds() int {
	if a.History != nil {
		return *a.History
	}
	return 30
}

// buildMachine constructs the Galaksija per the launch args (the
// headless buildConfig shape, defaults tuned for debugging).
func buildMachine(a launchArgs) (*core.Machine, error) {
	cfg := core.Config{Chargen: roms.ChargenElektronika()}
	switch a.ROM {
	case "", "v28":
		cfg.ROMA = roms.ROMA()
	case "v29":
		cfg.ROMA = roms.ROMAWithROMBInit()
		cfg.ROMB = roms.ROMB()
	default:
		return nil, fmt.Errorf(`invalid "rom" %q (want v28 or v29)`, a.ROM)
	}
	ram := a.RAM
	if ram == "" {
		if a.isTape() {
			ram = "6"
		} else {
			ram = "expanded" // raw binaries usually live above 0x4000
		}
	}
	switch ram {
	case "2":
		cfg.RAM = core.RAM2K
	case "4":
		cfg.RAM = core.RAM4K
	case "6":
		cfg.RAM = core.RAM6K
	case "expanded":
		cfg.RAM = core.RAMExpanded
	default:
		return nil, fmt.Errorf(`invalid "ram" %q (want 2, 4, 6, or expanded)`, ram)
	}
	return core.New(cfg)
}

// onLaunch builds the machine, loads the program, and leaves the
// engine paused; configurationDone starts execution (or reports the
// entry stop).
func (s *server) onLaunch(req *dap.LaunchRequest) {
	if s.eng != nil {
		s.fail(&req.Request, "already launched")
		return
	}
	var args launchArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		s.fail(&req.Request, "launch arguments: %v", err)
		return
	}
	eng, err := s.buildEngine(args)
	if err != nil {
		s.fail(&req.Request, "%v", err)
		return
	}
	s.eng = eng
	s.respond(&req.Request, &dap.LaunchResponse{})
	// The breakpoint-configuration window opens now.
	ev := &dap.InitializedEvent{}
	ev.Event.Event = "initialized"
	s.send(ev)
	s.announceScreen(eng)
}

// buildEngine performs the whole launch: machine, program, source
// map, history, screen view. Used by launch and restart.
func (s *server) buildEngine(args launchArgs) (*engine, error) {
	if args.Program == "" {
		return nil, fmt.Errorf(`launch needs a "program" (.gtp, .wav, or a raw .bin)`)
	}
	data, err := os.ReadFile(args.Program)
	if err != nil {
		return nil, err
	}
	m, err := buildMachine(args)
	if err != nil {
		return nil, err
	}
	eng := newEngine(m, args)
	eng.onStopped = func(reason, desc string, hits []int) {
		ev := &dap.StoppedEvent{Body: dap.StoppedEventBody{
			Reason:            reason,
			Description:       desc,
			ThreadId:          threadID,
			AllThreadsStopped: true,
			HitBreakpointIds:  hits,
		}}
		ev.Event.Event = "stopped"
		s.send(ev)
	}
	eng.onContinued = func() {
		ev := &dap.ContinuedEvent{Body: dap.ContinuedEventBody{
			ThreadId:            threadID,
			AllThreadsContinued: true,
		}}
		ev.Event.Event = "continued"
		s.send(ev)
	}

	if args.SLD != "" {
		sld, err := LoadSLD(args.SLD, args.SourceRoot)
		if err != nil {
			return nil, fmt.Errorf("sld: %v", err)
		}
		eng.sld = sld
	}

	if args.isTape() {
		err = eng.loadTape(data)
	} else {
		err = eng.loadBin(data)
	}
	if err != nil {
		return nil, err
	}

	if secs := args.historySeconds(); secs > 0 {
		m.EnableHistory(50*core.TstatesPerFrame, secs)
	}

	screenAddr := args.Screen
	if screenAddr == "" {
		screenAddr = s.screenAddr
	}
	if screenAddr != "" {
		scr, err := startScreen(eng, screenAddr)
		if err != nil {
			return nil, fmt.Errorf("screen view: %v", err)
		}
		eng.screen = scr
	}

	eng.start()
	return eng, nil
}

// announceScreen tells the user where the live screen lives — after
// the initialized/restart response so no client drops it.
func (s *server) announceScreen(eng *engine) {
	if eng.screen != nil {
		s.output("screen view: http://%s/", eng.screen.addr())
	}
}

// loadBin boots to READY, loads the raw binary at org, and points PC
// at the entry — the "assemble and run" flow.
func (e *engine) loadBin(data []byte) error {
	if !e.args.Org.set {
		return fmt.Errorf(`a raw .bin needs an "org" load address`)
	}
	entry, ok, err := e.args.Entry.resolve(e.sld)
	if err != nil {
		return err
	}
	if !ok {
		entry = e.args.Org.val
	}
	loader.ResetToReady(e.m)
	if err := e.m.LoadBinary(e.args.Org.val, data); err != nil {
		return err
	}
	// Point the CPU at the entry. SetState bypasses the journaled
	// entry points, so the history baseline must be rebased (the
	// monitor's `set` discipline).
	st := e.m.CPU().State()
	st.PC = entry
	st.Halted = false
	e.m.CPU().SetState(st)
	e.m.HistoryRebase()
	e.entryAddr = entry
	return nil
}

// loadTape boots to READY, fast-loads the tape sections, arms the
// entry breakpoint if requested, and types RUN — which executes once
// the engine starts running at configurationDone.
func (e *engine) loadTape(data []byte) error {
	name, secs, err := loader.ParseTapeImage(data)
	if err != nil {
		return err
	}
	_ = name
	loader.ResetToReady(e.m)
	for _, sec := range secs {
		if err := e.m.LoadBinary(sec.Start, sec.Data); err != nil {
			return fmt.Errorf("section [0x%04X,0x%04X): %w", sec.Start, sec.End, err)
		}
	}
	entry, ok, err := e.args.Entry.resolve(e.sld)
	if err != nil {
		return err
	}
	if !ok && len(secs) > 0 {
		entry = secs[0].Start
	}
	e.entryAddr = entry
	if e.args.stopOnEntry() {
		e.addTempBP(entry, "entry")
	}
	if _, err := e.m.TypeText("RUN\n"); err != nil {
		return err
	}
	return nil
}

// onConfigurationDone starts execution: a .bin with stopOnEntry
// reports the entry stop; everything else begins running.
func (s *server) onConfigurationDone(req *dap.ConfigurationDoneRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	eng.do(func() {
		if !eng.args.isTape() && eng.args.stopOnEntry() {
			eng.recordStop()
			eng.onStopped("entry", "", nil)
		} else {
			eng.running = true
		}
	})
	s.respond(&req.Request, &dap.ConfigurationDoneResponse{})
}

// onRestart tears the machine down and relaunches with the stored
// arguments; user breakpoints survive (clients don't re-send them).
func (s *server) onRestart(req *dap.RestartRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	args := eng.args
	srcBPs, instBPs, bpIDs, nextID := eng.srcBPs, eng.instBPs, eng.bpIDs, eng.nextID
	eng.stop()
	s.eng = nil

	fresh, err := s.buildEngine(args)
	if err != nil {
		s.fail(&req.Request, "restart: %v", err)
		return
	}
	fresh.srcBPs, fresh.instBPs, fresh.bpIDs, fresh.nextID = srcBPs, instBPs, bpIDs, nextID
	fresh.do(func() {
		fresh.reconcileBreakpoints()
		if !fresh.args.isTape() && fresh.args.stopOnEntry() {
			fresh.recordStop()
			fresh.onStopped("entry", "", nil)
		} else {
			fresh.running = true
		}
	})
	s.eng = fresh
	s.respond(&req.Request, &dap.RestartResponse{})
	s.announceScreen(fresh)
}

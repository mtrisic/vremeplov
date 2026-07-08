// Command headless boots a Galaksija without any frontend, runs it for a
// fixed amount of emulated time, and dumps machine state to files. It is
// Vremeplov's primary automation and test harness (SPEC.md §5.1).
//
// Program input (Phase 3): when --tape, --load-gtp, or --type is given,
// the machine first boots for --boot-frames, then loads/types in order:
// --load-gtp pokes decoded GTP sections into memory; --tape types OLD
// and plays the compiled pulse schedule through the ROM's own load
// routine (--turbo runs the whole tape before the --frames budget;
// without it the tape plays inside the budget, as on real hardware);
// --type pipes a BASIC listing through the keyboard. --frames/--tstates
// then run on top.
//
// Tape output: --record-tape arms the recorder for the whole run and
// writes every SAVE the program performs as a GTP image (name block =
// output filename base) — or as WAV audio when the path ends in .wav.
// It fails if nothing was captured. --tape and --load-gtp likewise
// accept digitized .wav audio in place of a GTP image.
//
// Examples:
//
//	headless --frames 100 --dump-frame frame.png --dump-mem mem.bin
//	headless --ram 4 --keys script.keys --dump-frame frame.png --crop
//	headless --load-bin 0x2C3A:prog.bin --tstates 500000
//	headless --snapshot-save state.gob --frames 50
//	echo RUN | headless --tape prog.gtp --turbo --type - --frames 300 --dump-frame out.png
//	headless --load-gtp prog.gtp --type run.bas --frames 300 --dump-frame out.png
//	printf '10 PRINT 123\nSAVE\n' | headless --type - --record-tape out.gtp --frames 1000
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
	"github.com/mtrisic/vremeplov/roms"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "headless:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		romAPath   = flag.String("rom-a", "", "ROM A: 'v28' (default), 'v29' (requires --rom-b), or a path")
		romBPath   = flag.String("rom-b", "", "ROM B image path, or 'embedded' (default: socket empty)")
		chargen    = flag.String("chargen", "elektronika", "chargen ROM: elektronika, mipro, or a path")
		ramFlag    = flag.String("ram", "6", "RAM size: 2, 4, 6, or expanded")
		frames     = flag.Uint64("frames", 0, "frames to run (20 ms each)")
		tstates    = flag.Uint64("tstates", 0, "T-states to run (alternative to --frames)")
		dumpFrame  = flag.String("dump-frame", "", "write the last completed frame as PNG")
		crop       = flag.Bool("crop", false, "crop --dump-frame to the standard active area")
		dumpMem    = flag.String("dump-mem", "", "write memory to file: path[:0xSTART-0xEND] (default full 64K)")
		keysScript = flag.String("keys", "", "key event script: lines '<tstate> <down|up> <KEY>'")
		loadBin    = flag.String("load-bin", "", "load raw binary: 0xADDR:path")
		snapSave   = flag.String("snapshot-save", "", "write a machine snapshot after running")
		snapLoad   = flag.String("snapshot-load", "", "restore a machine snapshot before running")
		screenText = flag.Bool("screen-text", false, "print the decoded 32x16 screen text after running")
		tapePath   = flag.String("tape", "", "tape image (.gtp, or .wav audio): faithful playback through the ROM's OLD routine")
		turbo      = flag.Bool("turbo", false, "with --tape: run the whole tape (plus settle time) before the --frames/--tstates budget")
		loadGTP    = flag.String("load-gtp", "", "tape image (.gtp or .wav): fast-load (poke decoded sections into memory after boot)")
		typeSrc    = flag.String("type", "", "BASIC listing to type after boot/loading ('-' for stdin)")
		bootFrames = flag.Uint64("boot-frames", 100, "frames to boot before --tape/--load-gtp/--type input")
		recordTape = flag.String("record-tape", "", "record tape output (SAVE) during the run; writes GTP, or WAV audio for a .wav path")
	)
	flag.Parse()

	cfg, err := buildConfig(*romAPath, *romBPath, *chargen, *ramFlag)
	if err != nil {
		return err
	}
	m, err := core.New(cfg)
	if err != nil {
		return err
	}

	if *snapLoad != "" {
		f, err := os.Open(*snapLoad)
		if err != nil {
			return err
		}
		s, err := core.ReadSnapshot(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("reading snapshot: %w", err)
		}
		if err := m.Restore(s); err != nil {
			return err
		}
	}
	if *loadBin != "" {
		if err := applyLoadBin(m, *loadBin); err != nil {
			return err
		}
	}
	if *keysScript != "" {
		events, err := parseKeyScript(*keysScript)
		if err != nil {
			return err
		}
		m.QueueKeyEvents(events...)
	}
	if *turbo && *tapePath == "" {
		return fmt.Errorf("--turbo only applies with --tape")
	}
	if *recordTape != "" {
		m.StartTapeRecording()
	}

	// Program-input phase (SPEC.md §5.1, Phase 3 additions).
	if *tapePath != "" || *loadGTP != "" || *typeSrc != "" {
		m.RunTstates(*bootFrames * core.TstatesPerFrame)
		if *loadGTP != "" {
			if err := fastLoad(m, *loadGTP); err != nil {
				return err
			}
		}
		if *tapePath != "" {
			if err := playTape(m, *tapePath, *turbo); err != nil {
				return err
			}
		}
		if *typeSrc != "" {
			text, err := readTypeSource(*typeSrc)
			if err != nil {
				return err
			}
			end, err := m.TypeText(text)
			if err != nil {
				return err
			}
			m.RunTstates(end - m.Tstates())
		}
	}

	switch {
	case *frames > 0 && *tstates > 0:
		return fmt.Errorf("--frames and --tstates are mutually exclusive")
	case *frames > 0:
		m.RunTstates(*frames * core.TstatesPerFrame)
	case *tstates > 0:
		m.RunTstates(*tstates)
	}

	if *dumpFrame != "" {
		if err := writeFramePNG(m, *dumpFrame, *crop); err != nil {
			return err
		}
	}
	if *dumpMem != "" {
		if err := writeMemDump(m, *dumpMem); err != nil {
			return err
		}
	}
	if *snapSave != "" {
		f, err := os.Create(*snapSave)
		if err != nil {
			return err
		}
		if _, err := m.Snapshot().WriteTo(f); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	if *recordTape != "" {
		streams := m.StopTapeRecording()
		if len(streams) == 0 {
			return fmt.Errorf("--record-tape: no tape output captured — did the program SAVE?")
		}
		var out []byte
		if isWAV(*recordTape) {
			out = core.CompileTapeBlocks(streams...).EncodeWAV(44100)
		} else {
			name := strings.TrimSuffix(filepath.Base(*recordTape), filepath.Ext(*recordTape))
			img, err := gtp.Build(name, streams...)
			if err != nil {
				return fmt.Errorf("--record-tape: %w", err)
			}
			out = img
		}
		if err := os.WriteFile(*recordTape, out, 0o644); err != nil {
			return err
		}
	}
	if *screenText {
		for _, row := range m.ScreenText() {
			fmt.Println(row)
		}
	}
	return nil
}

func buildConfig(romA, romB, chargen, ram string) (core.Config, error) {
	cfg := core.Config{}
	var err error
	switch romA {
	case "", "v28":
		cfg.ROMA = roms.ROMA()
	case "v29":
		cfg.ROMA = roms.ROMAWithROMBInit()
	default:
		if cfg.ROMA, err = os.ReadFile(romA); err != nil {
			return cfg, err
		}
	}
	switch romB {
	case "":
	case "embedded":
		cfg.ROMB = roms.ROMB()
	default:
		if cfg.ROMB, err = os.ReadFile(romB); err != nil {
			return cfg, err
		}
	}
	switch chargen {
	case "elektronika":
		cfg.Chargen = roms.ChargenElektronika()
	case "mipro":
		cfg.Chargen = roms.ChargenMipro()
	default:
		if cfg.Chargen, err = os.ReadFile(chargen); err != nil {
			return cfg, err
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
		return cfg, fmt.Errorf("invalid --ram %q (want 2, 4, 6, or expanded)", ram)
	}
	return cfg, nil
}

// parseKeyScript reads the stable key-script format (SPEC.md §5.1):
// '<tstate> <down|up> <KEY>' per line; '#' comments and blanks ignored.
func parseKeyScript(path string) ([]core.KeyEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []core.KeyEvent
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: want '<tstate> <down|up> <KEY>'", path, lineNo)
		}
		ts, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: bad tstate: %v", path, lineNo, err)
		}
		var down bool
		switch fields[1] {
		case "down":
			down = true
		case "up":
			down = false
		default:
			return nil, fmt.Errorf("%s:%d: want down or up, got %q", path, lineNo, fields[1])
		}
		key, err := core.ParseKey(fields[2])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, lineNo, err)
		}
		events = append(events, core.KeyEvent{Tstate: ts, Key: key, Down: down})
	}
	return events, sc.Err()
}

// fastLoadGTP pokes every decoded standard-block section of a GTP image
// straight into memory. The section dump includes the BASIC pointers at
// 0x2C36, so a subsequent RUN or LIST sees the program.
// isWAV dispatches tape files by extension: .wav is digitized audio,
// anything else is a GTP image.
func isWAV(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".wav")
}

// tapeStreamsFromFile returns the native byte streams of a tape file:
// GTP standard-block payloads, or the decoded content of a WAV.
func tapeStreamsFromFile(path string) ([][]byte, error) {
	if isWAV(path) {
		s, err := wavScheduleFromFile(path)
		if err != nil {
			return nil, err
		}
		return s.TapeStreams(), nil
	}
	f, err := parseGTPFile(path)
	if err != nil {
		return nil, err
	}
	var streams [][]byte
	for _, b := range f.Blocks {
		// Turbo payloads share the standard layout; the stock ROM can
		// only read standard pulses, so both compile at standard speed.
		if b.Type == gtp.BlockStandard || b.Type == gtp.BlockTurbo {
			streams = append(streams, b.Payload)
		}
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("%s: no data blocks", path)
	}
	return streams, nil
}

func wavScheduleFromFile(path string) (*core.TapeSchedule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := core.DecodeWAV(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.TapeStreams() == nil {
		return nil, fmt.Errorf("%s: no decodable tape data in the audio", path)
	}
	return s, nil
}

// fastLoad pokes a tape file's decoded sections straight into memory.
func fastLoad(m *core.Machine, path string) error {
	streams, err := tapeStreamsFromFile(path)
	if err != nil {
		return err
	}
	for i, s := range streams {
		b := gtp.Block{Type: gtp.BlockStandard, Payload: s}
		sec, err := b.Section()
		if err != nil {
			return fmt.Errorf("%s: stream %d: %w", path, i, err)
		}
		if err := m.LoadBinary(sec.Start, sec.Data); err != nil {
			return fmt.Errorf("%s: section [0x%04X,0x%04X): %w", path, sec.Start, sec.End, err)
		}
	}
	return nil
}

// playTape types OLD and plays the tape file through the deck: a GTP is
// compiled to the canonical pulse schedule, a WAV plays with its own
// decoded pulse timing. With turbo, it runs the machine through the
// whole tape plus settle time; otherwise the tape plays during the
// normal run budget.
func playTape(m *core.Machine, path string, turbo bool) error {
	var sched *core.TapeSchedule
	if isWAV(path) {
		s, err := wavScheduleFromFile(path)
		if err != nil {
			return err
		}
		sched = s
	} else {
		streams, err := tapeStreamsFromFile(path)
		if err != nil {
			return err
		}
		sched = core.CompileTapeBlocks(streams...)
	}
	end, err := m.TypeText("OLD\n")
	if err != nil {
		return err
	}
	m.RunTstates(end - m.Tstates())
	m.InsertTape(sched)
	m.PlayTape()
	if turbo {
		tapeEnd, _ := m.TapeEndTstate()
		const settleFrames = 50 // let OLD verify the checksum and prompt
		m.RunTstates(tapeEnd - m.Tstates() + settleFrames*core.TstatesPerFrame)
	}
	return nil
}

func parseGTPFile(path string) (*gtp.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := gtp.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func readTypeSource(src string) (string, error) {
	if src == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(src)
	return string(b), err
}

func applyLoadBin(m *core.Machine, spec string) error {
	addrStr, path, ok := strings.Cut(spec, ":")
	if !ok {
		return fmt.Errorf("--load-bin wants 0xADDR:path, got %q", spec)
	}
	addr, err := strconv.ParseUint(addrStr, 0, 16)
	if err != nil {
		return fmt.Errorf("--load-bin address: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return m.LoadBinary(uint16(addr), data)
}

func writeFramePNG(m *core.Machine, path string, crop bool) error {
	data, err := m.FramePNG(crop)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// writeMemDump handles path[:0xSTART-0xEND].
func writeMemDump(m *core.Machine, spec string) error {
	path := spec
	start, end := uint64(0), uint64(0x10000)
	if i := strings.LastIndex(spec, ":0x"); i > 0 {
		path = spec[:i]
		rangeStr := spec[i+1:]
		lo, hi, ok := strings.Cut(rangeStr, "-")
		if !ok {
			return fmt.Errorf("--dump-mem range wants 0xSTART-0xEND, got %q", rangeStr)
		}
		var err error
		if start, err = strconv.ParseUint(lo, 0, 32); err != nil {
			return err
		}
		if end, err = strconv.ParseUint(hi, 0, 32); err != nil {
			return err
		}
	}
	return os.WriteFile(path, m.DumpMemory(uint32(start), uint32(end)), 0o644)
}

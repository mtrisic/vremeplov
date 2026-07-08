// Command tui is Vremeplov's interactive terminal frontend: it renders
// the Galaksija screen with unicode sub-cell graphics at 50 Hz and maps
// the terminal keyboard onto the machine's key matrix.
//
// Machine keys: letters/digits/symbols type directly (shifted symbols
// like + * - < > ? follow the Galaksija layout), arrows are arrows,
// Enter=Return, Esc=Break, Backspace=Delete, Tab=List, Ctrl+R=Repeat.
//
// Frontend commands live in the clickable footer button bar and behind
// a Ctrl+X prefix: q quit, p pause, r reset, b rewind, w/l snapshot
// save/load, t tape recording, d memory dump, s sticky-keys (game
// mode), v renderer, f full-frame view, m monitor (a machine-language
// debugger panel: registers, live disassembly, breakpoints/watchpoints,
// memory REPL). On terminals too short for the buttons the footer
// collapses to the status line and Ctrl+X remains.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/roms"
)

// version is stamped by tools/build-tui.sh (-ldflags -X main.version=…).
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		romAPath    = flag.String("rom-a", "", "ROM A: 'v28' (default), 'v29' (requires --rom-b), or a path")
		romBPath    = flag.String("rom-b", "", "ROM B image path, or 'embedded' (default: socket empty)")
		chargen     = flag.String("chargen", "elektronika", "chargen ROM: elektronika, mipro, or a path")
		ramFlag     = flag.String("ram", "6", "RAM size: 2, 4, 6, or expanded")
		rewindSec   = flag.Int("rewind", 120, "seconds of rewind history to keep (~15 MB per minute; 0 disables)")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println("vremeplov-tui", version)
		return nil
	}

	cfg, err := buildConfig(*romAPath, *romBPath, *chargen, *ramFlag)
	if err != nil {
		return err
	}
	m, err := core.New(cfg)
	if err != nil {
		return err
	}
	mo := newModel(m)
	switch {
	case *rewindSec < 0:
		return fmt.Errorf("invalid --rewind %d (want seconds ≥ 0)", *rewindSec)
	case *rewindSec == 0:
		m.DisableHistory()
	default:
		m.EnableHistory(50*core.TstatesPerFrame, *rewindSec)
	}
	prog := tea.NewProgram(mo, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = prog.Run()
	return err
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

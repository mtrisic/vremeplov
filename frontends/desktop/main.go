// Command desktop is Vremeplov's native GUI frontend: an Ebiten window
// rendering the Galaksija screen pixel-perfect at 50 Hz, with real
// key-down/up keyboard input, cassette-port sound, and the same chrome
// the other frontends carry (pause, reset, rewind, snapshots,
// screenshots, tape loading and recording, monitor).
//
// Machine keys type directly (shifted symbols follow the Galaksija
// layout), Esc = Break, Backspace = Delete, Tab = List. Frontend
// commands live on F-keys — the Galaksija has none, so they never
// collide with the machine.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/roms"
)

// version is stamped by tools/build-desktop.sh (-ldflags -X main.version=…).
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "desktop:", err)
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
		fmt.Println("vremeplov-desktop", version)
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
	switch {
	case *rewindSec < 0:
		return fmt.Errorf("invalid --rewind %d (want seconds ≥ 0)", *rewindSec)
	case *rewindSec == 0:
		m.DisableHistory()
	default:
		m.EnableHistory(50*core.TstatesPerFrame, *rewindSec)
	}

	g := newGame(m)
	if err := g.initAudio(); err != nil {
		return err
	}
	// Positional argument: a tape image or snapshot to load at startup.
	if file := flag.Arg(0); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		g.applyFile(filepath.Base(file), data)
	}
	ebiten.SetWindowTitle("Vremeplov — Galaksija")
	ebiten.SetWindowSize(defaultWinW, defaultWinH)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowClosingHandled(true) // quit asks for confirmation
	ebiten.SetTPS(core.FramesPerSecond)
	return ebiten.RunGame(g)
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

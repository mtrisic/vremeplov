package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
	"github.com/mtrisic/vremeplov/core/loader"
)

// applyFile dispatches a file the user handed the window — a CLI
// argument or a drag-and-drop: .gob restores a snapshot, anything else
// loads as a tape image (.gtp or digitized .wav). Errors leave the
// machine untouched.
func (g *Game) applyFile(name string, data []byte) {
	if strings.HasSuffix(strings.ToLower(name), ".gob") {
		s, err := core.ReadSnapshot(bytes.NewReader(data))
		if err != nil {
			g.status = "snapshot: " + err.Error()
			return
		}
		g.keys.releaseAll()
		if err := g.m.Restore(s); err != nil {
			g.status = "snapshot: " + err.Error()
			return
		}
		g.dropAudio()
		g.setPaused(false)
		g.status = fmt.Sprintf("snapshot %s restored", name)
		return
	}
	prog, err := loader.LoadAndRun(g.m, data)
	if err != nil {
		g.status = "load failed: " + err.Error()
		return
	}
	if prog == "" {
		prog = "program"
	}
	g.lastTape = data
	g.dropAudio()
	g.setPaused(false)
	g.status = fmt.Sprintf("loaded %s — running", prog)
}

// reloadTape repeats the whole file-picker sequence for the current
// tape — reset, boot, fast-load, RUN.
func (g *Game) reloadTape() {
	if g.lastTape == nil {
		g.status = "no tape loaded (drop a .gtp or .wav on the window)"
		return
	}
	g.applyFile("reload", g.lastTape)
}

// handleDrops loads the first regular file dropped on the window. The
// fs.FS is only valid within the tick that returned it, so the bytes
// are read here and now.
func (g *Game) handleDrops() {
	ff := ebiten.DroppedFiles()
	if ff == nil {
		return
	}
	name, data, err := firstDroppedFile(ff)
	if err != nil {
		g.status = "drop: " + err.Error()
		return
	}
	if name == "" {
		return
	}
	g.applyFile(name, data)
}

// firstDroppedFile walks a dropped-files FS and reads the first
// regular file. name == "" means the drop held none.
func firstDroppedFile(ff fs.FS) (name string, data []byte, err error) {
	err = fs.WalkDir(ff, ".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		data, err = fs.ReadFile(ff, path)
		if err != nil {
			return err
		}
		name = filepath.Base(path)
		return fs.SkipAll
	})
	return name, data, err
}

// toggleRecording arms the tape recorder, or stops it and writes the
// captured SAVEs next to the working directory as both formats — the
// TUI's ^X t semantics.
func (g *Game) toggleRecording() {
	if !g.m.TapeRecording() {
		g.m.StartTapeRecording()
		g.status = "recording tape (F8 to stop; SAVE in BASIC)"
		return
	}
	streams := g.m.StopTapeRecording()
	if len(streams) == 0 {
		g.status = "recording stopped — no tape output captured"
		return
	}
	name := fmt.Sprintf("vremeplov-tape-%d", g.m.FrameSeq())
	img, err := gtp.Build(name, streams...)
	if err == nil {
		err = os.WriteFile(name+".gtp", img, 0o644)
	}
	if err == nil {
		wav := core.CompileTapeBlocks(streams...).EncodeWAV(44100)
		err = os.WriteFile(name+".wav", wav, 0o644)
	}
	if err != nil {
		g.status = "recording failed: " + err.Error()
	} else {
		g.status = fmt.Sprintf("%d tape block(s) written to %s.{gtp,wav}", len(streams), name)
	}
}

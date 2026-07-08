package main

// Snapshot chrome (^X w / ^X l): save-state files in the current
// directory, using core's gob snapshot format — the same files
// cmd/headless reads and writes via --snapshot-save/--snapshot-load.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtrisic/vremeplov/core"
)

// saveSnapshot writes the machine state to vremeplov-snap-<frame>.gob.
func (mo *model) saveSnapshot() {
	name := fmt.Sprintf("vremeplov-snap-%d.gob", mo.m.FrameSeq())
	f, err := os.Create(name)
	if err == nil {
		_, err = mo.m.Snapshot().WriteTo(f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		mo.status = "snapshot: " + err.Error()
		return
	}
	mo.status = "snapshot saved to " + name
}

// loadSnapshot restores the newest vremeplov-snap-*.gob in the current
// directory.
func (mo *model) loadSnapshot() {
	name, err := newestSnapshotFile()
	var s *core.Snapshot
	if err == nil {
		var f *os.File
		if f, err = os.Open(name); err == nil {
			s, err = core.ReadSnapshot(f)
			f.Close()
		}
	}
	if err == nil {
		// The held-key bookkeeping belongs to the old timeline; the
		// snapshot's own matrix state is authoritative.
		for k := range mo.holds {
			mo.m.ReleaseKey(k)
			delete(mo.holds, k)
		}
		err = mo.m.Restore(s)
	}
	if err != nil {
		mo.status = "snapshot: " + err.Error()
		return
	}
	mo.status = fmt.Sprintf("%s restored (t=%.1fs)", name,
		float64(mo.m.Tstates())/core.CPUClockHz)
}

// newestSnapshotFile picks the vremeplov-snap-*.gob with the highest
// frame number.
func newestSnapshotFile() (string, error) {
	names, _ := filepath.Glob("vremeplov-snap-*.gob")
	best, bestN := "", -1
	for _, n := range names {
		var v int
		if _, err := fmt.Sscanf(filepath.Base(n), "vremeplov-snap-%d.gob", &v); err == nil && v > bestN {
			best, bestN = n, v
		}
	}
	if best == "" {
		return "", fmt.Errorf("no vremeplov-snap-*.gob in the current directory (^X w saves one)")
	}
	return best, nil
}

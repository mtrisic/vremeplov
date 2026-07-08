// Command check-deps enforces the core-purity rule from SPEC.md §4.1:
// every package the core module depends on must be either the standard
// library, the core module itself, or gozilog (which is itself
// stdlib-only). It is part of every phase gate.
//
// Run from the repository root (workspace mode):
//
//	go run ./tools/check-deps
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

var allowedModules = map[string]bool{
	"github.com/mtrisic/vremeplov/core": true,
	"github.com/mtrisic/gozilog":        true,
}

type pkg struct {
	ImportPath string
	Standard   bool
	Module     *struct{ Path string }
}

func main() {
	cmd := exec.Command("go", "list", "-deps", "-json", "./...")
	cmd.Dir = "core"
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-deps: go list failed: %v\n", err)
		os.Exit(1)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	bad := 0
	total := 0
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			fmt.Fprintf(os.Stderr, "check-deps: parsing go list output: %v\n", err)
			os.Exit(1)
		}
		total++
		if p.Standard {
			continue
		}
		if p.Module == nil || !allowedModules[p.Module.Path] {
			mod := "<none>"
			if p.Module != nil {
				mod = p.Module.Path
			}
			fmt.Fprintf(os.Stderr, "check-deps: FORBIDDEN dependency %s (module %s)\n", p.ImportPath, mod)
			bad++
		}
	}
	if bad > 0 {
		fmt.Fprintf(os.Stderr, "check-deps: core module is not pure (%d forbidden packages)\n", bad)
		os.Exit(1)
	}
	fmt.Printf("check-deps: OK — %d packages, core depends only on stdlib + gozilog\n", total)
}

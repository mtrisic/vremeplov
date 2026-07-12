package main

// SLD ("Source Level Debugging") files are sjasmplus's debug-info
// format (assemble with --sld). Version 1 is line-oriented and
// pipe-delimited:
//
//	<source file>|<line>|<def file>|<def line>|<page>|<value>|<type>|<data>
//
// The adapter consumes two record types: T (instruction trace — this
// source line assembled code at this address) and L (label — <data>
// carries a comma-separated module path ending in the label name).
// Everything else (device records, EQUs, pages) is skipped; the
// Galaksija is an unbanked 64 K machine, so page numbers are ignored.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SLD is the loaded source map: line↔address both ways plus labels.
type SLD struct {
	byLine map[string]map[int][]uint16 // normalized path → line → addresses
	byAddr map[uint16]lineRef          // first record wins
	labels map[string]uint16
	sorted []labelRef // ascending by address, for NearestLabel
}

type lineRef struct {
	file string
	line int
}

type labelRef struct {
	addr uint16
	name string
}

// LoadSLD parses path. Relative source paths inside the file resolve
// against sourceRoot (default: the SLD file's own directory).
func LoadSLD(path, sourceRoot string) (*SLD, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if sourceRoot == "" {
		sourceRoot = filepath.Dir(path)
	}

	s := &SLD{
		byLine: map[string]map[int][]uint16{},
		byAddr: map[uint16]lineRef{},
		labels: map[string]uint16{},
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		raw := sc.Text()
		if raw == "" || strings.HasPrefix(raw, "|") {
			continue // header/comment records
		}
		fields := strings.Split(raw, "|")
		if len(fields) < 8 {
			continue
		}
		file := normalizePath(fields[0], sourceRoot)
		line, err1 := strconv.Atoi(fields[1])
		value, err2 := strconv.ParseUint(fields[5], 10, 32)
		if err1 != nil || err2 != nil || value > 0xFFFF {
			continue
		}
		addr := uint16(value)
		switch fields[6] {
		case "T": // instruction trace
			if s.byLine[file] == nil {
				s.byLine[file] = map[int][]uint16{}
			}
			s.byLine[file][line] = append(s.byLine[file][line], addr)
			if _, taken := s.byAddr[addr]; !taken {
				s.byAddr[addr] = lineRef{file: file, line: line}
			}
		case "L", "F": // label (F: sjasmplus function-typed labels)
			name := labelName(fields[7])
			if name == "" {
				continue
			}
			if _, taken := s.labels[name]; !taken {
				s.labels[name] = addr
				s.sorted = append(s.sorted, labelRef{addr: addr, name: name})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(s.byLine) == 0 && len(s.labels) == 0 {
		return nil, fmt.Errorf("%s: no usable SLD records (is it a sjasmplus --sld file?)", path)
	}
	sort.Slice(s.sorted, func(i, j int) bool { return s.sorted[i].addr < s.sorted[j].addr })
	return s, nil
}

// labelName extracts the label from an L-record data field: a
// comma-separated module path where the last plain component is the
// name (trait suffixes like +equ start with '+').
func labelName(data string) string {
	name := ""
	for _, part := range strings.Split(data, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "+") {
			continue
		}
		name = part
	}
	return name
}

// normalizePath makes the SLD file's source path absolute and clean
// so it can be compared with editor-sent paths.
func normalizePath(p, root string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p)
}

// AddrFor resolves a source breakpoint: the first address assembled
// from exactly this line. Unknown absolute paths fall back to a
// basename match (editors and assemblers often disagree about roots).
func (s *SLD) AddrFor(file string, line int) (uint16, bool) {
	clean := filepath.Clean(file)
	if lines, ok := s.byLine[clean]; ok {
		if addrs := lines[line]; len(addrs) > 0 {
			return addrs[0], true
		}
		return 0, false
	}
	base := filepath.Base(clean)
	for known, lines := range s.byLine {
		if filepath.Base(known) == base {
			if addrs := lines[line]; len(addrs) > 0 {
				return addrs[0], true
			}
			return 0, false
		}
	}
	return 0, false
}

// LineFor maps an address back to its source line (exact instruction
// starts only).
func (s *SLD) LineFor(addr uint16) (file string, line int, ok bool) {
	ref, ok := s.byAddr[addr]
	return ref.file, ref.line, ok
}

// Label resolves a label name to its address (exact, then unqualified
// last-component match).
func (s *SLD) Label(name string) (uint16, bool) {
	if addr, ok := s.labels[name]; ok {
		return addr, true
	}
	for known, addr := range s.labels {
		if strings.HasSuffix(known, "."+name) {
			return addr, true
		}
	}
	return 0, false
}

// NearestLabel finds the closest label at or below addr — the
// human-readable name of "where the PC is".
func (s *SLD) NearestLabel(addr uint16) (name string, off uint16, ok bool) {
	i := sort.Search(len(s.sorted), func(i int) bool { return s.sorted[i].addr > addr })
	if i == 0 {
		return "", 0, false
	}
	l := s.sorted[i-1]
	return l.name, addr - l.addr, true
}

// Package roms embeds the Galaksija ROM images committed under bin/ and
// exposes them as byte slices. The accessors return fresh copies so callers
// cannot corrupt the embedded data.
//
// Provenance, checksums, and licensing status of the binaries are documented
// in PROVENANCE.md. The core module deliberately does not import this
// package (it takes ROM bytes via its Config); frontends and cmd use it for
// embedded defaults.
package roms

import _ "embed"

//go:embed bin/rom_a_v28.bin
var romAv28 []byte

//go:embed bin/rom_a_v29.bin
var romAv29 []byte

//go:embed bin/rom_b.bin
var romB []byte

//go:embed bin/chrgen_elektronika.bin
var chrElektronika []byte

//go:embed bin/chrgen_mipro.bin
var chrMipro []byte

// ROMA returns ROM A version 28, 4096 bytes, mapped at 0x0000-0x0FFF —
// the default for a machine without ROM B. (Version 29 unconditionally
// CALLs 0x1000 during boot and therefore requires ROM B; see AGENTS.md
// discrepancy log 11.)
func ROMA() []byte { return clone(romAv28) }

// ROMAWithROMBInit returns ROM A version 29, 4096 bytes. It boots ROM B
// automatically and MUST be paired with a ROM B image: with an empty
// socket it executes 0xFF bytes at 0x1000 and never reaches READY.
func ROMAWithROMBInit() []byte { return clone(romAv29) }

// ROMB returns the original ROM B (advanced math functions), 4096 bytes,
// mapped at 0x1000-0x1FFF.
func ROMB() []byte { return clone(romB) }

// ChargenElektronika returns the common character generator ROM variant
// (Elektronika inženjering logo), 2048 bytes.
func ChargenElektronika() []byte { return clone(chrElektronika) }

// ChargenMipro returns the Mipro-logo character generator ROM variant,
// 2048 bytes.
func ChargenMipro() []byte { return clone(chrMipro) }

func clone(b []byte) []byte { return append([]byte(nil), b...) }

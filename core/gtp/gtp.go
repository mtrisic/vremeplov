// Package gtp parses the GTP cassette-image format used by the Galaksija
// community (SPEC.md §3.7; authoritative reference: MAME
// src/lib/formats/gtp_cas.cpp, BSD-3-Clause).
//
// A GTP file is a sequence of blocks, each introduced by a 5-byte header:
// block type, little-endian 16-bit payload size, and two unused zero
// bytes. A standard block's payload is the native tape byte stream of
// SPEC.md §3.6 starting at the 0xA5 sync byte (the zero leader is not
// stored): sync, start address (LE), end address (LE, exclusive), data,
// checksum, optionally followed by garbage byte(s) — the stream SAVE
// leaves on tape after the checksum.
//
// Turbo blocks carry the same payload layout at a faster recording speed.
// No stock ROM can read turbo pulses and no surviving tool defines their
// waveform (SPEC.md §3.7), so this package treats a turbo payload exactly
// like a standard one: same validation, same Section decoding — the
// content loads, at standard speed.
package gtp

import "fmt"

// BlockType identifies a GTP block.
type BlockType byte

// Block types defined by the format.
const (
	// BlockStandard is a native tape stream at the standard speed.
	BlockStandard BlockType = 0x00
	// BlockTurbo is a "turbo" speed stream (produced by third-party
	// fast-save software). Its payload layout is identical to a standard
	// block's, so it is validated and decoded the same way; only the
	// (unrepresentable) recording speed differed on real tape.
	BlockTurbo BlockType = 0x01
	// BlockName is a NUL-terminated program name.
	BlockName BlockType = 0x10
)

// Block is one raw GTP block.
type Block struct {
	Type    BlockType
	Payload []byte
}

// File is a parsed GTP image.
type File struct {
	// Name is the program name from the first name block, if any.
	Name string
	// Blocks preserves every block in file order.
	Blocks []Block
}

// Section is the decoded, checksum-validated content of a standard block:
// a memory image of [Start, End).
type Section struct {
	// Start is the load address of the first data byte.
	Start uint16
	// End is the exclusive end address (Start + len(Data)).
	End uint16
	// Data is the memory content, aliasing the parsed input.
	Data []byte
	// Checksum is the stored checksum byte. It has already been
	// validated: the 8-bit sum of everything from the 0xA5 sync byte
	// through the checksum is 0xFF.
	Checksum byte
}

// Parse decodes a GTP image. Block payloads alias data; the caller must
// not modify it afterwards. Standard and turbo blocks are validated
// eagerly so a successful Parse guarantees Sections succeeds.
func Parse(data []byte) (*File, error) {
	f := &File{}
	for n := 0; n < len(data); {
		if len(data)-n < 5 {
			return nil, fmt.Errorf("gtp: truncated block header at offset %d", n)
		}
		typ := BlockType(data[n])
		size := int(data[n+1]) | int(data[n+2])<<8
		n += 5
		if len(data)-n < size {
			return nil, fmt.Errorf("gtp: block at offset %d claims %d payload bytes, %d remain", n-5, size, len(data)-n)
		}
		b := Block{Type: typ, Payload: data[n : n+size]}
		n += size
		switch typ {
		case BlockName:
			if f.Name == "" {
				f.Name = cString(b.Payload)
			}
		case BlockStandard, BlockTurbo:
			if _, err := b.Section(); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("gtp: unknown block type 0x%02X at offset %d", byte(typ), n-5-size)
		}
		f.Blocks = append(f.Blocks, b)
	}
	if len(f.Blocks) == 0 {
		return nil, fmt.Errorf("gtp: empty file")
	}
	return f, nil
}

// Section decodes a standard or turbo block's payload and validates its
// checksum (the two share one layout; see the package comment).
func (b *Block) Section() (*Section, error) {
	if b.Type != BlockStandard && b.Type != BlockTurbo {
		return nil, fmt.Errorf("gtp: Section on block type 0x%02X", byte(b.Type))
	}
	p := b.Payload
	if len(p) < 6 {
		return nil, fmt.Errorf("gtp: standard block payload too short (%d bytes)", len(p))
	}
	if p[0] != 0xA5 {
		return nil, fmt.Errorf("gtp: standard block starts with 0x%02X, want 0xA5 sync", p[0])
	}
	start := uint16(p[1]) | uint16(p[2])<<8
	end := uint16(p[3]) | uint16(p[4])<<8
	if end < start {
		return nil, fmt.Errorf("gtp: block end 0x%04X below start 0x%04X", end, start)
	}
	n := int(end - start)
	if len(p) < 5+n+1 {
		return nil, fmt.Errorf("gtp: block [0x%04X,0x%04X) needs %d data bytes + checksum, payload has %d", start, end, n, len(p)-5)
	}
	sum := byte(0)
	for _, v := range p[:5+n+1] {
		sum += v
	}
	if sum != 0xFF {
		return nil, fmt.Errorf("gtp: checksum mismatch: stream sums to 0x%02X, want 0xFF", sum)
	}
	return &Section{
		Start:    start,
		End:      end,
		Data:     p[5 : 5+n],
		Checksum: p[5+n],
	}, nil
}

// Sections decodes every data block — standard or turbo — in file order.
func (f *File) Sections() ([]*Section, error) {
	var out []*Section
	for i := range f.Blocks {
		if f.Blocks[i].Type != BlockStandard && f.Blocks[i].Type != BlockTurbo {
			continue
		}
		s, err := f.Blocks[i].Section()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("gtp: no data blocks")
	}
	return out, nil
}

// Build serializes a GTP image: an optional name block (when name is
// non-empty), then one standard block per stream. Each stream must be a
// valid native payload starting at the 0xA5 sync byte — exactly what
// core's tape recorder returns — and is validated the same way Parse
// validates incoming blocks, so Parse(Build(...)) always succeeds.
func Build(name string, streams ...[]byte) ([]byte, error) {
	if len(streams) == 0 {
		return nil, fmt.Errorf("gtp: no streams to build")
	}
	var out []byte
	appendBlock := func(typ BlockType, payload []byte) error {
		if len(payload) > 0xFFFF {
			return fmt.Errorf("gtp: block payload of %d bytes exceeds the 16-bit size field", len(payload))
		}
		out = append(out, byte(typ), byte(len(payload)), byte(len(payload)>>8), 0, 0)
		out = append(out, payload...)
		return nil
	}
	if name != "" {
		if err := appendBlock(BlockName, append([]byte(name), 0)); err != nil {
			return nil, err
		}
	}
	for i, s := range streams {
		b := Block{Type: BlockStandard, Payload: s}
		if _, err := b.Section(); err != nil {
			return nil, fmt.Errorf("gtp: stream %d: %w", i, err)
		}
		if err := appendBlock(BlockStandard, s); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func cString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

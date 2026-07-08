package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// nativeStream builds a valid native tape payload (0xA5, addrs, data,
// checksum) for codec tests.
func nativeStream(start uint16, data []byte) []byte {
	end := start + uint16(len(data))
	p := []byte{0xA5, byte(start), byte(start >> 8), byte(end), byte(end >> 8)}
	p = append(p, data...)
	sum := byte(0)
	for _, v := range p {
		sum += v
	}
	return append(p, 0xFF-sum)
}

// makeWAV writes arbitrary float samples ([-1,1]) as a PCM WAV, for
// degradation tests (bits: 8 or 16; channels duplicate the signal).
func makeWAV(samples []float64, rate, bits, channels int) []byte {
	le := binary.LittleEndian
	frame := channels * bits / 8
	dataLen := len(samples) * frame
	buf := make([]byte, 44+dataLen)
	copy(buf[0:], "RIFF")
	le.PutUint32(buf[4:], uint32(36+dataLen))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	le.PutUint32(buf[16:], 16)
	le.PutUint16(buf[20:], 1)
	le.PutUint16(buf[22:], uint16(channels))
	le.PutUint32(buf[24:], uint32(rate))
	le.PutUint32(buf[28:], uint32(rate*frame))
	le.PutUint16(buf[32:], uint16(frame))
	le.PutUint16(buf[34:], uint16(bits))
	copy(buf[36:], "data")
	le.PutUint32(buf[40:], uint32(dataLen))
	at := 44
	for _, v := range samples {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		for c := 0; c < channels; c++ {
			if bits == 8 {
				buf[at] = byte(v*127 + 128)
				at++
			} else {
				le.PutUint16(buf[at:], uint16(int16(v*32767)))
				at += 2
			}
		}
	}
	return buf
}

// wavSamples decodes an EncodeWAV output back to floats via the
// production parser (validated separately), for building degraded
// variants.
func wavSamples(t *testing.T, wav []byte) ([]float64, int) {
	t.Helper()
	s, rate, err := parseWAV(wav)
	if err != nil {
		t.Fatal(err)
	}
	return s, rate
}

func decodeToStreams(t *testing.T, wav []byte) [][]byte {
	t.Helper()
	sch, err := DecodeWAV(wav)
	if err != nil {
		t.Fatal(err)
	}
	return sch.TapeStreams()
}

func assertOneStream(t *testing.T, streams [][]byte, want []byte) {
	t.Helper()
	if len(streams) != 1 {
		t.Fatalf("decoded %d streams, want 1", len(streams))
	}
	if !bytes.Equal(streams[0], want) {
		t.Fatalf("stream differs\nwant % X\ngot  % X", want, streams[0])
	}
}

func TestWAVRoundtripRates(t *testing.T) {
	stream := nativeStream(0x2C36, []byte{1, 2, 3, 0xAA, 0x55, 0xFF, 0x00})
	sch := CompileTapeBlocks(stream)
	for _, rate := range []int{8000, 22050, 44100, 48000} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			assertOneStream(t, decodeToStreams(t, sch.EncodeWAV(rate)), stream)
		})
	}
}

func TestWAVRoundtripTwoBlocks(t *testing.T) {
	s1 := nativeStream(0x2C36, []byte{10, 20, 30})
	s2 := nativeStream(0x3000, []byte{40})
	streams := decodeToStreams(t, CompileTapeBlocks(s1, s2).EncodeWAV(44100))
	if len(streams) != 2 || !bytes.Equal(streams[0], s1) || !bytes.Equal(streams[1], s2) {
		t.Fatalf("two-block roundtrip failed (%d streams)", len(streams))
	}
}

// TestWAVToolPulses: circulating WAVs are mostly synthesized by GTP→WAV
// tools whose pulse halves are far wider than the ROM's 662 T — 26
// samples at 44.1 kHz (≈1811 T, the archive.org collection) or 30
// (≈2090 T, MAME gtp_cas) — often with inverted polarity. The ROM reads
// them fine (edge-triggered), so the decoder must too (AGENTS.md log 16).
func TestWAVToolPulses(t *testing.T) {
	stream := nativeStream(0x2C36, []byte{1, 2, 3, 0xAA, 0x55, 0xFF, 0x00})
	sch := CompileTapeBlocks(stream)
	const rate = 44100
	toSample := func(tt uint64) int { return int(tt * rate / CPUClockHz) }
	for _, half := range []int{26, 30} {
		t.Run(fmt.Sprintf("%d-sample halves", half), func(t *testing.T) {
			x := make([]float64, toSample(sch.Duration())+2*half+rate/10)
			for _, st := range sch.Starts {
				at := toSample(st)
				for i := 0; i < half; i++ {
					x[at+i] = -0.8 // inverted: low half first
					x[at+half+i] = 0.8
				}
			}
			assertOneStream(t, decodeToStreams(t, makeWAV(x, rate, 16, 1)), stream)
		})
	}
}

// TestWAVTrailingJunk: wild files carry metadata and editor debris
// after the data chunk — often past the declared RIFF length, often not
// valid chunks at all (AGENTS.md log 16). Both must parse.
func TestWAVTrailingJunk(t *testing.T) {
	stream := nativeStream(0x2C36, []byte{1, 2, 3, 0xAA, 0x55})
	wav := CompileTapeBlocks(stream).EncodeWAV(44100)

	list := append([]byte("LIST\x10\x00\x00\x00INFO"), make([]byte, 12)...)
	junk := []byte("wav\x00\xCC\xCC\xCC\xCCleftovers") // absurd size field

	t.Run("beyond declared RIFF length", func(t *testing.T) {
		// RIFF length still describes only fmt+data, like the wild files.
		withTail := append(append([]byte(nil), wav...), list...)
		withTail = append(withTail, junk...)
		assertOneStream(t, decodeToStreams(t, withTail), stream)
	})
	t.Run("within a bogus RIFF length", func(t *testing.T) {
		// Break the declared length so the parser walks the whole file
		// and meets the junk chunk itself.
		withTail := append(append([]byte(nil), wav...), junk...)
		binary.LittleEndian.PutUint32(withTail[4:], 0xFFFFFFF0)
		assertOneStream(t, decodeToStreams(t, withTail), stream)
	})
	t.Run("truncation without fmt+data still errors", func(t *testing.T) {
		if _, err := DecodeWAV(wav[:20]); err == nil {
			t.Fatal("truncated header accepted")
		}
	})
}

func TestWAVDegraded(t *testing.T) {
	stream := nativeStream(0x2C36, []byte{1, 2, 3, 0xAA, 0x55})
	base, rate := wavSamples(t, CompileTapeBlocks(stream).EncodeWAV(44100))

	transform := map[string]func([]float64) []float64{
		"attenuated": func(x []float64) []float64 {
			out := make([]float64, len(x))
			for i, v := range x {
				out[i] = v * 0.3
			}
			return out
		},
		"dc-offset": func(x []float64) []float64 {
			out := make([]float64, len(x))
			for i, v := range x {
				out[i] = v + 0.2
			}
			return out
		},
		"inverted": func(x []float64) []float64 {
			out := make([]float64, len(x))
			for i, v := range x {
				out[i] = -v
			}
			return out
		},
		"noisy": func(x []float64) []float64 {
			rng := rand.New(rand.NewSource(1))
			out := make([]float64, len(x))
			for i, v := range x {
				out[i] = v + (rng.Float64()-0.5)*0.1
			}
			return out
		},
	}
	for name, tr := range transform {
		t.Run(name, func(t *testing.T) {
			assertOneStream(t, decodeToStreams(t, makeWAV(tr(base), rate, 16, 1)), stream)
		})
	}
	t.Run("8bit", func(t *testing.T) {
		assertOneStream(t, decodeToStreams(t, makeWAV(base, rate, 8, 1)), stream)
	})
	t.Run("stereo", func(t *testing.T) {
		assertOneStream(t, decodeToStreams(t, makeWAV(base, rate, 16, 2)), stream)
	})
}

// TestWAVTapeStreamsSharedPath pins that TapeStreams inverts
// CompileTapeBlocks exactly (the same decode the recorder uses).
func TestWAVTapeStreamsSharedPath(t *testing.T) {
	s1 := nativeStream(0x2C36, []byte{9, 8, 7})
	s2 := nativeStream(0x2D00, []byte{6})
	streams := CompileTapeBlocks(s1, s2).TapeStreams()
	if len(streams) != 2 || !bytes.Equal(streams[0], s1) || !bytes.Equal(streams[1], s2) {
		t.Fatalf("TapeStreams != compiler input (%d streams)", len(streams))
	}
}

// TestWAVMachineFullCircle: record a real SAVE, export it as audio,
// decode the audio, and load it back through the ROM's OLD.
func TestWAVMachineFullCircle(t *testing.T) {
	streams, _ := recordSave(t, "10 PRINT 123\n")
	if len(streams) == 0 {
		t.Fatal("nothing captured")
	}
	wav := CompileTapeBlocks(streams...).EncodeWAV(44100)
	sch, err := DecodeWAV(wav)
	if err != nil {
		t.Fatal(err)
	}
	if got := sch.TapeStreams(); len(got) != 1 || !bytes.Equal(got[0], streams[0]) {
		t.Fatal("WAV roundtrip of the recorded SAVE lost data")
	}

	m := bootMachine(t)
	typeString(m, "OLD\n")
	m.RunTstates(20 * TstatesPerFrame)
	m.InsertTape(sch)
	m.PlayTape()
	endT, ok := m.TapeEndTstate()
	if !ok {
		t.Fatal("tape not playing")
	}
	m.RunTstates(endT - m.Tstates() + 100*TstatesPerFrame)
	typeString(m, "RUN\n")
	m.RunTstates(100 * TstatesPerFrame)
	if screen := strings.Join(m.ScreenText(), "\n"); !strings.Contains(screen, "123") {
		t.Fatalf("program loaded from WAV did not run:\n%s", screen)
	}
}

func TestWAVReaderErrors(t *testing.T) {
	good := CompileTapeBlocks(nativeStream(0x2C36, []byte{1})).EncodeWAV(44100)

	cases := map[string][]byte{
		"truncated": good[:8],
		"not-riff":  append([]byte("JUNK"), good[4:]...),
		"low-rate":  makeWAV([]float64{0, 0.5, 0}, 4000, 16, 1),
		"no-data":   good[:44-8], // header without the data chunk body
	}
	// Non-PCM: patch the format tag.
	nonPCM := append([]byte(nil), good...)
	binary.LittleEndian.PutUint16(nonPCM[20:], 3)
	cases["non-pcm"] = nonPCM

	for name, data := range cases {
		if _, err := DecodeWAV(data); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Silence decodes without error into an empty schedule.
	silent := makeWAV(make([]float64, 44100), 44100, 16, 1)
	sch, err := DecodeWAV(silent)
	if err != nil {
		t.Fatalf("silence rejected: %v", err)
	}
	if len(sch.Starts) != 0 || sch.TapeStreams() != nil {
		t.Fatal("silence produced pulses")
	}
}

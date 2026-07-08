package core

import (
	"encoding/binary"
	"fmt"
	"math"
)

// WAV cassette codec: TapeSchedule ↔ digitized audio. EncodeWAV renders
// the exact waveform SAVE puts on the line (SPEC.md §3.6: +662 T, −662 T,
// mid rest); DecodeWAV recovers a pulse schedule from a recording of it —
// tolerant of DC offset, attenuation, inverted polarity, and resampling.
// T-states map to sample time through CPUClockHz (3.072 MHz).

const (
	// wavAmplitude is the synthesized pulse level, as a fraction of full
	// scale.
	wavAmplitude = 0.8
	// wavPadSeconds of silence pad each end of an encoded file.
	wavPadSeconds = 0.1
	// wavMinRate is the lowest accepted sample rate; below it one pulse
	// half (≈215 µs) spans less than two samples.
	wavMinRate = 8000
	// wavThresholdFrac sets the detector threshold relative to the
	// post-DC-block peak.
	wavThresholdFrac = 0.30
	// wavMinPhaseT/wavMaxPhaseT bound one pulse half. The floor is the
	// comparator's ≈140 T minimum (SPEC.md §3.6). The ceiling covers the
	// pulse widths found in the wild (AGENTS.md log 16): the real SAVE
	// writes 662 T halves, but most circulating WAVs are synthesized by
	// GTP→WAV tools with much wider pulses — 26 samples at 44.1 kHz
	// ≈ 1811 T (the archive.org collection), 30 samples ≈ 2090 T (MAME
	// gtp_cas.cpp) — which the ROM reads fine because its bit reader is
	// edge-triggered and ignores the width. 2200 accepts all of those
	// and stays below half the minimal "1" split (4705/2 ≈ 2352 T),
	// past which a biphase pulse would overlap the cell's second pulse.
	wavMinPhaseT = 140
	wavMaxPhaseT = 2200
	// wavMergeGapT merges detections closer than any legitimate pulse
	// spacing (minimum real gap is tapeOneSplit = 4705 T).
	wavMergeGapT = 3000
	// wavDCPole is the DC-blocker pole (y[n] = x[n] − x[n−1] + pole·y[n−1]).
	wavDCPole = 0.995
)

// EncodeWAV renders the schedule as a 16-bit mono PCM WAV at sampleRate:
// +0.8 FS for each pulse's Width, −0.8 FS for the same duration again,
// silence (the mid rest level) elsewhere, with ~0.1 s of silence at each
// end.
func (s *TapeSchedule) EncodeWAV(sampleRate int) []byte {
	toSample := func(t uint64) int {
		return int(t * uint64(sampleRate) / CPUClockHz)
	}
	pad := int(float64(sampleRate) * wavPadSeconds)
	n := toSample(s.Duration()+s.Width) + 2*pad
	samples := make([]int16, n)
	fullScale := float64(32767)
	amp := int16(wavAmplitude * fullScale)
	for _, st := range s.Starts {
		hi := pad + toSample(st)
		mid := pad + toSample(st+s.Width)
		lo := pad + toSample(st+2*s.Width)
		for i := hi; i < mid && i < n; i++ {
			samples[i] = amp
		}
		for i := mid; i < lo && i < n; i++ {
			samples[i] = -amp
		}
	}

	dataLen := 2 * len(samples)
	buf := make([]byte, 44+dataLen)
	le := binary.LittleEndian
	copy(buf[0:], "RIFF")
	le.PutUint32(buf[4:], uint32(36+dataLen))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	le.PutUint32(buf[16:], 16)
	le.PutUint16(buf[20:], 1) // PCM
	le.PutUint16(buf[22:], 1) // mono
	le.PutUint32(buf[24:], uint32(sampleRate))
	le.PutUint32(buf[28:], uint32(2*sampleRate)) // byte rate
	le.PutUint16(buf[32:], 2)                    // block align
	le.PutUint16(buf[34:], 16)                   // bits per sample
	copy(buf[36:], "data")
	le.PutUint32(buf[40:], uint32(dataLen))
	for i, v := range samples {
		le.PutUint16(buf[44+2*i:], uint16(v))
	}
	return buf
}

// DecodeWAV decodes digitized cassette audio into a pulse schedule ready
// for InsertTape. It errors only on malformed or unsupported WAV data;
// audio with no detectable pulses yields an empty schedule (check
// TapeStreams for decodable content).
func DecodeWAV(data []byte) (*TapeSchedule, error) {
	samples, rate, err := parseWAV(data)
	if err != nil {
		return nil, err
	}
	return &TapeSchedule{
		Starts: detectWAVPulses(samples, rate),
		Width:  tapePulseWidth,
	}, nil
}

// TapeStreams decodes the schedule's pulses into the native byte streams
// they encode (leader stripped, each starting at the 0xA5 sync byte —
// GTP standard-block payloads), or nil if nothing decodes. It applies
// exactly the decoding the tape recorder applies to captured SAVEs.
func (s *TapeSchedule) TapeStreams() [][]byte {
	return decodeTapeStarts(s.Starts)
}

// parseWAV walks the RIFF chunks and returns channel-averaged samples in
// [-1, 1] plus the sample rate. PCM 8/16-bit, any channel count.
func parseWAV(data []byte) ([]float64, int, error) {
	le := binary.LittleEndian
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("core: not a RIFF/WAVE file")
	}
	var (
		fmtSeen                      bool
		format, channels, rate, bits int
		raw                          []byte
	)
	off := 12
	// The RIFF header declares the form's length; bytes past it are
	// trailing junk some tools append (metadata, editor debris — the
	// archive.org files carry both). Never walk beyond it, and when the
	// declared length itself is nonsense, fall back to the file size.
	end := len(data)
	if rs := int(le.Uint32(data[4:8])); rs >= 4 && 8+rs <= len(data) {
		end = 8 + rs
	}
	for off+8 <= end {
		id := string(data[off : off+4])
		size := int(le.Uint32(data[off+4 : off+8]))
		off += 8
		if size < 0 || off+size > end {
			if fmtSeen && raw != nil {
				break // junk after the chunks that matter
			}
			return nil, 0, fmt.Errorf("core: WAV chunk %q overruns the file", id)
		}
		body := data[off : off+size]
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, fmt.Errorf("core: WAV fmt chunk too short (%d bytes)", size)
			}
			format = int(le.Uint16(body[0:2]))
			channels = int(le.Uint16(body[2:4]))
			rate = int(le.Uint32(body[4:8]))
			bits = int(le.Uint16(body[14:16]))
			fmtSeen = true
		case "data":
			raw = body
		}
		off += size + size&1 // chunks are word-aligned
	}
	switch {
	case !fmtSeen:
		return nil, 0, fmt.Errorf("core: WAV has no fmt chunk")
	case raw == nil:
		return nil, 0, fmt.Errorf("core: WAV has no data chunk")
	case format != 1:
		return nil, 0, fmt.Errorf("core: unsupported WAV format %d (want 1 = PCM)", format)
	case bits != 8 && bits != 16:
		return nil, 0, fmt.Errorf("core: unsupported WAV bit depth %d (want 8 or 16)", bits)
	case channels < 1:
		return nil, 0, fmt.Errorf("core: WAV declares %d channels", channels)
	case rate < wavMinRate:
		return nil, 0, fmt.Errorf("core: WAV sample rate %d Hz too low (need ≥ %d)", rate, wavMinRate)
	}
	frame := channels * bits / 8
	n := len(raw) / frame
	if n == 0 {
		return nil, 0, fmt.Errorf("core: WAV data chunk holds no samples")
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		sum := 0.0
		for c := 0; c < channels; c++ {
			at := i*frame + c*bits/8
			if bits == 8 {
				sum += (float64(raw[at]) - 128) / 128
			} else {
				sum += float64(int16(le.Uint16(raw[at:]))) / 32768
			}
		}
		out[i] = sum / float64(channels)
	}
	return out, rate, nil
}

// detectWAVPulses finds biphase pulses: after DC removal, an excursion
// beyond the threshold in either polarity, followed within a pulse-half
// window by the opposite excursion, both with plausible durations. The
// reported start T-state is the first excursion's onset.
func detectWAVPulses(x []float64, rate int) []uint64 {
	// DC blocker.
	y := make([]float64, len(x))
	var prevX, prevY float64
	for i, v := range x {
		yy := v - prevX + wavDCPole*prevY
		prevX, prevY = v, yy
		y[i] = yy
	}
	peak := 0.0
	for _, v := range y {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	if peak == 0 {
		return nil
	}
	thr := peak * wavThresholdFrac

	toT := func(i int) uint64 { return uint64(i) * CPUClockHz / uint64(rate) }
	toSamples := func(t uint64) int {
		return int(t * uint64(rate) / CPUClockHz)
	}
	minPhase := toSamples(wavMinPhaseT)
	maxPhase := toSamples(wavMaxPhaseT) + 2

	var starts []uint64
	i := 0
	for i < len(y) {
		if math.Abs(y[i]) <= thr {
			i++
			continue
		}
		sign := 1.0
		if y[i] < 0 {
			sign = -1
		}
		start := i
		for i < len(y) && sign*y[i] > thr {
			i++
		}
		p1 := i - start
		// Cross to the opposite excursion within one phase width.
		gap := i
		for i < len(y) && i-gap <= maxPhase && sign*y[i] >= -thr {
			i++
		}
		if i >= len(y) || i-gap > maxPhase {
			continue
		}
		opp := i
		for i < len(y) && sign*y[i] < -thr {
			i++
		}
		p2 := i - opp
		if p1 < minPhase || p1 > maxPhase || p2 < minPhase || p2 > maxPhase {
			continue
		}
		t := toT(start)
		if n := len(starts); n > 0 && t-starts[n-1] < wavMergeGapT {
			continue
		}
		starts = append(starts, t)
	}
	return starts
}

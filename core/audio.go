package core

// Audio: the machine's sound output as a pulled sample stream. The
// stock Galaksija has no sound hardware — games clicked and beeped by
// telling the user to plug a speaker into the cassette port, driven by
// the tape-out DAC (latch bits 6/2, three levels; SPEC.md §3.2). A
// frontend enables audio and pulls RenderAudio once per frame; the
// stream is the machine's mono mix, so a future sound source (the
// Galaksija Plus AY-3-8910 PSG) mixes into the same call without any
// frontend changes. Observational: never affects execution, excluded
// from snapshots; a rewind resets the stream at the next render.

const (
	// sndAmplitude scales the DAC's three levels (low/mid/high →
	// −1/0/+1) into sample space; a full-scale square wave is harsh.
	sndAmplitude = 0.25
	// sndPendingCap bounds the transition backlog. A frontend that
	// enables audio and stops pulling gets a reset (one audible
	// glitch), not unbounded growth.
	sndPendingCap = 1 << 16
)

// sndEvent is one tape-out level change.
type sndEvent struct {
	t     uint64
	level int8 // 0 low, 1 mid, 2 high (tapeOutLevel)
}

// EnableAudio starts capturing the machine's sound output for
// RenderAudio, from the current instant.
func (m *Machine) EnableAudio() {
	m.sndEnabled = true
	m.sndPending = m.sndPending[:0]
	m.sndAt = m.tstates
	m.sndLevel = int8(tapeOutLevel(m.latch))
}

// DisableAudio stops the capture and drops anything pending.
func (m *Machine) DisableAudio() {
	m.sndEnabled = false
	m.sndPending = nil
}

// AudioEnabled reports whether audio capture is on.
func (m *Machine) AudioEnabled() bool { return m.sndEnabled }

// soundLatch records a tape-out level change; next is the incoming
// latch value, m.latch still holds the outgoing one.
func (m *Machine) soundLatch(next byte) {
	lvl := int8(tapeOutLevel(next))
	if lvl == int8(tapeOutLevel(m.latch)) {
		return
	}
	if len(m.sndPending) >= sndPendingCap {
		m.sndPending = m.sndPending[:0]
		m.sndAt = m.tstates
	}
	m.sndPending = append(m.sndPending, sndEvent{t: m.tstates, level: lvl})
}

// samplePos maps a machine T-state to an absolute sample index at rate,
// without overflowing for arbitrarily long sessions.
func samplePos(t uint64, rate int) uint64 {
	r := uint64(rate)
	return t/CPUClockHz*r + t%CPUClockHz*r/CPUClockHz
}

// RenderAudio returns the machine's mono audio — float32 samples in
// [-1, 1] at sampleRate — covering exactly the machine time elapsed
// since the previous call (or EnableAudio). Deterministic: identical
// runs render identical samples. After a rewind it resynchronizes and
// returns nil once.
func (m *Machine) RenderAudio(sampleRate int) []float32 {
	if !m.sndEnabled || sampleRate <= 0 {
		return nil
	}
	now := m.tstates
	if now < m.sndAt { // the machine went backwards (rewind/restore)
		m.sndAt = now
		m.sndPending = m.sndPending[:0]
		m.sndLevel = int8(tapeOutLevel(m.latch))
		return nil
	}
	base := samplePos(m.sndAt, sampleRate)
	out := make([]float32, samplePos(now, sampleRate)-base)
	cur := 0
	for _, e := range m.sndPending {
		end := int(samplePos(e.t, sampleRate) - base)
		for ; cur < end && cur < len(out); cur++ {
			out[cur] = float32(m.sndLevel-1) * sndAmplitude
		}
		m.sndLevel = e.level
	}
	for ; cur < len(out); cur++ {
		out[cur] = float32(m.sndLevel-1) * sndAmplitude
	}
	m.sndPending = m.sndPending[:0]
	m.sndAt = now
	return out
}

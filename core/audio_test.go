package core

import "testing"

// saveAudio boots, types a program, enables audio, and renders the
// SAVE's sound frame by frame at rate — the cassette-port speaker
// path. It also returns the rendered T-state window [t0, t1).
func saveAudio(t *testing.T, rate int) (out []float32, t0, t1 uint64) {
	t.Helper()
	m := bootMachine(t)
	typeString(m, "10 PRINT 1\n")
	m.RunTstates(100 * TstatesPerFrame)
	m.EnableAudio()
	t0 = m.Tstates()
	typeString(m, "SAVE\n")
	for i := 0; i < 400; i++ {
		m.RunTstates(TstatesPerFrame)
		out = append(out, m.RenderAudio(rate)...)
	}
	return out, t0, m.Tstates()
}

// TestAudioSaveWaveform: a SAVE is audible — both DAC polarities show
// up — and the sample count covers the rendered window exactly.
func TestAudioSaveWaveform(t *testing.T) {
	out, t0, t1 := saveAudio(t, 44100)
	if want := int(samplePos(t1, 44100) - samplePos(t0, 44100)); len(out) != want {
		t.Fatalf("rendered %d samples, want %d", len(out), want)
	}
	pos, neg := 0, 0
	for _, v := range out {
		switch {
		case v > 0.2:
			pos++
		case v < -0.2:
			neg++
		}
	}
	if pos == 0 || neg == 0 {
		t.Fatalf("SAVE produced no square wave (pos=%d neg=%d)", pos, neg)
	}
}

// TestAudioDeterministic: identical runs render identical samples.
func TestAudioDeterministic(t *testing.T) {
	a, _, _ := saveAudio(t, 22050)
	b, _, _ := saveAudio(t, 22050)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs: %v vs %v", i, a[i], b[i])
		}
	}
}

// TestAudioRewindResync: after the machine jumps backwards, the next
// render returns nil once and the stream continues from the new now.
func TestAudioRewindResync(t *testing.T) {
	m := bootMachine(t)
	m.EnableHistory(10*TstatesPerFrame, 20)
	m.EnableAudio()
	m.RunTstates(50 * TstatesPerFrame)
	m.RenderAudio(44100)
	if err := m.Rewind(20 * TstatesPerFrame); err != nil {
		t.Fatal(err)
	}
	if got := m.RenderAudio(44100); got != nil {
		t.Fatalf("render after rewind = %d samples, want nil resync", len(got))
	}
	before := m.Tstates()
	m.RunTstates(5 * TstatesPerFrame)
	want := int(samplePos(m.Tstates(), 44100) - samplePos(before, 44100))
	if got := m.RenderAudio(44100); len(got) != want {
		t.Fatalf("post-resync render = %d samples, want %d", len(got), want)
	}
}

// TestAudioDisabled: no capture unless enabled; disable drops state.
func TestAudioDisabled(t *testing.T) {
	m := bootMachine(t)
	m.RunTstates(10 * TstatesPerFrame)
	if got := m.RenderAudio(44100); got != nil {
		t.Fatal("render without EnableAudio returned samples")
	}
	m.EnableAudio()
	if !m.AudioEnabled() {
		t.Fatal("AudioEnabled = false after enable")
	}
	m.DisableAudio()
	if m.AudioEnabled() || m.sndPending != nil {
		t.Fatal("disable did not drop the capture")
	}
}

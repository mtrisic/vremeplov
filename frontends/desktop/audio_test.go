package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
	"testing"
)

func f32le(vals ...float32) []byte {
	var b bytes.Buffer
	for _, v := range vals {
		binary.Write(&b, binary.LittleEndian, v)
	}
	return b.Bytes()
}

// TestAudioPipeRoundtrip: mono pushes come out as interleaved f32le
// stereo, byte-exact.
func TestAudioPipeRoundtrip(t *testing.T) {
	p := &audioPipe{}
	p.push([]float32{0.25, -0.5})
	got := make([]byte, 16)
	if n, err := p.Read(got); n != 16 || err != nil {
		t.Fatalf("Read = (%d, %v), want (16, nil)", n, err)
	}
	want := f32le(0.25, 0.25, -0.5, -0.5)
	if !bytes.Equal(got, want) {
		t.Fatalf("stereo bytes = % x, want % x", got, want)
	}
}

// TestAudioPipeUnderrun: an empty (or short) FIFO reads full-length
// silence, never an error — the player must keep running.
func TestAudioPipeUnderrun(t *testing.T) {
	p := &audioPipe{}
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if n, err := p.Read(b); n != len(b) || err != nil {
		t.Fatalf("Read = (%d, %v), want (%d, nil)", n, err, len(b))
	}
	for i, v := range b {
		if v != 0 {
			t.Fatalf("byte %d = %d, want silence", i, v)
		}
	}
	p.push([]float32{1})
	long := make([]byte, 24) // one pushed frame + 16 bytes of underrun
	if n, err := p.Read(long); n != 24 || err != nil {
		t.Fatalf("Read = (%d, %v), want (24, nil)", n, err)
	}
	if !bytes.Equal(long[:8], f32le(1, 1)) {
		t.Fatalf("pushed frame = % x, want % x", long[:8], f32le(1, 1))
	}
	for i := 8; i < 24; i++ {
		if long[i] != 0 {
			t.Fatalf("underrun byte %d = %d, want silence", i, long[i])
		}
	}
}

// TestAudioPipeBacklogCap: overflow drops the oldest samples, keeping
// latency bounded.
func TestAudioPipeBacklogCap(t *testing.T) {
	p := &audioPipe{}
	chunk := make([]float32, sndRate/10) // 100 ms per push
	for i := 0; i < 5; i++ {             // 500 ms total
		for j := range chunk {
			chunk[j] = float32(i)
		}
		p.push(chunk)
	}
	if len(p.buf) != sndBacklogCap {
		t.Fatalf("backlog = %d bytes, want capped at %d", len(p.buf), sndBacklogCap)
	}
	// The head of the FIFO must be recent data, not the first chunk.
	head := make([]byte, 8)
	p.Read(head)
	var v float32
	binary.Read(bytes.NewReader(head), binary.LittleEndian, &v)
	if v == 0 {
		t.Fatal("cap kept the oldest samples; should have dropped them")
	}
}

func TestAudioPipeDrop(t *testing.T) {
	p := &audioPipe{}
	p.push([]float32{1, 2, 3})
	p.drop()
	b := make([]byte, 8)
	p.Read(b)
	for i, v := range b {
		if v != 0 {
			t.Fatalf("byte %d = %d after drop, want silence", i, v)
		}
	}
}

// TestAudioPipeConcurrent: the player goroutine reads while Update
// pushes — run under -race in the gate.
func TestAudioPipeConcurrent(t *testing.T) {
	p := &audioPipe{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		mono := make([]float32, 960)
		for i := 0; i < 200; i++ {
			p.push(mono)
		}
	}()
	go func() {
		defer wg.Done()
		b := make([]byte, 4096)
		for i := 0; i < 200; i++ {
			if n, err := p.Read(b); n != len(b) || err != nil {
				t.Errorf("Read = (%d, %v)", n, err)
				return
			}
		}
	}()
	wg.Wait()
}

// Silence must really be the zero float32 pattern (not a denormal or
// negative zero surprise from the conversion).
func TestF32LEZero(t *testing.T) {
	if bits := math.Float32bits(0); bits != 0 {
		t.Fatalf("float32(0) bits = %x", bits)
	}
}

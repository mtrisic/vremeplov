package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtrisic/vremeplov/core"
)

func TestParseKeyScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.keys")
	script := `# boot, then type R
1000 down R
# hold across a comment
4000 up R
5000 down 0x31
6000 up BREAK
`
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := parseKeyScript(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.KeyEvent{
		{Tstate: 1000, Key: core.KeyR, Down: true},
		{Tstate: 4000, Key: core.KeyR, Down: false},
		{Tstate: 5000, Key: core.KeyBreak, Down: true},
		{Tstate: 6000, Key: core.KeyBreak, Down: false},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestParseKeyScriptErrors(t *testing.T) {
	for _, bad := range []string{
		"notanumber down R",
		"1000 sideways R",
		"1000 down NOSUCHKEY",
		"1000 down",
	} {
		path := filepath.Join(t.TempDir(), "bad.keys")
		if err := os.WriteFile(path, []byte(bad+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := parseKeyScript(path); err == nil {
			t.Errorf("script %q parsed without error", bad)
		}
	}
}

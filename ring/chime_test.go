package ring

import (
	"os"
	"path/filepath"
	"testing"
)

// s16le helper: one stereo frame with the given sample in both channels.
func frame(s int16) []byte {
	lo, hi := byte(uint16(s)), byte(uint16(s)>>8)
	return []byte{lo, hi, lo, hi}
}

func TestGainedChime(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "ding.pcm")
	pcm := append(frame(10000), frame(-20000)...)
	if err := os.WriteFile(src, pcm, 0o644); err != nil {
		t.Fatal(err)
	}

	// 100% and built-in names pass through untouched.
	if got := GainedChime(src, 100); got != src {
		t.Fatalf("100%% = %q, want source", got)
	}
	if got := GainedChime("shine", 50); got != "shine" {
		t.Fatalf("built-in = %q, want passthrough", got)
	}

	// 50% halves the samples into a cached sibling file.
	out := GainedChime(src, 50)
	if out == src {
		t.Fatal("50% did not produce a gained copy")
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := append(frame(5000), frame(-10000)...)
	if string(b) != string(want) {
		t.Fatalf("gained samples = %v, want %v", b, want)
	}

	// A different volume replaces the old cache file.
	out2 := GainedChime(src, 30)
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("old cache %s not removed", out)
	}
	if _, err := os.Stat(out2); err != nil {
		t.Fatal(err)
	}

	// 0 means DefaultVolume, not passthrough.
	if got := GainedChime(src, 0); got == src {
		t.Fatal("0 should gain at DefaultVolume")
	}
}

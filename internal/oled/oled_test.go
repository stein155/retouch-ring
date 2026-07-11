package oled

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTextDrawsPixels(t *testing.T) {
	c := NewCanvas()
	c.Text(0, 0, "A", 255)
	if !bytes.Contains(c.Pix(), []byte{255}) {
		t.Fatal("Text drew nothing")
	}
	blank := NewCanvas()
	blank.Text(0, 0, " ", 255)
	if bytes.Contains(blank.Pix(), []byte{255}) {
		t.Fatal("space drew pixels")
	}
}

func TestSetClips(t *testing.T) {
	c := NewCanvas()
	c.Set(-1, 0, 255)
	c.Set(Width, Height, 255)
	if bytes.Contains(c.Pix(), []byte{255}) {
		t.Fatal("out-of-bounds Set drew pixels")
	}
}

func TestWrap(t *testing.T) {
	got := Wrap("MORGEN WORDT GROENAFVAL OPGEHAALD", 20, 3)
	want := []string{"MORGEN WORDT", "GROENAFVAL OPGEHAALD"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q", got)
	}
	if got := Wrap("A B C D", 1, 2); len(got) != 2 {
		t.Fatalf("line cap: got %q", got)
	}
}

func TestSpriteSize(t *testing.T) {
	if w, h := SpriteSize([]string{"##", "####"}); w != 4 || h != 2 {
		t.Fatalf("got %dx%d", w, h)
	}
}

func TestFramebufferBackupRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fb0")
	orig := bytes.Repeat([]byte{7}, Width*Height)
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	fb := NewFramebuffer(path)

	if err := fb.Draw(make([]byte, 10)); err == nil {
		t.Fatal("short frame accepted")
	}
	frame := bytes.Repeat([]byte{200}, Width*Height)
	if err := fb.Draw(frame); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, frame) {
		t.Fatal("frame not written")
	}
	// second draw must not overwrite the backup with our own frame
	if err := fb.Draw(frame); err != nil {
		t.Fatal(err)
	}
	if err := fb.Restore(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, orig) {
		t.Fatal("original contents not restored")
	}
	// restore without draw is a no-op
	if err := fb.Restore(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, orig) {
		t.Fatal("no-op restore changed contents")
	}
}

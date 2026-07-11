// Package oled draws to the SoundTouch front-panel OLED framebuffer.
//
// The display is a 128x100 8-bit grayscale panel exposed as /dev/fb0. The
// package provides a small Canvas (text in a builtin 5x7 font, rectangles,
// ASCII-art sprites) and a Framebuffer that backs up the panel contents on
// the first draw and restores them afterwards, so a plugin or feature can
// borrow the display while the speaker is in standby and hand it back
// untouched.
package oled

import (
	"fmt"
	"image"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

// Panel dimensions of the SoundTouch 20 OLED.
const (
	Width  = 128
	Height = 100
)

// Canvas is an 8-bit grayscale drawing surface at panel size.
type Canvas struct {
	img *image.Gray
}

func NewCanvas() *Canvas {
	return &Canvas{img: image.NewGray(image.Rect(0, 0, Width, Height))}
}

// Pix returns the raw framebuffer bytes (row-major, one byte per pixel).
func (c *Canvas) Pix() []byte { return c.img.Pix }

// Image exposes the underlying image, e.g. for PNG previews in tests.
func (c *Canvas) Image() *image.Gray { return c.img }

func (c *Canvas) Set(x, y int, v byte) {
	if x >= 0 && x < Width && y >= 0 && y < Height {
		c.img.Pix[y*Width+x] = v
	}
}

func (c *Canvas) Fill(x0, y0, x1, y1 int, v byte) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c.Set(x, y, v)
		}
	}
}

// Rect draws a 1px outline.
func (c *Canvas) Rect(x0, y0, x1, y1 int, v byte) {
	for x := x0; x <= x1; x++ {
		c.Set(x, y0, v)
		c.Set(x, y1, v)
	}
	for y := y0; y <= y1; y++ {
		c.Set(x0, y, v)
		c.Set(x1, y, v)
	}
}

// Text draws s at (x, y) in the builtin 5x7 font (6px advance). Lowercase is
// mapped to uppercase and common accented Latin letters are folded to their
// base letter (the font is A-Z only); anything left without a glyph renders
// as '?'.
func (c *Canvas) Text(x, y int, s string, v byte) {
	for _, r := range strings.ToUpper(foldAccents.Replace(s)) {
		glyph, ok := font[r]
		if !ok {
			glyph = font['?']
		}
		for row, bits := range glyph {
			for col := 0; col < 5; col++ {
				if bits&(1<<(4-col)) != 0 {
					c.Set(x+col, y+row, v)
				}
			}
		}
		x += 6
	}
}

// TextCentered draws s horizontally centered at height y.
func (c *Canvas) TextCentered(y int, s string, v byte) {
	s = foldAccents.Replace(s)
	c.Text((Width-utf8.RuneCountInString(s)*6)/2, y, s, v)
}

// Sprite draws ASCII art at (x, y): '#' pixels get fg, '+' pixels accent,
// anything else is transparent.
func (c *Canvas) Sprite(x, y int, art []string, fg, accent byte) {
	for row, line := range art {
		for col, ch := range line {
			switch ch {
			case '#':
				c.Set(x+col, y+row, fg)
			case '+':
				c.Set(x+col, y+row, accent)
			}
		}
	}
}

// SpriteSize returns the bounding size of ASCII art.
func SpriteSize(art []string) (w, h int) {
	for _, line := range art {
		if len(line) > w {
			w = len(line)
		}
	}
	return w, len(art)
}

// Wrap greedily wraps s into at most n lines of max characters; overflow
// lines are dropped.
func Wrap(s string, max, n int) []string {
	var out []string
	cur := ""
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = w
		case len(cur)+len(w)+1 > max:
			out = append(out, cur)
			cur = w
		default:
			cur += " " + w
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// TextHeight is the line advance Wrap output is typically drawn at.
const TextHeight = 13

// Framebuffer writes frames to the panel and can restore what was on it.
// The first Draw snapshots the current contents; Restore puts them back.
// Safe for concurrent use.
type Framebuffer struct {
	path string

	mu      sync.Mutex
	backup  []byte
	drawing bool
}

// NewFramebuffer targets path (normally /dev/fb0). It does not open the
// device until the first Draw.
func NewFramebuffer(path string) *Framebuffer {
	return &Framebuffer{path: path}
}

// Available reports whether path is a readable framebuffer with the panel's
// exact size. Only the SoundTouch 20 exposes a 128x100 grayscale panel this
// way; on other models (or off-speaker) this returns false and callers must
// skip drawing entirely.
func Available(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && len(b) == Width*Height
}

// Draw writes a full frame (Width*Height bytes) to the panel, snapshotting
// the previous contents on the first call since the last Restore.
func (f *Framebuffer) Draw(pix []byte) error {
	if len(pix) != Width*Height {
		return fmt.Errorf("oled: frame is %d bytes, want %d", len(pix), Width*Height)
	}
	f.mu.Lock()
	if !f.drawing {
		if b, err := os.ReadFile(f.path); err == nil && len(b) == Width*Height {
			f.backup = append([]byte(nil), b...)
		}
		f.drawing = true
	}
	f.mu.Unlock()
	return os.WriteFile(f.path, pix, 0o600)
}

// Restore writes the snapshot taken by the first Draw back to the panel.
// A no-op when nothing was drawn since the last Restore.
func (f *Framebuffer) Restore() error {
	f.mu.Lock()
	b := f.backup
	wasDrawing := f.drawing
	f.drawing = false
	f.backup = nil
	f.mu.Unlock()
	if wasDrawing && len(b) == Width*Height {
		return os.WriteFile(f.path, b, 0o600)
	}
	return nil
}

var foldAccents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "Á", "A", "À", "A", "Â", "A", "Ä", "A", "Ã", "A",
	"é", "e", "è", "e", "ê", "e", "ë", "e", "É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "Í", "I", "Ì", "I", "Î", "I", "Ï", "I",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "Ó", "O", "Ò", "O", "Ô", "O", "Ö", "O", "Õ", "O",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "Ú", "U", "Ù", "U", "Û", "U", "Ü", "U",
	"ñ", "n", "Ñ", "N", "ç", "c", "Ç", "C", "ß", "ss", "’", "'",
)

var font = map[rune][]byte{
	' ': {0, 0, 0, 0, 0, 0, 0}, '?': {14, 17, 1, 2, 4, 0, 4}, '-': {0, 0, 0, 31, 0, 0, 0}, '/': {1, 2, 4, 8, 16, 0, 0},
	'.': {0, 0, 0, 0, 0, 0, 4}, ',': {0, 0, 0, 0, 0, 4, 8}, ':': {0, 0, 4, 0, 0, 4, 0}, '!': {4, 4, 4, 4, 4, 0, 4},
	'\'': {4, 4, 8, 0, 0, 0, 0}, '(': {2, 4, 8, 8, 8, 4, 2}, ')': {8, 4, 2, 2, 2, 4, 8},
	'0': {14, 17, 19, 21, 25, 17, 14}, '1': {4, 12, 4, 4, 4, 4, 14}, '2': {14, 17, 1, 2, 4, 8, 31}, '3': {30, 1, 1, 14, 1, 1, 30}, '4': {2, 6, 10, 18, 31, 2, 2}, '5': {31, 16, 30, 1, 1, 17, 14}, '6': {6, 8, 16, 30, 17, 17, 14}, '7': {31, 1, 2, 4, 8, 8, 8}, '8': {14, 17, 17, 14, 17, 17, 14}, '9': {14, 17, 17, 15, 1, 2, 12},
	'A': {14, 17, 17, 31, 17, 17, 17}, 'B': {30, 17, 17, 30, 17, 17, 30}, 'C': {14, 17, 16, 16, 16, 17, 14}, 'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'F': {31, 16, 16, 30, 16, 16, 16}, 'G': {14, 17, 16, 23, 17, 17, 14}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'I': {14, 4, 4, 4, 4, 4, 14}, 'J': {7, 2, 2, 2, 18, 18, 12}, 'K': {17, 18, 20, 24, 20, 18, 17}, 'L': {16, 16, 16, 16, 16, 16, 31}, 'M': {17, 27, 21, 21, 17, 17, 17}, 'N': {17, 25, 21, 19, 17, 17, 17}, 'O': {14, 17, 17, 17, 17, 17, 14}, 'P': {30, 17, 17, 30, 16, 16, 16}, 'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17}, 'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4}, 'U': {17, 17, 17, 17, 17, 17, 14}, 'V': {17, 17, 17, 17, 17, 10, 4}, 'W': {17, 17, 17, 21, 21, 21, 10}, 'X': {17, 17, 10, 4, 10, 17, 17}, 'Y': {17, 17, 10, 4, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31},
}

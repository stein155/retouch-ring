package ring

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/stein155/retouch-ring/internal/oled"
)

func TestRenderEvent(t *testing.T) {
	if got := len(renderEvent("nl", "ding", "Voordeur")); got != oled.Width*oled.Height {
		t.Fatalf("frame is %d bytes, want %d", got, oled.Width*oled.Height)
	}
}

func TestTrFallback(t *testing.T) {
	if got := Tr("nl", "oled.ding.at", "Voordeur"); got != "Er is iemand bij Voordeur" {
		t.Fatalf("got %q", got)
	}
	if got := Tr("nl", "oled.motion.at", "Voordeur"); got != "Beweging bij Voordeur" {
		t.Fatalf("got %q", got)
	}
	if got := Tr("xx", "oled.motion"); got != "Motion detected" {
		t.Fatalf("fallback: got %q", got)
	}
}

// TestDumpScreens writes preview PNGs to the ICONDUMP dir for visual checks.
func TestDumpScreens(t *testing.T) {
	dir := os.Getenv("ICONDUMP")
	if dir == "" {
		t.Skip("set ICONDUMP to dump preview PNGs")
	}
	for name, frame := range map[string][]byte{
		"ring-ding":    renderEvent("nl", "ding", "Voordeur"),
		"ring-motion":  renderEvent("nl", "motion", "Voordeur"),
		"ring-generic": renderEvent("nl", "ding", ""),
	} {
		img := &image.Gray{Pix: frame, Stride: oled.Width, Rect: image.Rect(0, 0, oled.Width, oled.Height)}
		f, err := os.Create(dir + "/" + name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

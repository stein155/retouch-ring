package ring

import "testing"

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

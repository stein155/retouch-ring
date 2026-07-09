package go_fcm_receiver

import (
	"regexp"
	"testing"
)

func TestChromeWebAppID(t *testing.T) {
	got := chromeWebAppID()
	want := regexp.MustCompile(`^wp:receiver\.push\.com#[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !want.MatchString(got) {
		t.Fatalf("chromeWebAppID() = %q", got)
	}
}

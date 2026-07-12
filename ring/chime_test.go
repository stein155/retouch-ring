package ring

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPlayChime(t *testing.T) {
	dir := t.TempDir()
	chime := filepath.Join(dir, "ding.mp3")
	const payload = "ID3fake-mp3-bytes"
	if err := os.WriteFile(chime, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotPath, gotQuery, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := PlayChime(srv.URL, chime, 40, "Voordeur"); err != nil {
		t.Fatalf("PlayChime: %v", err)
	}
	if gotPath != "/api/speaker/notify" {
		t.Errorf("path = %q, want /api/speaker/notify", gotPath)
	}
	if gotCT != "audio/mpeg" {
		t.Errorf("content-type = %q, want audio/mpeg", gotCT)
	}
	if gotBody != payload {
		t.Errorf("body = %q, want the chime bytes", gotBody)
	}
	// Volume, artist and the device label ride along as query params.
	for _, want := range []string{"volume=40", "artist=Ring", "track=Voordeur"} {
		if !containsParam(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestPlayChimeErrors(t *testing.T) {
	if err := PlayChime("", "x.mp3", 30, ""); err == nil {
		t.Error("empty host URL should error")
	}
	if err := PlayChime("http://x", "", 30, ""); err == nil {
		t.Error("empty chime should error")
	}
	if err := PlayChime("http://x", "/no/such/file.mp3", 30, ""); err == nil {
		t.Error("missing chime file should error")
	}
}

func TestPlayChimeSurfacesHTTPError(t *testing.T) {
	dir := t.TempDir()
	chime := filepath.Join(dir, "ding.mp3")
	if err := os.WriteFile(chime, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The firmware answers 403 on models without /speaker support.
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if err := PlayChime(srv.URL, chime, 30, ""); err == nil {
		t.Error("a non-2xx response should surface as an error")
	}
}

func containsParam(query, want string) bool {
	for _, p := range splitAmp(query) {
		if p == want {
			return true
		}
	}
	return false
}

func splitAmp(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

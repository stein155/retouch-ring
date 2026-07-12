package ring

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

// DefaultVolume is the chime volume used when none is configured. ReTouch's
// /speaker notification takes a 10–70 level (the firmware's accepted window),
// so 30 is a comfortable indoor default.
const DefaultVolume = 30

// PlayChime plays a chime through ReTouch's audio-notification API: it uploads the
// chime's audio bytes to hostURL and ReTouch drives the speaker's /speaker endpoint,
// which ducks the music, plays the clip, and resumes. hostURL is ReTouch's own base
// URL (--host-url); chimePath is an audio file (mp3/aac/...). track is the label the
// speaker shows while the clip plays (e.g. the Ring device name).
func PlayChime(hostURL, chimePath string, volume int, track string) error {
	if hostURL == "" {
		return fmt.Errorf("no ReTouch host URL configured")
	}
	if chimePath == "" {
		return fmt.Errorf("no chime configured")
	}
	audio, err := os.ReadFile(chimePath)
	if err != nil {
		return fmt.Errorf("read chime %q: %w", chimePath, err)
	}
	if volume <= 0 {
		volume = DefaultVolume
	}
	q := url.Values{}
	q.Set("volume", strconv.Itoa(volume))
	q.Set("artist", "Ring")
	if track != "" {
		q.Set("track", track)
	}
	endpoint := hostURL + "/api/speaker/notify?" + q.Encode()
	req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(audio))
	req.Header.Set("Content-Type", "audio/mpeg")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("speaker notify HTTP %d", resp.StatusCode)
	}
	return nil
}

package ring

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DefaultVolume is the chime gain (percent) used when none is configured.
// /playNotification plays at a fixed, fairly loud firmware level; 30% is a
// comfortable indoor default.
const DefaultVolume = 30

// playChime fires the firmware notification that ducks the music and plays a clip.
// chime is either a built-in name (-> /opt/Bose/chimes/<name>.pcm) or an absolute
// path (e.g. /mnt/nv/doorbell.pcm). The firmware controls the duck; we just trigger it.
func playChime(speaker, chime string) error {
	body := chimeBody(chime)
	req, _ := http.NewRequest(http.MethodPost, "http://"+speaker+"/playNotification", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("playNotification HTTP %d", resp.StatusCode)
	}
	return nil
}

func chimeBody(chime string) string {
	path := chime
	if !strings.HasPrefix(path, "/") {
		path = "/opt/Bose/chimes/" + chime + ".pcm"
	}
	return fmt.Sprintf(`<audioSource pathToFile="%s"/>`, path)
}

// GainedChime returns the chime path to play at vol percent (0 = DefaultVolume,
// 100 = the clip as-is). The firmware plays notifications at a fixed level, so
// volume is baked into the samples: a gained copy of the clip is written next
// to the original (<name>.vol<NN>.pcm) and reused until the source changes.
// Built-in chime names live on a read-only mount and play ungained.
func GainedChime(chime string, vol int) string {
	if vol <= 0 {
		vol = DefaultVolume
	}
	if vol == 100 || !strings.HasPrefix(chime, "/") || !strings.HasSuffix(chime, ".pcm") {
		return chime
	}
	base := strings.TrimSuffix(chime, ".pcm")
	out := fmt.Sprintf("%s.vol%d.pcm", base, vol)
	si, err := os.Stat(chime)
	if err != nil {
		return chime
	}
	if oi, err := os.Stat(out); err == nil && oi.ModTime().After(si.ModTime()) {
		return out
	}
	src, err := os.ReadFile(chime)
	if err != nil {
		return chime
	}
	// s16le samples, gain with clipping (the /playNotification PCM is headerless).
	g := float64(vol) / 100
	dst := make([]byte, len(src)&^1)
	for i := 0; i+1 < len(src); i += 2 {
		v := float64(int16(uint16(src[i])|uint16(src[i+1])<<8)) * g
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		s := int16(v)
		dst[i] = byte(uint16(s))
		dst[i+1] = byte(uint16(s) >> 8)
	}
	// Drop caches for other volumes so old settings don't pile up on flash.
	if old, err := filepath.Glob(base + ".vol*.pcm"); err == nil {
		for _, f := range old {
			_ = os.Remove(f)
		}
	}
	if err := os.WriteFile(out, dst, 0o644); err != nil {
		return chime
	}
	return out
}

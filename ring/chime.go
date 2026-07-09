package ring

import (
	"fmt"
	"net/http"
	"strings"
)

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

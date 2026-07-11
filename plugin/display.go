package plugin

// Display notifications go through ReTouch's loopback display API — the
// plugin never touches /dev/fb0. GET /api/display says whether this speaker
// has the ST20 panel; POST /api/display/notify shows the bell/motion screen
// for a few seconds and ReTouch restores the panel afterwards.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stein155/retouch-ring/ring"
)

// probeDisplay asks ReTouch whether this speaker has the ST20 panel.
func (p *Plugin) probeDisplay() bool {
	if p.hostURL == "" {
		return false
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(p.hostURL + "/api/display")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v struct {
		Available bool `json:"available"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&v)
	return v.Available
}

// notifyDisplay shows the doorbell/motion notification. Wired into the agent
// via ring.NotifyFunc; also used by the test action.
func (p *Plugin) notifyDisplay(kind, device string) {
	if !p.hasOLED {
		return
	}
	lang := p.language()
	icon, text := "bell", ring.Tr(lang, "oled.ding")
	if device != "" {
		text = ring.Tr(lang, "oled.ding.at", device)
	}
	if kind == "motion" {
		icon = "motion"
		text = ring.Tr(lang, "oled.motion")
		if device != "" {
			text = ring.Tr(lang, "oled.motion.at", device)
		}
	}
	body, _ := json.Marshal(map[string]any{"icon": icon, "text": text, "seconds": 8})
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Post(p.hostURL+"/api/display/notify", "application/json", strings.NewReader(string(body)))
	if err != nil {
		p.log.Printf("display notify failed: %v", err)
		return
	}
	resp.Body.Close()
}

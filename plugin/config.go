package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config mirrors the JSON the ring agent (package ring) reads from config.json. The
// plugin owns this file: it writes credentials + device choices here, and the agent
// reads the same path. Only the fields the plugin sets are modelled; FCM creds are
// carried opaquely so the agent's own rotation is preserved across our writes.
type Config struct {
	RefreshToken string          `json:"refresh_token"`
	HardwareID   string          `json:"hardware_id"`
	Speaker      string          `json:"speaker"`
	HostURL      string          `json:"host_url"` // ReTouch base URL; where the agent plays chimes
	Chime        string          `json:"chime"`
	DingChime    string          `json:"ding_chime"`
	DebounceSec  int             `json:"debounce_sec"`
	NoOled       bool            `json:"no_oled,omitempty"`
	Volume       int             `json:"volume,omitempty"` // chime volume (10–70); 0 = ring.DefaultVolume
	Devices      []Device        `json:"devices"`
	FCM          json.RawMessage `json:"fcm,omitempty"`
}

// Device is one Ring device's per-event chime preference (by Ring doorbot id).
type Device struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Motion bool   `json:"motion"`
	Ding   bool   `json:"ding"`
}

func loadConfig(path string) Config {
	var c Config
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// save writes config.json atomically so a crash mid-write never truncates it. The
// temp name deliberately differs from the ring agent's (.config.tmp): both sides
// write the same config.json, and a shared temp path would let one writer rename
// the other's half-written file into place.
func saveConfig(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), ".config.plugin.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

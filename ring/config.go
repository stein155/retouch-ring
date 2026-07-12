package ring

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// Config is the agent's on-device state, stored as JSON in /mnt/nv/soundtouch-ring/config.json.
// The Ring refresh token and the FCM credentials both rotate/persist here so the agent
// re-uses them across reboots instead of re-authenticating every start.
type Config struct {
	RefreshToken string `json:"refresh_token"` // Ring OAuth; rotates each refresh
	HardwareID   string `json:"hardware_id"`   // stable per install (minted by ring-auth)

	Speaker     string `json:"speaker"`           // SoundTouch API host, e.g. 127.0.0.1:8090
	HostURL     string `json:"host_url"`          // ReTouch base URL; where chimes are played (/api/speaker/notify)
	Chime       string `json:"chime"`             // motion chime: /mnt/nv/foo.mp3
	DingChime   string `json:"ding_chime"`        // doorbell-press chime; falls back to Chime if empty
	DeviceID    int64  `json:"device_id"`         // deprecated, superseded by Devices; kept for old configs
	DebounceSec int    `json:"debounce_sec"`      // min seconds between chimes
	NoOled      bool   `json:"no_oled,omitempty"` // suppress the ST20 OLED notification (default: show)
	Volume      int    `json:"volume,omitempty"`  // chime volume (10–70); 0 = DefaultVolume (30)

	// Devices gates which Ring devices chime, and for what. Empty = every device, both
	// events (back-compat). Non-empty = only listed devices, only their enabled events.
	// ring-auth fills this from the Ring account's device list.
	Devices []DeviceRule `json:"devices"`

	FCM FCMCreds `json:"fcm"` // empty on first run -> agent registers and fills it
}

// DeviceRule is one Ring device's per-event preference (by Ring's doorbot id).
type DeviceRule struct {
	ID     int64  `json:"id"`     // Ring doorbot_id (matched against the push)
	Name   string `json:"name"`   // human label, for logs/config readability
	Motion bool   `json:"motion"` // chime on motion from this device
	Ding   bool   `json:"ding"`   // chime on doorbell press (doorbells only)
}

type FCMCreds struct {
	FCMToken      string `json:"fcm_token"`
	GCMToken      string `json:"gcm_token"`
	AndroidID     uint64 `json:"android_id"`
	SecurityToken uint64 `json:"security_token"`
	PrivateKey    string `json:"private_key"` // base64
	AuthSecret    string `json:"auth_secret"` // base64
}

func (c *Config) applyDefaults() {
	c.unwrapRingClientToken()
	if c.Speaker == "" {
		c.Speaker = "127.0.0.1:8090"
	}
	if c.Chime == "" {
		c.Chime = "/mnt/nv/shine.mp3"
	}
	if c.DingChime == "" {
		c.DingChime = "/mnt/nv/doorbell.mp3"
	}
	if c.DebounceSec == 0 {
		c.DebounceSec = 15
	}
}

func (c *Config) unwrapRingClientToken() {
	var wrapped struct {
		RefreshToken string `json:"rt"`
		HardwareID   string `json:"hid"`
	}
	b, err := base64.RawStdEncoding.DecodeString(c.RefreshToken)
	if err != nil {
		b, err = base64.StdEncoding.DecodeString(c.RefreshToken)
	}
	if err != nil || json.Unmarshal(b, &wrapped) != nil || wrapped.RefreshToken == "" {
		return
	}
	c.RefreshToken = wrapped.RefreshToken
	if wrapped.HardwareID != "" {
		c.HardwareID = wrapped.HardwareID
	}
	log.Printf("ring: unwrapped ring-client-api refresh token (hardware_id=%v)", wrapped.HardwareID != "")
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	c.applyDefaults()
	return &c, nil
}

// save writes the config atomically (temp + rename) so a crash mid-write — e.g. while
// persisting a freshly rotated refresh token — never leaves a truncated file.
func (c *Config) save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), ".config.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

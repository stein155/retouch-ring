// Package ring is the SoundTouch-as-Ring-chime agent, decoupled from any host program.
// It listens for Ring motion via FCM push and plays a ducked chime through the speaker's
// /playNotification endpoint. Start it from a standalone main, or register it as a
// background service inside another single-runtime binary (e.g. retouch) to save RAM.
package ring

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	fcm "github.com/stein155/retouch-ring/internal/fcm"
)

// debugf logs only when RING_DEBUG is set — for register response bodies/status.
func debugf(format string, a ...any) {
	if os.Getenv("RING_DEBUG") != "" {
		log.Printf(format, a...)
	}
}

var (
	cfg      *Config
	cfgPath  string
	chimeMu  sync.Mutex
	lastFire = map[string]time.Time{}
)

const stalePushAfter = 2 * time.Minute

// Start loads the config at cfgPath, registers for Ring push, and plays a chime on
// motion until ctx is cancelled. Safe to run as `go Start(ctx, path)`.
func Start(ctx context.Context, path string) error {
	cfgPath = path
	var err error
	cfg, err = loadConfig(cfgPath)
	if err != nil {
		return err
	}
	if cfg.RefreshToken == "" || cfg.HardwareID == "" {
		return errMissingToken
	}

	client := &fcm.FCMClient{
		ApiKey:        fcmAPIKey,
		AppId:         fcmAppID,
		ProjectID:     fcmProjectID,
		OnDataMessage: onPush,
		OnRawMessage:  onRawPush,
	}
	if cert := os.Getenv("RING_ANDROID_FCM_CERT"); cert != "" {
		client.AndroidApp = &fcm.AndroidFCM{GcmSenderId: "876313859327", AndroidPackage: "com.ringapp", AndroidPackageCert: cert}
	}
	if err := setupFCM(client); err != nil {
		return err
	}
	log.Printf("ring: FCM token ...%s", tail(cfg.FCM.FCMToken))

	// Register with Ring (rotating + persisting the refresh token), then re-register
	// every 6h to keep the subscription fresh and roll the token forward. Never fatal:
	// a transient Ring error must not kill the agent (rc.local only restarts on reboot),
	// so retry with backoff until the first register succeeds.
	go func() {
		for ctx.Err() == nil {
			if err := ringSync(cfg.FCM.FCMToken); err != nil {
				log.Printf("ring: register failed: %v (retry in 60s)", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(60 * time.Second):
				}
				continue
			}
			log.Printf("ring: registered with Ring")
			break
		}
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := ringSync(cfg.FCM.FCMToken); err != nil {
					log.Printf("ring: re-register failed: %v", err)
				}
			}
		}
	}()

	log.Printf("ring: listening for motion -> chime %q on %s (device=%d, debounce=%ds)",
		cfg.Chime, cfg.Speaker, cfg.DeviceID, cfg.DebounceSec)
	go func() {
		for ctx.Err() == nil {
			err := client.StartListening()
			log.Printf("ring: listen dropped: %v — reconnecting in 30s", err)
			// Back off even on a nil return, so a clean close can't spin the loop hot.
			select {
			case <-ctx.Done():
			case <-time.After(30 * time.Second):
			}
		}
	}()

	<-ctx.Done()
	return nil
}

// setupFCM restores saved FCM credentials, or registers a fresh device and persists them.
func setupFCM(c *fcm.FCMClient) error {
	if cfg.FCM.FCMToken != "" && cfg.FCM.PrivateKey != "" {
		c.FcmToken = cfg.FCM.FCMToken
		c.GcmToken = cfg.FCM.GCMToken
		c.AndroidId = cfg.FCM.AndroidID
		c.SecurityToken = cfg.FCM.SecurityToken
		return c.LoadKeys(cfg.FCM.PrivateKey, cfg.FCM.AuthSecret)
	}
	if _, _, err := c.CreateNewKeys(); err != nil {
		return err
	}
	fcmTok, gcmTok, aid, sec, err := c.Register()
	if err != nil {
		return err
	}
	priv, err := c.GetPrivateKeyBase64()
	if err != nil {
		return err
	}
	// The token we hand Ring is the push registration token. go-fcm's modern FcmToken
	// can come back empty here; the GcmToken (the "xxx:APA91b…" registration token) is
	// what push is actually delivered to over MCS, so fall back to it.
	token := fcmTok
	if token == "" {
		token = gcmTok
	}
	cfg.FCM = FCMCreds{
		FCMToken: token, GCMToken: gcmTok, AndroidID: aid, SecurityToken: sec,
		PrivateKey: priv, AuthSecret: c.GetAuthSecretBase64(),
	}
	return cfg.save(cfgPath)
}

// ringSync refreshes the Ring access token (persisting the rotated refresh token) and
// re-registers our FCM token so Ring keeps delivering motion pushes.
func ringSync(fcmToken string) error {
	access, newRefresh, err := ringRefresh(cfg.RefreshToken, cfg.HardwareID)
	if err != nil {
		return err
	}
	if newRefresh != cfg.RefreshToken {
		cfg.RefreshToken = newRefresh
		if err := cfg.save(cfgPath); err != nil {
			log.Printf("ring: warning, could not persist rotated refresh token: %v", err)
		}
	}
	if err := ringCreateSession(access); err != nil {
		return err
	}
	return ringRegisterPush(access, fcmToken)
}

var pushDeviceRes = []*regexp.Regexp{
	regexp.MustCompile(`"(?:doorbot_id|device_id)"\s*:\s*"?(\d+)"?`),
	regexp.MustCompile(`"device"\s*:\s*\{[^}]*"id"\s*:\s*(\d+)`),
	regexp.MustCompile(`motion_channel_notification(\d+)`),
	regexp.MustCompile(`"snapshot_uuid"\s*:\s*"[^":]+:(\d+)"`),
}

// onPush handles one decrypted FCM data message (Ring sends JSON). It distinguishes a
// doorbell press (ding) from motion, applies per-device gating, and plays the chime.
// ponytail: substring match for the kind (verified live) + regex for the device id.
// Upgrade path if it ever misfires: full-parse android_config.category + data.device.
func onPush(msg []byte) {
	log.Printf("ring: push %s", truncate(string(msg), 400)) // raw, so device ids are visible in the log
	body := strings.ToLower(string(msg))

	var kind, chime string
	switch detectPushKind(body) {
	case "motion":
		kind, chime = "motion", cfg.Chime
	case "ding":
		kind, chime = "ding", cfg.DingChime
	default:
		return // not an event we chime on
	}
	if t, ok := pushEventTime(msg); ok && time.Since(t) > stalePushAfter {
		log.Printf("ring: stale %s from %s ignored", kind, t.Format(time.RFC3339))
		return
	}

	// Per-device gating. Empty Devices = chime for everything (back-compat). With a list,
	// the event must come from a listed device with that event enabled. If the device id
	// can't be parsed we ALLOW (never go silent on a parse miss) and log it loudly.
	if len(cfg.Devices) > 0 {
		if id, ok := pushDeviceID(body); ok {
			rule := findDevice(id)
			if rule == nil {
				log.Printf("ring: %s from device %d not in devices list — ignored", kind, id)
				return
			}
			if (kind == "motion" && !rule.Motion) || (kind == "ding" && !rule.Ding) {
				log.Printf("ring: %s from %s (%d) — disabled for this device, no chime", kind, rule.Name, id)
				return
			}
		} else {
			log.Printf("ring: WARNING could not parse device id from push; allowing %s", kind)
		}
	}

	if chime == "" {
		return // no chime configured for this event
	}

	if !allowChime(kind, time.Now()) {
		return
	}

	log.Printf("ring: %s -> chime %q", kind, chime)
	if err := playChime(cfg.Speaker, chime); err != nil {
		log.Printf("ring: chime failed: %v", err)
	}
}

func onRawPush(msg *fcm.DataMessageStanza) {
	data := map[string]string{}
	for _, item := range msg.AppData {
		data[item.GetKey()] = item.GetValue()
	}
	// Wrap in the {"data":{...}} envelope decrypted pushes have, so the structured
	// kind/stale/device parsers in onPush work instead of only the substring fallback.
	b, _ := json.Marshal(map[string]any{"data": data})
	log.Printf("ring: raw push %s", truncate(string(b), 400))
	if len(data) > 0 {
		onPush(b)
	}
}

func allowChime(kind string, now time.Time) bool {
	chimeMu.Lock()
	defer chimeMu.Unlock()
	if now.Sub(lastFire[kind]) < time.Duration(cfg.DebounceSec)*time.Second {
		return false
	}
	lastFire[kind] = now
	return true
}

func pushEventTime(msg []byte) (time.Time, bool) {
	times := []time.Time{}
	addMillis := func(v int64) {
		if v <= 0 {
			return
		}
		if v > 1_000_000_000_000 { // milliseconds since epoch
			v /= 1000
		}
		times = append(times, time.Unix(v, 0))
	}
	addRFC3339 := func(s string) {
		if s == "" {
			return
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			times = append(times, t)
		}
	}

	type ringPush struct {
		Data struct {
			Data      string `json:"data"`
			Analytics string `json:"analytics"`
		} `json:"data"`
	}
	var push ringPush
	if json.Unmarshal(msg, &push) != nil {
		return time.Time{}, false
	}
	if push.Data.Analytics != "" {
		var a struct {
			TriggeredAt int64 `json:"triggered_at"`
			SentAt      int64 `json:"sent_at"`
		}
		if json.Unmarshal([]byte(push.Data.Analytics), &a) == nil {
			addMillis(a.TriggeredAt)
			addMillis(a.SentAt)
		}
	}
	if push.Data.Data != "" {
		var d struct {
			Event struct {
				Ding struct {
					CreatedAt string `json:"created_at"`
				} `json:"ding"`
				Motion struct {
					CreatedAt string `json:"created_at"`
				} `json:"motion"`
			} `json:"event"`
			Eventito struct {
				Timestamp int64 `json:"timestamp"`
			} `json:"eventito"`
		}
		if json.Unmarshal([]byte(push.Data.Data), &d) == nil {
			addRFC3339(d.Event.Ding.CreatedAt)
			addRFC3339(d.Event.Motion.CreatedAt)
			addMillis(d.Eventito.Timestamp)
		}
	}
	if len(times) == 0 {
		return time.Time{}, false
	}
	latest := times[0]
	for _, t := range times[1:] {
		if t.After(latest) {
			latest = t
		}
	}
	return latest, true
}

func pushDeviceID(body string) (int64, bool) {
	body = strings.ReplaceAll(body, `\"`, `"`)
	for _, re := range pushDeviceRes {
		m := re.FindStringSubmatch(body)
		if m == nil {
			continue
		}
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err == nil {
			return id, true
		}
	}
	return 0, false
}

func detectPushKind(body string) string {
	type ringPush struct {
		Data struct {
			Data          string `json:"data"`
			AndroidConfig string `json:"android_config"`
			Analytics     string `json:"analytics"`
		} `json:"data"`
	}
	// Ring wraps MOTION events in event.ding too (with subtype/detection_type "human",
	// "motion", …); only a real button press is a ding. A bare event.ding without any
	// subtype/detection is treated as a press (nothing to detect on a button).
	type innerEvent struct {
		Event struct {
			Ding *struct {
				Subtype       string `json:"subtype"`
				DetectionType string `json:"detection_type"`
			} `json:"ding"`
			Motion json.RawMessage `json:"motion"`
		} `json:"event"`
	}

	var push ringPush
	if err := json.Unmarshal([]byte(body), &push); err == nil {
		if strings.Contains(push.Data.AndroidConfig, `live-event.motion`) || strings.Contains(push.Data.AndroidConfig, `motion_channel_notification`) {
			return "motion"
		}
		if strings.Contains(push.Data.AndroidConfig, `live-event.ding`) || strings.Contains(push.Data.AndroidConfig, `ding_channel_notification`) {
			return "ding"
		}
		if strings.Contains(push.Data.Analytics, `"subcategory":"human"`) {
			return "motion"
		}
		if push.Data.Data != "" {
			var evt innerEvent
			if json.Unmarshal([]byte(push.Data.Data), &evt) == nil {
				if d := evt.Event.Ding; d != nil {
					if d.Subtype == "ding" || d.Subtype == "button_press" ||
						(d.Subtype == "" && d.DetectionType == "") {
						return "ding"
					}
					return "motion" // human/motion/vehicle/… detection wrapped in event.ding
				}
				if len(evt.Event.Motion) > 0 {
					return "motion"
				}
			}
		}
	}

	// Substring fallback (unparseable payloads). Specific ding markers first, then motion
	// markers, and only then the bare event.ding shape — motion bodies contain that too.
	body = strings.ReplaceAll(body, `\"`, `"`)
	if strings.Contains(body, `live-event.ding`) || strings.Contains(body, `ding_channel_notification`) || strings.Contains(body, `"subtype":"ding"`) {
		return "ding"
	}
	if strings.Contains(body, `live-event.motion`) || strings.Contains(body, `motion_channel_notification`) ||
		strings.Contains(body, `"motion":{`) || strings.Contains(body, `"detection_type"`) || strings.Contains(body, `human`) {
		return "motion"
	}
	if strings.Contains(body, `"event":{"ding"`) || strings.Contains(body, `"ding":{`) {
		return "ding"
	}
	return ""
}

func findDevice(id int64) *DeviceRule {
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == id {
			return &cfg.Devices[i]
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func tail(s string) string {
	if len(s) > 6 {
		return s[len(s)-6:]
	}
	return s
}

// Package plugin adapts the Ring chime agent (package ring) to ReTouch's plugin
// contract. ReTouch launches this binary with --listen/--config-dir/--speaker-host/
// --host-url, then reverse-proxies its config UI. We serve:
//
//	GET  /health            liveness for the host
//	GET  /manifest          the current settings UI (server-driven schema)
//	POST /action/{id}       perform an action, return the NEXT manifest
//
// The Ring email/password/2FA login runs here (see auth.go) and writes config.json;
// the ring agent reads that same file and does the FCM listen + chime playback. We
// (re)start the agent whenever the credentials or device choices change.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch-ring/ring"
)

type pendingLogin struct {
	email, password, hardwareID, phone string
}

// Plugin holds the plugin's state and supervises the ring agent.
type Plugin struct {
	cfgPath string
	speaker string
	hostURL string
	hasOLED bool
	log     *log.Logger

	agentParent context.Context

	mu          sync.Mutex
	cfg         Config
	pending     *pendingLogin
	lang        string
	lastLang    time.Time
	agentCancel context.CancelFunc
	agentDone   chan struct{} // closed when superviseAgent returns
}

// New bootstraps the plugin: it loads (or seeds) config.json in cfgDir, writes the
// bundled chimes there on first run, points the agent at speaker, and — if already
// logged in — starts the ring agent under ctx.
func New(ctx context.Context, cfgDir, speaker, hostURL string, chimes fs.FS, logger *log.Logger) (*Plugin, error) {
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return nil, err
	}
	p := &Plugin{
		cfgPath:     filepath.Join(cfgDir, "config.json"),
		speaker:     speaker,
		hostURL:     strings.TrimRight(hostURL, "/"),
		log:         logger,
		lang:        "en",
		agentParent: ctx,
	}
	p.hasOLED = p.probeDisplay()
	ring.NotifyFunc = p.notifyDisplay // agent events -> display API notification
	p.cfg = loadConfig(p.cfgPath)
	p.cfg.Speaker = speaker // the host tells us where the speaker's API is
	if p.cfg.HardwareID == "" {
		p.cfg.HardwareID = newHardwareID()
	}
	if p.cfg.DebounceSec == 0 {
		p.cfg.DebounceSec = 15
	}
	// Ship the chimes into the config dir so playback has a real file to point at.
	p.cfg.Chime = ensureChime(chimes, cfgDir, "shine.pcm", p.cfg.Chime)
	p.cfg.DingChime = ensureChime(chimes, cfgDir, "doorbell.pcm", p.cfg.DingChime)
	if err := saveConfig(p.cfgPath, p.cfg); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.restartAgentLocked()
	p.mu.Unlock()
	return p, nil
}

// ensureChime writes the bundled chime to dir (once) and returns its path, keeping
// any path the user already configured.
func ensureChime(chimes fs.FS, dir, name, existing string) string {
	if existing != "" {
		return existing
	}
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); err == nil {
		return dst
	}
	b, err := fs.ReadFile(chimes, "assets/"+name)
	if err != nil {
		return dst // agent falls back to a built-in chime name if the file is absent
	}
	_ = os.WriteFile(dst, b, 0o644)
	return dst
}

// Handler returns the plugin's loopback HTTP API.
func (p *Plugin) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /manifest", func(w http.ResponseWriter, _ *http.Request) {
		p.refreshLang()
		p.mu.Lock()
		m := p.manifestLocked()
		p.mu.Unlock()
		writeJSON(w, http.StatusOK, m)
	})
	mux.HandleFunc("POST /action/{id}", p.handleAction)
	return mux
}

type actionBody struct {
	Values map[string]any             `json:"values"`
	Rows   map[string]map[string]bool `json:"rows"`
}

func (p *Plugin) handleAction(w http.ResponseWriter, r *http.Request) {
	p.refreshLang()
	var body actionBody
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	m, err := p.doAction(r.PathValue("id"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (p *Plugin) doAction(id string, body actionBody) (Manifest, error) {
	switch id {
	case "login":
		return p.login(str(body.Values["email"]), str(body.Values["password"]))
	case "verify":
		return p.verify(str(body.Values["code"]))
	case "cancel":
		p.mu.Lock()
		defer p.mu.Unlock()
		p.pending = nil
		return p.manifestLocked(), nil
	case "save":
		return p.saveDevices(body.Rows, body.Values)
	case "refresh":
		return p.refreshDevices()
	case "test":
		return p.testChime()
	case "logout":
		return p.logout()
	default:
		return Manifest{}, fmt.Errorf("unknown action %q", id)
	}
}

func (p *Plugin) login(email, password string) (Manifest, error) {
	if email == "" || password == "" {
		return Manifest{}, fmt.Errorf("enter your Ring email and password")
	}
	p.mu.Lock()
	hwid := p.cfg.HardwareID
	p.mu.Unlock()
	access, refresh, need2FA, phone, err := ringLogin(email, password, hwid, "")
	if err != nil {
		return Manifest{}, err
	}
	if need2FA {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.pending = &pendingLogin{email: email, password: password, hardwareID: hwid, phone: phone}
		return p.manifestLocked(), nil
	}
	return p.finishLogin(access, refresh)
}

func (p *Plugin) verify(code string) (Manifest, error) {
	if code == "" {
		return Manifest{}, fmt.Errorf("enter the code Ring sent you")
	}
	p.mu.Lock()
	pend := p.pending
	p.mu.Unlock()
	if pend == nil {
		return Manifest{}, fmt.Errorf("no login in progress")
	}
	access, refresh, _, _, err := ringLogin(pend.email, pend.password, pend.hardwareID, code)
	if err != nil {
		return Manifest{}, err
	}
	return p.finishLogin(access, refresh)
}

// reloadLocked stops the agent (waiting for its final persist) and replaces p.cfg
// with the on-disk config. The disk copy is the source of truth for the rotating
// credentials the agent owns (Ring refresh token, FCM creds): the in-memory copy
// from process start goes stale the moment the agent rotates, and writing it back
// would log the user out. Plugin-owned invariants are re-applied on top. Every
// mutation is therefore stop → reload → change → save → restart. Caller holds p.mu.
func (p *Plugin) reloadLocked() {
	p.stopAgentLocked()
	fresh := loadConfig(p.cfgPath)
	if fresh.HardwareID == "" {
		fresh.HardwareID = p.cfg.HardwareID
	}
	fresh.Speaker = p.speaker
	if fresh.Chime == "" {
		fresh.Chime = p.cfg.Chime
	}
	if fresh.DingChime == "" {
		fresh.DingChime = p.cfg.DingChime
	}
	if fresh.DebounceSec == 0 {
		fresh.DebounceSec = p.cfg.DebounceSec
	}
	p.cfg = fresh
}

// finishLogin persists the refresh token, fetches the device list, restarts the
// agent and returns the connected manifest.
func (p *Plugin) finishLogin(access, refresh string) (Manifest, error) {
	p.mu.Lock()
	hwid := p.cfg.HardwareID
	p.mu.Unlock()
	devices, err := ringDevices(access, hwid)
	if err != nil {
		p.log.Printf("device list failed after login: %v", err)
		// Login still succeeded; keep going with no devices (user can Refresh later).
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reloadLocked()
	p.cfg.RefreshToken = refresh // from this login — newer than anything on disk
	p.cfg.Devices = mergeDevices(p.cfg.Devices, devices)
	p.pending = nil
	if err := saveConfig(p.cfgPath, p.cfg); err != nil {
		return Manifest{}, err
	}
	p.restartAgentLocked()
	return p.manifestLocked(), nil
}

// refreshDevices rotates the token itself, so the agent must be stopped FIRST —
// two uncoordinated rotations of the same token invalidate one another. The Ring
// calls run under p.mu; a manifest poll blocks for those seconds, which is fine.
func (p *Plugin) refreshDevices() (Manifest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reloadLocked()
	if p.cfg.RefreshToken == "" {
		return Manifest{}, fmt.Errorf("not logged in")
	}
	access, newRefresh, err := ringRefreshToken(p.cfg.RefreshToken, p.cfg.HardwareID)
	if err != nil {
		p.restartAgentLocked() // leave things running even when the refresh fails
		return Manifest{}, err
	}
	p.cfg.RefreshToken = newRefresh
	if err := saveConfig(p.cfgPath, p.cfg); err != nil { // persist the rotation before anything else
		return Manifest{}, err
	}
	devices, err := ringDevices(access, p.cfg.HardwareID)
	if err != nil {
		p.restartAgentLocked()
		return Manifest{}, err
	}
	p.cfg.Devices = mergeDevices(p.cfg.Devices, devices)
	if err := saveConfig(p.cfgPath, p.cfg); err != nil {
		return Manifest{}, err
	}
	p.restartAgentLocked()
	return p.manifestLocked(), nil
}

func (p *Plugin) saveDevices(rows map[string]map[string]bool, values map[string]any) (Manifest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reloadLocked()
	for i := range p.cfg.Devices {
		id := strconv.FormatInt(p.cfg.Devices[i].ID, 10)
		if r, ok := rows[id]; ok {
			p.cfg.Devices[i].Motion = r["motion"]
			p.cfg.Devices[i].Ding = r["ding"]
		}
	}
	if v, ok := values["oled"].(bool); ok {
		p.cfg.NoOled = !v
	}
	if s, ok := values["volume"].(string); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 10 && n <= 200 {
			p.cfg.Volume = n
		}
	}
	if err := saveConfig(p.cfgPath, p.cfg); err != nil {
		return Manifest{}, err
	}
	p.restartAgentLocked()
	return p.manifestLocked(), nil
}

func (p *Plugin) testChime() (Manifest, error) {
	p.mu.Lock()
	speaker, chime, noOled, volume := p.cfg.Speaker, p.cfg.Chime, p.cfg.NoOled, p.cfg.Volume
	m := p.manifestLocked()
	p.mu.Unlock()
	if !noOled {
		p.notifyDisplay("ding", "")
	}
	if err := playNotification(speaker, ring.GainedChime(chime, volume)); err != nil {
		return Manifest{}, fmt.Errorf("could not play a test chime: %w", err)
	}
	return m, nil
}

// refreshLang syncs the UI language from ReTouch's settings API (cached for a
// minute); standalone runs keep the default.
func (p *Plugin) refreshLang() {
	if p.hostURL == "" {
		return
	}
	p.mu.Lock()
	stale := time.Since(p.lastLang) > time.Minute
	p.mu.Unlock()
	if !stale {
		return
	}
	var s struct {
		Language string `json:"language"`
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(p.hostURL + "/api/settings")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&s)
	p.mu.Lock()
	if s.Language != "" {
		p.lang = s.Language
	}
	p.lastLang = time.Now()
	p.mu.Unlock()
}

func (p *Plugin) language() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lang
}

func (p *Plugin) logout() (Manifest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reloadLocked() // stops the agent and waits before we clear its credentials
	p.cfg.RefreshToken = ""
	p.cfg.FCM = nil
	p.cfg.Devices = nil
	p.pending = nil
	if err := saveConfig(p.cfgPath, p.cfg); err != nil {
		return Manifest{}, err
	}
	return p.manifestLocked(), nil
}

// --- agent supervision -----------------------------------------------------

// restartAgentLocked (re)launches the ring agent if we're logged in, else stops it.
// Caller holds p.mu.
func (p *Plugin) restartAgentLocked() {
	p.stopAgentLocked()
	if p.cfg.RefreshToken == "" || p.agentParent == nil {
		return
	}
	ctx, cancel := context.WithCancel(p.agentParent)
	done := make(chan struct{})
	p.agentCancel = cancel
	p.agentDone = done
	go func() {
		defer close(done)
		p.superviseAgent(ctx)
	}()
}

// stopAgentLocked cancels the agent and WAITS for it to return. The wait matters:
// the agent persists rotated Ring/FCM credentials to config.json on its way down,
// and every caller is about to read-modify-write that same file — writing before
// the agent has finished would race it and clobber the freshest token.
func (p *Plugin) stopAgentLocked() {
	if p.agentCancel == nil {
		return
	}
	p.agentCancel()
	p.agentCancel = nil
	if p.agentDone != nil {
		select {
		case <-p.agentDone:
		case <-time.After(10 * time.Second):
			p.log.Printf("ring agent slow to stop; continuing")
		}
		p.agentDone = nil
	}
}

// superviseAgent runs ring.Start, restarting it with capped backoff until ctx ends.
func (p *Plugin) superviseAgent(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := ring.Start(ctx, p.cfgPath)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			p.log.Printf("ring agent exited: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// --- helpers ----------------------------------------------------------------

// mergeDevices keeps the user's existing motion/ding choices for devices Ring still
// reports, and adds newly-seen devices with sensible defaults.
func mergeDevices(existing, fetched []Device) []Device {
	prev := make(map[int64]Device, len(existing))
	for _, d := range existing {
		prev[d.ID] = d
	}
	out := make([]Device, 0, len(fetched))
	for _, d := range fetched {
		if old, ok := prev[d.ID]; ok {
			d.Motion, d.Ding = old.Motion, old.Ding
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return existing // a failed refresh shouldn't wipe known devices
	}
	return out
}

// playNotification triggers the speaker's ducked-notification playback of a local
// PCM file — the same firmware path the ring agent uses for a real chime.
func playNotification(speaker, chime string) error {
	xml := `<audioSource pathToFile="` + chime + `"/>`
	req, _ := http.NewRequest(http.MethodPost, "http://"+speaker+"/playNotification", strings.NewReader(xml))
	req.Header.Set("Content-Type", "application/xml")
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("speaker returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

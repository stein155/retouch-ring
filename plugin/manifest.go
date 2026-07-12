package plugin

import (
	"strconv"

	"github.com/stein155/retouch-ring/ring"
)

// The manifest is the server-driven settings UI ReTouch renders. GET /manifest
// returns the current state; POST /action/{id} performs an action and returns the
// NEXT manifest. Multi-step flows (log in → 2FA code → connected) are just the
// sequence of manifests the plugin returns — ReTouch needs no Ring-specific code.
//
// This schema is the contract with ReTouch's frontend manifest renderer
// (frontend/src/components/organisms/PluginsSection.jsx).

type Manifest struct {
	Title    string    `json:"title"`
	Status   *Status   `json:"status,omitempty"`
	Sections []Section `json:"sections"`
}

// Status is the coloured one-line summary at the top. Level is idle|ok|warn|error.
type Status struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

type Section struct {
	Title   string   `json:"title,omitempty"`
	Text    string   `json:"text,omitempty"`
	Fields  []Field  `json:"fields,omitempty"`
	Rows    []Row    `json:"rows,omitempty"`
	Actions []Action `json:"actions,omitempty"`
}

// Field is a single input. Type is text|password|number|otp|toggle|slider.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Value       any    `json:"value,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	// For type "slider". Older ReTouch hosts render unknown field types as a
	// text input, so the field degrades to a free-form number.
	Min  int    `json:"min,omitempty"`
	Max  int    `json:"max,omitempty"`
	Step int    `json:"step,omitempty"`
	Unit string `json:"unit,omitempty"`
}

// Row is a labelled line with one or more toggles — e.g. a device with Motion/Doorbell.
type Row struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Toggles []RowToggle `json:"toggles"`
}

type RowToggle struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value bool   `json:"value"`
}

// Action is a button. Style is primary|danger|"" (default). Confirm, when set, is
// shown to the user before the action runs.
type Action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Style   string `json:"style,omitempty"`
	Confirm string `json:"confirm,omitempty"`
}

// manifest builds the current UI from plugin state. Caller must hold p.mu.
func (p *Plugin) manifestLocked() Manifest {
	lang := p.lang
	if p.pending != nil {
		return Manifest{
			Title:  "Ring chime",
			Status: &Status{Level: "warn", Text: ring.Tr(lang, "status.2fa") + phoneSuffix(lang, p.pending.phone)},
			Sections: []Section{{
				Title:  ring.Tr(lang, "section.2fa"),
				Fields: []Field{{Key: "code", Label: ring.Tr(lang, "field.code"), Type: "otp", Placeholder: "123456"}},
				Actions: []Action{
					{ID: "verify", Label: ring.Tr(lang, "action.verify"), Style: "primary"},
					{ID: "cancel", Label: ring.Tr(lang, "action.cancel")},
				},
			}},
		}
	}
	if p.cfg.RefreshToken == "" {
		return Manifest{
			Title:  "Ring chime",
			Status: &Status{Level: "idle", Text: ring.Tr(lang, "status.notconn")},
			Sections: []Section{{
				Title: ring.Tr(lang, "section.account"),
				Text:  ring.Tr(lang, "text.login"),
				Fields: []Field{
					{Key: "email", Label: ring.Tr(lang, "field.email"), Type: "text", Placeholder: "you@example.com"},
					{Key: "password", Label: ring.Tr(lang, "field.password"), Type: "password"},
				},
				Actions: []Action{{ID: "login", Label: ring.Tr(lang, "action.login"), Style: "primary"}},
			}},
		}
	}

	rows := make([]Row, 0, len(p.cfg.Devices))
	for _, d := range p.cfg.Devices {
		rows = append(rows, Row{
			ID:    strconv.FormatInt(d.ID, 10),
			Label: d.Name,
			Toggles: []RowToggle{
				{Key: "motion", Label: ring.Tr(lang, "toggle.motion"), Value: d.Motion},
				{Key: "ding", Label: ring.Tr(lang, "toggle.ding"), Value: d.Ding},
			},
		})
	}
	// Chime gain; 0 in the config means "unset", played at ring.DefaultVolume.
	// Sent as a string: saveDevices round-trips inputs as strings, and the old
	// text renderer shows it the same way.
	volume := p.cfg.Volume
	if volume <= 0 {
		volume = ring.DefaultVolume
	}
	devSection := Section{
		Title: ring.Tr(lang, "section.devices"),
		Text:  ring.Tr(lang, "text.devices"),
		Rows:  rows,
		Fields: []Field{
			{Key: "volume", Label: ring.Tr(lang, "field.volume"), Type: "slider", Value: strconv.Itoa(volume), Min: 10, Max: 70, Step: 5, Unit: "%"},
		},
		Actions: []Action{{ID: "save", Label: ring.Tr(lang, "action.save"), Style: "primary"}, {ID: "test", Label: ring.Tr(lang, "action.test")}},
	}
	if p.hasOLED {
		// The bell/motion notification on the ST20 front panel; hidden on models
		// without the framebuffer, where it would be a dead switch.
		devSection.Fields = append(devSection.Fields, Field{
			Key: "oled", Label: ring.Tr(lang, "field.oled"), Type: "toggle", Value: !p.cfg.NoOled,
		})
	}
	if len(rows) == 0 {
		devSection.Text = ring.Tr(lang, "text.nodevices")
		devSection.Actions = []Action{{ID: "refresh", Label: ring.Tr(lang, "action.refresh"), Style: "primary"}, {ID: "test", Label: ring.Tr(lang, "action.test")}}
	}
	return Manifest{
		Title:  "Ring chime",
		Status: &Status{Level: "ok", Text: ring.Tr(lang, "status.connected")},
		Sections: []Section{
			devSection,
			{Title: ring.Tr(lang, "section.acct"), Actions: []Action{{ID: "logout", Label: ring.Tr(lang, "action.logout"), Style: "danger", Confirm: ring.Tr(lang, "confirm.logout")}}},
		},
	}
}

func phoneSuffix(lang, phone string) string {
	if phone == "" {
		return "."
	}
	return ring.Tr(lang, "2fa.to", phone)
}

package plugin

import "strconv"

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

// Field is a single input. Type is text|password|number|otp|toggle.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Value       any    `json:"value,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
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
	if p.pending != nil {
		return Manifest{
			Title:  "Ring chime",
			Status: &Status{Level: "warn", Text: "Enter the code Ring just sent" + phoneSuffix(p.pending.phone)},
			Sections: []Section{{
				Title:  "Two-factor code",
				Fields: []Field{{Key: "code", Label: "Verification code", Type: "otp", Placeholder: "123456"}},
				Actions: []Action{
					{ID: "verify", Label: "Verify", Style: "primary"},
					{ID: "cancel", Label: "Cancel"},
				},
			}},
		}
	}
	if p.cfg.RefreshToken == "" {
		return Manifest{
			Title:  "Ring chime",
			Status: &Status{Level: "idle", Text: "Not connected to Ring"},
			Sections: []Section{{
				Title: "Ring account",
				Text:  "Log in with your Ring email and password. If your account has two-factor authentication, you'll be asked for a code next.",
				Fields: []Field{
					{Key: "email", Label: "Email", Type: "text", Placeholder: "you@example.com"},
					{Key: "password", Label: "Password", Type: "password"},
				},
				Actions: []Action{{ID: "login", Label: "Log in", Style: "primary"}},
			}},
		}
	}

	rows := make([]Row, 0, len(p.cfg.Devices))
	for _, d := range p.cfg.Devices {
		rows = append(rows, Row{
			ID:    strconv.FormatInt(d.ID, 10),
			Label: d.Name,
			Toggles: []RowToggle{
				{Key: "motion", Label: "Motion", Value: d.Motion},
				{Key: "ding", Label: "Doorbell", Value: d.Ding},
			},
		})
	}
	devSection := Section{
		Title:   "Devices",
		Text:    "Choose which Ring devices chime, and for what.",
		Rows:    rows,
		Actions: []Action{{ID: "save", Label: "Save", Style: "primary"}, {ID: "test", Label: "Test chime"}},
	}
	if len(rows) == 0 {
		devSection.Text = "No Ring devices found on this account."
		devSection.Actions = []Action{{ID: "refresh", Label: "Refresh devices", Style: "primary"}, {ID: "test", Label: "Test chime"}}
	}
	return Manifest{
		Title:  "Ring chime",
		Status: &Status{Level: "ok", Text: "Connected to Ring"},
		Sections: []Section{
			devSection,
			{Title: "Account", Actions: []Action{{ID: "logout", Label: "Log out", Style: "danger", Confirm: "Disconnect this speaker from Ring?"}}},
		},
	}
}

func phoneSuffix(phone string) string {
	if phone == "" {
		return "."
	}
	return " to " + phone + "."
}

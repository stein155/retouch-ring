package plugin

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Ring consumer-API auth. The ring agent only knows how to *refresh* a token; the
// initial email/password (+2FA) login lives here so the whole flow can run from the
// web UI instead of a separate ring-auth CLI. Values mirror ring-client-api.
const (
	ringOAuthURL  = "https://oauth.ring.com/oauth/token"
	ringDevicesURL = "https://api.ring.com/clients_api/ring_devices"
	ringUA        = "android:com.ringapp"
)

var authHTTP = &http.Client{Timeout: 30 * time.Second}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrDesc      string `json:"error_description"`
	Phone        string `json:"phone"`
	TSVState     string `json:"tsv_state"`
}

// ringLogin performs the password grant. With code == "" it's the first attempt: if
// the account has 2FA, Ring replies 412 (having just sent a code) and need2FA is
// true. Re-call with the code to finish. On success it returns an access token (for
// an immediate device fetch) and the refresh token to persist.
func ringLogin(email, password, hardwareID, code string) (access, refresh string, need2FA bool, phone string, err error) {
	body, _ := json.Marshal(map[string]any{
		"client_id":  "ring_official_android",
		"scope":      "client",
		"grant_type": "password",
		"username":   email,
		"password":   password,
	})
	req, _ := http.NewRequest(http.MethodPost, ringOAuthURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ringUA)
	req.Header.Set("hardware_id", hardwareID)
	req.Header.Set("2fa-support", "true")
	if code != "" {
		req.Header.Set("2fa-code", code)
	}
	resp, err := authHTTP.Do(req)
	if err != nil {
		return "", "", false, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr tokenResp
	_ = json.Unmarshal(b, &tr)

	// 412 Precondition Required = two-factor needed; Ring has sent the code.
	if resp.StatusCode == http.StatusPreconditionFailed || tr.TSVState != "" {
		if code == "" {
			return "", "", true, tr.Phone, nil
		}
		return "", "", false, "", fmt.Errorf("that code was not accepted — try again")
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		if tr.ErrDesc != "" {
			return "", "", false, "", fmt.Errorf("ring login failed: %s", tr.ErrDesc)
		}
		return "", "", false, "", fmt.Errorf("ring login failed (HTTP %d)", resp.StatusCode)
	}
	return tr.AccessToken, tr.RefreshToken, false, "", nil
}

// ringRefreshToken exchanges a stored refresh token for an access token. Ring
// rotates the refresh token on every call, so the caller MUST persist the new one.
func ringRefreshToken(refresh, hardwareID string) (access, newRefresh string, err error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     "ring_official_android",
		"scope":         "client",
		"grant_type":    "refresh_token",
		"refresh_token": refresh,
	})
	req, _ := http.NewRequest(http.MethodPost, ringOAuthURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ringUA)
	req.Header.Set("hardware_id", hardwareID)
	req.Header.Set("2fa-support", "true")
	resp, err := authHTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr tokenResp
	_ = json.Unmarshal(b, &tr)
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		return "", "", fmt.Errorf("ring token refresh failed (HTTP %d)", resp.StatusCode)
	}
	if tr.RefreshToken == "" {
		tr.RefreshToken = refresh
	}
	return tr.AccessToken, tr.RefreshToken, nil
}

// ringDevices lists the account's devices. Doorbells (doorbots) support both motion
// and doorbell-press; cameras support motion only.
func ringDevices(access, hardwareID string) ([]Device, error) {
	req, _ := http.NewRequest(http.MethodGet, ringDevicesURL, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("User-Agent", ringUA)
	req.Header.Set("hardware_id", hardwareID)
	resp, err := authHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ring device list failed (HTTP %d)", resp.StatusCode)
	}
	var payload struct {
		Doorbots           []ringDevice `json:"doorbots"`
		AuthorizedDoorbots []ringDevice `json:"authorized_doorbots"`
		StickupCams        []ringDevice `json:"stickup_cams"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	var out []Device
	add := func(list []ringDevice, ding bool) {
		for _, d := range list {
			out = append(out, Device{ID: d.ID, Name: d.Description, Motion: true, Ding: ding})
		}
	}
	add(payload.Doorbots, true)
	add(payload.AuthorizedDoorbots, true)
	add(payload.StickupCams, false)
	return out, nil
}

type ringDevice struct {
	ID          int64  `json:"id"`
	Description string `json:"description"`
}

// newHardwareID mints a random RFC-4122 v4 UUID, stable per install once persisted.
func newHardwareID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

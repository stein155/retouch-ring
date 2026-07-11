package ring

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var errMissingToken = errors.New("config missing refresh_token/hardware_id — run ring-auth")

// Ring consumer-API constants (reverse-engineered; same values ring-client-api uses).
const (
	ringOAuthURL    = "https://oauth.ring.com/oauth/token"
	ringSessionURL  = "https://api.ring.com/clients_api/session"
	ringDeviceURL   = "https://api.ring.com/clients_api/device"
	ringUA          = "android:com.ringapp"
	ringAPIVersion  = 11
	ringDeviceModel = "soundtouch-ring"

	// Firebase/FCM app identity Ring registers push under.
	fcmAPIKey    = "AIzaSyCv-hdFBmmdBBJadNy-TFwB-xN_H5m3Bk8"
	fcmProjectID = "ring-17770"
	fcmAppID     = "1:876313859327:android:e10ec6ddb3c81f39"
)

var httpc = &http.Client{Timeout: 30 * time.Second}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrDesc      string `json:"error_description"`
}

// ringRefresh exchanges a stored refresh token for an access token. Ring rotates the
// refresh token on every call, so the caller MUST persist the returned one.
func ringRefresh(refreshToken, hardwareID string) (access, newRefresh string, err error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     "ring_official_android",
		"scope":         "client",
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	})
	req, _ := http.NewRequest(http.MethodPost, ringOAuthURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ringUA)
	req.Header.Set("hardware_id", hardwareID)
	req.Header.Set("2fa-support", "true")

	resp, err := httpc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var tr tokenResp
	_ = json.Unmarshal(b, &tr)
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		return "", "", fmt.Errorf("ring refresh failed (%d): %s %s", resp.StatusCode, tr.Error, tr.ErrDesc)
	}
	if tr.RefreshToken == "" {
		tr.RefreshToken = refreshToken // some responses omit it; keep the old one
	}
	return tr.AccessToken, tr.RefreshToken, nil
}

// ringCreateSession registers our hardware_id as an Android session with Ring. Ring
// only delivers push to a token whose device has an active session, so this must run
// before ringRegisterPush (mirrors ring-client-api's POST clients_api/session).
func ringCreateSession(access string) error {
	body, _ := json.Marshal(map[string]any{
		"device": map[string]any{
			"hardware_id": cfg.HardwareID,
			"metadata": map[string]any{
				"api_version":  ringAPIVersion,
				"device_model": ringDeviceModel,
			},
			"os": "android",
		},
	})
	req, _ := http.NewRequest(http.MethodPost, ringSessionURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("User-Agent", ringUA) // ring-client-api sets these on every request
	req.Header.Set("hardware_id", cfg.HardwareID)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ring session failed (%d): %s", resp.StatusCode, string(b))
	}
	debugf("ring: session ok (%d): %s", resp.StatusCode, truncate(string(b), 300))
	return nil
}

// ringRegisterPush tells Ring to deliver motion/ding events to our FCM token.
func ringRegisterPush(access, fcmToken string) error {
	body, _ := json.Marshal(map[string]any{
		"device": map[string]any{
			"metadata": map[string]any{
				"api_version":     ringAPIVersion,
				"device_model":    ringDeviceModel,
				"pn_dict_version": "2.0.0",
				"pn_service":      "fcm",
			},
			"os":                      "android",
			"push_notification_token": fcmToken,
		},
	})
	req, _ := http.NewRequest(http.MethodPatch, ringDeviceURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ringUA)
	req.Header.Set("Authorization", "Bearer "+access)
	if cfg != nil {
		req.Header.Set("hardware_id", cfg.HardwareID)
	}

	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ring push register failed (%d): %s", resp.StatusCode, string(b))
	}
	debugf("ring: device PATCH ok (%d): %s", resp.StatusCode, truncate(string(b), 300))
	return nil
}

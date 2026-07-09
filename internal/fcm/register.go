package go_fcm_receiver

import (
	"encoding/base64"
	"errors"
)

// Register should be called only for a new device. returns the newly created FcmToken, GcmToken, AndroidId, SecurityToken, err
func (f *FCMClient) Register() (string, string, uint64, uint64, error) {

	if f.AppId == "" || f.ProjectID == "" || f.ApiKey == "" {
		err := errors.New("FCMClient must receive an AppId, ProjectID, and ApiKey. read more at https://github.com/morhaviv/go-fcm-receiver/blob/main/README.md#api-deprecation")
		return "", f.GcmToken, f.AndroidId, f.SecurityToken, err
	}

	if f.privateKey == nil || f.authSecret == nil {
		err := errors.New("client's private key hasn't been set. use FCMClient.LoadKeys() or FCMClient.CreateNewKeys()")
		return "", f.GcmToken, f.AndroidId, f.SecurityToken, err
	}

	// Order matters: GCM checkin + register must run BEFORE the FCM registration, because
	// the FCM registration's endpoint embeds the GcmToken (fcm/send/<GcmToken>). Running
	// registerFCM first (as the original did) builds an empty endpoint and yields no token.
	if f.AndroidId == 0 || f.SecurityToken == 0 {
		if err := f.checkInRequestGCM(); err != nil {
			return "", "", 0, 0, err
		}
	}

	if f.GcmToken == "" {
		if err := f.registerRequestGCM(); err != nil {
			return "", "", 0, 0, err
		}
	}

	// FCM registration (web flow, applicationPubKey omitted for non-web). This token is
	// the one to register with the app server (e.g. Ring); it routes via the GcmToken to
	// this client's MCS connection.
	if err := f.registerFCM(); err != nil {
		return "", f.GcmToken, f.AndroidId, f.SecurityToken, err
	}

	return f.FcmToken, f.GcmToken, f.AndroidId, f.SecurityToken, nil
}

// CreateNewKeys returns the newly created privateKey (base64), authSecret (base64), err
func (f *FCMClient) CreateNewKeys() (string, string, error) {
	privateKey, publicKey, authSecret, err := CreateKeys()
	if err != nil {
		return "", "", err
	}
	f.privateKey = privateKey
	f.publicKey = publicKey
	f.authSecret = authSecret

	privateKeyString, err := EncodePrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}

	return base64.StdEncoding.EncodeToString(privateKeyString), base64.StdEncoding.EncodeToString(authSecret), nil
}

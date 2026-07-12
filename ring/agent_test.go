package ring

import (
	"testing"
	"time"
)

func TestPushDeviceID(t *testing.T) {
	cases := []struct {
		body string
		want int64
		ok   bool
	}{
		{`{"ding":{"doorbot_id":12345,"kind":"motion"}}`, 12345, true},
		{`{"data":{"device_id":"67890"}}`, 67890, true},
		{`{"data":{"data":"{\"device\":{\"id\":371329860,\"name\":\"Voordeur\"}}"}}`, 371329860, true},
		{`{"data":{"android_config":"{\"channel\":\"motion_channel_notification141160890\"}"}}`, 141160890, true},
		{`{"data":{"img":"{\"snapshot_uuid\":\"60677b8c-67b0-4893-83c1-0cc1f77bf4b3:158130090\"}"}}`, 158130090, true},
		{`{"alert":"Motion at Front Door"}`, 0, false}, // no id -> caller allows + logs
	}
	for _, c := range cases {
		got, ok := pushDeviceID(c.body)
		if ok != c.ok || got != c.want {
			t.Errorf("pushDeviceID(%q) = %d,%v want %d,%v", c.body, got, ok, c.want, c.ok)
		}
	}
}

func TestFindDeviceGating(t *testing.T) {
	cfg = &Config{Devices: []DeviceRule{
		{ID: 1, Name: "Front", Motion: true, Ding: true},
		{ID: 2, Name: "Back", Motion: false, Ding: false},
	}}
	if findDevice(2) == nil || findDevice(2).Motion {
		t.Error("back door should exist with motion off")
	}
	if findDevice(99) != nil {
		t.Error("unknown device must not match")
	}
}

func TestDetectPushKind(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"data":{"data":"{\"device\":{\"id\":44971689},\"event\":{\"ding\":{\"id\":\"x\"}}}"}}`, "ding"},
		{`{"data":{"data":"{\"device\":{\"id\":44971689},\"event\":{\"ding\":{\"id\":\"x\",\"subtype\":\"ding\"}}}"}}`, "ding"},
		// Ring wraps motion in event.ding too — subtype/detection_type says what it really is.
		{`{"data":{"data":"{\"device\":{\"id\":141160890},\"event\":{\"ding\":{\"id\":\"x\",\"subtype\":\"human\",\"detection_type\":\"human\"}}}"}}`, "motion"},
		{`{"data":{"data":"{\"device\":{\"id\":730096441},\"event\":{\"ding\":{\"id\":\"x\",\"subtype\":\"other_motion\"}}}"}}`, "motion"},
		{`{"data":{"version":"2.0.0","analytics":"{\"subcategory\":\"human\"}","android_config":"{\"category\":\"com.ring.pn.live-event.motion\",\"channel\":\"motion_channel_notification371329860\"}"}}`, "motion"},
		{`{"data":{"android_config":"{\"category\":\"com.ring.pn.live-event.motion\",\"body\":\"There is a Person at your Achterdeur\"}"}}`, "motion"},
		// Non-human motion without analytics must not fall into the ding branch (silent-miss bug).
		{`{"data":{"analytics":"{\"subcategory\":\"motion\"}","data":"{\"device\":{\"id\":141160890},\"event\":{\"ding\":{\"id\":\"x\",\"detection_type\":\"vehicle\"}}}"}}`, "motion"},
		{`{"data":{"msg":"hello"}}`, ""},
	}
	for _, c := range cases {
		if got := detectPushKind(c.body); got != c.want {
			t.Errorf("detectPushKind(%q) = %q want %q", c.body, got, c.want)
		}
	}
}

func TestPushEventTime(t *testing.T) {
	msg := []byte(`{"data":{"analytics":"{\"triggered_at\":1782628248000}","data":"{\"event\":{\"ding\":{\"created_at\":\"2026-06-28T08:30:48Z\"}},\"eventito\":{\"timestamp\":1782628249000}}"}}`)
	got, ok := pushEventTime(msg)
	if !ok {
		t.Fatal("pushEventTime did not find timestamp")
	}
	want, _ := time.Parse(time.RFC3339, "2026-06-28T08:30:48Z")
	if !got.Equal(want) {
		t.Fatalf("pushEventTime = %s want %s", got, want)
	}
}

func TestPushEventTimeMissingAllows(t *testing.T) {
	if _, ok := pushEventTime([]byte(`{"data":{"android_config":"{}"}}`)); ok {
		t.Fatal("missing timestamp should return ok=false")
	}
}

func TestAllowChimeDebouncesPerKind(t *testing.T) {
	cfg = &Config{DebounceSec: 15}
	lastFire = map[string]time.Time{}
	now := time.Unix(100, 0)

	if !allowChime("motion", now) {
		t.Fatal("first motion should chime")
	}
	if allowChime("motion", now.Add(10*time.Second)) {
		t.Fatal("second motion inside debounce should not chime")
	}
	if !allowChime("ding", now.Add(10*time.Second)) {
		t.Fatal("ding should not be muted by motion debounce")
	}
}

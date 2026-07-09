# internal/fcm — vendored go-fcm-receiver

Vendored copy of [morhaviv/go-fcm-receiver](https://github.com/morhaviv/go-fcm-receiver) v1.2.0
(MIT, see LICENSE) plus the patches this project needs (previously carried as a
`replace` directive in go.mod pointing at a private fork):

- Fix 32-bit (arm/386) build: `ReadUint32` hint constant overflows int
- Android FCM register: reorder (checkin+gcm before fcm) + omit empty `applicationPubKey`
- Web GCM register: `X-subtype` must be `wp:receiver.push.com#<uuid>`
- `FCM_DEBUG` MCS-layer logging + `OnRawMessage` hook for unencrypted pushes
- Create `errChan` before starting socket goroutines (race fix)

Vendored because upstream is unmaintained (last commit Dec 2024) and the fix PR
(morhaviv/go-fcm-receiver#26) was closed unmerged — and we don't want to keep a
separate fork repo alive just for a `replace` line.

Package name is kept as `go_fcm_receiver` to minimize the diff against the
original source; import it aliased, e.g. `fcm "github.com/stein155/retouch-ring/internal/fcm"`.

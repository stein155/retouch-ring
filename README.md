# retouch-ring

A **ReTouch plugin** that turns a Bose SoundTouch speaker into a Ring chime: Ring
motion and doorbell presses play short ducked notification sounds over whatever the
speaker is already playing.

Instead of shipping its own installer and autostart, the plugin is downloaded,
verified, run and supervised by ReTouch as a child process, and ReTouch renders its
settings UI. See ReTouch's `docs/plugins.md` for the host contract.

> **Unofficial.** This is a community project, not affiliated with, endorsed by, or
> connected to Bose or Ring/Amazon. "Bose" and "SoundTouch" are trademarks of Bose
> Corporation; "Ring" is a trademark of Ring LLC / Amazon. It talks to your own
> speaker on your own network and to Ring's API with your own account — use at your
> own risk.

> **Status: private for now.** Install it through ReTouch's **Settings → Plugins →
> Install from file** (sideload) using a binary from `sh build.sh`, until the release
> repo is public and can be verified from the catalog.

## What ReTouch provides

ReTouch launches the plugin with:

```
--speaker-host 127.0.0.1:8090   # the speaker's local API
--config-dir   <dir>            # config.json + chimes live here
--listen       127.0.0.1:<port> # loopback address ReTouch reverse-proxies
--host-url     http://…:8000    # ReTouch's base URL (reserved for callbacks)
```

## The plugin API (rendered by ReTouch)

- `GET /health` — liveness.
- `GET /manifest` — the settings UI: log in with Ring **email + password**, then a
  **2FA code** if the account needs one, then per-device **Motion/Doorbell** toggles
  and a **Test chime** button. Status shows whether Ring is connected.
- `POST /action/{id}` — `login`, `verify` (2FA), `save`, `refresh`, `test`, `logout`.
  Each returns the next manifest, so the login → 2FA → connected flow is just a
  sequence of manifests (no Ring-specific code in ReTouch).

The Ring OAuth password + 2FA login lives in `plugin/auth.go`; the credentials it
obtains are written to `config.json` (never leaves the speaker, gitignored), which
the `ring` package (the FCM push listener + chime playback) reads to run the agent.
The plugin (re)starts that agent whenever credentials or device choices change.

## Layout

```
main.go        plugin flag contract + HTTP server + small-RAM runtime tuning
plugin/        the ReTouch adapter: manifest, Ring auth (email/pw/2FA), agent supervision
ring/          the Ring/FCM chime agent (FCM push listen + SoundTouch playback)
internal/fcm/  vendored FCM push receiver (patched go-fcm-receiver; see its README)
assets/        bundled mp3 chimes, embedded and written into the config dir on first run
build.sh       Docker build -> build/retouch-ring-armv7l + SHA256SUMS
.github/       CI (build/vet/test) + Release Drafter + release workflow
```

## Build & test

```sh
sh build.sh
docker run --rm -v "$PWD":/src -w /src golang:1.22 sh -c 'go vet ./... && go test ./...'
```

## Releases

CI builds, vets and tests every push and PR, and confirms the ARMv7 cross-compile.
[Release Drafter](.github/release-drafter.yml) keeps a draft release up to date as
PRs merge to `main`; publishing that draft runs the release workflow, which
cross-compiles `retouch-ring-armv7l`, writes `SHA256SUMS`, and attaches both to the
release — the exact shape ReTouch's plugin catalog installs and verifies.

Releases are **checksum-verified** by default. To make them signature-verified, set
the `RELEASE_SIGNING_KEY` secret (base64 ed25519 private key) and put the matching
public key in ReTouch's catalog entry (`internal/plugins.Catalog`, the `ring` entry's
`PubKey`); the release workflow then also attaches `SHA256SUMS.sig`.

## The FCM receiver

The Firebase push receiver is vendored under [`internal/fcm/`](internal/fcm/): a copy
of `github.com/morhaviv/go-fcm-receiver` (MIT) plus the patches this project needs
(32-bit ARM build fix, web-flow register `X-subtype`, `OnRawMessage`). Upstream is
unmaintained, so it lives in-tree rather than behind a `replace` on a personal fork —
see [`internal/fcm/README.md`](internal/fcm/README.md) for provenance.

## License

[MIT](LICENSE) © Stein Milder. The vendored `internal/fcm/` is MIT-licensed by its
original author (see [`internal/fcm/LICENSE`](internal/fcm/LICENSE)).

## Credits

Chime sounds by [benkirb](https://pixabay.com/users/benkirb-8692052/) on Pixabay
(Pixabay Content License). Ring API values mirror `ring-client-api`.

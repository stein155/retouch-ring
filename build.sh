#!/bin/sh
# Build the retouch-ring plugin for the speaker (ARMv7) plus a SHA256SUMS manifest,
# the way ReTouch's plugin installer expects a release to look. No host Go needed —
# builds inside Docker, matching retouch's own convention.
#
#   sh build.sh            # -> build/retouch-ring-armv7l, build/SHA256SUMS
#
# To publish a verifiable release, also sign SHA256SUMS with the plugin's ed25519
# key and attach SHA256SUMS.sig; put the matching public key in retouch's catalog
# entry (internal/plugins.Catalog). Until the repo is public, install via the UI's
# "Install from file" (sideload) instead.
set -eu

OUT=build
mkdir -p "$OUT"

docker run --rm -v "$PWD":/src -w /src golang:1.22-alpine \
	sh -c 'GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w" -o '"$OUT"'/retouch-ring-armv7l .'

( cd "$OUT" && sha256sum retouch-ring-armv7l > SHA256SUMS )
echo "built $OUT/retouch-ring-armv7l"
cat "$OUT/SHA256SUMS"

#!/bin/sh
# End-to-end check of the app: the mock server serves an album, the app runs
# headless on the NULL audio device (silent), scripted keys go
# Home -> Recently added -> album -> Play, and every frame is saved as PNG.
# Passes if the app exits cleanly, drew frames and streamed the first track.
set -eu
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
mock=""
cleanup() { [ -n "$mock" ] && kill "$mock" 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT

go build -o "$tmp/" ./cmd/mistersubsonic ./tools/mocksubsonic
mkdir "$tmp/music" "$tmp/frames"
cp internal/audio/testdata/tone-44k16.flac "$tmp/music/01-a.flac"
cp internal/audio/testdata/tone-96k24.flac "$tmp/music/02-b.flac"
port=${E2E_UI_PORT:-14535}
cat > "$tmp/config.toml" <<CFG
[[server]]
name = "mock"
url = "http://127.0.0.1:$port"
username = "test"
password = "test"
CFG

"$tmp/mocksubsonic" -dir "$tmp/music" -addr "127.0.0.1:$port" > "$tmp/mock.log" 2>&1 &
mock=$!
sleep 0.5

"$tmp/mistersubsonic" -config "$tmp/config.toml" -display headless -frames "$tmp/frames" -null \
  -keys "a:1500ms,a:800ms,a:800ms" -exit-after 6s > "$tmp/app.log" 2>&1 || {
  echo "e2e-ui failed: app exited with an error" >&2
  cat "$tmp/app.log" >&2
  exit 1
}
frames=$(ls "$tmp/frames" | wc -l)
if [ "$frames" -lt 5 ] || ! grep -q 'stream so-0' "$tmp/mock.log"; then
  echo "e2e-ui failed: $frames frames; mock log:" >&2
  cat "$tmp/mock.log" >&2
  exit 1
fi
echo "e2e-ui ok: $frames frames rendered; Home -> album -> Play streamed the first track (null device)"

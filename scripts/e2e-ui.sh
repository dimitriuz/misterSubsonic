#!/bin/sh
# End-to-end check of the app: the mock server serves an album, the app runs
# headless on the NULL audio device (silent), and every frame is saved as PNG.
# Scripted keys: the home feed's first cover -> album -> Play (streams the
# first track); back to the root, the sidebar -> Search, type "mock", into
# the results -> Tracks -> the second track (streams it).
# Passes if the app exits cleanly, drew frames, searched and streamed both.
set -eu
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
mock=""
cleanup() { if [ -n "$mock" ]; then kill "$mock" 2>/dev/null; fi; rm -rf "$tmp"; }
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
up=""
for _ in $(seq 50); do
  if curl -fs "http://127.0.0.1:$port/rest/ping.view" >/dev/null 2>&1; then up=1; break; fi
  sleep 0.1
done
if [ -z "$up" ]; then
  echo "e2e-ui failed: mock server did not start; log:" >&2
  cat "$tmp/mock.log" >&2
  exit 1
fi

"$tmp/mistersubsonic" -config "$tmp/config.toml" -display headless -frames "$tmp/frames" -null \
  -keys "a:1500ms,a:800ms,b:800ms,b,left,down,down,down,down,down,right,'mock,right:1s$(printf ',right:100ms%.0s' 1 2 3 4 5 6 7 8 9),up,right,right,down,down,a" \
  -exit-after 14s > "$tmp/app.log" 2>&1 || {
  echo "e2e-ui failed: app exited with an error" >&2
  cat "$tmp/app.log" >&2
  exit 1
}
frames=$(ls "$tmp/frames" | wc -l)
# A stream after the search3 line comes from the search results (the album's
# tracks, including the gapless prefetch, were streamed before it).
searched_then_streamed() { awk '/search3 .*query="mock"/ { s = 1 } s && /stream so-/ { ok = 1 } END { exit !ok }' "$tmp/mock.log"; }
if [ "$frames" -lt 5 ] || ! grep -q 'stream so-0' "$tmp/mock.log" || ! searched_then_streamed; then
  echo "e2e-ui failed: $frames frames; mock log:" >&2
  cat "$tmp/mock.log" >&2
  exit 1
fi

# Second run, no config yet: the setup wizard (typed as keyboard text)
# tests the mock server, saves the config (a token, not the password) and
# connects; then the feed's first cover plays.
setup="$tmp/setup"
mkdir "$setup"
before=$(wc -l < "$tmp/mock.log")
"$tmp/mistersubsonic" -config "$setup/config.toml" -display headless -null \
  -keys "pause:1500ms,'127.0.0.1:$port,enter,'test,enter,'test,enter,enter,a:2s,a:3s,a:1s" \
  -exit-after 14s > "$tmp/setup.log" 2>&1 || {
  echo "e2e-ui failed: the setup run exited with an error" >&2
  cat "$tmp/setup.log" >&2
  exit 1
}
if ! grep -q '^ *token = ' "$setup/config.toml" 2>/dev/null || ! grep -q '^ *salt = ' "$setup/config.toml" ||
  grep -q 'password' "$setup/config.toml" || ! tail -n +"$((before + 1))" "$tmp/mock.log" | grep -q 'stream so-'; then
  echo "e2e-ui failed: the wizard didn't save a token config or nothing played after it; config:" >&2
  cat "$setup/config.toml" >&2 2>/dev/null
  tail -n +"$((before + 1))" "$tmp/mock.log" >&2
  exit 1
fi
echo "e2e-ui ok: $frames frames rendered; the feed and Search played; the setup wizard saved a token config and played (null device)"

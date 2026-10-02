#!/bin/sh
# End-to-end check of the app: the mock server serves an album, the app runs
# headless on the NULL audio device (silent), and every frame is saved as PNG.
# Scripted keys: the home feed's first cover -> album -> Play (streams the
# first track); back to the root, the sidebar -> Search, type "mock", into
# the results -> Tracks -> the second track (streams it).
# Passes if the app exits cleanly, drew frames, searched and streamed both.
# A third run turns the web remote on and drives it with curl (localhost only).
set -eu
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
mock=""
app=""
cleanup() {
  if [ -n "$mock" ]; then kill "$mock" 2>/dev/null; fi
  if [ -n "$app" ]; then kill "$app" 2>/dev/null; fi
  rm -rf "$tmp"
}
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
  -exit-after 18s > "$tmp/setup.log" 2>&1 || {
  echo "e2e-ui failed: the setup run exited with an error" >&2
  cat "$tmp/setup.log" >&2
  exit 1
}
if ! grep -q '^ *token = ' "$setup/config.toml" 2>/dev/null || ! grep -q '^ *salt = ' "$setup/config.toml" ||
  grep -q '^ *password *=' "$setup/config.toml" || ! tail -n +"$((before + 1))" "$tmp/mock.log" | grep -q 'stream so-'; then
  echo "e2e-ui failed: the wizard didn't save a token config or nothing played after it; config:" >&2
  cat "$setup/config.toml" >&2 2>/dev/null
  tail -n +"$((before + 1))" "$tmp/mock.log" >&2
  exit 1
fi

# Third run: the web remote. The app runs headless on the null device with
# [remote] on, on its own port; curl reads the state, is refused without the
# right headers, searches, plays the album and toggles pause.
rport=${E2E_REMOTE_PORT:-14537}
base="http://127.0.0.1:$rport"
mkdir "$tmp/remote"
cat > "$tmp/remote/config.toml" <<CFG
[[server]]
name = "mock"
url = "http://127.0.0.1:$port"
username = "test"
password = "test"

[remote]
enabled = true
port = $rport
CFG
remote_fail() {
  echo "e2e-ui failed: the web remote: $1; app log:" >&2
  cat "$tmp/remote.log" >&2
  exit 1
}
rget() { curl -fs --max-time 5 "$base$1"; }
rpost() { curl -fs --max-time 5 -H 'Content-Type: application/json' -H "Origin: $base" -d "$2" "$base$1"; }
# rwait PATH PATTERN: poll a GET until its body matches (5 s at most).
rwait() { for _ in $(seq 50); do rget "$1" 2>/dev/null | grep -q "$2" && return 0; sleep 0.1; done; return 1; }
rbefore=$(wc -l < "$tmp/mock.log")
"$tmp/mistersubsonic" -config "$tmp/remote/config.toml" -display headless -null -exit-after 20s > "$tmp/remote.log" 2>&1 &
app=$!
rwait /api/state '"status":"stopped"' || remote_fail "/api/state never answered"
# Without a JSON content type, or from another site, or for another host name: refused.
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -d '{"do":"toggle"}' "$base/api/cmd")" = 403 ] || remote_fail "a POST without a JSON content type was not refused"
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' -H 'Origin: http://evil.example' -d '{"do":"toggle"}' "$base/api/cmd")" = 403 ] || remote_fail "a foreign Origin was not refused"
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Host: evil.example' "$base/api/state")" = 403 ] || remote_fail "a foreign Host was not refused"
# A song by its id alone, before anything was browsed: the app asks the server
# (getSong). Retried until the app has connected to the mock server.
one=""
for _ in $(seq 50); do
  one=$(rpost /api/play '{"what":"songs","ids":["so-1"],"how":"now"}' 2>/dev/null) && break
  sleep 0.1
done
case "$one" in *'"added":1'*) ;; *) remote_fail "playing one song by id answered '$one'" ;; esac
rwait /api/queue '"id":"so-1"' || remote_fail "the song played by id is not in the queue"
tail -n +"$((rbefore + 1))" "$tmp/mock.log" | grep -q 'getSong so-1' || remote_fail "the app never asked the server for the song"
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' -H "Origin: $base" -d '{"what":"songs","ids":["so-99"],"how":"end"}' "$base/api/play")" = 404 ] || remote_fail "an unknown song id was not a 404"
# The search works too.
rwait '/api/search?q=mock' '"id":"al-mock"' || remote_fail "the search found no album"
added=$(rpost /api/play '{"what":"album","id":"al-mock","how":"now"}') || remote_fail "play failed"
case "$added" in *'"added":2'*) ;; *) remote_fail "play answered $added" ;; esac
rwait /api/state '"status":"playing"' || remote_fail "nothing played after the play request"
[ "$(rget /api/queue | grep -o '"id":"so-[0-9]*"' | wc -l)" = 2 ] || remote_fail "the queue does not hold the album"
rget /api/state | grep -q '"title":"' || remote_fail "the state has no song"
rpost /api/cmd '{"do":"toggle"}' >/dev/null || remote_fail "toggle failed"
rwait /api/state '"status":"paused"' || remote_fail "toggle did not pause"
# A stale song id is a conflict.
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' -H "Origin: $base" -d '{"do":"jump","index":0,"song_id":"nope"}' "$base/api/cmd")" = 409 ] || remote_fail "a stale jump was not a conflict"
# The app ends cleanly and the remote closes with it.
kill -TERM "$app"
wait "$app" || remote_fail "the app exited with an error"
app=""
if curl -s --max-time 2 "$base/api/state" >/dev/null 2>&1; then remote_fail "the remote still answers after the app exited"; fi
tail -n +"$((rbefore + 1))" "$tmp/mock.log" | grep -q 'stream so-' || remote_fail "the mock server streamed nothing"
echo "e2e-ui ok: $frames frames rendered; the feed and Search played; the setup wizard saved a token config and played; the web remote searched, played and paused (null device)"

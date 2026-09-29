#!/bin/sh
# End-to-end check of the playback core: the mock server serves a
# four-track album and mss-cli plays it on the NULL audio device (silent).
# Track 4 goes through the transcode path (HTTP 200, chunked, no length),
# as a Navidrome transcode does. Passes if all four tracks start, none
# reports an error, and the queue finishes.
set -eu
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
mock=""
cleanup() { [ -n "$mock" ] && kill "$mock" 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT

go build -o "$tmp/" ./cmd/mss-cli ./tools/mocksubsonic
mkdir "$tmp/music"
cp internal/audio/testdata/half-a.flac "$tmp/music/01-a.flac"
cp internal/audio/testdata/half-b.flac "$tmp/music/02-b.flac"
cp internal/audio/testdata/tone-44k16.mp3 "$tmp/music/03-c.mp3"
cp internal/audio/testdata/tone-44k16-noxing.mp3 "$tmp/music/04-d-transcoded.mp3"
port=${E2E_PORT:-14533}
cat > "$tmp/config.toml" <<CFG
[[server]]
name = "mock"
url = "http://127.0.0.1:$port"
username = "test"
password = "test"
CFG

"$tmp/mocksubsonic" -dir "$tmp/music" -addr "127.0.0.1:$port" > "$tmp/mock.log" 2>&1 &
mock=$!
i=0
until "$tmp/mss-cli" -config "$tmp/config.toml" ping > /dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 50 ] || { echo "mock server did not start" >&2; cat "$tmp/mock.log" >&2; exit 1; }
  sleep 0.1
done

rc=0
timeout 30 "$tmp/mss-cli" -config "$tmp/config.toml" -null -exit-at-end play-album al-mock < /dev/null > "$tmp/cli.log" 2>&1 || rc=$?
started=$(tr '\r' '\n' < "$tmp/cli.log" | grep -c '^▶' || true)
errors=$(tr '\r' '\n' < "$tmp/cli.log" | grep -c '^!' || true)
transcoded=$(grep -c 'stream so-3 format="mp3"' "$tmp/mock.log" || true)
if [ "$rc" -ne 0 ] || [ "$started" -ne 4 ] || [ "$errors" -ne 0 ] || [ "$transcoded" -eq 0 ] || ! grep -q 'queue finished' "$tmp/cli.log"; then
  echo "e2e failed: exit $rc, $started tracks started, $errors errors, $transcoded transcode requests" >&2
  tr '\r' '\n' < "$tmp/cli.log" >&2
  cat "$tmp/mock.log" >&2
  exit 1
fi
echo "e2e ok: 4 tracks (1 transcoded) played through mock server -> stream -> decode -> resample -> engine (null device)"

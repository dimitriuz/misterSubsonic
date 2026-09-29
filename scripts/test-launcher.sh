#!/bin/bash
# Tests the Scripts-menu launcher with stand-ins for what it talks to on a
# MiSTer: the app, BGM's socket (socat), SAM, pidof and ps.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
launcher=$root/sdcard/Scripts/MiSTer_Subsonic.sh
tmp=$(mktemp -d)
bg=()
cleanup() {
	for p in "${bg[@]}"; do kill "$p" 2>/dev/null; done
	rm -rf "$tmp"
}
trap cleanup EXIT
failures=0

# sandbox makes a fresh case directory with the stand-ins on PATH.
sandbox() {
	c=$tmp/$1
	mkdir -p "$c/bin" "$c/app"
	export MSS_DIR=$c/app MSS_LOCK=$c/lock MSS_BGM_SOCK=$c/bgm.sock MSS_SAM=$c/sam.sh MSS_LOCK_WAIT=1
	export LOG=$c/log PIDS=$c/pids APP_EXIT=0 BGM_STATUS="" PS_OUT=""
	: >"$LOG"
	cat >"$MSS_DIR/mistersubsonic" <<'S'
#!/bin/bash
echo "app $*" >>"$LOG"
exit "$APP_EXIT"
S
	cat >"$c/bin/socat" <<'S'
#!/bin/bash
cmd=$(cat)
echo "bgm $cmd" >>"$LOG"
[ "$cmd" = status ] && printf '%s' "$BGM_STATUS"
exit 0
S
	cat >"$c/bin/pidof" <<'S'
#!/bin/bash
# The live pids listed in $PIDS.<name>.
f=$PIDS.$1
[ -s "$f" ] || exit 1
live=
for p in $(cat "$f"); do kill -0 "$p" 2>/dev/null && live="$live $p"; done
[ -n "$live" ] || exit 1
echo $live
S
	cat >"$c/bin/ps" <<'S'
#!/bin/bash
printf '%s\n' "  PID USER COMMAND" "$PS_OUT"
S
	cat >"$MSS_SAM" <<'S'
#!/bin/bash
echo "sam $1" >>"$LOG"
S
	chmod +x "$c/bin/"* "$MSS_DIR/mistersubsonic" "$MSS_SAM"
	PATH=$c/bin:$ORIG_PATH
}
ORIG_PATH=$PATH

socket() { python3 -c 'import socket,sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$MSS_BGM_SOCK"; }

# expect NAME CODE LINES...: the launcher's exit code and the log, in order.
run_case() {
	name=$1 want_code=$2
	shift 2
	"$launcher" -volume -20 >"$c/out" 2>&1
	code=$?
	want=$(printf '%s\n' "$@")
	got=$(cat "$LOG")
	if [ "$code" != "$want_code" ] || [ "$got" != "$want" ]; then
		echo "FAIL $name: exit $code (want $want_code)"
		echo "  log:"; sed 's/^/    /' "$LOG"
		echo "  want:"; printf '    %s\n' "$@"
		echo "  output:"; sed 's/^/    /' "$c/out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

sandbox plain
run_case "runs the app, then restores the console" 0 "app -volume -20" "app -restore-console"
grep -q "MiSTer Subsonic closed." "$c/out" || { echo "FAIL plain: no closing message"; failures=$((failures + 1)); }

sandbox bgm
socket
BGM_STATUS=$'yes\trandom\tall\t/media/fat/music/x.mp3'
run_case "stops BGM and plays it again after" 0 "bgm status" "bgm stop" "app -volume -20" "app -restore-console" "bgm play"

sandbox bgm-disabled
socket
BGM_STATUS=$'no\tdisabled\tall\t'
run_case "leaves a disabled BGM alone" 0 "bgm status" "app -volume -20" "app -restore-console"

sandbox bgm-stale
touch "$MSS_BGM_SOCK" # a plain file, not a socket
run_case "ignores a BGM socket that isn't one" 0 "app -volume -20" "app -restore-console"

sandbox sam
echo $$ >"$PIDS.MiSTer_SAM_MCP"
run_case "disables SAM and enables it after" 0 "sam disable" "app -volume -20" "app -restore-console" "sam enable"

sandbox sam-old
PS_OUT="  812 root python /media/fat/Scripts/.MiSTer_SAM/MiSTer_SAM_MCP.py"
run_case "finds an older SAM by its process" 0 "sam disable" "app -volume -20" "app -restore-console" "sam enable"

sandbox leftover
sleep 60 &
left=$!
bg+=("$left")
echo "$left" >"$PIDS.mistersubsonic"
run_case "stops a leftover app first" 0 "app -volume -20" "app -restore-console"
if kill -0 "$left" 2>/dev/null; then echo "FAIL leftover: still running"; failures=$((failures + 1)); fi

sandbox crash
socket
BGM_STATUS=$'yes\trandom\tall\tx'
APP_EXIT=2
run_case "restores everything after a crash" 2 "bgm status" "bgm stop" "app -volume -20" "app -restore-console" "bgm play"
grep -q "log.txt" "$c/out" || { echo "FAIL crash: no pointer to the log"; failures=$((failures + 1)); }

sandbox locked
flock "$MSS_LOCK" sleep 30 &
bg+=("$!")
sleep 0.2
run_case "won't start while another launcher holds the lock" 1

sandbox missing
rm "$MSS_DIR/mistersubsonic"
run_case "says when the app is missing" 1

if [ "$failures" -gt 0 ]; then
	echo "test-launcher: $failures failed"
	exit 1
fi
echo "test-launcher ok"

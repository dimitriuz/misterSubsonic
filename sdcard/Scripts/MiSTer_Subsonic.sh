#!/bin/bash
# MiSTer Subsonic: plays music from a Subsonic or Navidrome server.
#
# This launcher runs the app full screen and comes back here when you exit
# it (Settings → Exit, or hold B on the home screen). While the app runs,
# background music (BGM) is stopped and Super Attract Mode (SAM) is
# disabled; both come back afterwards. The app keeps its settings and log
# in /media/fat/mistersubsonic.
#
# The MSS_* variables are for testing.

DIR=${MSS_DIR:-/media/fat/mistersubsonic}
APP=$DIR/mistersubsonic
LOCK=${MSS_LOCK:-/tmp/mistersubsonic.lock}
LOCK_WAIT=${MSS_LOCK_WAIT:-5}
BGM_SOCK=${MSS_BGM_SOCK:-/tmp/bgm.sock}
SAM=${MSS_SAM:-/media/fat/Scripts/MiSTer_SAM_on.sh}

if [ ! -x "$APP" ]; then
	echo "MiSTer Subsonic isn't installed: $APP is missing."
	exit 1
fi

# An app left running (after a crash, or started by hand) would fight this
# one for the screen and the controllers: stop it first.
stop_leftovers() {
	local pids
	pids=$(pidof mistersubsonic) || return 0
	echo "Stopping a MiSTer Subsonic left running..."
	kill $pids 2>/dev/null
	for _ in 1 2 3 4 5; do
		pidof mistersubsonic >/dev/null || return 0
		sleep 1
	done
	pids=$(pidof mistersubsonic) && kill -9 $pids 2>/dev/null
	sleep 1
}
stop_leftovers

# One launcher at a time. Only the app inherits the lock (fd 9) and holds it
# while it runs; everything else closes it, so nothing outlives the launcher
# holding the lock.
exec 9>"$LOCK" || { echo "MiSTer Subsonic can't open its lock file: $LOCK"; exit 1; }
# BusyBox's flock has no -w: poll with -n.
locked=
for ((i = 0; i < LOCK_WAIT; i++)); do
	flock -n 9 && { locked=1; break; }
	sleep 1
done
[ -n "$locked" ] || flock -n 9 || { echo "MiSTer Subsonic is already being started."; exit 1; }

# What the launcher has changed, put back on any way out. A signal during
# the restore must not cut it short, so it ignores them.
bgm_stopped=
sam_disabled=
restore() {
	trap '' INT TERM
	"$APP" -restore-console >/dev/null 2>&1 9>&- # text mode again, even after a crash
	printf '\033[?25h\033[2J\033[H'          # the cursor back, the screen cleared
	[ -n "$bgm_stopped" ] && bgm play
	[ -n "$sam_disabled" ] && "$SAM" enable >/dev/null 2>&1 9>&-
	return 0
}
# INT and TERM go on to the app, which closes cleanly; the launcher then
# restores as usual. Before the app runs there is nothing to pass them to.
app=
sig=
forward() {
	# $! covers a signal between the launch and app=$!.
	local target=${app:-$!}
	if [ -n "$target" ]; then sig=1; kill -TERM "$target" 2>/dev/null; else exit 130; fi
}
trap restore EXIT
trap forward INT TERM

# BGM takes commands on its socket. It can't pause: stop it, play it again after.
bgm() { printf '%s' "$1" | socat -t 2 - "UNIX-CONNECT:$BGM_SOCK" 2>/dev/null 9>&-; }
if [ -S "$BGM_SOCK" ]; then
	status=$(bgm status)
	if [ -n "$status" ] && [ "$(printf '%s' "$status" | cut -f2)" != disabled ]; then
		bgm stop
		bgm_stopped=1
	fi
fi

# SAM would start a game over the app once it thinks the MiSTer is idle.
if [ -x "$SAM" ] && { pidof MiSTer_SAM_MCP >/dev/null || ps | grep -q '[M]iSTer_SAM_MCP'; }; then
	"$SAM" disable >/dev/null 2>&1 9>&-
	sam_disabled=1
fi

printf '\033[?25l' # hide the cursor
# In the background so a signal reaches the trap at once (<&0 keeps its
# stdin). A trapped signal makes wait return 128+n; wait again until a wait
# ends without one: bash keeps the reaped app's status, so that returns its
# own exit code even if it had already ended.
"$APP" "$@" <&0 &
app=$!
while :; do
	sig=
	wait "$app"
	code=$?
	[ -n "$sig" ] || break
done
trap - EXIT
restore
if [ "$code" -eq 0 ]; then
	echo "MiSTer Subsonic closed."
else
	echo "MiSTer Subsonic stopped with an error ($code). See $DIR/log.txt and, after a crash, $DIR/crash.txt."
fi
exit "$code"

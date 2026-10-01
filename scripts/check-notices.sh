#!/bin/bash
# Fails when the app links a module whose licence notice isn't shipped:
# every non-main module must map to a file in third_party/licenses/.
set -u -o pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root" || exit 1

notice_for() {
	case $1 in
	github.com/BurntSushi/toml) echo BurntSushi-toml-COPYING ;;
	golang.org/x/image) echo GO-LICENSE ;;
	golang.org/x/sys) echo GO-LICENSE ;;
	golang.org/x/text) echo GO-LICENSE ;;
	esac
}

# The host's build and the MiSTer's (ARM) build can link different modules.
# (The "VAR=x deps" form below sets the variables for that call only; deps is a
# function, which bash allows, and it does not leak them into later calls.)
deps() { go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/mistersubsonic; }
host=$(deps) || exit 1
arm=$(GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 deps) || exit 1
mods=$(printf '%s\n%s\n' "$host" "$arm" | sort -u) || exit 1
bad=0
for m in $mods; do
	f=$(notice_for "$m")
	if [ -z "$f" ]; then
		echo "check-notices: no licence notice for $m: add it to third_party/licenses/ and to this script" >&2
		bad=1
	elif [ ! -f "third_party/licenses/$f" ]; then
		echo "check-notices: third_party/licenses/$f (for $m) is missing" >&2
		bad=1
	fi
done
[ "$bad" -eq 0 ] && echo "check-notices ok"
exit "$bad"

#!/bin/sh
# Fails if an ELF binary needs a glibc symbol version newer than the MiSTer's.
# usage: check-glibc.sh <binary> [max-version]
set -eu
bin=$1
max=${2:-2.31}
need=$(readelf -V "$bin" | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/GLIBC_//' | sort -uV | tail -1)
if [ -z "$need" ]; then # pure Go, statically linked
  echo "$bin: needs no glibc ok"
  exit 0
fi
top=$(printf '%s\n%s\n' "$need" "$max" | sort -V | tail -1)
if [ "$top" != "$max" ]; then
  echo "$bin needs glibc $need, MiSTer has $max" >&2
  exit 1
fi
echo "$bin: needs glibc $need (<= $max) ok"

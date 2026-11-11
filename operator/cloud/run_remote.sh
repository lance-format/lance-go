#!/usr/bin/env bash
# Task-only bounded execution wrapper; argv must contain no credential values.
set -uo pipefail
output=$1
shift
if [[ ! -d $output || -e $output/exit.txt || $# == 0 ]]; then
  echo 'Provide a fresh evidence directory and a command' >&2
  exit 2
fi
umask 077
printf '%s\0' "$@" > "$output/argv.nul"
date -u +%FT%TZ > "$output/started-at.txt"
timeout --signal=TERM --kill-after=10s 240s "$@" > "$output/stdout.log" 2> "$output/stderr.log"
status=$?
printf '%s\n' "$status" > "$output/exit.txt"
date -u +%FT%TZ > "$output/finished-at.txt"
exit "$status"

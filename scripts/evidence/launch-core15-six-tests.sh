#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 3 ]]; then
	printf 'usage: %s CHECKOUT_DIR EVIDENCE_DIR TMUX_SESSION\n' "$0" >&2
	exit 64
fi

CHECKOUT_DIR=$1
EVIDENCE_DIR=$2
TMUX_SESSION=$3

DRIVER_BRANCH=xuanwo/core15-six-tests-driver-382ed25
SOURCE_COMMIT=0e186d62d0fe3758e190502f9273df7dd7409c55
SOURCE_TREE=8816f47b6112dbf9812a978cbae2e0f56862ecb7
DRIVER_PATH="${CHECKOUT_DIR}.driver.sh"
LAUNCH_LOG="${CHECKOUT_DIR}.launch.txt"

[[ -d "$CHECKOUT_DIR/.git" ]] || {
	printf 'checkout is not a Git repository: %s\n' "$CHECKOUT_DIR" >&2
	exit 65
}
[[ ! -e "$EVIDENCE_DIR" ]] || {
	printf 'evidence directory already exists: %s\n' "$EVIDENCE_DIR" >&2
	exit 65
}
[[ ! -e "$DRIVER_PATH" ]] || {
	printf 'driver path already exists: %s\n' "$DRIVER_PATH" >&2
	exit 65
}
if tmux has-session -t "$TMUX_SESSION" 2>/dev/null; then
	printf 'tmux session already exists: %s\n' "$TMUX_SESSION" >&2
	exit 65
fi

git -C "$CHECKOUT_DIR" fetch origin '+refs/heads/main:refs/remotes/origin/main'
git -C "$CHECKOUT_DIR" fetch origin "+refs/heads/$DRIVER_BRANCH:refs/remotes/origin/$DRIVER_BRANCH"

driver_head=$(git -C "$CHECKOUT_DIR" rev-parse HEAD)
driver_parent=$(git -C "$CHECKOUT_DIR" rev-parse 'HEAD^')
origin_main=$(git -C "$CHECKOUT_DIR" rev-parse origin/main)
origin_driver=$(git -C "$CHECKOUT_DIR" rev-parse "origin/$DRIVER_BRANCH")

[[ "$origin_main" == "$SOURCE_COMMIT" ]] || {
	printf 'origin/main is %s, want %s\n' "$origin_main" "$SOURCE_COMMIT" >&2
	exit 2
}
[[ "$origin_driver" == "$driver_head" ]] || {
	printf 'origin/%s is %s, checked-out driver is %s\n' "$DRIVER_BRANCH" "$origin_driver" "$driver_head" >&2
	exit 2
}
[[ "$driver_parent" == "$SOURCE_COMMIT" ]] || {
	printf 'driver parent is %s, want %s\n' "$driver_parent" "$SOURCE_COMMIT" >&2
	exit 2
}
[[ "$driver_head" != "$SOURCE_COMMIT" ]] || {
	printf 'driver commit unexpectedly equals source commit\n' >&2
	exit 2
}

git -C "$CHECKOUT_DIR" show "$driver_head:scripts/evidence/run-core15-six-tests.sh" >"$DRIVER_PATH"
chmod +x "$DRIVER_PATH"
bash -n "$DRIVER_PATH"

git -C "$CHECKOUT_DIR" checkout --detach "$SOURCE_COMMIT"
checked_out_head=$(git -C "$CHECKOUT_DIR" rev-parse HEAD)
checked_out_tree=$(git -C "$CHECKOUT_DIR" rev-parse 'HEAD^{tree}')
[[ "$checked_out_head" == "$SOURCE_COMMIT" ]] || {
	printf 'checked-out HEAD is %s, want %s\n' "$checked_out_head" "$SOURCE_COMMIT" >&2
	exit 2
}
[[ "$checked_out_tree" == "$SOURCE_TREE" ]] || {
	printf 'checked-out tree is %s, want %s\n' "$checked_out_tree" "$SOURCE_TREE" >&2
	exit 2
}
if [[ -n "$(git -C "$CHECKOUT_DIR" status --porcelain=v1 --untracked-files=all)" ]]; then
	printf 'checked-out source tree is not clean\n' >&2
	exit 2
fi

{
	printf 'launched_at_utc=%s\n' "$(date -u +%FT%TZ)"
	printf 'driver_branch=%s\n' "$DRIVER_BRANCH"
	printf 'driver_commit=%s\n' "$driver_head"
	printf 'origin_driver=%s\n' "$origin_driver"
	printf 'origin_main=%s\n' "$origin_main"
	printf 'checked_out_head=%s\n' "$checked_out_head"
	printf 'checked_out_tree=%s\n' "$checked_out_tree"
	printf 'checkout_dir=%s\n' "$CHECKOUT_DIR"
	printf 'driver_path=%s\n' "$DRIVER_PATH"
	printf 'evidence_dir=%s\n' "$EVIDENCE_DIR"
	printf 'tmux_session=%s\n' "$TMUX_SESSION"
	tmux -V
} >"$LAUNCH_LOG"

tmux new-session -d -s "$TMUX_SESSION" \
	"bash $(printf '%q' "$DRIVER_PATH") $(printf '%q' "$CHECKOUT_DIR") $(printf '%q' "$EVIDENCE_DIR")"
printf 'launched session %s for %s\n' "$TMUX_SESSION" "$checked_out_head"

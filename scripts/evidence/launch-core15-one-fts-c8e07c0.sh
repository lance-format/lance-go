#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 3 ]]; then
	printf 'usage: %s CHECKOUT_DIR EVIDENCE_DIR TMUX_SESSION\n' "$0" >&2
	exit 64
fi

CHECKOUT_DIR=$1
EVIDENCE_DIR=$2
TMUX_SESSION=$3

DRIVER_BRANCH=xuanwo/core15-one-fts-driver-c8e07c0
SOURCE_BRANCH=xuanwo/go-fts-empty-shard-acceptance
SOURCE_COMMIT=6c21e7520f40888a2f780f7b3ecf0ec18a7cd91c
SOURCE_TREE=260fa4a8c9c73a36da52feb0f769af1f31a1680e
DRIVER_PATH="${CHECKOUT_DIR}.driver.sh"
FINALIZER_PATH="${CHECKOUT_DIR}.finalize.py"
LAUNCH_LOG="${CHECKOUT_DIR}.launch.txt"
WAIT_CHANNEL="${TMUX_SESSION}-done"
LAUNCHER_PATH=$0

[[ -d "$CHECKOUT_DIR/.git" ]] || {
	printf 'checkout is not a Git repository: %s\n' "$CHECKOUT_DIR" >&2
	exit 65
}
[[ ! -e "$EVIDENCE_DIR" ]] || {
	printf 'evidence directory already exists: %s\n' "$EVIDENCE_DIR" >&2
	exit 65
}
for path in "$DRIVER_PATH" "$FINALIZER_PATH" "$LAUNCH_LOG"; do
	[[ ! -e "$path" ]] || {
		printf 'output path already exists: %s\n' "$path" >&2
		exit 65
	}
done
if tmux has-session -t "$TMUX_SESSION" 2>/dev/null; then
	printf 'tmux session already exists: %s\n' "$TMUX_SESSION" >&2
	exit 65
fi

git -C "$CHECKOUT_DIR" fetch origin "+refs/heads/$SOURCE_BRANCH:refs/remotes/origin/$SOURCE_BRANCH"
git -C "$CHECKOUT_DIR" fetch origin "+refs/heads/$DRIVER_BRANCH:refs/remotes/origin/$DRIVER_BRANCH"

origin_source=$(git -C "$CHECKOUT_DIR" rev-parse "origin/$SOURCE_BRANCH")
origin_driver=$(git -C "$CHECKOUT_DIR" rev-parse "origin/$DRIVER_BRANCH")

[[ "$origin_source" == "$SOURCE_COMMIT" ]] || {
	printf 'origin/%s is %s, want %s\n' "$SOURCE_BRANCH" "$origin_source" "$SOURCE_COMMIT" >&2
	exit 2
}
git -C "$CHECKOUT_DIR" merge-base --is-ancestor "$SOURCE_COMMIT" "$origin_driver" || {
	printf 'driver commit %s does not descend from %s\n' "$origin_driver" "$SOURCE_COMMIT" >&2
	exit 2
}
[[ "$origin_driver" != "$SOURCE_COMMIT" ]] || {
	printf 'driver commit unexpectedly equals source commit\n' >&2
	exit 2
}

git -C "$CHECKOUT_DIR" show "$origin_driver:scripts/evidence/run-core15-one-fts-c8e07c0.sh" >"$DRIVER_PATH"
git -C "$CHECKOUT_DIR" show "$origin_driver:scripts/evidence/finalize-core15-one-fts-c8e07c0.py" >"$FINALIZER_PATH"
chmod +x "$DRIVER_PATH"
bash -n "$DRIVER_PATH"
python3 -B -m py_compile "$FINALIZER_PATH"

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
	printf 'source_branch=%s\n' "$SOURCE_BRANCH"
	printf 'driver_branch=%s\n' "$DRIVER_BRANCH"
	printf 'origin_source=%s\n' "$origin_source"
	printf 'origin_driver=%s\n' "$origin_driver"
	printf 'checked_out_head=%s\n' "$checked_out_head"
	printf 'checked_out_tree=%s\n' "$checked_out_tree"
	printf 'checkout_dir=%s\n' "$CHECKOUT_DIR"
	printf 'driver_path=%s\n' "$DRIVER_PATH"
	printf 'finalizer_path=%s\n' "$FINALIZER_PATH"
	printf 'evidence_dir=%s\n' "$EVIDENCE_DIR"
	printf 'tmux_session=%s\n' "$TMUX_SESSION"
	printf 'wait_channel=%s\n' "$WAIT_CHANNEL"
	tmux -V
} >"$LAUNCH_LOG"

driver_cmd="bash $(printf '%q' "$DRIVER_PATH") $(printf '%q' "$CHECKOUT_DIR") $(printf '%q' "$EVIDENCE_DIR") $(printf '%q' "$FINALIZER_PATH") $(printf '%q' "$LAUNCHER_PATH") $(printf '%q' "$LAUNCH_LOG"); status=\$?; tmux wait-for -S $(printf '%q' "$WAIT_CHANNEL"); exit \$status"
tmux new-session -d -s "$TMUX_SESSION" "$driver_cmd"
printf 'launched session %s for %s; wait channel %s\n' "$TMUX_SESSION" "$checked_out_head" "$WAIT_CHANNEL"

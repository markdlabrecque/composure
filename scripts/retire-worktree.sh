#!/usr/bin/env bash

# Project teardown hook. The shared retirement engine owns Git removal.
set -u

if [[ "${RETIRE_WORKTREE_IN_HOOK:-}" != "1" ]]; then
  echo "Use the retire-worktree skill with this script as RETIRE_HOOK." >&2
  exit 3
fi

if [[ ! -d .ddev ]]; then
  echo "retire-worktree: no DDEV project in this worktree."
elif ! command -v ddev >/dev/null 2>&1; then
  echo "retire-worktree: DDEV is unavailable; its environment was not removed." >&2
elif ddev delete -yO >/dev/null 2>&1; then
  echo "retire-worktree: DDEV project removed."
else
  echo "retire-worktree: DDEV teardown failed; inspect the environment before reporting complete cleanup." >&2
fi

# The engine's existing best-effort teardown policy continues Git removal.
exit 0

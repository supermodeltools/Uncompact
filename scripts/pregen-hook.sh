#!/usr/bin/env bash
# Uncompact pregen hook — spawns background cache warming after Write/Edit ops.
# Designed to be fast: exits in <1s even when spawning a background job.
# Invoked by the Claude Code PostToolUse hook.

# Find the uncompact binary across common install paths.
find_uncompact() {
  if command -v uncompact >/dev/null 2>&1; then
    command -v uncompact
    return
  fi
  for candidate in \
    "${HOME}/go/bin/uncompact" \
    "${HOME}/.local/bin/uncompact" \
    "/usr/local/bin/uncompact" \
    "/opt/homebrew/bin/uncompact"; do
    if [ -x "$candidate" ]; then
      echo "$candidate"
      return
    fi
  done
}

UNCOMPACT="$(find_uncompact)"

if [ -z "$UNCOMPACT" ]; then
  # Binary not installed — exit silently.
  exit 0
fi

# Per-user lock file to prevent concurrent API calls.
LOCK_FILE="${TMPDIR:-/tmp}/uncompact-pregen-${UID:-$(id -u)}.lock"

# If a pregen is already running, exit immediately.
if [ -f "$LOCK_FILE" ]; then
  LOCK_PID="$(cat "$LOCK_FILE" 2>/dev/null)"
  if [ -n "$LOCK_PID" ] && kill -0 "$LOCK_PID" 2>/dev/null; then
    # Pregen already in progress — skip.
    exit 0
  fi
  # Stale lock (process exited without cleanup) — remove it.
  rm -f "$LOCK_FILE"
fi

# Spawn pregen in the background.
# The trap inside the subshell ensures the lock is removed on exit.
(
  trap 'rm -f "${LOCK_FILE}"' EXIT
  "${UNCOMPACT}" pregen
) >/dev/null 2>&1 &

# Write the actual background job PID to the lock file.
# Must be done outside the subshell so $! refers to the spawned process.
echo $! > "${LOCK_FILE}"

# Exit immediately — never block Claude Code hooks.
exit 0

#!/usr/bin/env bash
# Uncompact plugin hook — reinjects project context after Claude Code compaction.
# Invoked by the Claude Code SessionStart:compact hook.

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
  # Binary not installed — exit silently to avoid blocking Claude Code.
  # Install with: go install github.com/supermodeltools/uncompact@latest
  exit 0
fi

# SUPERMODEL_API_KEY is read directly from the environment by the uncompact binary.
# Do not pass it as a CLI argument to avoid exposing it in process listings (ps aux).

# Run uncompact and capture output.
# --post-compact appends an instruction so Claude acknowledges restoration in its response.
OUTPUT="$("$UNCOMPACT" run --fallback --post-compact)"

DISPLAY_CACHE="${TMPDIR:-/tmp}/uncompact-display-${UID:-$(id -u)}.txt"

if [ -n "$OUTPUT" ]; then
  # Write securely and atomically to avoid disclosure/races.
  umask 077
  TMP_CACHE="$(mktemp "${DISPLAY_CACHE}.XXXXXX")" || exit 0
  printf '%s\n' "$OUTPUT" > "$TMP_CACHE"
  mv -f "$TMP_CACHE" "$DISPLAY_CACHE"
fi

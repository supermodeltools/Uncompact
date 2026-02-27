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
OUTPUT="$("$UNCOMPACT" run --fallback)"

if [ -n "$OUTPUT" ]; then
  # Emit to stdout — injected into Claude Code's context (AI-visible).
  echo "$OUTPUT"

  # Cache output for user-visible display on the next UserPromptSubmit.
  # uncompact show-cache picks this up and replays it into the context.
  DISPLAY_CACHE="${TMPDIR:-/tmp}/uncompact-display-${UID:-$(id -u)}.txt"
  echo "$OUTPUT" > "$DISPLAY_CACHE"

  # Print a status line to stderr — visible in the terminal during compact.
  CHAR_COUNT="${#OUTPUT}"
  APPROX_TOKENS=$(( CHAR_COUNT / 4 ))
  echo "[uncompact] Context injected (~${APPROX_TOKENS} tokens)" >&2
fi

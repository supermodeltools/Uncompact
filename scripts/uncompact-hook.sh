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

DISPLAY_CACHE="${TMPDIR:-/tmp}/uncompact-display-${UID:-$(id -u)}.txt"

if [ -n "$OUTPUT" ]; then
  CHAR_COUNT="${#OUTPUT}"
  APPROX_TOKENS=$(( CHAR_COUNT / 4 ))

  # Write to display cache — UserPromptSubmit hook will show this as a visible
  # transcript message on the user's next message.
  printf '%s\n\n[uncompact] Context restored (~%d tokens)\n' "$OUTPUT" "$APPROX_TOKENS" > "$DISPLAY_CACHE"
fi

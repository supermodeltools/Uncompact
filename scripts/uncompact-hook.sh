#!/usr/bin/env bash
# Uncompact plugin hook — reinjects project context after Claude Code compaction.
# Invoked by the Claude Code Stop hook via ${CLAUDE_PLUGIN_ROOT}/scripts/uncompact-hook.sh.

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

# Build argument list.
# Always enable --fallback so something is emitted even if the cache isn't warm
# yet (e.g. first run after install before pregen completes) or the API is slow.
ARGS=("run" "--fallback")

# SUPERMODEL_API_KEY is read directly from the environment by the uncompact binary.
# Do not pass it as a CLI argument to avoid exposing it in process listings (ps aux).

# Execute uncompact run — stdout is injected into Claude Code's context after compaction.
exec "$UNCOMPACT" "${ARGS[@]}"

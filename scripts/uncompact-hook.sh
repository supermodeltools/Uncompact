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
ARGS=("run")

# In remote/CI environments (CLAUDE_CODE_REMOTE=true), enable --fallback so a
# minimal context is emitted even if the Supermodel API is unreachable.
if [ "${CLAUDE_CODE_REMOTE:-}" = "true" ]; then
  ARGS+=("--fallback")
fi

# Allow API key override via environment variable (useful in CI).
if [ -n "${SUPERMODEL_API_KEY:-}" ]; then
  ARGS+=("--api-key" "${SUPERMODEL_API_KEY}")
fi

# Execute uncompact run — stdout is injected into Claude Code's context after compaction.
exec "$UNCOMPACT" "${ARGS[@]}"

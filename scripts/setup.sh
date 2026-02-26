#!/usr/bin/env bash
# Uncompact plugin setup — auto-installs the uncompact binary on SessionStart.
# Safe to run repeatedly: exits immediately if already installed.

set -e

BINARY="uncompact"
INSTALL_URL="github.com/supermodeltools/uncompact@latest"

# Find the uncompact binary across common install paths.
find_uncompact() {
  if command -v "$BINARY" >/dev/null 2>&1; then
    command -v "$BINARY"
    return 0
  fi
  for candidate in \
    "${HOME}/go/bin/${BINARY}" \
    "${HOME}/.local/bin/${BINARY}" \
    "/usr/local/bin/${BINARY}" \
    "/opt/homebrew/bin/${BINARY}"; do
    if [ -x "$candidate" ]; then
      echo "$candidate"
      return 0
    fi
  done
  return 1
}

# Already installed — nothing to do.
if find_uncompact >/dev/null 2>&1; then
  exit 0
fi

# Try to install via go install (requires Go 1.22+).
if command -v go >/dev/null 2>&1; then
  echo "[uncompact] Installing via go install..." >&2
  if go install "${INSTALL_URL}" >/dev/null 2>&1; then
    echo "[uncompact] Installed successfully. Run 'uncompact auth login' to authenticate." >&2
    exit 0
  fi
fi

# Binary not installed and go not available — prompt user.
echo "[uncompact] Binary not found. Install with:" >&2
echo "  go install ${INSTALL_URL}" >&2
echo "Then run: uncompact auth login" >&2

# Exit 0 — never block Claude Code sessions.
exit 0

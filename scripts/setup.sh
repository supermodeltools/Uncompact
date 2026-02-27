#!/usr/bin/env bash
# Uncompact plugin setup — auto-installs the uncompact binary on SessionStart.
# Safe to run repeatedly: exits immediately if already installed.

set -e

BINARY="uncompact"
INSTALL_URL="github.com/supermodeltools/uncompact@latest"

# Print a reminder to stderr if the user hasn't authenticated yet.
# Uses a fast config-file check (no network call) so it doesn't slow every startup.
check_auth() {
  local binary="$1"  # unused, kept for forward-compat
  # Env var takes precedence — if set, assume authenticated.
  [ -n "${SUPERMODEL_API_KEY:-}" ] && return 0

  # Locate the platform-specific config file (mirrors config.go ConfigDir logic).
  local config_file
  case "$(uname -s)" in
    Darwin)
      config_file="${HOME}/Library/Application Support/uncompact/config.json" ;;
    MINGW*|MSYS*|CYGWIN*)
      config_file="${APPDATA:-${HOME}/AppData/Roaming}/uncompact/config.json" ;;
    *)
      config_file="${XDG_CONFIG_HOME:-${HOME}/.config}/uncompact/config.json" ;;
  esac

  # If config file has a non-empty api_key value, auth is configured.
  if [ -f "$config_file" ] && grep -qE '"api_key"[[:space:]]*:[[:space:]]*"[^"]+"' "$config_file" 2>/dev/null; then
    return 0
  fi

  echo "" >&2
  echo "[uncompact] ⚠️  Authentication required — the Stop hook won't inject context until you log in." >&2
  echo "[uncompact]    Run: uncompact auth login" >&2
  echo "" >&2
}

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

# Already installed — check auth and exit.
if find_uncompact >/dev/null 2>&1; then
  UNCOMPACT="$(find_uncompact)"
  check_auth "$UNCOMPACT"
  exit 0
fi

# Try to install via go install (requires Go 1.22+).
if command -v go >/dev/null 2>&1; then
  echo "[uncompact] Installing via go install..." >&2
  install_output=$(go install "${INSTALL_URL}" 2>&1)
  if [ $? -eq 0 ]; then
    echo "[uncompact] Installed successfully." >&2
    UNCOMPACT="$(find_uncompact)"
    check_auth "$UNCOMPACT"
    exit 0
  else
    echo "[uncompact] go install failed:" >&2
    echo "$install_output" >&2
  fi
fi

# Binary not installed and go not available — prompt user.
echo "[uncompact] Binary not found. Install with:" >&2
echo "  go install ${INSTALL_URL}" >&2
echo "Then run: uncompact auth login" >&2

# Exit 0 — never block Claude Code sessions.
exit 0

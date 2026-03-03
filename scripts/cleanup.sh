#!/usr/bin/env bash
# Uncompact Total Cleanup — wipe everything for end-to-end testing.
# This script removes:
# 1. Claude Code hooks
# 2. Global npm package
# 3. Config directory
# 4. Data/Cache directory

set -e

echo "🧹 Starting total cleanup of Uncompact..."

# 1. Remove Claude Code hooks
if command -v uncompact >/dev/null 2>&1; then
  echo "👉 Removing Claude Code hooks..."
  uncompact uninstall --yes || true
fi

# 2. Remove global npm package
if command -v npm >/dev/null 2>&1; then
  echo "👉 Uninstalling global npm package..."
  npm uninstall -g uncompact || true
fi

# 3. Remove config and data directories
echo "👉 Wiping config and data directories..."

case "$(uname -s)" in
  Darwin)
    rm -rf "${HOME}/.config/uncompact"
    rm -rf "${HOME}/Library/Application Support/uncompact"
    ;;
  MINGW*|MSYS*|CYGWIN*)
    # Windows paths are usually handled via env vars in shell
    [ -n "$APPDATA" ] && rm -rf "$APPDATA/uncompact"
    [ -n "$LOCALAPPDATA" ] && rm -rf "$LOCALAPPDATA/uncompact"
    ;;
  *)
    # Linux / Others
    rm -rf "${XDG_CONFIG_HOME:-${HOME}/.config}/uncompact"
    rm -rf "${XDG_DATA_HOME:-${HOME}/.local/share}/uncompact"
    ;;
esac

echo ""
echo "✅ Uncompact has been completely removed."
echo "You can now test the one-command install:"
echo "  npm install -g uncompact"

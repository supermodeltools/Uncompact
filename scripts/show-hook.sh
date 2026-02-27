#!/usr/bin/env bash
# Uncompact show hook — displays the post-compact context bomb in the chat transcript.
# Invoked by the Claude Code UserPromptSubmit hook.
#
# uncompact-hook.sh caches its output to a temp file after each compaction.
# This hook replays that output exactly once (on the first user message after
# compact), making it visible in the chat history, then removes the cache.

DISPLAY_CACHE="${TMPDIR:-/tmp}/uncompact-display-${UID:-$(id -u)}.txt"

if [ ! -f "$DISPLAY_CACHE" ]; then
  exit 0
fi

# Read and remove atomically — prevent double-display if hooks fire concurrently.
OUTPUT="$(cat "$DISPLAY_CACHE")"
rm -f "$DISPLAY_CACHE"

if [ -n "$OUTPUT" ]; then
  echo "$OUTPUT"
fi

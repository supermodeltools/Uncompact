#!/usr/bin/env bash
# Uncompact show hook — displays the post-compact context bomb in the chat transcript.
# Invoked by the Claude Code UserPromptSubmit hook.
#
# uncompact-hook.sh caches its output to a temp file after each compaction.
# This hook replays that output exactly once (on the first user message after
# compact), making it visible in the chat history, then removes the cache.

DISPLAY_CACHE="${TMPDIR:-/tmp}/uncompact-display-${UID:-$(id -u)}.txt"

# Atomically claim the cache file via mv (POSIX guarantees mv on same filesystem
# is atomic). Whichever concurrent invocation wins the mv gets the content; any
# racing invocations see no file at the original path and exit cleanly.
TMP_READ="$(mktemp)"
mv -f "$DISPLAY_CACHE" "$TMP_READ" 2>/dev/null || { rm -f "$TMP_READ"; exit 0; }
OUTPUT="$(cat "$TMP_READ")"
rm -f "$TMP_READ"

if [ -n "$OUTPUT" ]; then
  CHAR_COUNT="${#OUTPUT}"
  APPROX_TOKENS=$(( CHAR_COUNT / 4 ))
  printf '%s\n\n[uncompact] Context restored (~%d tokens)\n' "$OUTPUT" "$APPROX_TOKENS"
fi

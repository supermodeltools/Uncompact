# Uncompact

Uncompact prevents Claude Code compaction from degrading your workflow. Instead of losing context at the end of a context window, Uncompact re-injects a dense Markdown "context bomb" via Claude Code hooks — powered by the [Supermodel Public API](https://dashboard.supermodeltools.com).

## How It Works

1. Uncompact registers as a Claude Code hook
2. When a hook fires (post-compaction, session start, etc.), Uncompact fetches your project graph from the Supermodel API
3. It renders a prioritized Markdown context bomb (≤2,000 tokens by default) and injects it back into your Claude Code session

## Prerequisites

- A Supermodel subscription — get your API key at [dashboard.supermodeltools.com](https://dashboard.supermodeltools.com)
- Go 1.22+ (for building from source) or download a pre-built binary from Releases

## Installation

### 1. Install the binary

```bash
go install github.com/supermodeltools/uncompact@latest
```

### 2. Set your API key

```bash
export SUPERMODEL_API_KEY=<your-key>
```

Add this to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) to persist it.

### 3. Install hooks into Claude Code

```bash
uncompact install
```

This detects your `settings.json` location, merges the Uncompact hooks without clobbering existing configuration, and shows a diff before writing.

Or use the guided setup wizard:

```bash
uncompact init
```

### Manual hook configuration

Add the following to your Claude Code `settings.json` (see the [hooks guide](https://code.claude.com/docs/en/hooks-guide#re-inject-context-after-compaction)):

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "uncompact run"
          }
        ]
      }
    ]
  }
}
```

## Usage

```
uncompact run              # Generate and output a context bomb (used by hooks)
uncompact dry-run          # Preview what would be injected without writing
uncompact install          # Merge hooks into Claude Code settings.json
uncompact init             # Interactive setup wizard
uncompact verify-install   # Check hook configuration is correct
uncompact status           # Show last injection time, token size, cache freshness
uncompact logs             # View recent activity
uncompact stats            # Token usage, cache hit rate, API call count
```

### Flags

```
--max-tokens N     Cap context bomb output (default: 2000)
--force-refresh    Bypass cache and fetch fresh data from API
--fallback         Emit minimal static context if full mode fails
--debug            Enable debug logging
```

## Architecture

- **CLI**: Go + [Cobra](https://github.com/spf13/cobra)
- **Cache**: SQLite via `mattn/go-sqlite3` — versioned graph snapshots with TTL, staleness warnings, and a 100MB storage cap
- **API**: Supermodel Public API — subscription auth via Bearer token
- **Output**: Prompt templating (`text/template`) linearizes the JSON graph into prioritized Markdown sections

### Graceful degradation

When the API is unavailable, Uncompact serves the last successful cached output (with a `[STALE]` prefix). If both the API and cache are unavailable, it exits silently (exit 0, no output) to avoid blocking your Claude Code session.

## Development

```bash
git clone https://github.com/supermodeltools/uncompact
cd uncompact
go build ./...
go test ./...
```

# Uncompact

Compaction is ruining your Claude Code workflow. When compacting, Claude becomes
stupid and unusable. Uncompact fixes this by hooking into Claude Code's lifecycle
to reinject a high-density "context bomb" after compaction.

Powered by the [Supermodel Public API](https://supermodeltools.com).

## How It Works

1. Claude Code compaction fires the `Stop` hook
2. Uncompact queries the Supermodel API for a project-specific context graph
3. The graph is linearized into a compact Markdown "context bomb" (≤2,000 tokens by default)
4. The context bomb is injected back into Claude Code's context window
5. Claude resumes with full situational awareness

Local SQLite caching means the context bomb is served instantly on repeat
injections, with automatic staleness warnings when the API is unreachable.

## Installation

### 1. Subscribe & Authenticate

```bash
# Visit the dashboard to subscribe and get your API key
uncompact auth login
```

Get your API key at **[dashboard.supermodeltools.com](https://dashboard.supermodeltools.com)**.

Or set it via environment variable:

```bash
export SUPERMODEL_API_KEY=your_key_here
```

### 2. Install Claude Code Hooks

```bash
uncompact install
```

This auto-detects your `settings.json`, shows a diff, and merges the hooks
non-destructively. No manual copy-pasting required.

For manual installation, see the
[Claude Code hooks guide](https://code.claude.com/docs/en/hooks-guide#re-inject-context-after-compaction).

### 3. Verify

```bash
uncompact verify-install
uncompact status
```

## Usage

```
uncompact auth login             Authenticate via dashboard.supermodeltools.com
uncompact auth status            Show auth status and subscription tier

uncompact install                Auto-install hooks into settings.json (with diff)
uncompact init                   Interactive first-time setup wizard
uncompact verify-install         Validate hooks are correctly configured

uncompact run                    Emit context bomb to stdout (used by hooks)
uncompact dry-run                Show what would be injected without outputting it

uncompact status                 Last injection time, cache state, token size
uncompact logs [--tail N]        Recent injection activity
uncompact stats                  Token usage, cache hit rate, API call counts

uncompact cache clear            Clear workspace cache
uncompact cache clear --all      Clear all workspace caches
uncompact cache prune            Remove expired entries
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--max-tokens N` | 2000 | Maximum tokens in the context bomb output |
| `--force-refresh` | false | Bypass cache and fetch from API |
| `--fallback` | false | Use minimal static context if API fails |
| `--rate-limit N` | 5 | Minimum minutes between injections (0 = off) |
| `--debug` | false | Enable debug logging to stderr |

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  Claude Code                                         │
│  ┌─────────────────────────────────────────────────┐ │
│  │  Stop Hook ──► uncompact run ──► stdout inject  │ │
│  └─────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────┐
│  Uncompact CLI                                       │
│                                                      │
│  ┌───────────┐    ┌───────────┐    ┌──────────────┐ │
│  │ API Client│───►│ SQLite DB │    │ Context Bomb │ │
│  │(Supermodel│    │ (cache +  │───►│   Renderer   │ │
│  │   API)    │    │  logs)    │    │  (Markdown)  │ │
│  └───────────┘    └───────────┘    └──────────────┘ │
└──────────────────────────────────────────────────────┘
                          │
                          ▼
          https://api.supermodeltools.com
```

### Context Bomb Design

The context bomb is tiered by signal priority:

| Tier | Sections | Behavior |
|------|----------|----------|
| Required | Project Overview, Recent Changes, Active Tasks | Always included |
| Optional | Project Structure, Dependencies, Custom Context | Included if token budget allows |

The bomb is capped at `--max-tokens` (default: 2,000) to avoid triggering the
next compaction.

### Caching

| Concern | Policy |
|---------|--------|
| TTL | 15 minutes (configurable in `config.json`) |
| Stale cache | Served with `[STALE: last updated N ago]` warning |
| Storage cap | Auto-prune entries older than 30 days |
| Force refresh | `--force-refresh` flag or `uncompact cache clear` |
| Schema | Embedded migrations, forward-compatible |

### Graceful Degradation

Uncompact never blocks a Claude Code session:

1. API unavailable + fresh cache → serve cached context bomb with staleness warning
2. API unavailable + stale cache → serve stale cache with warning
3. API unavailable + no cache + `--fallback` → emit minimal static fallback
4. API unavailable + no cache → exit 0 silently (never blocks session)

## Hook Configuration

The hooks installed by `uncompact install` look like this in `settings.json`:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": "uncompact run" }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "session_start",
        "hooks": [
          { "type": "command", "command": "uncompact run --rate-limit 5" }
        ]
      }
    ]
  }
}
```

The `Stop` hook (primary) fires after compaction. The `UserPromptSubmit` hook
(optional, rate-limited) reinjects context at session start.

Reference: [Claude Code hooks guide](https://code.claude.com/docs/en/hooks-guide#re-inject-context-after-compaction)

## Configuration

Config file location:
- **macOS**: `~/Library/Application Support/uncompact/config.json`
- **Linux**: `~/.config/uncompact/config.json`
- **Windows**: `%APPDATA%\uncompact\config.json`

Override with `UNCOMPACT_CONFIG_DIR` environment variable.

```json
{
  "api_key": "your_key_here",
  "api_url": "https://api.supermodeltools.com",
  "cache_ttl_minutes": 15,
  "max_cache_mb": 100
}
```

`SUPERMODEL_API_KEY` env var takes priority over the config file.

## KPIs

| Metric | How to Measure |
|--------|----------------|
| Clarifying questions per session | Lower = better context retention post-injection |
| Post-compaction task completion | Higher = less context degradation |
| Token efficiency | `uncompact stats` — useful tokens / total tokens |
| Cache hit rate | `uncompact stats` — % of injections from cache |
| 7-day retention | % of users who keep hooks enabled after first week |

## Building

```bash
go build -o uncompact ./...
```

Cross-compile:

```bash
GOOS=linux GOARCH=amd64 go build -o uncompact-linux-amd64 .
GOOS=darwin GOARCH=arm64 go build -o uncompact-darwin-arm64 .
GOOS=windows GOARCH=amd64 go build -o uncompact-windows-amd64.exe .
```

## License

MIT

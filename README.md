# Uncompact

[![context bomb](https://raw.githubusercontent.com/supermodeltools/Uncompact/main/assets/badge.svg)](https://github.com/supermodeltools/Uncompact)

> Stop Claude Code compaction from making your AI stupid.

Uncompact hooks into Claude Code's lifecycle to reinject a high-density "context bomb" after compaction. It's powered by the [Supermodel Public API](https://supermodeltools.com) and stores versioned project graphs locally in SQLite.

## How It Works

```text
Claude Code compaction occurs
         ↓
Stop hook fires → uncompact run
         ↓
Check local SQLite cache (TTL: 15 min)
         ↓ (cache miss or stale)
Zip repo → POST /v1/graphs/supermodel
         ↓
Poll for result (async job)
         ↓
Render token-budgeted Markdown context bomb
         ↓
Claude Code receives context via stdout
```

## Claude Code Plugin

The fastest way to integrate Uncompact is via the Claude Code plugin system — no manual hook installation required.

### Install the plugin

```bash
/plugin marketplace add supermodeltools/Uncompact
/plugin install uncompact@supermodeltools-Uncompact
```

This installs both hooks automatically:

- **`SessionStart`** — runs `scripts/setup.sh` which auto-installs the `uncompact` binary via `go install` if not already present.
- **`Stop`** — runs `scripts/uncompact-hook.sh` which reinjects context after every compaction event.

> **Note:** After plugin installation, authenticate once with `uncompact auth login` to connect your Supermodel API key. That's it — no manual binary install or hook setup required.

### CI / GitHub Actions

In remote environments (`CLAUDE_CODE_REMOTE=true`), the hook automatically enables `--fallback` mode. Pass your API key via the `SUPERMODEL_API_KEY` environment variable:

```yaml
env:
  SUPERMODEL_API_KEY: ${{ secrets.SUPERMODEL_API_KEY }}
```

---

## Quick Start

### 1. Install

```bash
go install github.com/supermodeltools/uncompact@latest
```

Or download a binary from [Releases](https://github.com/supermodeltools/Uncompact/releases).

### 2. Authenticate

```bash
uncompact auth login
```

This opens [dashboard.supermodeltools.com](https://dashboard.supermodeltools.com) where you can subscribe and generate an API key.

### 3. Install Claude Code Hooks

```bash
uncompact install
```

This auto-detects your Claude Code `settings.json`, shows a diff, and merges the hooks non-destructively.

### 4. Verify

```bash
uncompact verify-install
uncompact run --debug
```

## CLI Reference

```text
uncompact auth login             # Authenticate via dashboard.supermodeltools.com
uncompact auth status            # Show auth status and API key validity
uncompact auth logout            # Remove stored API key

uncompact install                # Merge hooks into Claude Code settings.json (with diff preview)
uncompact install --dry-run      # Preview changes without writing
uncompact verify-install         # Check if hooks are correctly installed

uncompact run                    # Emit context bomb to stdout (used by the hook)
uncompact run --debug            # Show debug output on stderr
uncompact run --force-refresh    # Bypass cache and fetch fresh from API
uncompact run --max-tokens 1500  # Cap context bomb at 1500 tokens
uncompact run --fallback         # Emit minimal static context on failure

uncompact dry-run                # Preview context bomb without emitting it
uncompact status                 # Show last injection, cache state, auth status
uncompact logs                   # Show recent injection activity
uncompact logs --tail 50         # Show last 50 entries
uncompact stats                  # Token usage, cache hit rate, API call count

uncompact cache clear            # Clear all cached graph data
uncompact cache clear --project  # Clear only the current project's cache
```

## Configuration

### Environment Variables

| Variable | Description |
|----------|-------------|
| `SUPERMODEL_API_KEY` | Supermodel API key (overrides config file) |

### Config File

Located at `~/.config/uncompact/config.json` (Linux/macOS) or `%APPDATA%\uncompact\config.json` (Windows).

```json
{
  "api_key": "your-api-key-here",
  "base_url": "https://api.supermodeltools.com",
  "max_tokens": 2000
}
```

### CLI Flags (Global)

| Flag | Default | Description |
|------|---------|-------------|
| `--api-key` | | Override API key |
| `--max-tokens` | `2000` | Max tokens in context bomb |
| `--force-refresh` | `false` | Bypass cache |
| `--fallback` | `false` | Emit minimal static context on failure |
| `--debug` | `false` | Debug output to stderr |

## Manual Hook Installation

If `uncompact install` doesn't work for your setup, add this to your Claude Code `settings.json`:

```json
{
  "hooks": {
    "Stop": [
      {
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

See the [Claude Code hooks guide](https://code.claude.com/docs/en/hooks-guide#re-inject-context-after-compaction) for more details.

## Architecture

```text
uncompact/
├── main.go
├── cmd/
│   ├── root.go        # Root command, global flags
│   ├── run.go         # Core hook command
│   ├── auth.go        # auth login/status/logout
│   ├── install.go     # install, verify-install
│   └── status.go      # status, logs, stats, dry-run, cache
└── internal/
    ├── api/
    │   └── client.go  # Supermodel API client (async 200/202 polling)
    ├── cache/
    │   └── store.go   # SQLite cache (TTL, staleness, injection log)
    ├── config/
    │   └── config.go  # Config loading (flag > env > file)
    ├── hooks/
    │   └── hooks.go   # settings.json installer (non-destructive)
    ├── project/
    │   └── project.go # Git-aware project detection
    ├── template/
    │   └── render.go  # Token-budgeted Markdown renderer
    └── zip/
        └── zip.go     # Repo zipper (excludes .git, node_modules, etc.)
```

### Caching Strategy

| Concern | Policy |
|---------|--------|
| Default TTL | 15 minutes |
| Stale cache | Served with `⚠️ STALE` warning; fresh fetch attempted |
| API unavailable | Serve most recent cache entry silently |
| No cache + API down | Silent exit 0 (never blocks Claude Code) |
| Storage growth | Auto-prune entries older than 30 days |
| Force refresh | `--force-refresh` flag |

### Context Bomb Design

The context bomb is capped at `--max-tokens` (default: 2,000) to avoid triggering the very compaction it's trying to prevent. Sections are rendered in priority order:

1. **Required** (always included): Project overview, language, stats
2. **Optional** (filled to token budget): Domain map, key files, dependencies

### Fallback Chain

```text
1. Fresh cache hit → serve immediately
2. Cache miss / stale → fetch from API → cache result
3. API fails + stale cache exists → serve stale with warning
4. API fails + no cache → silent exit 0 (or minimal static if --fallback)
```

## KPIs

| Metric | Target |
|--------|--------|
| Clarifying questions per session (proxy for context loss) | ↓ after Uncompact |
| Post-compaction task completion rate | ↑ after Uncompact |
| Token efficiency (useful tokens / injected tokens) | > 80% |
| 7-day retention | % of users who keep hooks enabled |

## Building from Source

```bash
git clone https://github.com/supermodeltools/Uncompact
cd Uncompact
go mod tidy
go build -o uncompact .
```

Requires Go 1.22+.

## Add to your project

Show that your project uses Uncompact by adding this badge to your README:

```markdown
[![context bomb](https://raw.githubusercontent.com/supermodeltools/Uncompact/main/assets/badge.svg)](https://github.com/supermodeltools/Uncompact)
```

Or with Shields.io:

```markdown
[![context bomb](https://img.shields.io/badge/context--bomb-Uncompact-5b21b6)](https://github.com/supermodeltools/Uncompact)
```

## License

MIT

# Uncompact

> Stop Claude Code compaction from making Claude forget everything.

Uncompact hooks into [Claude Code](https://claude.ai/code) to automatically reinject a high-density **context bomb** after compaction — keeping Claude sharp and context-aware throughout long sessions.

## How It Works

1. Uncompact zips your current repository
2. Sends it to the [Supermodel API](https://supermodeltools.com) for code graph analysis
3. Renders a compressed Markdown "context bomb" from the Supermodel IR
4. The context bomb is injected back into Claude's context via a Claude Code hook

The context bomb includes your project's **domain map**, **key file structure**, **cross-domain relationships**, and **codebase stats** — everything Claude needs to stay oriented after compaction.

## Quick Start

### 1. Install

```bash
go install github.com/supermodeltools/uncompact@latest
```

Or download a binary from [Releases](https://github.com/supermodeltools/Uncompact/releases).

### 2. Authenticate

Get your API key from [dashboard.supermodeltools.com](https://dashboard.supermodeltools.com), then:

```bash
uncompact auth login
```

Or set the environment variable:

```bash
export SUPERMODEL_API_KEY=your-key-here
```

### 3. Install Claude Code Hooks

```bash
uncompact install
```

This merges the Uncompact hooks into your Claude Code `settings.json` — showing a diff before writing. To preview without writing:

```bash
uncompact install --dry-run
```

That's it. Uncompact will now run automatically whenever Claude Code compacts.

---

## CLI Reference

```
uncompact auth login             # Authenticate via dashboard.supermodeltools.com
uncompact auth status            # Show auth status

uncompact install                # Auto-detect settings.json and merge hooks
uncompact install --dry-run      # Preview changes without writing

uncompact run                    # Emit a context bomb to stdout (used by hooks)
uncompact run --max-tokens 3000  # Increase token budget
uncompact run --force-refresh    # Bypass cache and fetch fresh data

uncompact dry-run                # Preview what would be injected
uncompact status                 # Show last injection time and cache state

uncompact cache clear            # Clear local cache for current project
uncompact cache clear --all      # Clear cache for all projects
```

## Configuration

| Source | Key |
|--------|-----|
| Environment variable | `SUPERMODEL_API_KEY` |
| Config file | `~/.config/uncompact/config.json` |
| CLI flag | `--api-key` |

Config file location: `~/.config/uncompact/config.json`

## How the Hook Works

`uncompact install` adds the following to your Claude Code `settings.json`:

```json
{
  "hooks": {
    "PostCompact": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "uncompact run --max-tokens 2000"
          }
        ]
      }
    ]
  }
}
```

The `PostCompact` hook fires after Claude Code compacts the context. The output of `uncompact run` is injected back into the conversation as context.

See the [Claude Code Hooks Guide](https://code.claude.com/docs/en/hooks-guide#re-inject-context-after-compaction) for more on hook types.

## Supermodel API

Uncompact uses the [Supermodel Public API](https://supermodeltools.com) — a code graphing and analysis API. The `/v1/graphs/supermodel` endpoint generates a Supermodel Intermediate Representation (SIR) that includes:

- **Parse graph** — file and function structure
- **Call graph** — function-level call relationships
- **Domain classification** — LLM-powered domain/subdomain grouping
- **Dependency graph** — file import relationships

A subscription is required. Sign up at [dashboard.supermodeltools.com](https://dashboard.supermodeltools.com).

## Caching

Uncompact caches graph results locally in SQLite (`~/.cache/uncompact/uncompact.db`):

- **TTL**: 15 minutes by default
- **Stale fallback**: If the API is unavailable, the last cached result is served with a staleness warning
- **Force refresh**: `uncompact run --force-refresh` bypasses the cache
- **Storage**: Auto-pruned after 30 days

## Development

```bash
git clone https://github.com/supermodeltools/Uncompact
cd Uncompact
go mod tidy
go build ./...
go run . --help
```

## License

See [LICENSE](LICENSE).

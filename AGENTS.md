## Cursor Cloud specific instructions

This is a single-binary Go CLI project (`uncompact`). No Docker, databases, or external services are needed for development.

**Build / Lint / Test** -- see `CLAUDE.md` for canonical commands:
- Build: `go build ./...`
- Lint: `go vet ./...`
- Tests: `go test ./...`

**Running the CLI:** Build with `go build -o uncompact .` then invoke `./uncompact <subcommand>`. The `run` subcommand is the core hook command; use `--mode local` for local-only operation (no API key required). Use `--debug` to get stderr diagnostics.

**Pre-existing test failures:** As of this writing, tests in `cmd`, `internal/local`, and `internal/snapshot` have known failures that predate current development work. The build and lint are clean.

**SQLite is embedded:** The project uses `modernc.org/sqlite` (pure-Go, no CGO). The database is auto-created at `~/.local/share/uncompact/uncompact.db` on first run.

**go.mod declares Go 1.24.0** but CLAUDE.md says Go 1.22+. The VM has Go 1.24.0 available via the Go toolchain.

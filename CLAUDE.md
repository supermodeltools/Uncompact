# Uncompact — Claude Instructions

## Pull Requests

When asked to implement something and raise a PR, always create the PR using `gh pr create`. Never substitute a "Create PR →" compare link. The command to use:

```
gh pr create \
  --repo supermodeltools/Uncompact \
  --title "..." \
  --body "..." \
  --base main \
  --head <branch>
```

Always run this as the final step. The PR must exist before marking the task complete.

## Development

- Language: Go 1.22
- Build: `go build ./...`
- Lint: `go vet ./...`
- No test suite yet — at minimum verify `go build ./...` passes before committing

## Branch naming

`claude/issue-{number}-{YYYYMMDD}-{HHMM}`

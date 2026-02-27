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

## Issues

When creating a GitHub issue, always include `@claude` at the end of the body so the workflow auto-triggers. Example closing line:

```
@claude please implement this
```

## Development

- Language: Go 1.22
- Build: `go build ./...`
- Lint: `go vet ./...`
- No test suite yet — at minimum verify `go build ./...` passes before committing

## Commits

Always include both co-authors in every commit message:

```
Co-Authored-By: Grey Newell <greyshipscode@gmail.com>
Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

## Branch naming

`claude/issue-{number}-{YYYYMMDD}-{HHMM}`

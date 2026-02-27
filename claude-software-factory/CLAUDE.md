# Project Name — Claude Instructions

## Pull Requests

Always create PRs using `gh pr create`. Never substitute a compare link.

```
gh pr create \
  --repo OWNER/REPO \
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

<!-- CUSTOMIZE: Replace with your language, build, lint, and test commands -->

- Language: (your language)
- Build: `your-build-command`
- Lint: `your-lint-command`
- Test: `your-test-command`
- At minimum verify the build passes before committing

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — new feature (bumps MINOR version)
- `fix:` — bug fix (bumps PATCH version)
- `feat!:` or `BREAKING CHANGE` — breaking change (bumps MAJOR version)
- `chore:`, `docs:`, `test:`, `refactor:` — no version bump

<!-- CUSTOMIZE: Add co-authors or other commit message requirements -->

## Branch Naming

`claude/issue-{number}-{YYYYMMDD}-{HHMM}`

# Claude Software Factory

A repository template that turns GitHub into an autonomous software factory. No application code included — just the infrastructure that lets Claude Code build, review, fix, and ship software on its own.

Drop your code into this template and the factory runs itself.

## What This Is

This is the extracted automation backbone from [Uncompact](https://github.com/supermodeltools/Uncompact), generalized into a language-agnostic template. It implements a closed-loop system where:

- **Issues become pull requests** without human intervention
- **Pull requests get reviewed** by AI code review
- **Review comments get addressed** automatically
- **Passing PRs get merged** when all checks are green
- **The codebase gets scanned** for new issues on a schedule
- **Releases get tagged and built** from conventional commits

The result is a self-sustaining development loop. Humans steer by writing issues and setting priorities. Claude does the rest.

## The Lifecycle

```
 ┌─────────────────────────────────────────────────────────────┐
 │                                                             │
 │   ┌──────────┐    ┌──────────────┐    ┌──────────────┐     │
 │   │  ISSUE   │───▶│ AUTO-ASSIGN  │───▶│  CLAUDE CODE │     │
 │   │ created  │    │ @claude      │    │  implements   │     │
 │   └──────────┘    └──────────────┘    └──────┬───────┘     │
 │        ▲                                      │             │
 │        │                                      ▼             │
 │   ┌────┴─────┐                         ┌──────────────┐    │
 │   │ PROACTIVE│                         │  PULL REQUEST │    │
 │   │ SCANNER  │                         │  opened       │    │
 │   │ (hourly) │                         └──────┬───────┘    │
 │   └──────────┘                                │             │
 │        ▲                                      ▼             │
 │        │                              ┌───────────────┐    │
 │        │                              │  CODE REVIEW   │    │
 │        │                              │  (automated)   │    │
 │        │                              └───────┬───────┘    │
 │        │                                      │             │
 │        │                                      ▼             │
 │        │                              ┌───────────────┐    │
 │        │                              │  PR SHEPHERD   │    │
 │        │                              │  fix comments  │    │
 │        │                              │  check CI      │    │
 │        │                              │  merge when    │    │
 │        │                              │  ready         │    │
 │        │                              └───────┬───────┘    │
 │        │                                      │             │
 │        │         ┌──────────────┐             │             │
 │        │         │  AUTO-TAG    │◀────────────┘             │
 │        └─────────│  semantic    │     (merged to main)      │
 │                  │  versioning  │                            │
 │                  └──────────────┘                            │
 │                                                             │
 └─────────────────────────────────────────────────────────────┘
```

### Phase 1: Issue Creation

An issue is created — either by a human or by the proactive scanner. The issue body ends with `@claude` to signal that Claude should pick it up.

**Workflow:** `claude-auto-assign.yml`
**Trigger:** `issues.opened`
**Behavior:** Checks if the author is an org member (or Claude itself). If so, posts `@claude please implement this issue` as a comment, which triggers the next phase.

### Phase 2: Implementation

Claude Code receives the `@claude` mention and goes to work. It reads the issue, creates a branch, writes the code, verifies the build, and opens a pull request.

**Workflow:** `claude.yml`
**Trigger:** `@claude` mention in issue comment, PR comment, or review
**Behavior:** Full implementation cycle — branch, code, build, commit, PR. Claude has access to git, gh, and your project's build/lint tools.

### Phase 3: Code Review

The moment a PR is opened (or updated), automated code review kicks in. Claude Code reviews the diff and posts comments on potential issues.

**Workflow:** `claude-code-review.yml`
**Trigger:** `pull_request` opened, synchronized, ready_for_review, reopened
**Behavior:** Runs the `code-review` plugin from Claude Code Actions, posting review comments directly on the PR.

### Phase 4: PR Shepherd

Every 15 minutes, the shepherd checks all open PRs. It reads review comments, applies fixes, verifies CI, and merges when everything is green.

**Workflow:** `claude-pr-shepherd.yml`
**Trigger:** Cron (`*/15 * * * *`) + manual dispatch
**Behavior:**
1. Fetches unresolved review comments (including from CodeRabbit or other bots)
2. Applies fixes and commits them
3. Checks CI status
4. Merges via rebase when: CI green, no unresolved comments, not draft, no conflicts

### Phase 5: Semantic Versioning

When a PR merges to `main`, the auto-tagger examines the commit message and bumps the version accordingly.

**Workflow:** `auto-tag.yml`
**Trigger:** Push to `main`
**Behavior:**
| Commit pattern | Version bump |
|---|---|
| `BREAKING CHANGE` or `type!:` | MAJOR (resets minor + patch) |
| `feat:` or `feat(scope):` | MINOR (resets patch) |
| Everything else | PATCH |

### Phase 6: Proactive Scanning

Once per hour, Claude scans the codebase looking for problems and opportunities. It creates up to 3 issues per run, each ending with `@claude please implement this`, feeding the loop.

**Workflow:** `claude-proactive.yml`
**Trigger:** Cron (`0 * * * *`) + manual dispatch
**Detects:**
- Logic errors, unhandled errors, race conditions
- Missing tests
- Performance issues
- Security concerns
- TODO/FIXME comments
- Feature gaps

## Setup

### Prerequisites

- A GitHub repository (use this as a template)
- A `CLAUDE_CODE_OAUTH_TOKEN` secret ([get one from Anthropic](https://docs.anthropic.com/en/docs/claude-code))
- Org membership configured (or modify `claude-auto-assign.yml` to your needs)

### Steps

1. **Use this template** to create a new repository (or copy the `.github/workflows/` directory into an existing one)

2. **Add the secret:**
   ```
   Settings → Secrets and variables → Actions → New repository secret
   Name: CLAUDE_CODE_OAUTH_TOKEN
   Value: <your token>
   ```

3. **Customize `CLAUDE.md`** with your project's:
   - Language and build commands
   - Lint and test commands
   - Branch naming convention
   - Commit message requirements
   - PR creation instructions

4. **Customize the workflows:**
   - `claude.yml` → Update `allowed_tools` with your build/test commands
   - `claude-pr-shepherd.yml` → Update `allowed_tools` with your build/test/lint commands
   - `claude-proactive.yml` → Update `allowed_tools` with your build/lint commands
   - `auto-tag.yml` → Works as-is for any language using conventional commits

5. **Create your first issue** with `@claude please implement this` at the end

6. **Watch it go.**

## File Structure

```
.github/
└── workflows/
    ├── claude.yml               # Core: Claude responds to @claude mentions
    ├── claude-auto-assign.yml   # Auto-assigns Claude to new issues
    ├── claude-code-review.yml   # AI code review on every PR
    ├── claude-pr-shepherd.yml   # Fixes review comments, merges when ready
    ├── claude-proactive.yml     # Hourly codebase scan, creates issues
    └── auto-tag.yml             # Semantic versioning from commit messages
CLAUDE.md                        # Project instructions for Claude
```

## Customization

### Language Support

The template ships language-agnostic. Anywhere you see `# CUSTOMIZE:` in the workflow files, replace the placeholder commands with your own:

| Placeholder | Example (Go) | Example (Node) | Example (Python) |
|---|---|---|---|
| `your-build-command` | `go build ./...` | `npm run build` | `python -m py_compile *.py` |
| `your-lint-command` | `go vet ./...` | `npm run lint` | `ruff check .` |
| `your-test-command` | `go test ./...` | `npm test` | `pytest` |

### Org Membership Check

`claude-auto-assign.yml` verifies the issue author is an org member before triggering Claude. To change this:

- **Open to everyone:** Remove the org membership check entirely
- **Specific users:** Replace with a username allowlist
- **Label-based:** Trigger only on issues with a specific label

### PR Merge Strategy

The shepherd uses `--rebase` by default. Change to `--squash` or `--merge` in `claude-pr-shepherd.yml` to match your preference.

### Proactive Scanner Frequency

Default: hourly. Adjust the cron in `claude-proactive.yml`:
- `0 */4 * * *` — every 4 hours
- `0 9 * * 1-5` — weekdays at 9am
- Remove entirely if you only want human-created issues

## Secrets Reference

| Secret | Required | Used By |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | Yes | All Claude workflows |
| `GITHUB_TOKEN` | Auto-provided | All workflows (GitHub Actions default) |

## Design Principles

**Closed loop.** Every output feeds back into the system. Merged PRs trigger tags. Proactive scans create issues. Issues trigger implementations.

**Human steering.** Humans create issues and set priorities. They can review PRs before the shepherd merges, or let it run fully autonomous. The level of oversight is a dial, not a switch.

**Fail safe.** Every workflow is designed to do nothing rather than do harm. If CI is red, the shepherd waits. If the build breaks, Claude won't merge. If the proactive scanner finds nothing, it creates no issues.

**One PR at a time.** The shepherd processes PRs sequentially to avoid merge conflicts and maintain a clean history.

**Conventional commits.** The auto-tagger relies on [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `BREAKING CHANGE`) to determine version bumps. Claude is instructed to follow this convention in `CLAUDE.md`.

## Credits

Extracted from [Uncompact](https://github.com/supermodeltools/Uncompact) by [Supermodel](https://github.com/supermodeltools).

## License

MIT

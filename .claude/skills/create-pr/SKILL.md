---
name: create-pr
description: Create a GitHub pull request following lstk conventions with proper title, description, and ticket references.
argument-hint: [base-branch]
disable-model-invocation: true
allowed-tools: Bash(git log *), Bash(git diff *), Bash(git branch *), Bash(git push -u origin HEAD), Bash(gh pr create *), Bash(gh label list *), mcp__claude_ai_Linear__get_issue
---

# Create Pull Request

Create a PR for the current branch following lstk's conventions.

## Step 1: Gather context

Run these to understand what's being submitted:

```
git log main..HEAD --oneline
git diff main...HEAD --stat
git diff main...HEAD
git branch --show-current
```

If `$ARGUMENTS` is provided, use it as the base branch instead of `main`.

## Step 2: Determine the Linear ticket

Extract the ticket ID from the branch name. The ticket ID is the last path segment, uppercased (e.g., branch `user/abc-123` → ticket `ABC-123`).

Use the Linear MCP tool (`mcp__claude_ai_Linear__get_issue`) to fetch the ticket details — title and description. Use this context to inform the PR motivation.

If the branch name carries no ticket ID, ask whether a Linear issue exists (issues live in Linear, not GitHub Issues). When one exists, prefer its Linear-generated branch name for future work; when none exists and the change warrants tracking, offer to create the issue first and reference it.

## Step 3: Write the PR title

- Start with an action verb: Add, Fix, Improve, Remove, Migrate, Update, Refactor
- Keep under 70 characters
- Be specific about what changed, not how
- Match the style of recent commits: `Telemetry client`, `Improve container engine error`, `Migrate stop command to output event system`

## Step 4: Write the PR body

Use this structure:

```markdown
## Motivation

<Why this change is needed. 1-2 sentences.>

## Changes

- Bullet point per meaningful change
- Group related changes together

## Tests

<How this was tested — new tests added, manual verification, etc.>

## Todo

- [ ] Any remaining follow-up items (omit section if none)

Closes TICKET-ID
```

Rules:
- Always end the body with a ticket reference — do not add a Related section or link to Linear
  - Use `Closes TICKET-ID` if the PR fully resolves the issue
  - Use `Towards TICKET-ID` if it's a partial contribution
- Keep bullet points concise — what changed, not how every line was modified
- Omit Todo section if there are no follow-up items
- Don't over-explain; the diff speaks for itself

## Step 5: Choose labels

Always apply two labels, based on the size and nature of the change:

1. A semver label reflecting the impact of the change:
   - `semver: patch` — bug fixes, dependency/toolchain bumps, internal refactors, and other backward-compatible changes with no new user-facing behavior.
   - `semver: minor` — new backward-compatible functionality (e.g. a new command or flag).
   - `semver: major` — breaking changes to existing behavior or interfaces.
2. A docs label reflecting whether documentation needs updating:
   - `docs: skip` — no user-facing documentation change is required (most dependency/toolchain upgrades, internal refactors).
   - `docs: needed` — the change adds or alters user-facing behavior that requires documentation updates.
3. A review label surfacing whether this PR needs a human approval or is a self-merge candidate under the review pilot — run `/review-pr`'s "Review scope" checklist (or apply it inline if that skill wasn't run) to decide:
   - `review: self-merged` — straightforward bug fix or internal refactor, no new user-facing behavior, small self-contained diff.
   - `review: needs-approval` — new/changed user-facing behavior, undiscussed or speculative work, or any doubt.

This applies to automated PRs too (dependency and toolchain upgrades), which are almost always `semver: patch` + `docs: skip` + `review: self-merged`.

## Step 6: Commit and push

If there are uncommitted changes, commit them with a concise message and a `Co-Authored-By: Claude <noreply@anthropic.com>` trailer.

Then push and create the PR with the chosen labels:

```
git push -u origin HEAD
gh pr create --draft --title "<title>" --body "<body>" --base <base-branch> --label "<semver-label>" --label "<docs-label>" --label "<review-label>"
```

Return the PR URL when done.

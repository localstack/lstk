---
name: create-pr
description: Create a GitHub pull request following lstk conventions with proper title, description, and ticket references.
argument-hint: [base-branch]
disable-model-invocation: true
allowed-tools: Bash(git log *), Bash(git diff *), Bash(git branch *), Bash(git push -u origin HEAD), Bash(gh pr create *), mcp__plugin_linear_linear__get_issue
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

Use the Linear MCP tool (`mcp__plugin_linear_linear__get_issue`) to fetch the ticket details — title, description, and URL. Use this context to inform the PR motivation and to link in the Related section.

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

## Related

- [TICKET-ID](https://linear.app/localstack/issue/TICKET-ID)
- Links to related PRs, issues, or design docs
```

Rules:
- The Related section always includes the Linear ticket link (use the URL from the Linear API response)
- Keep bullet points concise — what changed, not how every line was modified
- Omit Todo section if there are no follow-up items
- Don't over-explain; the diff speaks for itself

## Step 5: Commit and push

If there are uncommitted changes, commit them with a concise message. Do NOT add `Co-Authored-By: Claude` unless the user explicitly asks for it.

Then push and create the PR:

```
git push -u origin HEAD
gh pr create --title "<title>" --body "<body>" --base <base-branch>
```

Return the PR URL when done.

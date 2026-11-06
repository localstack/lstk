# Implementing in lstk

Spiral's persona above covers the driver contract and the generic craft. This file adds what is specific to lstk, LocalStack's Go CLI, where `CLAUDE.md` is the contract and integration tests that drive the real binary are the source of truth.

## What you read

- `CLAUDE.md`, all of it: architecture, code style, output routing, user input, testing. It is the rulebook the reviewer holds the diff to.
- The docs it points at for the area you change: `docs/structured-output.md` for `--json`, `docs/extensions-authoring.md` for extensions, `docs/container-runtimes.md` for runtime discovery.
- `openspec/specs/<capability>/spec.md` for the behaviour you change, and any in-flight `openspec/changes/<id>/` the issue names: read them, never edit them.
- The skill for what the spec adds, if any: `.claude/skills/add-command/`, `add-event/` or `add-component/`. Read its `SKILL.md` and follow it.
- The neighbouring code and tests. lstk has a strong house style; match it.

## Where Spiral overrides CLAUDE.md

`CLAUDE.md` is written for a person driving an interactive session. Under Spiral these rules replace the matching ones there:

- **Commits.** A delegated Spiral task is the explicit request to commit. Commit as the persona above says: subject `<ISSUE-ID>: <what changed>` and `## Deviations` in the body, not `CLAUDE.md`'s single-line message.
- **No push, PR or Linear.** The driver pushes and opens the PR. Don't run `/create-pr` or `/review-pr`, and don't write a PR description or a "Review" line.
- **Never edit `.spiral/`.** It runs as root at the image bake; changes to it are human PRs. If the gate itself is wrong, stop the round and say so.

## The worker

- **No Docker emulator.** dockerd cannot pull images and there is no `LOCALSTACK_AUTH_TOKEN`. Tests that call `requireDocker(t)` skip in the gate, which runs with `DOCKER_HOST=tcp://localhost:1`.
- **Network** goes through Spiral's proxy: `proxy.golang.org` and `github.com` work; `go.dev`, `api.github.com`, `vuln.go.dev` and the LocalStack platform API do not. Tests must mock every external system, as `CLAUDE.md`'s Testing section already requires.
- **No `terraform`, `tofu`, `cdk`, `sam` or `aws`.** Their e2e tests skip; use `writeFakeTool`/`installFakeTool` for anything new.
- **You run as root**, so tests that rely on directory permissions skip themselves.

## The openspec change

Every task that changes behaviour adds a new openspec change in the same PR as its code. That is stricter than lstk's own history, where contract-level features carry one and many small fixes don't; for Spiral it is the rule. You write it; the spec in the Linear issue is its source. Copy the shape of the changes under `openspec/changes/` (`fix-extension-emulator-endpoint-prefix` is a small, complete one, though most files open straight with their first `##` section, with no `#` title):

- **`openspec/changes/<id>/`**, where `<id>` is the name the spec gives, or a short verb-led kebab-case name (`add-…`, `fix-…`) that no change or archive entry already uses. Create it with `openspec new change <id>`, or by hand with an `.openspec.yaml` of `schema: spec-driven` and `created: <today>`.
- **`proposal.md`**: `## Why` (cite the issue identifier), `## What Changes`, `## Capabilities` (`### New Capabilities` and `### Modified Capabilities`, by their `openspec/specs/<capability>` names), `## Impact`. An empty capability list says `None.` with a reason.
- **`design.md`**: `## Context`, `## Goals / Non-Goals`, `## Decisions` (each with the alternatives rejected), `## Risks / Trade-offs`, and `## Migration Plan` or `## Open Questions` when they apply. Record the decisions the spec settled and the ones you had to take. An open question may only name follow-up work: one this change depends on means the round stops.
- **`specs/<capability>/spec.md`**: the delta. `## ADDED Requirements`, `## MODIFIED Requirements` or `## REMOVED Requirements`; each `### Requirement:` uses SHALL and has at least one `#### Scenario:` with **WHEN** / **THEN** (and **AND**) lines. A MODIFIED requirement repeats the whole requirement from `openspec/specs/<capability>/spec.md`, edited, not just the changed lines. A new capability's delta is all `## ADDED`.
- **`tasks.md`**: numbered groups (`## 1. …`) in the order you work, named for the work, failing tests first (`## 1. Reproduce (red)`, `## 2. Implement (green)`, then docs and a final check, as the example does). Tasks are numbered checkboxes (`- [ ] 1.1 …`), each saying how it was verified. Work you find mid-round goes in a `## N. Discovered during implementation` group. Tick each task (`- [x]`) when done; the gate fails an unticked one.

Write the change before the code, and keep it true as the code moves: if a decision changes mid-round, change `design.md` and the delta too. Run `openspec validate <id> --strict` before you commit.

**Never touch an existing openspec file**: not another change (in flight or not), not `openspec/changes/archive/`, not `openspec/specs/`, not `openspec/config.yaml`, and never run `openspec archive`. Syncing and archiving are done by people. When the spec relates to an in-flight change, your new change says so in its proposal and leaves the old one alone. The gate fails any edit outside your new change's directory.

A task with no behaviour change (a refactor, a test-only or docs-only change, or a fix that restores what an existing requirement already says) needs no change. Put a line `openspec: none — <reason>` in the commit body instead; the gate looks for it when the branch touches Go code under `cmd/` or `internal/`.

## Workflow in this repo

1. **The openspec change first**, as above. Commit it before the code, so the reviewer can read the plan against the result.
2. **Test first**, as `CLAUDE.md` mandates: write the integration test in `test/integration/`, run it red, implement, run it green, ticking the matching tasks. Run one test the way the gate does:
   `DOCKER_HOST=tcp://localhost:1 make test-integration RUN=TestName`.
3. **Behaviour only a running emulator shows** (start, stop, logs, snapshots against a container): write that test like its neighbours (`requireDocker(t)` first, then `env.Require(t, env.AuthToken)`), and cover everything else you can without Docker (a mock platform API, a fake tool, a unit test). Say in `## Deviations` which tests were not run here; CI runs them.
4. **The gate.** After committing, run `sh .spiral/check.sh`: the openspec stage, gitleaks over your commits, `make lint`, `make test` plus the npm launcher tests, then the integration suite with Docker unreachable. Every stage runs on its own too (`sh .spiral/checks/openspec.sh`).
5. **No secrets in commits.** gitleaks scans every commit on the branch and a later fix doesn't clear a finding, so test fixtures use obviously fake values (`test-token`, never anything shaped like a real key).
6. **Snapshots** (`__snapshots__/*.snap`) change only with an intended output change: regenerate with `UPDATE_SNAPS=true go test ./<pkg>/` and say why in the commit body.
7. **Mocks** come from `//go:generate mockgen …` directives; after changing a mocked interface or adding a directive, run `make mock-generate` (mockgen is installed) and commit the regenerated `mock_*_test.go` files.
8. **Leave release machinery alone** unless the spec names it: `.github/workflows/`, `.goreleaser.yaml`, `scripts/bundled-extensions/`, `scripts/third-party-notices/`. A new dependency whose licence is outside the notices allowlist is a legal question: stop the round instead of editing the allowlist.

## Deviations

Typical ones here: a Docker- or token-backed test written but not run on the worker (its task says so too); a design decision the spec left open, recorded in `design.md`; a `CLAUDE.md` update left out because the change stays inside one package; a spec's flag or output wording adjusted to match an existing convention.

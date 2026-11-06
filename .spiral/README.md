# Spiral in lstk

[Spiral](https://github.com/localstack/spiral) takes a Linear issue, fixes it on a disposable worker and opens a PR. This directory is what the worker runs in this repo: `setup.sh` at boot (and at the image bake), `check.sh` after every implementer turn. CI and developers' tooling don't read it.

## Filing an issue Spiral can work

In Linear, team DEVX:

| What | Why |
|---|---|
| Delegate the issue to `spiral` | That is what makes Spiral pick it up. Only delegate lstk issues: lstk is DEVX's only Spiral repo, so every delegated DEVX issue lands here. |
| A `spec` label if the issue is already specified | Without it Spiral plans first, and may split the issue or send it back as `needs design`. |

Spiral works best on bug fixes and small, already-decided features whose behaviour shows through the CLI. A new command, flag or output shape nobody has decided on comes back `needs design` (`agents/planner.md`).

## OpenSpec changes

Spiral works end to end, so every PR that changes behaviour adds a new openspec change next to the code. That is stricter than the repo's history, where contract-level features carry one and many small fixes don't. The planner's spec names the change id, the capability and the requirements. The implementer writes `openspec/changes/<id>/` (proposal, design, spec delta, tasks) in the repo's existing style before the code, then ticks each task as it lands. A task that changes no behaviour gets an `openspec: none — <reason>` line in a commit body instead.

Spiral only adds new changes. It never edits an existing change, `openspec/specs/`, the archive or `openspec/config.yaml`, and never archives; syncing specs and archiving stay with people.

## What the gate runs (`check.sh`)

1. `checks/openspec.sh`: the branch only adds new changes; each passes `openspec validate --strict` and has every task ticked. A branch that changes non-test Go code under `cmd/` or `internal/` must add a change or carry an `openspec: none` line.
2. `checks/gitleaks.sh`: gitleaks over the branch's commits, at the rev `.pre-commit-config.yaml` pins. Developers get this from the pre-commit hook; CI doesn't run it.
3. `checks/lint.sh`: `make lint`, golangci-lint on both modules at the `.tool-versions` pin (CI: Lint).
4. `checks/unit.sh`: `make test` and `node --test npm/*.test.js` (CI: Unit Tests, Launcher Tests).
5. `checks/integration.sh`: the integration suite with `DOCKER_HOST` pointed at a closed port, so every `requireDocker` test skips, as on Windows CI (CI: Integration Tests). Workers can't pull images and hold no `LOCALSTACK_AUTH_TOKEN`. Tests that reach the internet unmocked are skipped by name, with the reason, in the script.

Left to CI: the Docker-backed tests, the macOS and Windows runs, govulncheck, and the release checks (GoReleaser, release scripts, licences, the Homebrew cask, zizmor on workflows).

## Reviewing and merging Spiral's PRs

Spiral opens every PR with `semver: patch` and `docs: skip`, because the release-label checks need one of each. Correct them before merging: the planner's spec says when a change is `minor` or needs docs. A person approves and merges every PR; `main`'s review rules apply as to any other PR.

## Agent personas (`agents/`)

Spiral ships a base persona per role (planner, implementer, reviewer, investigator) and appends `agents/<role>.md` under a "Repository instructions" heading. These files say what is specific to lstk: where Spiral overrides `CLAUDE.md` (the agent commits; the driver pushes and opens the PR), how the openspec change is written and reviewed, what a worker can and cannot run, and what to review against. Beyond that they point at `CLAUDE.md` and `.claude/skills/` rather than repeat them.

## Changing this directory

Everything here runs as root at the image bake and ends up in every worker's image, so changes are human PRs; the implementer persona forbids agents from touching it. A change to any file here re-bakes the image within the hour. So do `go.sum` changes, and changes to `go.mod`, `.tool-versions` and `.pre-commit-config.yaml` through `dependency_files` in Spiral's config: the tool pins `setup.sh` reads live there.

## Running it by hand

From the repo root:

```sh
sh .spiral/check.sh                    # the whole gate
sh .spiral/checks/integration.sh      # one stage on its own
```

The stages need the tools `setup/toolchain.sh` pins on `PATH`: golangci-lint, gitleaks (which `pre-commit install` users already have through the hook, but not as a command) and the OpenSpec CLI (`npm i -g @fission-ai/openspec@1.15.0`). `setup.sh` itself is for the worker image: it installs Go and those tools into `/usr/local` as root. A developer's machine doesn't need it.

# Investigating in lstk

Spiral's persona above says how to investigate and which verdict lines the driver parses. This file lists the lstk patterns to check first. The repo's contracts are `CLAUDE.md` and `.spiral/agents/implementer.md`.

## Where the evidence is

- The `checks-failed` feedback carries the **tail** of the gate's output. The full output of `.spiral/check.sh` and `.spiral/setup.sh` is in the per-script `.log` files in the transcript directory your prompt names.
- The gate's stages run on their own from the repo root: `sh .spiral/checks/openspec.sh`, `gitleaks.sh`, `lint.sh`, `unit.sh`, `integration.sh`. One integration test: `DOCKER_HOST=tcp://localhost:1 make test-integration RUN=TestName`.
- The setup log names the Go, mockgen, golangci-lint, gitleaks and OpenSpec versions the worker ended up with.

## Patterns

- **The openspec stage failed.** `openspec validate --strict` names the file and the rule (a requirement without SHALL, a requirement without a scenario, a MODIFIED requirement that doesn't match one in `openspec/specs/`): clear it with that correction. Unticked tasks mean the round stopped early or the task list drifted from the work; tick what is done and finish or drop the rest, saying why in `design.md`. "Changes Go code but adds no openspec change": write the change, or the `openspec: none — <reason>` line if the branch really changes no behaviour. "Changes existing openspec files": revert those paths to `origin/main` (`git checkout origin/main -- <path>`, or delete a file the branch added to an existing change) and carry the content in the new change instead.
- **gitleaks found a leak.** It scans every commit on the branch, so a later commit that removes the value does not clear it. `stuck`, quoting the finding's file and rule: a person decides whether the value is real (and rotates it), rewrites the branch, or allowlists a fixture. Never add an allowlist or `.gitleaksignore` entry yourself.
- **`openspec`, `mockgen` or `gitleaks`: command not found**: a boot install failed (npm registry, proxy.golang.org, GitHub releases). That is the environment: `stuck`.
- **`golangci-lint X required (found: Y)`**, or a Go toolchain download failure. `.tool-versions` or `go.mod` moved past the image and the boot install failed. That is the environment, not the code: `stuck`, naming the version.
- **`LOCALSTACK_AUTH_TOKEN must be set to run this test`.** A test reaches `env.Require` with Docker unreachable. If it needs a running emulator, `requireDocker(t)` must come first so it skips here; if it does not, it should run against a mock platform API with a fake token (`With(env.AuthToken, "test-token")`). Clear it with that correction.
- **A test that reaches the internet** (`api.github.com`, the LocalStack API, Docker Hub). A new test must mock it; clear with the mock to use. An existing test that newly fails this way needs the skip list in `.spiral/checks/integration.sh`, which only a person may edit: `stuck`, naming the test.
- **Snapshot mismatch** (`__snapshots__/*.snap`). An intended output change is regenerated with `UPDATE_SNAPS=true`; an unintended one is a code bug.
- **PTY test timeouts** (`Condition never satisfied` from `pty_helpers_test.go`). Re-run the single test before believing it: under parallel load these can be flaky. A failure that reproduces alone is real.
- **`root bypasses directory permissions`** skips are expected on a worker, not a failure.

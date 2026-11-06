# Planning in lstk

Spiral's persona above covers the driver contract and how a spec is shaped. This file adds what is specific to lstk, LocalStack's Go CLI.

## What you read

- `CLAUDE.md`: architecture, conventions, and the design decisions already taken (each one names its rationale or its ticket).
- `openspec/specs/`: the current requirements per capability. `openspec/changes/`: in-flight designs; an issue that implements part of one cites it, and the spec follows its `design.md`. Spiral never edits an existing change, so the work gets a new change of its own that names the in-flight one in its proposal.
- The command's own help (`Short`/`Long` in `cmd/`) and `docs/`: the user-facing contract as shipped.
- `.spiral/agents/implementer.md`: what a worker can and cannot run.

## The class of issue Spiral works here

**Bug fixes and small, already-decided features in the CLI** whose behaviour shows through the binary: output, exit codes, files written, requests a mock platform API or a fake tool receives.

- It is a **leaf** when one change fixes it and one integration test pins it. A behaviour that only a running emulator shows is still a leaf; the spec says its test is Docker-backed and verified in CI only.
- **Split** only into changes that are tested and reviewed separately, each with its own acceptance criteria.
- `needs_design` when the issue needs a user-facing decision nobody has taken (a new command, flag, output shape or default that `CLAUDE.md`, an openspec change or the issue does not settle), touches the release, packaging or licensing pipeline (`.goreleaser.yaml`, `scripts/`, the npm wrapper, notices), or the defect is in `.spiral/` or in Spiral itself. Name the decision or the file in `comment`.
- `not_a_bug` only on cited evidence: the behaviour is the documented one (quote `CLAUDE.md`, the help text or `docs/`), or the report ran a release older than the fix (name the commit). Anything else is `needs_design`.

## What the spec must carry

The implementer turns your spec into an openspec change under `openspec/changes/<id>/` (`.spiral/agents/implementer.md`, "The openspec change"), so give it what that change needs:

- **The change id**: a short verb-led kebab-case name (`add-…`, `fix-…`) that no directory under `openspec/changes/` or its `archive/` already uses.
- **The capability**: which `openspec/specs/<capability>` the behaviour belongs to, and whether a requirement there is added, modified or removed. Read that spec first: a "bug" that contradicts an existing requirement is a fix to that requirement's implementation, while a behaviour no requirement covers is a new requirement. Name a new capability when none fits.
- **Requirements and scenarios**: each behaviour as a SHALL statement with WHEN/THEN scenarios. These become the delta, and the integration tests pin them.
- **Decisions already taken**, and the alternatives rejected, for `design.md`. A decision nobody has taken is `needs_design`, not something to leave to the implementer.
- A behaviour specified only by an in-flight change, not yet in `openspec/specs/`, cannot be modified by a new change (the delta validates against `openspec/specs/`) and the in-flight change may not be edited: that is `needs_design`, naming the change.
- **`openspec: none`** when the task changes no behaviour (a refactor, or a fix that restores what a requirement already says), so the implementer writes that line instead of a change.

And for the code:

- The observable behaviour: the exact command, flags, environment, expected output or exit code, and files written.
- The integration test that pins it, and whether it needs Docker or `LOCALSTACK_AUTH_TOKEN` (not run on a worker).
- Release labels for the person merging: Spiral opens every lstk PR with `semver: patch` and `docs: skip`. Say when the change is `semver: minor` (new user-facing behaviour) or needs docs, so they relabel before merging.

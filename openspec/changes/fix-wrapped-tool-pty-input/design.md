# PTY input for wrapped tools: bridge vs. full virtualization

## Context

`proc.RunInPTY` (DEVX-1026) gives a wrapped tool a pseudo-terminal on stdout/stderr so lstk can observe output (spinner stop-on-write) without making the tool see a pipe. `lstk aws` and `lstk az` use it today; `terraform`, `cdk`, `sam`, and extensions are potential future adopters, so this design covers wrapped-tool input generally, not just pagers.

The PTY is currently output-only, which is the bug (DEVX-1049):

```
lstk aws dynamodb create-table ...        # output taller than the window
→ pager draws page 1, shows ":"           # SPACE, ENTER, q: nothing. Ctrl-C to escape.
```

`less` 577 and newer (2021; macOS ships 668) opens the terminal named by `ttyname(2)` — the terminal attached to its **stderr** — for keyboard input, falling back to `/dev/tty` and finally to fd 2 itself. Under lstk, stderr is the inner PTY, which nothing feeds. `less` older than 577 (e.g. 551 on Ubuntu 20.04) opens `/dev/tty` — the process's *controlling terminal*, i.e. the real one — which is why the bug does not reproduce there.

```
keys → real TTY → ???                                  nobody read these
       tool/pager → inner PTY → lstk → real TTY        output worked fine
```

Any fix pumps keystrokes `real TTY → inner PTY` with the real terminal in raw mode (per-key, no echo). The fork is **authority**: which of the two terminals owns signal keys (^C/^Z), echo, and `/dev/tty`. These can't be split freely — `/dev/tty` follows the *session's* controlling terminal, so pointing it at the inner PTY requires the child to leave lstk's session, which is precisely what severs native job control. No hybrid exists.

Shared by both options: input activates only when lstk's stdin is a real terminal; redirected stdin (`yes | lstk aws ...`) keeps today's byte-exact passthrough (piped data must never meet a line discipline: echo, `0x03`→SIGINT); termios restored on exit; Windows unchanged (no PTY there, callers fall back to `proc.Run`).

The intended future adopters constrain the contract beyond pagers — whatever is chosen must be able to preserve:

| Tool | Behavior the PTY path must preserve |
|---|---|
| `terraform` | Approval-prompt and `console` REPL input; distinct repeated interrupts for cleanup (state-lock release) |
| `cdk` | Approval prompts, long-running `watch`/progress output, child app processes |
| `sam` | Guided-init input, long-running local servers, Docker/debugger children |

Prototypes: **A** = branch `devx-1049-lstk-aws-paged-output-ignores-all-keyboard-input`, **B** = branch `worktree-devx-1049-pager-input`.

## Options at a glance

| | Option A: input bridge | Option B: full virtualization |
|---|---|---|
| Session | Child stays in lstk's session and foreground process group | Child starts a new session (`Setsid`+`Setctty`) |
| Child's terminal | stdout/stderr on the inner PTY; `/dev/tty` still resolves to the real terminal | Standard fds and `/dev/tty` all agree on the inner PTY |
| Control keys (^C/^Z) | Real terminal keeps `ISIG`; keys signal the shared foreground process group | Inner PTY's line discipline, under the child's own termios control |
| Existing `proc.Run` signal model | Preserved | Changed: signal keys arrive via the PTY; a PID-targeted `kill -INT <lstk-pid>` is not relayed to the child's separate session (matching `proc.Run`'s existing on-a-terminal behavior) |
| Shell ^Z / `fg` | Native: the whole job suspends and resumes | Discarded: SIGTSTP to the child's orphaned process group is a no-op unless lstk proxies suspension |

## Option A: input bridge — keystrokes move, authority stays

Real terminal raw **with ISIG kept on**; no new session; a cancelable reader pumps non-signal keys.

```
        one session (ctty = real TTY): fg pgrp = { lstk, tool, pager }
keys → real TTY [raw+ISIG] → pump → inner PTY [no session] → tool/pager
          │
  ^C/^Z ──┴→ signals to the whole fg pgrp (native, unchanged)
  /dev/tty (child) ──→ real TTY  ← old less reads here, racing the pump
```

(The race: `/dev/tty` and lstk's stdin are the same device; each typed byte goes to whichever of the two blocked readers the kernel wakes first.)

A must also handle signal keys it does not bridge deliberately: with the real terminal raw but `ISIG` on, an unhandled Ctrl-\ delivers SIGQUIT to lstk itself, which can terminate it before the raw-mode restore runs and strand the shell's terminal.

## Option B: full virtualization — the inner PTY becomes the child's terminal

Real terminal fully raw (dumb byte pipe); child gets `Setsid`+`Setctty`, owning the inner PTY as its controlling terminal. The `ssh -t` / `script(1)` / `docker run -it` architecture.

```
   session A (ctty=real TTY)      session B (ctty=inner PTY)   ← Setsid+Setctty
          lstk                          { tool, pager }
keys → real TTY [raw] → pump → inner PTY [child-controlled, ISIG] → tool/pager
                                    │
  ^C: 0x03 passes as a byte ────────┴→ inner ldisc → SIGINT → tool
  /dev/tty (child) ────────────────────→ inner PTY  (all pager generations work)
```

## Evidence

Measured with a throwaway harness (an outer PTY driving each wrapper architecture with pager stand-ins; real `aws` CLI + real `less` 668 for the first row):

| Experiment | A: bridge | B: virtualization |
|---|---|---|
| DEVX-1049 repro (real aws + less 668) | fixed | fixed |
| Old-less stand-in (`/dev/tty` reader), 3 trials of SPACE+q | **keystrokes lost nondeterministically** — trial 1 lost SPACE, trial 2 lost q (hang), trial 3 lost SPACE | 3/3 keys delivered |
| ISIG-off child (holds its tty raw, wants ^C as a key), send ^C | **killed by SIGINT**, byte never delivered — reproduced on real tools, see survey | receives `0x03` as data, native cancel behavior |
| ^Z during a run (measured on `terraform apply`, `sam local start-lambda`) | native: whole job suspends, `fg` resumes | **silent no-op**: SIGTSTP to the child's orphaned process group is discarded — the run continues, it just cannot be suspended |
| `kill -9` lstk mid-run | child exits (master close) | child exits (master close) — no orphaning difference |

Remaining inherent differences not in the table: on `MakeRaw` failure, A falls back to pipes (loses DEVX-1026 streaming), B degrades to today's output-only PTY (keeps it). ^C on long runs (`terraform apply` lock release) is delivered exactly once under both — different path, same outcome.

Portable hygiene, adopted regardless of the decision: A's cancelable input pump (join + surfaced restore errors) and faketool pager-mode tests with SPACE/ENTER/q coverage; B's unit guards (termios restore, piped-stdin passthrough) and unix/windows file split.

## Adopter survey (measured)

Each intended adopter was probed under both options with the same harness — REPLs, approval prompts, ^C/^Z, piped stdin/stdout:

| Tool | Needs the PTY at all? | Under A | Under B |
|---|---|---|---|
| `aws` (frozen Python) | yes: pipe block-buffering (DEVX-1026) + pager input (DEVX-1049) | pager keystroke race (old less) | fixed |
| `az` (Python) | yes: terminal-gated output (DEVX-1028) | same as aws | fixed |
| `cdk` (Node) | partly: no buffering, but every confirmation prompt hard-fails without a TTY (`TtyNotAttached`), and progress/colors are TTY-gated | **^C during any prompt kills CDK without its cancel/cleanup path** (prompts hold stdin raw; enquirer menus left the cursor hidden) | byte-identical to native in all 8 scenarios |
| `terraform` (Go) | no: no buffering, colors unconditional, prompts not TTY-gated | **`terraform console` wedged by ^C** (readline holds the tty raw; even Ctrl-D dead afterwards); apply/plan fine | byte-identical to native, incl. console ^C |
| `sam` (frozen Python) | no: click flushes explicitly; streaming goes to stderr | no hazard found | no hazard found; click semantics byte-identical |

Two cross-tool constants: real workflows depend on the non-TTY-stdin passthrough rule (`terraform console < exprs.txt`, `sam local invoke -e -`, CI piping); and every A divergence shares one mechanism — the outer terminal's ISIG reinstates signal semantics that a child holding its terminal raw deliberately disabled.

## Considered and dismissed: disable the pager instead

Setting `AWS_PAGER=`/`PAGER=cat` in the child env (or documenting it as a workaround) fixes today's two symptomatic commands in a few lines. Dismissed: it silently overrides user pager configuration, and it fixes only pagers — the proposal's scope is wrapped-tool *input* (terraform prompts, REPLs, extensions), which the env variable does nothing for. It remains the user-side workaround for unfixed versions.

## Decision (proposed): Option B, with A's hygiene

**Full virtualization.** B reproduced native behavior byte-for-byte across every tool and scenario measured; A's divergences are silent, intermittent, or configuration-dependent — the keystroke race, the wedged `terraform console`, CDK prompts dying without their cleanup path — exactly the shape of bug DEVX-1049 was. B's one cost measured milder than expected: ^Z becomes a silent no-op (stop signal discarded), so suspension is unavailable but nothing hangs; `ssh`/`docker run -it` precedent applies. We have no usage data on suspension; if suspending wrapped runs turns out to matter to users, that flips the decision (Open Questions). Either choice is a contained revert inside `internal/proc` until more proxies adopt PTY semantics — and per the survey, only `cdk` is a motivated next adopter; `terraform` and `sam` gain nothing from a PTY, so adoption should stay per-tool rather than assumed.

## Open Questions

- **Flip condition:** does anyone rely on suspending a wrapped run (^Z/`fg`)? Under B it becomes a silent no-op; keeping it native means Option A, accepting the measured race and prompt/REPL breakage.
- Under B, a later ^Z path is plausible (the pump spotting the VSUSP byte and stopping child and self so the shell regains control — the child never stops on its own, its orphaned group discards SIGTSTP) — unprototyped; shipping B first tells us whether anyone actually hits the gap before we pay for it.
- **Mixed stdio:** input activates only when lstk's stdin is a terminal, so a run with piped stdin whose output still pages (`producer | lstk aws ...` on a terminal) leaves the pager unresponsive under both options — feeding keys there would need a second input source (`/dev/tty`) alongside the byte-exact stdin pipe. Deliberately out of scope until someone hits it; the passthrough rule wins.
- PTY adoption stays per-tool: the survey says `cdk` is the one motivated next adopter (prompts + progress), `terraform`/`sam` gain nothing. Does anyone see a reason to wrap them anyway (uniformity)?
- Neither option forwards SIGWINCH (size snapshotted at start) — same follow-up either way, not a differentiator.

## References

- [`internal/proc/run.go`](../../../internal/proc/run.go) and [`internal/proc/pty_unix.go`](../../../internal/proc/pty_unix.go)
- [`less` terminal input selection](https://github.com/gwsw/less/blob/master/ttyin.c) (`ttyname(2)` first since v577, 2021; `/dev/tty`, then fd 2 as fallbacks)
- [POSIX orphaned process-group behavior](https://pubs.opengroup.org/onlinepubs/9799919799/functions/V2_chap02.html)
- [Terraform interrupt handling](https://github.com/hashicorp/terraform/blob/main/internal/command/meta.go)
- [CDK `watch`](https://docs.aws.amazon.com/cdk/v2/guide/ref-cli-cmd-watch.html)
- [SAM `local start-api`](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/using-sam-cli-local-start-api.html)

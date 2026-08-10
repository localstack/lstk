## Why

LocalStack derives the AWS account from the access key id: a 12-digit key selects that account, anything else falls back to `000000000000`. Working against more than one account is therefore a matter of controlling `AWS_ACCESS_KEY_ID` for the wrapped tool, and lstk already does this for two of its three AWS proxies — `lstk terraform` and `lstk sam` both accept `--account <id>` in leading position and both fall back to the ambient `AWS_ACCESS_KEY_ID`. (`lstk cdk` deliberately rejects `--account`; CDK resolves the account through an STS round-trip behind its own account cache, which did not track the flag reliably.)

`lstk aws` — the proxy people reach for first, and the one they use to inspect what the others created — has neither. It has no `--account` flag, and its support for the ambient variable is accidental and conditional:

- With **no** `localstack` AWS profile present, `AWS_ACCESS_KEY_ID=111111111111 lstk aws s3 ls` happens to work, because lstk seeds mock credentials with set-if-absent semantics and leaves an existing value alone.
- Once the user has run `lstk setup aws`, the same command **silently stops working**. lstk passes `--profile localstack` whenever the profile exists, and botocore drops the environment credential provider entirely when a profile is named explicitly. The variable is discarded without a word and every call lands in `000000000000`.

That silent, setup-dependent divergence is the real defect. A user who reads the terraform or sam help, learns the `--account` convention, and applies it to `lstk aws` gets no flag; a user who reaches for the environment variable instead gets an answer that depends on whether they ever ran `lstk setup aws`.

Separately, `lstk aws` is the only AWS-family proxy that hands a real-looking ambient access key (`AKIA…`/`ASIA…`) straight to the wrapped tool. terraform and sam both neutralize it first. The account is `000000000000` either way, so nothing functional changes — but the live key currently reaches the emulator, and it need not.

There is no `openspec/specs/aws-proxy/` capability today. This change creates it, scoped to credentials and account selection. The rest of the `lstk aws` contract (endpoint resolution, help short-circuiting, shell completion, PTY handling) is deliberately left unspecified for now and will be added to this capability over time.

## What Changes

- **Add `--account <id>` to `lstk aws`**, in leading position (between the `aws` token and the AWS service), with the same 12-digit validation and the same `--account` → ambient `AWS_ACCESS_KEY_ID` → `test` precedence that `lstk terraform` and `lstk sam` already use.
- **Do not claim `--region` for `lstk aws`.** Unlike terraform, cdk, and sam, the aws CLI has its own global `--region`, which must reach it untouched in every position. `lstk aws` recognizes exactly one leading lstk flag.
- **Select the `localstack` profile through `AWS_PROFILE` instead of `--profile`.** An explicitly named profile on the command line removes the environment credential provider from the AWS CLI's resolver chain; the environment form does not. Switching makes account selection possible *without* losing the profile: credentials come from the environment while `region`, `output`, `s3` addressing style, and everything else the user put in `[profile localstack]` continue to apply. This is what makes the ambient variable behave identically with and without `lstk setup aws`.
- **Own the whole credential triple whenever the profile is in play.** With the environment provider back in the chain, lstk must either set `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` together (an account was selected) or remove them along with `AWS_SESSION_TOKEN` (none was) — never a partial pair, which the AWS CLI rejects outright. Correspondingly, the `us-east-1` default is seeded only when there is no profile, since an environment region would override the profile's.
- **Neutralize a real-looking ambient access key** before it reaches the `aws` subprocess, matching terraform and sam.
- **Fix account propagation in terraform's S3 backend provisioning.** `lstk terraform` provisions the state bucket and lock table by shelling out to the `aws` CLI, and that invocation ignores the resolved account: the bucket is created in the account implied by the ambient environment while the generated backend block points at `--account`. The two must agree.
- **Extract the 12-digit account-id rule into `internal/validate`.** It exists today as an inline `regexp` in `cmd/iac.go`, which is the kind of local fork the project's validation rule prohibits; `lstk aws` becoming a second consumer is the moment to unify it.
- **End the leading run at the wrapped tool's action, not at the first unrecognized argument.** The old rule made `lstk aws --region eu-west-1 --account 5… sqs …` leak `--account` to the AWS CLI, which rejects it as unknown, while the reverse order worked — an ordering distinction with no meaning a user could see. All four proxies share the parser, so all four are fixed. Discovered by using the new flag.
- **Pass `lstk sam --region` on sam's command line, not only in its environment.** SAM injects `samconfig.toml` values as though typed on the command line, so a `region` key there outranked the environment and silently defeated the flag. Only an explicitly named region is forwarded, so projects that configured their own are unaffected.

## Capabilities

### Added Capabilities

- `aws-proxy`: a new capability covering the credentials `lstk aws` supplies to the wrapped AWS CLI, the `--account` flag and its precedence, and how an explicit account selection interacts with the `localstack` AWS profile. Scoped to credentials and account selection only — endpoint resolution, help handling, completion, and PTY behavior are existing `lstk aws` behavior that this capability does not yet describe.

### Modified Capabilities

- `terraform-proxy`: two requirements change. *State bucket and lock table provisioning* is tightened from "using mock credentials" to "using the resolved account", so the provisioned bucket and the generated backend block address the same LocalStack account. *Region and account selection* — the single normative statement of leading-flag parsing, which `cdk-proxy`, `sam-proxy`, and `aws-proxy` all refer to rather than restate — replaces "parsing stops at the first unrecognized argument" with "the leading run ends at the action", and states the bound that makes locating the action safe. Precedence and validation are unchanged.
- `sam-proxy`: *Region selection* gains the command-line half. It previously required only that the resolved region be encoded into the subprocess environment, which is insufficient whenever `samconfig.toml` sets a region.

## Impact

- **New code**: `validate.AWSAccountID` (`internal/validate/validate.go`).
- **Touched code**: `cmd/aws.go` (flag handling, profile decision, help text, completion), `internal/awscli/exec.go` (account-aware child environment; `Exec`'s parameter list becomes an options struct), `cmd/proxy.go` / `cmd/iac.go` (the leading-flag and account-resolution helpers become shared by all four AWS-family proxies rather than the three IaC ones, and the parser's stop condition changes), `internal/iac/sam/cli/` (the command-line region), `internal/iac/terraform/cli/provision.go` (the account fix), and `internal/iac/terraform/cli/account.go` (`DeactivateAccessKey` moves into `internal/awsconfig/credentials.go`, alongside the other credential-value helpers — all three account-selecting proxies reach it through the shared command boundary, so it belongs to no single proxy's package).
- **Unchanged behavior**: `lstk terraform`, `lstk sam`, and `lstk cdk` account handling, including cdk's rejection of `--account`. `lstk aws` with no account selected resolves the same credentials from the same profile as today; only the mechanism by which the profile is named changes, and the ambient credential variables are now removed rather than passed through unused.
- **Tests**: `internal/validate/validate_test.go`, `internal/awscli/exec_test.go`, `cmd/proxy_test.go`, `internal/iac/terraform/cli/provision_test.go`, and integration coverage in `test/integration/aws_cmd_test.go` and the terraform integration tests.
- **Docs**: the `lstk aws` `Long` help (the user-facing documentation for proxy commands, and the source `lstk docs` generates from) and the root `CLAUDE.md`.
- **Not in scope**: `lstk setup aws` still writes a fixed `aws_access_key_id = test` into `~/.aws/credentials`; per-account profiles are a separate question. `lstk cdk` remains without account selection.

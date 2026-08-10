## 1. Unify the account-id validation rule

- [x] 1.1 Add `AWSAccountID(value string) error` to `internal/validate/validate.go`, following the ordered deny-switch shape of `PodName`/`ContainerName` so the most specific reason wins: `RuleEmpty` → `RuleControlChars` → `RuleRange` (not 12 characters) → `RuleFormat` (not all digits). Document in the doc comment that the rule mirrors LocalStack's account derivation from the access key id, not an AWS API contract
- [x] 1.2 Table test with `wantRule` assertions in `internal/validate/validate_test.go`, following `TestPodName`: valid `111111111111`, empty, 11 and 13 digits (`range`), `12345678901a` and `AKIA…` (`format`), embedded control char
- [x] 1.3 Delete the inline `accountIDRe` from `cmd/iac.go` and call the validator from `resolveAccount`, keeping the user-facing message byte-identical (`--account must be a 12-digit AWS account id, got %q`) so the existing terraform and sam spec scenarios and tests are unperturbed

## 2. Relocate `DeactivateAccessKey`

- [x] 2.1 Move `DeactivateAccessKey` from `internal/iac/terraform/cli/account.go` into `internal/awsconfig/credentials.go`, alongside the existing credential-value helpers (`CredentialsFromEnv`, `ReadProfileCredentials`), keeping the doc comment and the tflocal reference. All three account-selecting proxies reach it through the shared command boundary, so it belongs to no single proxy's package — that, not any import cycle, is why it moves
- [x] 2.2 Fold `account_test.go` into `internal/awsconfig/credentials_test.go` and update the single call site (`cmd/proxy.go`, `tfcli.DeactivateAccessKey` → `awsconfig.DeactivateAccessKey`)

## 3. Make the leading-flag helpers shared by all four AWS-family proxies

- [x] 3.1 Move `rejectPreSubcommandFlags`, `stripLeadingIaCFlags`, and `resolveAccount` from `cmd/iac.go` to `cmd/proxy.go`, which already owns the shared argument surgery the `DisableFlagParsing` proxies need. Leave `resolveRegion` and the emulator/container helpers in `cmd/iac.go` — they stay IaC-only
- [x] 3.2 Parameterize `rejectPreSubcommandFlags(calledAs string, flagNames ...string)`. terraform/cdk/sam pass `"--region", "--account"`; aws passes `"--account"`. Build the message from the names with a per-flag sample value (`--region`→`us-west-2`, `--account`→`111111111111`) so the existing terraform/sam wording is unchanged
- [x] 3.3 Rename `stripLeadingIaCFlags` to `stripLeadingProxyFlags(args []string, opts leadingFlags)`, with `opts` selecting the flags each proxy recognizes. Record in the doc comment why aws opts out of `--region` — the AWS CLI owns that flag in every position, and translating it into an environment variable would override a profile's `region`. (The struct initially left `--account` implicit and always-on; task 10.4 makes it an explicit field.)
- [x] 3.4 Add `resolveAccountSelection(flag string) (account string, selected bool, err error)` with today's precedence plus a `selected` signal: true for a validated `--account`, or for an ambient `AWS_ACCESS_KEY_ID` that itself passes `validate.AWSAccountID`. Keep `resolveAccount(flag)` as a thin wrapper so terraform/cdk/sam are untouched
- [x] 3.5 Move `TestResolveAccount` out of `cmd/terraform_test.go` into `cmd/proxy_test.go` and extend it into a precedence/selection matrix; add cases proving `stripLeadingProxyFlags(args, leadingFlags{})` leaves a leading `--region` in place, and that the single-flag `rejectPreSubcommandFlags` message names only `--account`

## 4. Rebuild the AWS CLI child environment around three credential sources

The environment differs on three axes, and conflating them is what makes this subtle. Model it explicitly rather than as a pair of booleans.

- [x] 4.1 Replace `awscli.Exec`'s parameter list with an `ExecOptions{EndpointURL, Account string; UseProfile, AccountSelected, UsePTY bool}` struct — it reaches eight parameters otherwise, most of them positional bools and strings
- [x] 4.2 **No profile**: `AWS_ACCESS_KEY_ID` hard-set to the resolved account (strip-then-set, mirroring `internal/iac/sam/cli`'s managed-key loop); `AWS_SECRET_ACCESS_KEY` and `AWS_DEFAULT_REGION` seeded set-if-absent, as today
- [x] 4.3 **Profile, account selected**: set `AWS_PROFILE=localstack` and pass no `--profile` argument; hard-set `AWS_ACCESS_KEY_ID` to the account and ensure `AWS_SECRET_ACCESS_KEY` is present (set-if-absent `test`). Seed **no** `AWS_DEFAULT_REGION` — an environment region outranks the profile's `region` and would silently override it
- [x] 4.4 **Profile, no account selected**: set `AWS_PROFILE=localstack`; remove `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` outright so the profile is the sole credentials source. Seed nothing
- [x] 4.5 Remove `AWS_SESSION_TOKEN` on every path, matching `internal/iac/sam/cli`'s `strippedKeys`. Apply `DeactivateAccessKey` wherever an ambient access key is passed through rather than replaced or removed
- [x] 4.6 Never emit a partial credential pair. Add a comment at the branch naming the failure it prevents — `aws: [ERROR]: Partial credentials found in env, missing: AWS_SECRET_ACCESS_KEY` — since the code reads like defensive redundancy otherwise
- [x] 4.7 Unit tests in `internal/awscli/exec_test.go` covering all three sources: hard override with an account; set-if-absent secret/region only on the no-profile path; no region seeded on either profile path; the credential triple fully removed on the profile/no-account path; `AWS_PROFILE` set and `--profile` absent from the argv whenever a profile is in use; deactivation and session-token stripping

## 5. Wire `--account` into `lstk aws`

- [x] 5.1 In `cmd/aws.go`'s `RunE`, before the `awscli.IsHelp` short-circuit: `rejectPreSubcommandFlags(cmd.CalledAs(), "--account")`, then `stripLeadingProxyFlags(passthrough, leadingFlags{})`, then `resolveAccountSelection`. Ordering matters — `lstk aws --account … help` must consume the flag before help short-circuits
- [x] 5.2 Pass `awscli.ExecOptions{EndpointURL, Account, UseProfile: profileExists, AccountSelected, UsePTY}`. Note that `UseProfile` is now simply `profileExists` — the profile is never dropped, so there is no longer a bypass condition to compute. Keep the existing "No AWS profile found, run 'lstk setup aws'" note keyed to `profileExists`
- [x] 5.3 Strip the leading flags in `ValidArgsFunction` too, before delegating to `awscli.Complete` — an unstripped `--account 111111111111` corrupts the `COMP_LINE` handed to `aws_completer`. A strip error there degrades to `ShellCompDirectiveDefault` (the mid-typed `lstk aws --account <TAB>` case); nothing may be printed on that path
- [x] 5.4 Extend the `Long` help with the "lstk-specific flags (must appear before the aws command)" and "Supported environment variables" blocks that terraform and sam already carry. State that the `localstack` profile still applies when an account is selected, and that a user-supplied `--profile` overrides account selection. Unbroken paragraphs, one line each — `wrapText` re-wraps at render time

## 6. Fix account propagation in terraform backend provisioning

- [x] 6.1 Thread the account into `newAWSRunner(endpointURL, region, account string)` in `internal/iac/terraform/cli/provision.go` and pass it to the account-aware env builder; `provisionBackend` already holds it on the `endpointForm` it receives. Update the doc comment, which currently says credentials are "forced to the mock values"
- [x] 6.2 Extend `provision_test.go` to assert the child environment carries the resolved account and that an ambient `AWS_ACCESS_KEY_ID` does not win — the current tests assert nothing about `Env`

## 7. Integration tests

Reuse `writeFakeAWS`, `writeAWSProfile`, and `testEnvWithHome` in `test/integration/aws_cmd_test.go`; never inherit the real `$HOME`. Each case below must fail before the change.

- [x] 7.1 `AWS_ACCESS_KEY_ID=111111111111` with no profile → the child sees `AWS_ACCESS_KEY_ID=111111111111` (regression guard for the path that works today)
- [x] 7.2 The same, with a `localstack` profile written → the child still sees `111111111111`, `AWS_PROFILE=localstack` is set, and no `--profile` appears in the forwarded args. This is the silent failure the change fixes
- [x] 7.3 `lstk aws --account 111111111111 s3 ls` → `AWS_ACCESS_KEY_ID=111111111111`, forwarded args exactly `s3 ls`
- [x] 7.4 `--account` beats an ambient `AWS_ACCESS_KEY_ID`
- [x] 7.5 `lstk aws --account 12345 s3 ls` → non-zero exit, error naming the 12-digit rule, and the fake `aws` never invoked
- [x] 7.6 `lstk --account 111111111111 aws s3 ls` → placement error
- [x] 7.7 `lstk aws s3 ls --account 111111111111` → forwarded verbatim; a non-leading flag belongs to the AWS CLI
- [x] 7.8 `lstk aws --region us-west-2 s3 ls` → forwarded verbatim, proving `--region` was not stolen
- [x] 7.9 Ambient `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE` with a profile present → the credential variables are absent from the child environment and `AWS_PROFILE=localstack` is set; with no profile present → the child sees `LKIA…`
- [x] 7.10 Profile present, no account selected → `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` are all absent from the child environment
- [x] 7.11 Profile present with a partial ambient credential (`AWS_ACCESS_KEY_ID` set, no secret) → the child environment holds either both or neither, never one
- [x] 7.12 Profile setting a non-default `region` + `--account` → lstk does not set `AWS_DEFAULT_REGION`, so the profile's region survives. The fake `aws` must echo `AWS_DEFAULT_REGION` and `AWS_PROFILE` for this to be observable
- [x] 7.13 Terraform: `lstk tf --account 111111111111 init` against an S3-backend configuration → the provisioning `aws` invocation carries `AWS_ACCESS_KEY_ID=111111111111`
- [x] 7.14 Loosen `writeFakeAWS` where needed — its `shift 2` assumes `--endpoint-url URL` leads the arguments, and it echoes neither `AWS_PROFILE` nor `AWS_SESSION_TOKEN`, both of which these cases need

## 8. Verify the precedence assumption against real AWS CLIs

The whole design rests on `AWS_PROFILE` leaving the environment credential provider in botocore's resolver chain where `--profile` removes it. This was measured on `aws-cli/2.35.15`; it is long-standing botocore behavior shared by both majors, but lstk supports pip-installed v1 as well and a fake `aws` script cannot detect a regression here.

- [x] 8.1 Add an integration test that exercises the real `aws` binary (skipped when absent) and asserts `aws configure list` reports `access_key` sourced from `env` and `region` sourced from the config file, under `AWS_PROFILE` plus credentials in the environment
- [x] 8.2 Confirm the same holds for AWS CLI v1 if a v1 environment is available; if not, note the gap in the test's comment rather than leaving the assumption undocumented

## 9. Documentation

- [x] 9.1 `cmd/aws.go` `Long` (task 5.4) is the user-facing documentation; `lstk docs` generates from it, so no separate `docs/` file is needed
- [x] 9.2 Root `CLAUDE.md`: add a short account-selection note covering all four AWS-family proxies — `aws`/`terraform`/`sam` accept `--account` with the shared precedence, `cdk` rejects it — plus the `AWS_PROFILE`-not-`--profile` rule and why it exists, and the relocation of `DeactivateAccessKey`

## 10. End the leading run at the action, not the first unrecognized argument

Found by using the new flag: `lstk aws --region eu-west-1 --account 5… sqs …` failed while the reverse order worked. The parser is shared, so all four proxies were affected.

- [x] 10.1 In `stripLeadingProxyFlags` (`cmd/proxy.go`), forward an argument belonging to the wrapped tool and keep scanning instead of stopping at it. Absorb at most one following bare argument as that flag's presumed value, and only when the flag contains no `=` — the `=` is what stops terraform's `-chdir=DIR` from swallowing the action
- [x] 10.2 Record the bound in the doc comment (halts at or before the second consecutive bare argument) and why it is load-bearing: it is what keeps the AWS CLI's ten genuine `--account` parameters out of lstk's reach
- [x] 10.3 Verify by execution — not by parsing help output — which subcommands own a real `--account`; a sweep of all 425 botocore service definitions found ten
- [x] 10.4 Add `account` to `leadingFlags` and gate the branch on it, so the struct lists every lstk flag rather than leaving the always-on one implicit. All four call sites set it; cdk parses `--account` only to reject it
- [x] 10.5 Unit tests in `cmd/proxy_test.go`: both orderings equivalent, valueless and multiple tool flags before the flag, the `-chdir=` action-absorption case, the genuine `--account` operations never claimed, and `account: false`

## 11. Pass `lstk sam --region` on sam's command line

- [x] 11.1 Add `withRegionFlag` (`internal/iac/sam/cli/defaults.go`), appending `--region` for AWS-contacting subcommands only, and skipping when the forwarded args already carry one
- [x] 11.2 Split `resolveRegion` into `resolveRegionSelection` reporting whether the flag named the region, and thread `regionSelected` through `samcli.Run`. An ambient `AWS_REGION` and the default must not override a project's `samconfig.toml`
- [x] 11.3 Record the measured evidence in the doc comment (samconfig.toml outranks the environment; only a command-line `--region` beats it)
- [x] 11.4 Unit tests for `withRegionFlag` and `resolveRegionSelection`; five integration cases covering injection, the three non-injection paths, and the user's own flag
- [x] 11.5 `cmd/sam.go` help text: state that `--region` is also passed to sam and that `samconfig.toml` still decides without it

## 12. Test portability

- [x] 12.1 `aws_profile_precedence_test.go`: inherit the environment and strip `AWS_*` rather than building a minimal `PATH`+`HOME` one. Windows resolves the home directory from `USERPROFILE`, so the frozen AWS CLI aborted during import with "Could not determine home directory"
- [x] 12.2 `writeFakeAWS`: match `--endpoint-url` before `shift 2`. The help path passes no endpoint, and an unconditional shift aborts under dash (Ubuntu's `/bin/sh`, though not macOS's)

## 13. Spec accuracy

- [x] 13.1 `terraform-proxy` *Region and account selection*: replace the "parsing stops at the first unrecognized argument" rule with the leading-run-ends-at-the-action rule, and state the bound. This is the single normative statement — `cdk-proxy`, `sam-proxy`, and `aws-proxy` refer to it rather than restating it, so one edit propagates
- [x] 13.2 `sam-proxy` *Region selection*: add the command-line half
- [x] 13.3 `aws-proxy`: correct the parsing rule and add the genuine-`--account` protection scenario, before archiving makes this delta the canonical spec for a capability that does not exist yet
- [x] 13.4 `proposal.md`: correct `DeactivateAccessKey`'s destination (`internal/awsconfig`, not `internal/awscli`) and drop the import-cycle rationale, which was disproved — nothing in `internal/awscli` calls it
- [x] 13.5 `design.md`: correct the claim that the only `--account`-like flags are `--account-id` service parameters

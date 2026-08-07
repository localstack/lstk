## 1. Unify the account-id validation rule

- [ ] 1.1 Add `AWSAccountID(value string) error` to `internal/validate/validate.go`, following the ordered deny-switch shape of `PodName`/`ContainerName` so the most specific reason wins: `RuleEmpty` → `RuleControlChars` → `RuleRange` (not 12 characters) → `RuleFormat` (not all digits). Document in the doc comment that the rule mirrors LocalStack's account derivation from the access key id, not an AWS API contract
- [ ] 1.2 Table test with `wantRule` assertions in `internal/validate/validate_test.go`, following `TestPodName`: valid `111111111111`, empty, 11 and 13 digits (`range`), `12345678901a` and `AKIA…` (`format`), embedded control char
- [ ] 1.3 Delete the inline `accountIDRe` from `cmd/iac.go` and call the validator from `resolveAccount`, keeping the user-facing message byte-identical (`--account must be a 12-digit AWS account id, got %q`) so the existing terraform and sam spec scenarios and tests are unperturbed

## 2. Relocate `DeactivateAccessKey`

- [ ] 2.1 Move `DeactivateAccessKey` from `internal/iac/terraform/cli/account.go` to `internal/awscli` (e.g. `internal/awscli/credentials.go`), keeping the doc comment and the tflocal reference. `internal/awscli` now needs it and `internal/iac/terraform/cli` already imports `internal/awscli`, so the reverse import would cycle — the move is required, not cosmetic
- [ ] 2.2 Move `account_test.go` alongside it and update the single call site (`cmd/iac.go`, `tfcli.DeactivateAccessKey` → `awscli.DeactivateAccessKey`)

## 3. Make the leading-flag helpers shared by all four AWS-family proxies

- [ ] 3.1 Move `rejectPreSubcommandFlags`, `stripLeadingIaCFlags`, and `resolveAccount` from `cmd/iac.go` to `cmd/proxy.go`, which already owns the shared argument surgery the `DisableFlagParsing` proxies need. Leave `resolveRegion` and the emulator/container helpers in `cmd/iac.go` — they stay IaC-only
- [ ] 3.2 Parameterize `rejectPreSubcommandFlags(calledAs string, flagNames ...string)`. terraform/cdk/sam pass `"--region", "--account"`; aws passes `"--account"`. Build the message from the names with a per-flag sample value (`--region`→`us-west-2`, `--account`→`111111111111`) so the existing terraform/sam wording is unchanged
- [ ] 3.3 Rename `stripLeadingIaCFlags` to `stripLeadingProxyFlags(args []string, opts leadingFlags)` with `type leadingFlags struct{ region, chdir bool }`; `--account` is always recognized. terraform passes `{region: true, chdir: true}`, cdk/sam `{region: true}`, aws `{}`. Record in the doc comment why aws opts out of `--region` — the AWS CLI owns that flag in every position, and translating it into an environment variable would override a profile's `region`
- [ ] 3.4 Add `resolveAccountSelection(flag string) (account string, selected bool, err error)` with today's precedence plus a `selected` signal: true for a validated `--account`, or for an ambient `AWS_ACCESS_KEY_ID` that itself passes `validate.AWSAccountID`. Keep `resolveAccount(flag)` as a thin wrapper so terraform/cdk/sam are untouched
- [ ] 3.5 Move `TestResolveAccount` out of `cmd/terraform_test.go` into `cmd/proxy_test.go` and extend it into a precedence/selection matrix; add cases proving `stripLeadingProxyFlags(args, leadingFlags{})` leaves a leading `--region` in place, and that the single-flag `rejectPreSubcommandFlags` message names only `--account`

## 4. Rebuild the AWS CLI child environment around three credential sources

The environment differs on three axes, and conflating them is what makes this subtle. Model it explicitly rather than as a pair of booleans.

- [ ] 4.1 Replace `awscli.Exec`'s parameter list with an `ExecOptions{EndpointURL, Account string; UseProfile, AccountSelected, UsePTY bool}` struct — it reaches eight parameters otherwise, most of them positional bools and strings
- [ ] 4.2 **No profile**: `AWS_ACCESS_KEY_ID` hard-set to the resolved account (strip-then-set, mirroring `internal/iac/sam/cli`'s managed-key loop); `AWS_SECRET_ACCESS_KEY` and `AWS_DEFAULT_REGION` seeded set-if-absent, as today
- [ ] 4.3 **Profile, account selected**: set `AWS_PROFILE=localstack` and pass no `--profile` argument; hard-set `AWS_ACCESS_KEY_ID` to the account and ensure `AWS_SECRET_ACCESS_KEY` is present (set-if-absent `test`). Seed **no** `AWS_DEFAULT_REGION` — an environment region outranks the profile's `region` and would silently override it
- [ ] 4.4 **Profile, no account selected**: set `AWS_PROFILE=localstack`; remove `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` outright so the profile is the sole credentials source. Seed nothing
- [ ] 4.5 Remove `AWS_SESSION_TOKEN` on every path, matching `internal/iac/sam/cli`'s `strippedKeys`. Apply `DeactivateAccessKey` wherever an ambient access key is passed through rather than replaced or removed
- [ ] 4.6 Never emit a partial credential pair. Add a comment at the branch naming the failure it prevents — `aws: [ERROR]: Partial credentials found in env, missing: AWS_SECRET_ACCESS_KEY` — since the code reads like defensive redundancy otherwise
- [ ] 4.7 Unit tests in `internal/awscli/exec_test.go` covering all three sources: hard override with an account; set-if-absent secret/region only on the no-profile path; no region seeded on either profile path; the credential triple fully removed on the profile/no-account path; `AWS_PROFILE` set and `--profile` absent from the argv whenever a profile is in use; deactivation and session-token stripping

## 5. Wire `--account` into `lstk aws`

- [ ] 5.1 In `cmd/aws.go`'s `RunE`, before the `awscli.IsHelp` short-circuit: `rejectPreSubcommandFlags(cmd.CalledAs(), "--account")`, then `stripLeadingProxyFlags(passthrough, leadingFlags{})`, then `resolveAccountSelection`. Ordering matters — `lstk aws --account … help` must consume the flag before help short-circuits
- [ ] 5.2 Pass `awscli.ExecOptions{EndpointURL, Account, UseProfile: profileExists, AccountSelected, UsePTY}`. Note that `UseProfile` is now simply `profileExists` — the profile is never dropped, so there is no longer a bypass condition to compute. Keep the existing "No AWS profile found, run 'lstk setup aws'" note keyed to `profileExists`
- [ ] 5.3 Strip the leading flags in `ValidArgsFunction` too, before delegating to `awscli.Complete` — an unstripped `--account 111111111111` corrupts the `COMP_LINE` handed to `aws_completer`. A strip error there degrades to `ShellCompDirectiveDefault` (the mid-typed `lstk aws --account <TAB>` case); nothing may be printed on that path
- [ ] 5.4 Extend the `Long` help with the "lstk-specific flags (must appear before the aws command)" and "Supported environment variables" blocks that terraform and sam already carry. State that the `localstack` profile still applies when an account is selected, and that a user-supplied `--profile` overrides account selection. Unbroken paragraphs, one line each — `wrapText` re-wraps at render time

## 6. Fix account propagation in terraform backend provisioning

- [ ] 6.1 Thread the account into `newAWSRunner(endpointURL, region, account string)` in `internal/iac/terraform/cli/provision.go` and pass it to the account-aware env builder; `provisionBackend` already holds it on the `endpointForm` it receives. Update the doc comment, which currently says credentials are "forced to the mock values"
- [ ] 6.2 Extend `provision_test.go` to assert the child environment carries the resolved account and that an ambient `AWS_ACCESS_KEY_ID` does not win — the current tests assert nothing about `Env`

## 7. Integration tests

Reuse `writeFakeAWS`, `writeAWSProfile`, and `testEnvWithHome` in `test/integration/aws_cmd_test.go`; never inherit the real `$HOME`. Each case below must fail before the change.

- [ ] 7.1 `AWS_ACCESS_KEY_ID=111111111111` with no profile → the child sees `AWS_ACCESS_KEY_ID=111111111111` (regression guard for the path that works today)
- [ ] 7.2 The same, with a `localstack` profile written → the child still sees `111111111111`, `AWS_PROFILE=localstack` is set, and no `--profile` appears in the forwarded args. This is the silent failure the change fixes
- [ ] 7.3 `lstk aws --account 111111111111 s3 ls` → `AWS_ACCESS_KEY_ID=111111111111`, forwarded args exactly `s3 ls`
- [ ] 7.4 `--account` beats an ambient `AWS_ACCESS_KEY_ID`
- [ ] 7.5 `lstk aws --account 12345 s3 ls` → non-zero exit, error naming the 12-digit rule, and the fake `aws` never invoked
- [ ] 7.6 `lstk --account 111111111111 aws s3 ls` → placement error
- [ ] 7.7 `lstk aws s3 ls --account 111111111111` → forwarded verbatim; a non-leading flag belongs to the AWS CLI
- [ ] 7.8 `lstk aws --region us-west-2 s3 ls` → forwarded verbatim, proving `--region` was not stolen
- [ ] 7.9 Ambient `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE` with a profile present → the credential variables are absent from the child environment and `AWS_PROFILE=localstack` is set; with no profile present → the child sees `LKIA…`
- [ ] 7.10 Profile present, no account selected → `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` are all absent from the child environment
- [ ] 7.11 Profile present with a partial ambient credential (`AWS_ACCESS_KEY_ID` set, no secret) → the child environment holds either both or neither, never one
- [ ] 7.12 Profile setting a non-default `region` + `--account` → lstk does not set `AWS_DEFAULT_REGION`, so the profile's region survives. The fake `aws` must echo `AWS_DEFAULT_REGION` and `AWS_PROFILE` for this to be observable
- [ ] 7.13 Terraform: `lstk tf --account 111111111111 init` against an S3-backend configuration → the provisioning `aws` invocation carries `AWS_ACCESS_KEY_ID=111111111111`
- [ ] 7.14 Loosen `writeFakeAWS` where needed — its `shift 2` assumes `--endpoint-url URL` leads the arguments, and it echoes neither `AWS_PROFILE` nor `AWS_SESSION_TOKEN`, both of which these cases need

## 8. Verify the precedence assumption against real AWS CLIs

The whole design rests on `AWS_PROFILE` leaving the environment credential provider in botocore's resolver chain where `--profile` removes it. This was measured on `aws-cli/2.35.15`; it is long-standing botocore behavior shared by both majors, but lstk supports pip-installed v1 as well and a fake `aws` script cannot detect a regression here.

- [ ] 8.1 Add an integration test that exercises the real `aws` binary (skipped when absent) and asserts `aws configure list` reports `access_key` sourced from `env` and `region` sourced from the config file, under `AWS_PROFILE` plus credentials in the environment
- [ ] 8.2 Confirm the same holds for AWS CLI v1 if a v1 environment is available; if not, note the gap in the test's comment rather than leaving the assumption undocumented

## 9. Documentation

- [ ] 9.1 `cmd/aws.go` `Long` (task 5.4) is the user-facing documentation; `lstk docs` generates from it, so no separate `docs/` file is needed
- [ ] 9.2 Root `CLAUDE.md`: add a short account-selection note covering all four AWS-family proxies — `aws`/`terraform`/`sam` accept `--account` with the shared precedence, `cdk` rejects it — plus the `AWS_PROFILE`-not-`--profile` rule and why it exists, and the relocation of `DeactivateAccessKey`

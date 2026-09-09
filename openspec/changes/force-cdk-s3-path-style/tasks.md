## 1. Failing tests first (red)

- [x] 1.1 Extend the fake-cdk config in `test/integration/cdk_cmd_test.go` to echo `ENV_CDK_S3_FORCE_PATH_STYLE={env:CDK_S3_FORCE_PATH_STYLE}` alongside the existing `ENV_AWS_ENDPOINT_URL_S3` line, so the subprocess environment is observable through the CLI's own output.
- [x] 1.2 Add an integration test asserting `CDK_S3_FORCE_PATH_STYLE=1` reaches the `cdk` subprocess on a plain `lstk cdk` invocation with no S3 override — with a fake `cdk` reporting **2.1138.0 or newer**. The existing fakes report 2.177.0, which now selects the old branch where the flag is deliberately absent.
- [x] 1.3 Add an integration test asserting the variable is **absent** when the caller sets `AWS_ENDPOINT_URL_S3`, against a **2.1138.0+** fake so the assertion is meaningful rather than vacuously true on the old branch (use `testEnvWithHome` + `env.With(...)`, never the developer's real environment).
- [x] 1.4 Against a **2.1138.0+** fake, invert the caller-value test: a caller-supplied `CDK_S3_FORCE_PATH_STYLE` (try `0` and the empty string) is overridden with `1`, and is **stripped entirely** when `AWS_ENDPOINT_URL_S3` is also set — the both-set case is what makes "not overridable" true.
- [x] 1.5 Run all three and confirm they fail for the expected reason (variable absent / overwritten), not on harness errors.

## 2. Implementation (green)

- [x] 2.1 Add a `forcePathStyle bool` parameter (or equivalent explicit input — no reading of `os.Getenv` inside `BuildEnv`) to `BuildEnv` in `internal/iac/cdk/cli/exec.go`, appending `CDK_S3_FORCE_PATH_STYLE=1` when true.
- [x] 2.2 Move `CDK_S3_FORCE_PATH_STYLE` into the `managed` list so membership strips any caller-supplied value, with the value `"1"` normally and `""` when suppressed (the existing empty-value skip then means "strip but do not set"). Delete the `callerSetsPathStyle` scan and the trailing conditional append.
- [x] 2.3 In `Run`, pass `forcePathStyle` as "the CDK is 2.1138.0+ **and** the caller did not set `AWS_ENDPOINT_URL_S3`" — reuse the existing `s3EndpointOverride()` result rather than reading the variable a second time.
- [x] 2.4 Leave `endpoint.S3Addressing`'s `pathStyle` return unused on the CDK path; do not branch the new variable on host shape.
- [x] 2.5 Run the tests from group 1 and confirm they pass.

## 3. Unit coverage

- [x] 3.1 Rework the `BuildEnv` subtests: variable set to `1` when `forcePathStyle` is true, absent when false, caller value overridden in the first case and stripped in the second, exactly one entry for the key, and the produced environment still deterministic in order.
- [x] 3.2 Assert the existing managed values (`AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`, mock creds, region) are unchanged by the addition.

## 4. Retire superseded guidance

- [x] 4.1 Update the doc comment on `endpoint.S3Addressing` (`internal/endpoint/s3.go`): drop "CDK has no env-only lever for path style, so a `pathStyle==true` result there is informational only", and note that the CDK proxy forces path style via `CDK_S3_FORCE_PATH_STYLE` on CDK 2.1138.0+ while terraform — and the pre-2.1138.0 CDK branch — still consume both return values. The comment currently on disk says the function is terraform-only, which the version gate makes wrong.
- [x] 4.2 Loopback S3 warning stays deleted. Measured with cdk 2.1137.0 (old branch) against an IP endpoint through a logging proxy: `PUT host=127.0.0.1:4599 path=/<bucket>/<key>` — the AWS SDK ruleset uses path style for IP endpoints regardless of the flag, so the warning was never accurate on either branch.
- [ ] 4.3 Correct the `CDK_S3_FORCE_PATH_STYLE` line in the `lstk cdk` `Long` help text (`cmd/cdk.go`): it currently reads as a variable the user may set. It is set by lstk and ignored from the caller; `AWS_ENDPOINT_URL_S3` is the only lever over it. Single unbroken line per the help-text rule.

## 5. Verify end to end

- [x] 5.1 Against a real LocalStack, run an asset-bearing `cdk deploy` with `--endpoint-url` pointing at a non-loopback, non-`localstack.cloud` hostname and confirm it succeeds (before the change it fails with `invalid XML received: b'PK\x03\x04...'`).
- [x] 5.2 Regression-check the two paths that already worked: `--endpoint-url http://localhost:4566` and the default `localhost.localstack.cloud` resolution, both with an asset-bearing deploy.
- [x] 5.3 Run `cdk bootstrap` and `cdk gc --unstable=gc --action=print --type=s3` — the `toolkit-lib` S3 path, distinct from `cdk-assets`.
- [x] 5.4 `make test` (0 failures) and `make lint` (0 issues) pass. `make test-integration`: all CDK tests pass and no failure is attributable to this change; 52 tests fail in this environment for lack of `LOCALSTACK_AUTH_TOKEN` (46) or a running emulator (6) — needs a re-run in a credentialed environment to be conclusive.

## 6. Documentation

- [x] 6.1 `README.md` does not document CDK environment variables (only a one-line proxy mention) — no change needed.
- [x] 6.2 No `CLAUDE.md` line: the behavior is one declaration deep, documented on `pathStyleEnvVar`/`BuildEnv` and in `S3Addressing`'s comment.
- [x] 6.3 Resolved: introduced in aws-cdk 2.1138.0 (aws/aws-cdk-cli#1625, absent in 2.1137.0, verified by inspecting both packages). Recorded on `pathStyleEnvVar` and in the help text.

## 7. Version-gate the addressing pair

Current on-disk state regresses CDK < 2.1138.0: the `s3.` prefix was dropped unconditionally, so an old CLI does virtual-host addressing against a host LocalStack does not recognize. Verified against aws-cdk 2.1137.0.

- [x] 7.1 Return the parsed version from `CheckVersion` (`internal/iac/cdk/cli/version.go`) instead of discarding it, so `Run` can gate without a second `cdk --version` exec. Add a 2.1138.0 threshold constant beside the floor constants in `defaults.go`.
- [x] 7.2 In `Run`, select the pair: 2.1138.0+ → force path style and pass the base endpoint verbatim; below → no flag and `endpoint.S3Addressing`'s prefixed endpoint (restoring the `internal/endpoint` import). The caller's `AWS_ENDPOINT_URL_S3` override still wins over both.
- [x] 7.3 Integration test both sides with fakes reporting 2.1137.0 and 2.1138.0: assert flag absent + `s3.`-prefixed endpoint on the old side, flag `1` + unprefixed on the new side. `TestCDKS3EndpointHasNoHostPrefix` as written asserts the new-branch behavior against a 2.177.0 fake and must be re-pointed.
- [x] 7.4 Check the snapshot fallout: the 14 `ENV_CDK_S3_FORCE_PATH_STYLE=1` lines were regenerated against 2.177.0 fakes and belong on the old branch, where the flag is now absent. Either bump those fakes' reported version or expect the lines to disappear.
- [x] 7.5 Fix the stale comment in `test/integration/cdk_e2e_test.go` about the derived `s3.localhost.localstack.cloud` endpoint — it is accurate again for the old branch, so make it say which branch it describes.
- [ ] 7.6 Verify on a live sandbox with CDK 2.1138.0+: an asset-bearing `cdk deploy --endpoint-url https://ls-<id>.sandbox.localstack.cloud` succeeds with no extra environment variables.
- [x] 7.7 Verify no regression on CDK 2.1137.0 (installed under the scratchpad): bootstrap and an asset-bearing deploy against a local emulator both succeed.
- [x] 7.8 Re-run `make test`, `make lint`, and the CDK integration tests; re-check 5.1/5.2/5.3 on the new branch.

## ADDED Requirements

### Requirement: S3 path-style addressing for the CDK subprocess

When the resolved CDK CLI is version 2.1138.0 or newer — the release that introduced `CDK_S3_FORCE_PATH_STYLE` — the system SHALL set `CDK_S3_FORCE_PATH_STYLE=1` in the `cdk` subprocess environment by default, so that CDK's asset publisher and toolkit use path-style S3 addressing against the resolved LocalStack endpoint.

S3 addressing mode and S3 endpoint host form one matched pair, and the system SHALL select the pair that matches the resolved CDK version: path style with the unprefixed base endpoint at 2.1138.0 and above, virtual-host addressing with the `s3.`-prefixed endpoint below it. Applying half of either pair breaks asset publishing. The system SHALL determine the version from the `cdk --version` output it already reads for the minimum-version check, without a second invocation.

The value SHALL be set irrespective of whether the resolved endpoint host is virtual-host-capable: path style works against LocalStack for every host lstk resolves, so the addressing decision does not depend on the host.

lstk SHALL NOT set `CDK_S3_FORCE_PATH_STYLE` when the caller has explicitly set `AWS_ENDPOINT_URL_S3`, because that override means the caller is directing S3 themselves.

`CDK_S3_FORCE_PATH_STYLE` SHALL be lstk's to set: the system SHALL remove any caller-supplied value from the `cdk` subprocess environment whether or not it goes on to set its own, so the caller cannot force, suppress, or otherwise influence the addressing mode through that variable. `AWS_ENDPOINT_URL_S3` is the only caller-facing lever over it.

On the 2.1138.0-and-newer branch, because addressing is always path style, the S3 endpoint no longer needs a virtual-host host prefix: the system SHALL set `AWS_ENDPOINT_URL_S3` to the resolved base endpoint verbatim, without the `s3.` prefix that `endpoint.S3Addressing` applies to virtual-host-capable hosts. `endpoint.S3Addressing` is unchanged and continues to serve both `lstk terraform` and the pre-2.1138.0 branch.

This requirement does not change the minimum CDK version, which remains 2.177.0.

#### Scenario: Path style is forced by default

- **WHEN** lstk runs a CDK command and the caller has set neither `AWS_ENDPOINT_URL_S3` nor `CDK_S3_FORCE_PATH_STYLE`
- **THEN** the `cdk` subprocess environment contains `CDK_S3_FORCE_PATH_STYLE=1`
- **AND** `AWS_ENDPOINT_URL_S3` equals `AWS_ENDPOINT_URL` — the resolved base endpoint verbatim, with no `s3.` host prefix, for every host including virtual-host-capable ones

#### Scenario: Assets publish against a LocalStack Cloud sandbox instance

- **WHEN** the resolved endpoint is an HTTPS sandbox or ephemeral instance such as `https://ls-<id>.sandbox.localstack.cloud`, whose TLS certificate is a single-label wildcard covering neither `s3.<id>.sandbox…` nor `<bucket>.<id>.sandbox…`
- **THEN** lstk directs S3 at the base host with path style, the TLS handshake succeeds, and an asset-bearing `cdk deploy` publishes and completes
- **AND** lstk does not derive an `s3.`-prefixed S3 endpoint, which would otherwise fail the handshake with `tlsv1 alert internal error` before any S3 request is sent

#### Scenario: Assets publish against a non-loopback endpoint host

- **WHEN** the resolved endpoint host is an ordinary hostname that is neither a loopback literal (`localhost`, `127.*`, `::1`) nor virtual-host-capable — for example `http://localstack:4566` — and the user runs a `cdk deploy` that publishes file assets
- **THEN** the asset uploads address the bucket in the request path rather than as a host prefix, and the deploy succeeds

#### Scenario: Caller-set AWS_ENDPOINT_URL_S3 suppresses the default

- **WHEN** `AWS_ENDPOINT_URL_S3` is set in the caller's environment
- **THEN** lstk does not set `CDK_S3_FORCE_PATH_STYLE` in the subprocess environment
- **AND** the caller's `AWS_ENDPOINT_URL_S3` value is still forwarded to the `cdk` subprocess as it is today

#### Scenario: Caller-set CDK_S3_FORCE_PATH_STYLE is overridden

- **WHEN** `CDK_S3_FORCE_PATH_STYLE` is set to any value in the caller's environment — including `0` and the empty string — and `AWS_ENDPOINT_URL_S3` is not set
- **THEN** the `cdk` subprocess receives `CDK_S3_FORCE_PATH_STYLE=1`, not the caller's value

#### Scenario: Caller-set CDK_S3_FORCE_PATH_STYLE is stripped on the suppressed path

- **WHEN** the caller sets both `AWS_ENDPOINT_URL_S3` and `CDK_S3_FORCE_PATH_STYLE`
- **THEN** `CDK_S3_FORCE_PATH_STYLE` is absent from the `cdk` subprocess environment
- **AND** setting it alongside the endpoint override is therefore not a way to force path style on that path

#### Scenario: CDK older than 2.1138.0 keeps today's behavior

- **WHEN** the resolved `cdk` binary reports a version at or above the 2.177.0 floor but below 2.1138.0, so it does not recognize `CDK_S3_FORCE_PATH_STYLE`
- **THEN** lstk does not set `CDK_S3_FORCE_PATH_STYLE` at all, and derives `AWS_ENDPOINT_URL_S3` with the `s3.` host prefix for virtual-host-capable hosts exactly as it does today
- **AND** an asset-bearing `cdk deploy` against a local emulator continues to succeed, as it did before this change

#### Scenario: The version gate needs no extra subprocess

- **WHEN** lstk runs any CDK command
- **THEN** it invokes `cdk --version` once, and uses that single result for both the minimum-version check and the addressing-pair selection

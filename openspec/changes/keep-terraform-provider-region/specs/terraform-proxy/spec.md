## MODIFIED Requirements

### Requirement: Provider override file generation

Before invoking `terraform` for any command that provisions or reads infrastructure, the system SHALL generate a Terraform override file (named `localstack_providers_override.tf` by default, configurable via `LSTK_TF_OVERRIDE_FILE_NAME`) that, for each `aws` provider block discovered in the working directory, defines a matching `provider "aws"` block configured to target LocalStack.

Provider-block discovery SHALL recurse into sub-directories of the working directory, so `aws` provider blocks declared in nested modules are also represented. Hidden directories (for example `.terraform`, which holds the downloaded provider/module cache, and `.git`) SHALL be skipped. A `*.tf` file that cannot be parsed SHALL be skipped individually (logged, not fatal) rather than aborting discovery. This recursive scan does not guarantee coverage of every possible layout (e.g. remote modules), but is broader than scanning only the top-level directory.

Each generated provider block SHALL set `secret_key = "test"`, the resolved `access_key` (the target account ID, see "Region and account selection"), the resolved `region` (only when the source block sets none, see below), `skip_credentials_validation = true`, `skip_metadata_api_check = true`, and an `endpoints {}` block mapping supported AWS service keys to the LocalStack endpoint. The block SHALL preserve the `alias` of the source provider block when present.

The resolved `access_key` SHALL be encoded directly into the override block (rather than passed as an environment variable to the `terraform` subprocess), so that it takes deterministic precedence over any value present in the user's own provider configuration.

The resolved `region` SHALL be encoded only into override blocks whose alias has no `region` attribute (literal or expression) on a `provider "aws"` block in the working directory itself. A provider block that sets its own region keeps it, as with plain `terraform`, so a configuration can pin an aliased provider to a different region from the default one (e.g. `us-east-1` for CloudFront certificates and Lambda@Edge). Blocks found only in sub-directories do not count, because an override file merges only with provider blocks in its own module directory.

The system SHALL set `s3_use_path_style = true` when the resolved host does not support virtual-host-style addressing (for example `127.0.0.1`/`localhost`), and `false` when the resolved host is a virtual-host-capable `*.localstack.cloud` domain.

When the resolved host is virtual-host-capable (path style off), the `s3` endpoint value SHALL use a host prefixed with `s3.` (for example `https://s3.localhost.localstack.cloud:4566`), unless the host already begins with `s3.`. When path style is on, the `s3` endpoint SHALL be the bare resolved endpoint, identical to the other services. This prefix applies only to the `s3` endpoint key; all other service endpoints SHALL use the resolved endpoint unchanged.

#### Scenario: Override generated for default provider

- **WHEN** the working directory contains an `aws` provider block with no alias and the user runs `lstk terraform plan`
- **THEN** an override file is created containing a `provider "aws"` block with LocalStack credentials, region, skip flags, and an `endpoints {}` block

#### Scenario: Override generated per provider alias

- **WHEN** the working directory contains multiple `aws` provider blocks distinguished by `alias`
- **THEN** the override file contains one matching `provider "aws"` block per alias, each carrying the same `alias`

#### Scenario: Provider blocks in sub-directories are discovered

- **WHEN** an `aws` provider block (e.g. an aliased one) is declared in a `*.tf` file inside a sub-directory of the working directory
- **THEN** the override file includes a matching `provider "aws"` block for it, in addition to any blocks discovered at the top level
- **AND** provider blocks under hidden directories such as `.terraform` are not discovered

#### Scenario: Override file name is configurable

- **WHEN** the `LSTK_TF_OVERRIDE_FILE_NAME` environment variable is set to a custom file name
- **THEN** the override file is written using that name

#### Scenario: S3 endpoint host is prefixed for virtual-host addressing

- **WHEN** the resolved host is a virtual-host-capable `*.localstack.cloud` domain (e.g. `localhost.localstack.cloud`)
- **THEN** the generated `endpoints {}` block sets the `s3` endpoint to the resolved endpoint with an `s3.` host prefix (e.g. `https://s3.localhost.localstack.cloud:4566`)
- **AND** `s3_use_path_style` is `false`
- **AND** all non-S3 endpoints use the resolved endpoint without a prefix

#### Scenario: S3 endpoint uses path style for non-domain hosts

- **WHEN** the resolved host does not support virtual-host addressing (e.g. `127.0.0.1`/`localhost`)
- **THEN** the `s3` endpoint is the bare resolved endpoint with no `s3.` prefix
- **AND** `s3_use_path_style` is `true`

#### Scenario: A provider block's own region is kept

- **WHEN** the working directory declares a default `aws` provider with `region = "eu-central-1"` and an aliased `aws` provider with `region = "us-east-1"`
- **THEN** neither generated override block sets `region`
- **AND** resources created through the aliased provider land in `us-east-1`, and those through the default provider in `eu-central-1`

#### Scenario: A provider block without a region gets the resolved region

- **WHEN** a root-level `aws` provider block sets no `region`
- **THEN** its generated override block sets the resolved `region`

#### Scenario: A region set only in a sub-directory is ignored

- **WHEN** an aliased `aws` provider block in the working directory sets no `region`, and a block with the same alias in a sub-directory does
- **THEN** the generated override block for that alias sets the resolved `region`

### Requirement: Region and account selection

The command SHALL accept two lstk-specific flags, `--region` and `--account`, that set the deployment region and the target AWS account ID for the generated provider override. Because these are not standard `terraform` flags, the command SHALL parse and remove them (together with their values) from the argument list before forwarding the remaining arguments to the `terraform` binary. Both `--flag value` and `--flag=value` forms SHALL be supported.

These flags SHALL be recognized only in leading position — that is, after `terraform`/`tf` and before the terraform action. The leading run ends at the action, **not** at the first argument lstk does not recognize: an argument belonging to the wrapped tool SHALL be forwarded unchanged and scanning SHALL continue, so lstk's flags are recognized in any order relative to the tool's own. Any `--region`/`--account` appearing after the action SHALL be treated as ordinary arguments for the wrapped tool and forwarded unchanged. The flags SHALL NOT be defined as root/persistent flags, so a flag placed before the `terraform` subcommand (e.g. `lstk --account … terraform`) is not accepted.

Locating the action without knowledge of every wrapped tool's own flags SHALL rest on a single rule: a bare argument following a flag that may still take a value (one containing no `=`) is that flag's value. At most one bare argument SHALL be absorbed per flag, so scanning always halts at or before the second consecutive bare argument, which bounds it to the wrapped tool's action at the latest. That bound is what prevents lstk from consuming an `--account` that genuinely belongs to the wrapped tool — see the `aws-proxy` capability, where the AWS CLI defines a real `--account` parameter on several operations.

This paragraph is the normative statement of leading-flag parsing for every proxy; `cdk-proxy`, `sam-proxy`, and `aws-proxy` refer to it rather than restating it.

The resolved region SHALL be selected with precedence: `--region` flag, then the `AWS_REGION` environment variable, then a default of `us-east-1`. The deprecated `AWS_DEFAULT_REGION` environment variable SHALL NOT be consulted. The system SHALL additionally record whether the region was named by the flag rather than inherited or defaulted, because `sam-proxy` needs that distinction (see its "Region selection" requirement).

The resolved account (provider `access_key`) SHALL be selected with precedence: `--account` flag, then the `AWS_ACCESS_KEY_ID` environment variable, then a default of `test`.

The `--account` flag value SHALL be validated to be exactly 12 digits (`^\d{12}$`). A value supplied via `AWS_ACCESS_KEY_ID` SHALL NOT be validated, but SHALL be passed through an access-key deactivation step: if it begins with the letter `A` (the prefix of real AWS access key ids such as `AKIA…` long-term keys and `ASIA…` temporary session keys), the leading `A` SHALL be replaced with `L` before the value is encoded into the override. This guards against a real AWS credential accidentally present in the environment being written into the generated override file or sent to LocalStack. The validated 12-digit `--account` flag value is used as-is (it cannot begin with `A`).

For unproxied subcommands (`fmt`, `validate`, `version`), both flags SHALL be a no-op: they are stripped from the arguments, not forwarded to `terraform`, and have no other effect.

#### Scenario: Region flag encoded into override

- **WHEN** a user runs `lstk terraform --region us-west-2 plan`
- **THEN** `--region us-west-2` is removed from the forwarded arguments (leaving `plan`)
- **AND** the generated provider blocks whose source block sets no `region` set `region = "us-west-2"`

#### Scenario: Account flag encoded into override

- **WHEN** a user runs `lstk terraform --account 111111111111 apply`
- **THEN** `--account 111111111111` is removed from the forwarded arguments (leaving `apply`)
- **AND** the generated provider blocks set `access_key = "111111111111"`

#### Scenario: Flags must lead — placement after the action is not recognized

- **WHEN** a user runs `lstk terraform plan --region us-west-2`
- **THEN** parsing stops at `plan`, so `--region us-west-2` is NOT consumed by lstk
- **AND** `--region us-west-2` is forwarded to `terraform` as-is (where terraform rejects it as an unknown flag)

#### Scenario: Flags before the terraform subcommand are rejected

- **WHEN** a user runs `lstk --account 111111111111 terraform plan`
- **THEN** the command fails with an error stating that `--region`/`--account` must appear after the terraform subcommand
- **AND** the `terraform` binary is not invoked

#### Scenario: Region falls back to environment then default

- **WHEN** `--region` is not supplied
- **THEN** the region is taken from `AWS_REGION` if set, otherwise `us-east-1`

#### Scenario: Account falls back to environment then default

- **WHEN** `--account` is not supplied
- **THEN** the `access_key` is taken from `AWS_ACCESS_KEY_ID` if set, otherwise `test`

#### Scenario: Real AWS access key from the environment is deactivated

- **WHEN** `--account` is not supplied and `AWS_ACCESS_KEY_ID` holds a real-looking AWS access key id beginning with `A` (e.g. `AKIAIOSFODNN7EXAMPLE`)
- **THEN** the value's leading `A` is replaced with `L` (e.g. `LKIAIOSFODNN7EXAMPLE`) before it is encoded into the override `access_key`
- **AND** the original (live) key is never written to disk nor sent to LocalStack

#### Scenario: Mock access key from the environment is unchanged

- **WHEN** `--account` is not supplied and `AWS_ACCESS_KEY_ID` holds a value that does not begin with `A` (e.g. `test`)
- **THEN** the value is used as the `access_key` unchanged

#### Scenario: Flag overrides environment

- **WHEN** both `--region` and `AWS_REGION` are set (or both `--account` and `AWS_ACCESS_KEY_ID`)
- **THEN** the flag value takes precedence over the environment variable

#### Scenario: Invalid account is rejected

- **WHEN** a user runs `lstk terraform --account 12345 plan`
- **THEN** the command fails with a clear error stating the account ID must be 12 digits
- **AND** the `terraform` binary is not invoked

#### Scenario: Flag with missing value

- **WHEN** a user runs `lstk terraform --region` with no value following it
- **THEN** the command fails with a clear error stating the flag requires a value

#### Scenario: Flags are a no-op for unproxied subcommands

- **WHEN** a user runs `lstk terraform --region us-west-2 --account 111111111111 validate`
- **THEN** both flags are stripped (leaving `validate`) and not forwarded to `terraform`
- **AND** no override file is generated and the flags have no other effect

#### Scenario: Resolved values take precedence over user provider config

- **WHEN** the user's own `aws` provider block specifies an `access_key` and `--account` (or its env fallback) resolves a value
- **THEN** the generated override block's encoded `access_key` takes effect over the user's value

#### Scenario: Resolved region does not override a provider's own region

- **WHEN** the user's own root-level `aws` provider block specifies a `region` and `--region` (or its env fallback/default) resolves a different value
- **THEN** the generated override block for that provider sets no `region`, and the user's value takes effect

#### Scenario: lstk flags are recognized in any order relative to the tool's own

- **WHEN** a user runs `lstk terraform -chdir=infra --account 111111111111 apply`, or places any argument the wrapped tool owns before an lstk flag
- **THEN** the tool's argument is forwarded unchanged, scanning continues past it, and `--account` is still consumed
- **AND** the result is identical to placing the lstk flag first

#### Scenario: A bare argument after the action ends the leading run

- **WHEN** a user runs `lstk terraform -chdir=infra plan --region us-west-2`
- **THEN** `plan` ends the leading run — it is not absorbed as `-chdir`'s value, because `-chdir=infra` carries its value inline
- **AND** `--region us-west-2` is forwarded to `terraform` unchanged

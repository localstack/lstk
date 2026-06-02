## ADDED Requirements

### Requirement: Terraform command proxy

The system SHALL provide a `lstk terraform` command, with the alias `lstk tf`, that forwards all of its arguments to the underlying `terraform` binary so that Terraform runs against the local LocalStack instance.

The command SHALL pass through the child process's standard input, standard output, and standard error unmodified, and SHALL propagate the child process's exit code as the command's result.

#### Scenario: Arguments are forwarded to terraform

- **WHEN** a user runs `lstk terraform plan -out=tfplan`
- **THEN** the underlying `terraform` binary is invoked with `plan -out=tfplan`
- **AND** terraform's stdout and stderr are streamed to the user unaltered

#### Scenario: Alias tf behaves identically

- **WHEN** a user runs `lstk tf apply`
- **THEN** the behavior is identical to `lstk terraform apply`

#### Scenario: Non-zero terraform exit is propagated

- **WHEN** the underlying `terraform` invocation exits with a non-zero status
- **THEN** `lstk` exits non-zero without printing an additional lstk-level error message

#### Scenario: Terraform binary missing

- **WHEN** no `terraform` binary (nor the configured `TF_CMD`) is found on `PATH`
- **THEN** the command fails with a clear error explaining that Terraform must be installed and available on `PATH`

### Requirement: LocalStack must be running

The command SHALL require a running LocalStack AWS emulator and SHALL resolve its endpoint automatically using lstk's container discovery and host resolution, without requiring the user to specify a host or port.

#### Scenario: No running emulator

- **WHEN** a user runs `lstk terraform plan` and no LocalStack AWS emulator is running
- **THEN** the command fails with an error stating LocalStack is not running and suggesting how to start it (`lstk`)
- **AND** the `terraform` binary is not invoked

#### Scenario: Endpoint resolved from running emulator

- **WHEN** a LocalStack AWS emulator is running
- **THEN** the command resolves the endpoint via the same discovery used by `lstk aws` (container discovery plus host resolution)
- **AND** uses that endpoint as the base for all generated provider endpoints

#### Scenario: Explicit endpoint override

- **WHEN** the `AWS_ENDPOINT_URL` environment variable is set
- **THEN** its host and port take precedence over the auto-resolved endpoint when building the provider override

### Requirement: Provider override file generation

Before invoking `terraform` for any command that provisions or reads infrastructure, the system SHALL generate a Terraform override file (named `localstack_providers_override.tf` by default, configurable via `LS_PROVIDERS_FILE`) that, for each `aws` provider block discovered in the working directory, defines a matching `provider "aws"` block configured to target LocalStack.

Each generated provider block SHALL set `secret_key = "test"`, the resolved `access_key` (the target account ID, see "Region and account selection"), the resolved `region`, `skip_credentials_validation = true`, `skip_metadata_api_check = true`, and an `endpoints {}` block mapping supported AWS service keys to the LocalStack endpoint. The block SHALL preserve the `alias` of the source provider block when present.

The resolved `access_key` and `region` values SHALL be encoded directly into the override block (rather than passed as environment variables to the `terraform` subprocess), so that they take deterministic precedence over any values present in the user's own provider configuration.

The system SHALL set `s3_use_path_style = true` when the resolved host does not support virtual-host-style addressing (for example `127.0.0.1`/`localhost`), and `false` when the resolved host is a virtual-host-capable `*.localstack.cloud` domain.

When the resolved host is virtual-host-capable (path style off), the `s3` endpoint value SHALL use a host prefixed with `s3.` (for example `https://s3.localhost.localstack.cloud:4566`), unless the host already begins with `s3.`. When path style is on, the `s3` endpoint SHALL be the bare resolved endpoint, identical to the other services. This prefix applies only to the `s3` endpoint key; all other service endpoints SHALL use the resolved endpoint unchanged.

#### Scenario: Override generated for default provider

- **WHEN** the working directory contains an `aws` provider block with no alias and the user runs `lstk terraform plan`
- **THEN** an override file is created containing a `provider "aws"` block with LocalStack credentials, region, skip flags, and an `endpoints {}` block

#### Scenario: Override generated per provider alias

- **WHEN** the working directory contains multiple `aws` provider blocks distinguished by `alias`
- **THEN** the override file contains one matching `provider "aws"` block per alias, each carrying the same `alias`

#### Scenario: Aliases can be skipped

- **WHEN** the `SKIP_ALIASES` environment variable lists one or more provider alias names
- **THEN** no override block is generated for those aliases

#### Scenario: Override file name is configurable

- **WHEN** the `LS_PROVIDERS_FILE` environment variable is set to a custom file name
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

### Requirement: Region and account selection

The command SHALL accept two lstk-specific flags, `--region` and `--account`, that set the deployment region and the target AWS account ID for the generated provider override. Because these are not standard `terraform` flags, the command SHALL parse and remove them (together with their values) from the argument list before forwarding the remaining arguments to the `terraform` binary. Both `--flag value` and `--flag=value` forms SHALL be supported.

These flags SHALL be recognized only in leading position — that is, immediately after `terraform`/`tf` and before the terraform action and any other arguments. Parsing SHALL stop at the first argument that is not one of these flags (or their values); any `--region`/`--account` appearing after that point SHALL be treated as ordinary terraform arguments and forwarded unchanged. The flags SHALL NOT be defined as root/persistent flags, so a flag placed before the `terraform` subcommand (e.g. `lstk --account … terraform`) is not accepted.

The resolved region SHALL be selected with precedence: `--region` flag, then the `AWS_REGION` environment variable, then a default of `us-east-1`. The deprecated `AWS_DEFAULT_REGION` environment variable SHALL NOT be consulted.

The resolved account (provider `access_key`) SHALL be selected with precedence: `--account` flag, then the `AWS_ACCESS_KEY_ID` environment variable, then a default of `test`.

The `--account` flag value SHALL be validated to be exactly 12 digits (`^\d{12}$`); a value supplied via `AWS_ACCESS_KEY_ID` SHALL be forwarded verbatim without validation.

For unproxied subcommands (`fmt`, `validate`, `version`), both flags SHALL be a no-op: they are stripped from the arguments, not forwarded to `terraform`, and have no other effect.

#### Scenario: Region flag encoded into override

- **WHEN** a user runs `lstk terraform --region us-west-2 plan`
- **THEN** `--region us-west-2` is removed from the forwarded arguments (leaving `plan`)
- **AND** the generated provider blocks set `region = "us-west-2"`

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
- **THEN** the command fails because `--account` is not a known root flag
- **AND** the `terraform` binary is not invoked

#### Scenario: Region falls back to environment then default

- **WHEN** `--region` is not supplied
- **THEN** the region is taken from `AWS_REGION` if set, otherwise `us-east-1`

#### Scenario: Account falls back to environment then default

- **WHEN** `--account` is not supplied
- **THEN** the `access_key` is taken from `AWS_ACCESS_KEY_ID` if set, otherwise `test`

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

- **WHEN** the user's own `aws` provider block specifies a `region` or `access_key` and `--region`/`--account` (or their env fallbacks) resolve a value
- **THEN** the generated override block's encoded `region`/`access_key` take effect over the user's values

### Requirement: Dynamic endpoint discovery from provider schema

The set of AWS service endpoint keys written into the `endpoints {}` block SHALL be derived dynamically by querying the installed Terraform AWS provider schema (`terraform providers schema -json`) rather than from a hard-coded service list. The system SHALL read the endpoint attribute keys from the AWS provider's `endpoints` nested block in the schema JSON. Discovery SHALL work for the Terraform AWS provider version 4.0 and higher.

The system SHALL NOT maintain a built-in fallback endpoint-key list. Each discovered endpoint key SHALL be written into the `endpoints {}` block exactly as named in the provider schema, without case transformation, and SHALL map to the single resolved LocalStack endpoint — except the `s3` key, whose host may carry an `s3.` prefix for virtual-host addressing (see "Provider override file generation").

#### Scenario: Endpoint keys come from the provider schema

- **WHEN** the override file is generated and the AWS provider is installed
- **THEN** the endpoint keys written into the `endpoints {}` block are exactly the endpoint attribute names reported by `terraform providers schema -json` for the AWS provider

#### Scenario: Provider schema unavailable because terraform init has not run

- **WHEN** the AWS provider schema cannot be retrieved because `terraform init` has not installed the provider (the `providers schema` query fails or reports no AWS provider)
- **THEN** the command fails with a specific error instructing the user to run `terraform init` first
- **AND** no override file is generated and the `terraform` binary is not invoked

#### Scenario: No endpoint keys discovered

- **WHEN** the AWS provider schema is retrieved but yields no endpoint keys
- **THEN** the command fails rather than generating an override with an empty `endpoints {}` block

### Requirement: Cleanup of generated files

The system SHALL remove any override file(s) it generated after the `terraform` invocation completes, including when terraform exits non-zero or is interrupted.

#### Scenario: Override removed after run

- **WHEN** `lstk terraform plan` completes (successfully or with an error)
- **THEN** the generated override file is deleted from the working directory

#### Scenario: Pre-existing override file is not clobbered

- **WHEN** an override file with the target name already exists before the command runs
- **THEN** the command fails with an error rather than overwriting or deleting a file it did not create

### Requirement: Unproxied and dry-run commands

For Terraform subcommands that do not interact with provider endpoints (`fmt`, `validate`, `version`), the system SHALL invoke `terraform` directly without generating an override file. The set of unproxied subcommands SHALL be fixed and not configurable via environment variable.

When the `DRY_RUN` environment variable is enabled, the system SHALL generate the override file but SHALL NOT invoke `terraform`.

#### Scenario: Unproxied subcommand skips override generation

- **WHEN** a user runs `lstk terraform fmt`
- **THEN** no override file is generated
- **AND** `terraform fmt` is invoked directly

#### Scenario: Dry run generates override but does not run terraform

- **WHEN** `DRY_RUN` is enabled and a user runs `lstk terraform plan`
- **THEN** the override file is generated and left in place for inspection
- **AND** `terraform` is not invoked

### Requirement: Configurable Terraform binary

The system SHALL invoke the binary named by the `TF_CMD` environment variable when set (for example `tofu`), defaulting to `terraform` otherwise.

#### Scenario: Use OpenTofu binary

- **WHEN** `TF_CMD=tofu` is set and the user runs `lstk terraform apply`
- **THEN** the `tofu` binary is invoked instead of `terraform`

### Requirement: Non-interactive streaming output

The command SHALL NOT display a spinner or other progress animation, since `terraform` is a long-running command whose streaming output must remain unobstructed. The command SHALL honor the `--non-interactive` flag by stripping it from the forwarded arguments.

#### Scenario: No spinner is shown

- **WHEN** a user runs `lstk terraform apply` in an interactive terminal
- **THEN** no lstk spinner is rendered around terraform's output

#### Scenario: Non-interactive flag is not forwarded

- **WHEN** a user runs `lstk terraform plan --non-interactive`
- **THEN** the `--non-interactive` flag is consumed by lstk and not passed to the `terraform` binary

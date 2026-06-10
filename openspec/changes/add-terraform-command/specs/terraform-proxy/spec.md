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

- **WHEN** no `terraform` binary (nor the configured `LSTK_TF_CMD`) is found on `PATH`
- **THEN** the command fails with a clear error explaining that Terraform must be installed and available on `PATH`

### Requirement: LocalStack must be running

The command SHALL require a running LocalStack **AWS** emulator and SHALL resolve its endpoint automatically using lstk's container discovery and host resolution, without requiring the user to specify a host or port. `lstk terraform` operates only against the AWS emulator; other emulator types (e.g. Snowflake, Azure) are not supported.

#### Scenario: No running emulator

- **WHEN** a user runs `lstk terraform plan` and no LocalStack AWS emulator is running
- **THEN** the command fails with an error stating LocalStack is not running and suggesting how to start it (`lstk`)
- **AND** the `terraform` binary is not invoked

#### Scenario: A non-AWS emulator is running

- **WHEN** a user runs `lstk terraform plan` while a non-AWS LocalStack emulator (e.g. Snowflake or Azure) is running but the AWS emulator is not
- **THEN** the command fails with an error that specifically states `lstk terraform` requires the AWS emulator and identifies the running emulator type
- **AND** the `terraform` binary is not invoked

#### Scenario: Endpoint resolved from running emulator

- **WHEN** a LocalStack AWS emulator is running
- **THEN** the command resolves the endpoint via the same discovery used by `lstk aws` (container discovery plus host resolution)
- **AND** uses that endpoint as the base for all generated provider endpoints

#### Scenario: Explicit endpoint override

- **WHEN** the `AWS_ENDPOINT_URL` environment variable is set
- **THEN** its host and port take precedence over the auto-resolved endpoint when building the provider override

### Requirement: Provider override file generation

Before invoking `terraform` for any command that provisions or reads infrastructure, the system SHALL generate a Terraform override file (named `localstack_providers_override.tf` by default, configurable via `LSTK_TF_OVERRIDE_FILE_NAME`) that, for each `aws` provider block discovered in the working directory, defines a matching `provider "aws"` block configured to target LocalStack.

Provider-block discovery SHALL recurse into sub-directories of the working directory, so `aws` provider blocks declared in nested modules are also represented. Hidden directories (for example `.terraform`, which holds the downloaded provider/module cache, and `.git`) SHALL be skipped. A `*.tf` file that cannot be parsed SHALL be skipped individually (logged, not fatal) rather than aborting discovery. This recursive scan does not guarantee coverage of every possible layout (e.g. remote modules), but is broader than scanning only the top-level directory.

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

### Requirement: Region and account selection

The command SHALL accept two lstk-specific flags, `--region` and `--account`, that set the deployment region and the target AWS account ID for the generated provider override. Because these are not standard `terraform` flags, the command SHALL parse and remove them (together with their values) from the argument list before forwarding the remaining arguments to the `terraform` binary. Both `--flag value` and `--flag=value` forms SHALL be supported.

These flags SHALL be recognized only in leading position — that is, immediately after `terraform`/`tf` and before the terraform action and any other arguments. Parsing SHALL stop at the first argument that is not one of these flags (or their values); any `--region`/`--account` appearing after that point SHALL be treated as ordinary terraform arguments and forwarded unchanged. The flags SHALL NOT be defined as root/persistent flags, so a flag placed before the `terraform` subcommand (e.g. `lstk --account … terraform`) is not accepted.

The resolved region SHALL be selected with precedence: `--region` flag, then the `AWS_REGION` environment variable, then a default of `us-east-1`. The deprecated `AWS_DEFAULT_REGION` environment variable SHALL NOT be consulted.

The resolved account (provider `access_key`) SHALL be selected with precedence: `--account` flag, then the `AWS_ACCESS_KEY_ID` environment variable, then a default of `test`.

The `--account` flag value SHALL be validated to be exactly 12 digits (`^\d{12}$`). A value supplied via `AWS_ACCESS_KEY_ID` SHALL NOT be validated, but SHALL be passed through an access-key deactivation step: if it begins with the letter `A` (the prefix of real AWS access key ids such as `AKIA…` long-term keys and `ASIA…` temporary session keys), the leading `A` SHALL be replaced with `L` before the value is encoded into the override. This guards against a real AWS credential accidentally present in the environment being written into the generated override file or sent to LocalStack. The validated 12-digit `--account` flag value is used as-is (it cannot begin with `A`).

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

- **WHEN** the user's own `aws` provider block specifies a `region` or `access_key` and `--region`/`--account` (or their env fallbacks) resolve a value
- **THEN** the generated override block's encoded `region`/`access_key` take effect over the user's values

### Requirement: Working directory selection (`-chdir`)

The command SHALL support Terraform's global `-chdir=DIR` option, which selects the directory Terraform operates in. Only the `-chdir=DIR` form (with `=`) SHALL be recognized by lstk; the space-separated form is not a valid Terraform spelling and SHALL be forwarded to `terraform` unchanged.

When `-chdir=DIR` is present, the system SHALL compute an effective working directory by resolving `DIR` against the process working directory (an absolute `DIR` SHALL be used as-is; a relative `DIR` SHALL be joined to the process working directory). All directory-relative work — provider-schema discovery (`terraform providers schema -json`), `aws` provider-block discovery, the location the override file is written to, and its cleanup — SHALL be anchored to this effective working directory rather than the process working directory.

The `-chdir=DIR` token SHALL be kept in the arguments forwarded to `terraform` so that `terraform` itself also switches into `DIR`. lstk SHALL read the value for its own directory resolution without removing it (this differs from `--region`/`--account`, which are consumed and removed).

`-chdir=DIR` SHALL be recognized in leading position alongside `--region`/`--account` (Terraform requires `-chdir` to precede the subcommand). The leading-flag scan SHALL treat `-chdir=DIR` as a recognized leading token and continue scanning past it, so a `--region`/`--account` flag positioned after `-chdir` is still consumed.

Before invoking `terraform`, the system SHALL validate that the effective working directory exists; if it does not, the command SHALL fail with a clear error naming the directory and SHALL NOT invoke `terraform` or generate an override.

For unproxied subcommands (`fmt`, `validate`, `version`, `init`), `-chdir=DIR` requires no special handling beyond being forwarded verbatim — lstk does no directory-relative work for them, and `terraform` performs the directory switch itself.

#### Scenario: Override is generated in the chdir directory

- **WHEN** a user runs `lstk terraform -chdir=infra apply` and `infra/` contains an `aws` provider block
- **THEN** the provider schema is discovered from `infra/`, the override file is written into `infra/`, and `aws` provider blocks are discovered under `infra/`
- **AND** `terraform` is invoked with `-chdir=infra` retained in its arguments
- **AND** the generated override file is removed from `infra/` after the run

#### Scenario: chdir directory does not exist

- **WHEN** a user runs `lstk terraform -chdir=missing apply` and `missing/` does not exist
- **THEN** the command fails with a clear error identifying the missing directory
- **AND** no override file is generated and the `terraform` binary is not invoked

#### Scenario: chdir is forwarded to terraform

- **WHEN** a user runs `lstk terraform -chdir=infra init`
- **THEN** `-chdir=infra` is included in the forwarded arguments so `terraform` operates in `infra/`

#### Scenario: chdir combines with leading region/account flags

- **WHEN** a user runs `lstk terraform -chdir=infra --region us-west-2 apply` (or with the two leading flags in the opposite order)
- **THEN** `--region us-west-2` is consumed and removed, `-chdir=infra` is retained and forwarded, and the effective working directory is `infra/`
- **AND** the generated provider blocks set `region = "us-west-2"`

#### Scenario: Space-separated chdir form is not interpreted by lstk

- **WHEN** a user runs `lstk terraform -chdir infra apply` (no `=`)
- **THEN** lstk does not interpret it as a working-directory change
- **AND** the arguments are forwarded to `terraform`, which reports its own error for the unrecognized form

### Requirement: Dynamic endpoint discovery from provider schema

The set of AWS service endpoint keys written into the `endpoints {}` block SHALL be derived dynamically by querying the installed Terraform AWS provider schema (`terraform providers schema -json`) rather than from a hard-coded service list. The system SHALL read the endpoint attribute keys from the AWS provider's `endpoints` nested block in the schema JSON. Discovery SHALL work for the Terraform AWS provider version 4.0 and higher.

The system SHALL NOT maintain a built-in fallback endpoint-key list. Each endpoint key that is written SHALL appear exactly as named in the provider schema, without case transformation, and SHALL map to the single resolved LocalStack endpoint — except the `s3` key, whose host may carry an `s3.` prefix for virtual-host addressing (see "Provider override file generation"). (Mutually-exclusive aliases are reduced to one member per group, as described below.)

Some endpoint keys reported by the schema are aliases for the same AWS service and are mutually exclusive: setting more than one member of such an alias group in a single `endpoints {}` block makes the provider report an "Invalid Attribute Combination" diagnostic ("Only one of the following attributes should be set …", with a stated intent to become an error in a future provider release). Because the schema JSON does NOT encode this mutual exclusivity, the system SHALL maintain a table of known alias groups and, for each group, write at most one member into the generated block (retaining one and omitting the rest). This table resolves conflicts only; it is not a fallback endpoint-key list — the keys themselves still come from the schema. Since all members of a group address the same service endpoint, retaining any single member SHALL correctly route that service to LocalStack.

#### Scenario: Endpoint keys come from the provider schema

- **WHEN** the override file is generated and the AWS provider is installed
- **THEN** the endpoint keys written into the `endpoints {}` block are the endpoint attribute names reported by `terraform providers schema -json` for the AWS provider, with mutually-exclusive aliases reduced to a single member per group

#### Scenario: Mutually-exclusive endpoint aliases are de-duplicated

- **WHEN** the provider schema reports multiple endpoint keys that are aliases for the same service (for example `lexmodels`, `lexmodelbuilding`, `lexmodelbuildingservice`, and `lex`; or `databrew` and `gluedatabrew`)
- **THEN** the generated `endpoints {}` block contains at most one key from each such alias group
- **AND** the `terraform` invocation does not raise an "Invalid Attribute Combination" diagnostic for those endpoints

#### Scenario: Provider schema unavailable because terraform init has not run

- **WHEN** the AWS provider schema cannot be retrieved because `terraform init` has not installed the provider (the `providers schema` query fails or reports no AWS provider)
- **THEN** the command fails with a generic error instructing the user to run `terraform init` first, phrased for end users and NOT referencing internal details such as the provider schema
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
- **THEN** the command fails with an error rather than overwriting or deleting it
- **AND** the error instructs the user to remove the file or set `LSTK_TF_OVERRIDE_FILE_NAME` to a different name

Because lstk keeps no persistent record of whether it created the override file, an existing file is treated as a conflict regardless of its contents — it is either authored by the user or orphaned by a previous lstk run that was interrupted before cleanup, and in both cases the user is asked to resolve it.

### Requirement: Unproxied and dry-run commands

For Terraform subcommands that do not require provider endpoints (`fmt`, `validate`, `version`, `init`), the system SHALL invoke `terraform` directly without generating an override file and without requiring a running LocalStack emulator. The set of unproxied subcommands SHALL be fixed and not configurable via environment variable.

`init` is included because the override's endpoint keys are discovered from the AWS provider schema, which does not exist until `terraform init` has installed the provider. `init` must therefore pass through to bootstrap the provider for subsequent `plan`/`apply`. `init` does not call AWS service endpoints (S3-backend support is out of scope), so it needs no override.

When the `LSTK_TF_DRY_RUN` environment variable is enabled, the system SHALL generate the override file but SHALL NOT invoke `terraform`.

#### Scenario: Unproxied subcommand skips override generation

- **WHEN** a user runs `lstk terraform fmt`
- **THEN** no override file is generated
- **AND** `terraform fmt` is invoked directly

#### Scenario: init passes through to bootstrap the provider

- **WHEN** a user runs `lstk terraform init` before the AWS provider is installed
- **THEN** no override file is generated and schema discovery is not attempted
- **AND** `terraform init` is invoked directly so it can install the provider
- **AND** the command does not require a running LocalStack emulator

#### Scenario: Dry run generates override but does not run terraform

- **WHEN** `LSTK_TF_DRY_RUN` is enabled and a user runs `lstk terraform plan`
- **THEN** the override file is generated and left in place for inspection
- **AND** `terraform` is not invoked

### Requirement: Configurable Terraform binary

The system SHALL invoke the binary named by the `LSTK_TF_CMD` environment variable when set (for example `tofu`), defaulting to `terraform` otherwise.

#### Scenario: Use OpenTofu binary

- **WHEN** `LSTK_TF_CMD=tofu` is set and the user runs `lstk terraform apply`
- **THEN** the `tofu` binary is invoked instead of `terraform`

### Requirement: Non-interactive streaming output

The command SHALL NOT display a spinner or other progress animation, since `terraform` is a long-running command whose streaming output must remain unobstructed. The command SHALL honor the `--non-interactive` flag by stripping it from the forwarded arguments.

#### Scenario: No spinner is shown

- **WHEN** a user runs `lstk terraform apply` in an interactive terminal
- **THEN** no lstk spinner is rendered around terraform's output

#### Scenario: Non-interactive flag is not forwarded

- **WHEN** a user runs `lstk terraform plan --non-interactive`
- **THEN** the `--non-interactive` flag is consumed by lstk and not passed to the `terraform` binary

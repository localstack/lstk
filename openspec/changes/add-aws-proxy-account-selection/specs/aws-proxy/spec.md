## ADDED Requirements

### Requirement: Credentials for the AWS CLI subprocess

The system SHALL supply LocalStack-compatible credentials to the `aws` subprocess so that `lstk aws` works against a running emulator without the user configuring anything first.

When no `localstack` AWS profile is configured, the system SHALL set `AWS_ACCESS_KEY_ID` in the subprocess environment to the resolved account (see "Account selection"), and SHALL seed `AWS_SECRET_ACCESS_KEY` with `test` and `AWS_DEFAULT_REGION` with `us-east-1` only when those variables are not already set, so an explicit user value continues to win. When a `localstack` profile is configured, credential handling and the seeding of defaults are governed instead by "Selecting the localstack AWS profile".

The system SHALL NOT hand a real-looking AWS access key id to the `aws` subprocess. An ambient `AWS_ACCESS_KEY_ID` beginning with the letter `A` — the prefix of real AWS access key ids such as `AKIA…` long-term keys and `ASIA…` temporary session keys — SHALL have that leading `A` replaced with `L`, rendering it inert. This mirrors the deactivation `lstk terraform` and `lstk sam` already apply. Since a deactivated key is not 12 digits, LocalStack resolves it to the default account `000000000000`, which is the same account a real key would have resolved to.

The system SHALL NOT pass an ambient `AWS_SESSION_TOKEN` to the subprocess. lstk supplies the credentials itself, a token from an unrelated session cannot correspond to them, and the endpoint is always LocalStack. This matches the stripping `lstk terraform` and `lstk sam` already perform.

lstk SHALL NOT require, read, or inject the LocalStack auth token for AWS CLI calls to the emulator; the auth token only activates the emulator container.

#### Scenario: Mock credentials are seeded

- **WHEN** the user runs `lstk aws s3 ls` with no AWS credentials in the environment and no `localstack` profile configured
- **THEN** the `aws` subprocess environment contains `AWS_ACCESS_KEY_ID=test`, `AWS_SECRET_ACCESS_KEY=test`, and `AWS_DEFAULT_REGION=us-east-1`

#### Scenario: An explicit secret or region is preserved

- **WHEN** no `localstack` profile is configured and the environment already sets `AWS_SECRET_ACCESS_KEY` or `AWS_DEFAULT_REGION`
- **THEN** lstk leaves those values unchanged in the subprocess environment

#### Scenario: A real access key is deactivated

- **WHEN** no `localstack` profile is configured and the environment sets `AWS_ACCESS_KEY_ID` to a real-looking key such as `AKIAIOSFODNN7EXAMPLE`
- **THEN** the `aws` subprocess sees `LKIAIOSFODNN7EXAMPLE` instead, and the live key value is never sent to the emulator
- **AND** the resolved account is the default `000000000000`, unchanged from what the real key would have produced

#### Scenario: An ambient session token is dropped

- **WHEN** the environment sets `AWS_SESSION_TOKEN`
- **THEN** the `aws` subprocess does not receive it, on any credentials path

### Requirement: Account selection

The system SHALL accept an lstk-specific `--account <id>` flag on `lstk aws`, mirroring `lstk terraform` and `lstk sam`, because LocalStack derives the AWS account from the access key id it receives. The resolved account SHALL be written to `AWS_ACCESS_KEY_ID` in the `aws` subprocess environment.

The flag SHALL be recognized only in leading position — after the `aws` token and before the AWS service name — and both `--account value` and `--account=value` forms SHALL be supported. Leading-flag parsing is the shared rule stated normatively in the `terraform-proxy` capability's "Region and account selection" requirement: the leading run ends at the service name, not at the first argument lstk does not recognize, so `--account` is recognized in any order relative to the AWS CLI's own global flags. An `--account` appearing after the service name SHALL be forwarded to the AWS CLI unchanged. The flag SHALL NOT be defined as a root or persistent flag; a `--account` placed before the `aws` token SHALL be rejected with an error explaining the required placement, rather than silently dropped during command resolution.

Forwarding a post-service `--account` is not a convenience but a correctness requirement: the AWS CLI defines a real `--account` parameter on several operations — `opensearch` and `es` authorize/revoke-vpc-endpoint-access, `redshift` authorize/revoke-endpoint-access and describe-endpoint-authorization, `events` create/delete-partner-event-source, and `macie2` create-member. Each follows a service *and* an operation, so the shared rule's bound (at most one bare argument absorbed per flag, halting at or before the second consecutive bare argument) guarantees lstk has stopped scanning before reaching them. The system SHALL NOT claim `--account` from anywhere in the argument list, which would silently steal those parameters.

The resolved account SHALL be selected with precedence: the `--account` flag, then the ambient `AWS_ACCESS_KEY_ID` environment variable, then a default of `test` (which LocalStack resolves to account `000000000000`). A `--account` value SHALL be validated to be exactly 12 digits and rejected at the command boundary before the AWS CLI is invoked. An ambient `AWS_ACCESS_KEY_ID` SHALL NOT be validated, but SHALL be subject to the access-key deactivation described in "Credentials for the AWS CLI subprocess".

The system SHALL NOT consume a `--region` flag on `lstk aws` in any position. Unlike `terraform`, `cdk`, and `sam`, the AWS CLI defines its own global `--region`, which SHALL reach it untouched — and which, being command-line tier, correctly outranks a profile's `region` where an environment variable set by lstk would not.

#### Scenario: Explicit account

- **WHEN** the user runs `lstk aws --account 111111111111 s3 ls`
- **THEN** the `aws` subprocess environment contains `AWS_ACCESS_KEY_ID=111111111111`, so the call reads the `111111111111` LocalStack account
- **AND** the forwarded arguments are `s3 ls`, with the `--account` flag and its value removed

#### Scenario: Account from the environment

- **WHEN** the user runs `AWS_ACCESS_KEY_ID=111111111111 lstk aws s3 ls` with no `--account` flag
- **THEN** the `aws` subprocess environment contains `AWS_ACCESS_KEY_ID=111111111111`
- **AND** this holds whether or not a `localstack` AWS profile has been configured

#### Scenario: Flag beats the environment

- **WHEN** `AWS_ACCESS_KEY_ID=111111111111` is set and the user runs `lstk aws --account 222222222222 s3 ls`
- **THEN** the `aws` subprocess environment contains `AWS_ACCESS_KEY_ID=222222222222`

#### Scenario: Invalid account value

- **WHEN** `--account` is given a value that is not exactly 12 digits, such as `lstk aws --account 12345 s3 ls`
- **THEN** lstk fails at the command boundary with an error naming the 12-digit requirement, and does not invoke the AWS CLI

#### Scenario: Missing account value

- **WHEN** `--account` appears in leading position with no following value, such as `lstk aws --account`
- **THEN** lstk fails with an error stating the flag requires a value, and does not invoke the AWS CLI

#### Scenario: Flag before the aws token is rejected

- **WHEN** the user runs `lstk --account 111111111111 aws s3 ls`
- **THEN** lstk fails with an error explaining that `--account` must appear after the `aws` subcommand, and does not invoke the AWS CLI

#### Scenario: Account selection is order-free among the AWS CLI's own flags

- **WHEN** the user runs `lstk aws --region eu-west-1 --account 555555555555 sqs create-queue --queue-name q`
- **THEN** `--account` is consumed, `--region eu-west-1` is forwarded to the AWS CLI unchanged, and the call reads account `555555555555`
- **AND** the result is identical to `lstk aws --account 555555555555 --region eu-west-1 sqs create-queue --queue-name q`

#### Scenario: A non-leading account flag belongs to the AWS CLI

- **WHEN** the user runs `lstk aws organizations describe-account --account-id 111111111111`, or any command where `--account` follows the AWS service name
- **THEN** lstk forwards the argument to the AWS CLI unchanged and does not interpret it as account selection

#### Scenario: A genuine AWS account parameter is never claimed

- **WHEN** the user runs `lstk aws events create-partner-event-source --name x --account 123456789012`, with or without AWS CLI global flags before the service name
- **THEN** lstk forwards `--account 123456789012` to the AWS CLI as the operation's own parameter
- **AND** lstk's own account resolution falls back to the ambient value or the default, unaffected

#### Scenario: The region flag is never consumed

- **WHEN** the user runs `lstk aws --region us-west-2 s3 ls`
- **THEN** lstk forwards `--region us-west-2 s3 ls` to the AWS CLI unchanged, letting the AWS CLI's own global `--region` apply

#### Scenario: Account selection with help

- **WHEN** the user runs `lstk aws --account 111111111111 help`, or any leading-flag form combined with a help request
- **THEN** the `--account` flag is consumed before the help short-circuit, and the AWS CLI's help output is produced without contacting an emulator

#### Scenario: Shell completion ignores the leading flag

- **WHEN** the user requests completion for `lstk aws --account 111111111111 s3 <TAB>`
- **THEN** lstk removes the leading `--account` flag and its value before delegating to the AWS CLI's completer, so the candidates match those for `lstk aws s3 <TAB>`
- **AND** an incomplete leading flag (such as `lstk aws --account <TAB>`) produces no candidates rather than malformed ones

### Requirement: Selecting the localstack AWS profile

When a `localstack` AWS profile is configured, the system SHALL select it by setting `AWS_PROFILE=localstack` in the `aws` subprocess environment, and SHALL NOT pass `--profile` on the command line.

The two forms select the same profile but resolve credentials differently: an explicitly named profile on the command line removes the environment credential provider from the AWS CLI's resolver chain, while the environment form leaves it in place. Selecting the profile through the environment is therefore what allows an account to be expressed *and* the profile's other settings — `region`, `output`, `s3` addressing style, retry configuration, and anything else the user has put there — to continue applying. The profile is never dropped in order to select an account.

Whenever the profile is in use, the system SHALL treat the credential triple as all-or-nothing:

- When an account has been selected, it SHALL set both `AWS_ACCESS_KEY_ID` (to the resolved account) and `AWS_SECRET_ACCESS_KEY`, so the environment supplies a complete credential pair that outranks the profile's.
- When no account has been selected, it SHALL remove `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` from the subprocess environment, so the profile is the sole credentials source.

The system SHALL NOT leave a partial pair, because the AWS CLI fails outright on an access key id without a secret once the environment provider is in the chain. Removing the pair when no account is selected is likewise required rather than tidy: it prevents ambient credentials from beating the profile in cases where the command-line form shielded it, including a 12-digit `aws_access_key_id` written directly into the `[localstack]` credentials section, which is a legitimate way to pin an account and which lstk already honors.

An account is selected when either a valid `--account` flag is supplied, or the ambient `AWS_ACCESS_KEY_ID` is itself exactly 12 digits — the documented way to address a specific LocalStack account. An ambient value of any other shape SHALL NOT count as a selection, so a stray real credential in the user's shell does not displace the profile.

While the profile is in use, the system SHALL NOT seed `AWS_DEFAULT_REGION` or any other configuration default into the subprocess environment. Environment variables outrank config-file values, so seeding a default region would override the profile's `region` and reintroduce the silent misdirection this requirement exists to prevent. lstk supplies defaults only when there is no profile to supply them.

The system SHALL continue to advise the user to run `lstk setup aws` when no `localstack` profile exists, based on the profile's presence and not on whether it supplied credentials for the current invocation.

#### Scenario: Profile supplies credentials when no account is selected

- **WHEN** a `localstack` profile exists and the user runs `lstk aws s3 ls` with no `--account` and no ambient `AWS_ACCESS_KEY_ID`
- **THEN** the subprocess environment sets `AWS_PROFILE=localstack` and carries no `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, or `AWS_SESSION_TOKEN`
- **AND** the credentials used are the profile's, as they are today

#### Scenario: Explicit account applies without losing the profile

- **WHEN** a `localstack` profile sets `region = eu-west-1` and the user runs `lstk aws --account 111111111111 s3 ls`
- **THEN** the subprocess environment sets `AWS_PROFILE=localstack`, `AWS_ACCESS_KEY_ID=111111111111`, and a secret, and no `--profile` argument is passed
- **AND** the call reads account `111111111111` in region `eu-west-1` — the account from the environment, the region from the profile

#### Scenario: A 12-digit environment value applies without losing the profile

- **WHEN** a `localstack` profile exists and the user runs `AWS_ACCESS_KEY_ID=111111111111 lstk aws s3 ls`
- **THEN** the subprocess sees `AWS_ACCESS_KEY_ID=111111111111` alongside `AWS_PROFILE=localstack`, and the result is the same as running the identical command with no profile configured except that the profile's other settings still apply

#### Scenario: Non-credential profile settings always apply

- **WHEN** a `localstack` profile sets `output`, `max_attempts`, or an `s3` addressing style, and any `lstk aws` command runs
- **THEN** those settings take effect, whether or not an account was selected

#### Scenario: No default region is seeded over the profile

- **WHEN** a `localstack` profile sets `region = eu-west-1` and an account is selected
- **THEN** lstk does not set `AWS_DEFAULT_REGION` in the subprocess environment, so the profile's region is not overridden

#### Scenario: A partial ambient credential is never forwarded

- **WHEN** a `localstack` profile exists and the environment sets `AWS_ACCESS_KEY_ID` without `AWS_SECRET_ACCESS_KEY`
- **THEN** lstk either supplies both (when the value selects an account) or removes both (when it does not), and the AWS CLI does not fail with a partial-credentials error

#### Scenario: A real key in the environment does not displace the profile

- **WHEN** a `localstack` profile exists and the environment sets `AWS_ACCESS_KEY_ID` to a real-looking key such as `AKIAIOSFODNN7EXAMPLE`
- **THEN** no account is selected, the ambient credentials are removed from the subprocess environment, and the profile supplies credentials
- **AND** the live key value is not sent to the emulator

#### Scenario: An account pinned in the profile itself is honored

- **WHEN** the `[localstack]` credentials section sets `aws_access_key_id = 111111111111` and the user runs `lstk aws s3 ls` with no `--account` and no ambient credentials
- **THEN** the call reads account `111111111111`, because lstk removes the ambient credential variables rather than seeding `test` over the profile

#### Scenario: A user-supplied profile flag wins and disables account selection

- **WHEN** the user runs `lstk aws --account 111111111111 --profile mine s3 ls`
- **THEN** the AWS CLI resolves credentials from the `mine` profile and the selected account does not apply, because an explicitly named profile on the command line removes the environment credential provider

#### Scenario: Setup advice tracks the profile, not the credentials source

- **WHEN** no `localstack` profile exists
- **THEN** lstk notes that the user can run `lstk setup aws`, whether or not an account was selected for this invocation

## MODIFIED Requirements

### Requirement: Region selection
The system SHALL accept the lstk-specific `--region` flag in leading position (before the SAM subcommand) and encode it into the subprocess environment, with the same parsing and precedence as `lstk terraform` and `lstk cdk`. Because SAM honors `AWS_DEFAULT_REGION` (and not `AWS_REGION`), lstk SHALL write the resolved region to both `AWS_REGION` and `AWS_DEFAULT_REGION`.

The environment alone is not sufficient to make the flag effective. SAM injects `samconfig.toml` values as though they had been supplied on the command line, so a `region` key there outranks both environment variables. When — and only when — the user named the region with the `--region` flag, the system SHALL additionally pass `--region <region>` to `sam` on its command line, which is what outranks `samconfig.toml`.

This SHALL apply only to the AWS-contacting subcommands (the complement of the offline set): `init` and `docs` reject `--region` outright, and an offline subcommand contacts nothing that needs a region. If the forwarded arguments already contain a `--region`, the system SHALL leave them unchanged — that flag is the user addressing `sam` directly, and appending another would silently outrank it.

An ambient `AWS_REGION` and the `us-east-1` default SHALL NOT cause the flag to be passed. Both are still used for the environment, but neither is a request to override a region the project configured in `samconfig.toml`: `AWS_REGION` is commonly exported globally for real-AWS work, and the default would override every such project.

#### Scenario: Region precedence
- **WHEN** `--region` is omitted
- **THEN** lstk resolves the region from `AWS_REGION`, falling back to `us-east-1`

#### Scenario: Region reaches SAM via AWS_DEFAULT_REGION
- **WHEN** lstk runs a SAM command with a resolved region
- **THEN** the subprocess environment sets both `AWS_REGION` and `AWS_DEFAULT_REGION` to that region, so SAM (which reads `AWS_DEFAULT_REGION`) uses it

#### Scenario: A selected region outranks samconfig.toml
- **WHEN** the project's `samconfig.toml` sets `region = "us-east-1"` and the user runs `lstk sam --region ap-northeast-1 deploy`
- **THEN** lstk passes `--region ap-northeast-1` to `sam` on its command line
- **AND** the deployment targets `ap-northeast-1`, not the region named in `samconfig.toml`

#### Scenario: Without the flag, samconfig.toml still decides
- **WHEN** the project's `samconfig.toml` sets a region and the user runs `lstk sam deploy` with no `--region`
- **THEN** lstk passes no `--region` to `sam`, and the region in `samconfig.toml` applies as it did before

#### Scenario: An ambient region is not a selection
- **WHEN** `AWS_REGION` is set in the environment and `--region` is omitted
- **THEN** lstk uses it for the subprocess environment but passes no `--region` to `sam`

#### Scenario: Offline subcommands never receive the flag
- **WHEN** `lstk sam --region ap-northeast-1 build` runs, or any other offline subcommand
- **THEN** lstk forwards the subcommand without `--region`, since `init` and `docs` reject the flag and offline subcommands contact nothing

#### Scenario: A user-supplied region flag is left alone
- **WHEN** the user runs `lstk sam --region ap-northeast-1 deploy --region us-west-1`
- **THEN** lstk forwards `deploy --region us-west-1` unchanged and does not append a second `--region`

#### Scenario: Flags only in leading position
- **WHEN** `--region` appears after the SAM subcommand (e.g. `lstk sam deploy --region us-west-2`)
- **THEN** lstk forwards it to `sam` unchanged rather than consuming it

#### Scenario: Reject a leading flag before the subcommand at the lstk boundary
- **WHEN** `--region` or `--account` appears before the `sam` subcommand on the lstk command line (e.g. `lstk --region us-west-2 sam deploy`)
- **THEN** lstk fails with an error explaining the flag must appear after the `sam` subcommand, and does not invoke `sam`

# Spec Delta

## MODIFIED Requirements

### Requirement: Running emulators are provided as a JSON array

The `LSTK_EXT_CONTEXT` object SHALL include an `emulators` array, with one entry per LocalStack emulator currently running, so an extension can work against every running emulator rather than a single one. Each entry SHALL carry the emulator `type` (e.g. `aws`, `snowflake`, `azure`), the `endpoint` (a full URL resolved with the same discovery and host resolution used by built-in commands), and the `port`. When no emulator is running, `emulators` SHALL be an empty array (`[]`), not omitted, so an extension always decodes a list.

The `endpoint` SHALL be the URL at which that emulator type is served, built from the resolved `host:port`:

- `aws`: `http://<host>:<port>`
- `snowflake`: `http://snowflake.<host>:<port>`, the same URL lstk prints as the Snowflake endpoint after starting the emulator
- `azure`: `https://azure.<host>:<port>`, the same URL `lstk az` and `lstk setup azure` target

When the resolved host is an IP address (for example because the `localhost.localstack.cloud` DNS check failed, or `LOCALSTACK_HOST` names an IP), lstk SHALL NOT add a subdomain, since a subdomain of an IP address is not a valid host. The `endpoint` SHALL then be `<scheme>://<host>:<port>`, keeping the type's scheme. The extension is still executed.

#### Scenario: Single emulator provided when one is running

- **WHEN** an AWS emulator is running and lstk invokes an extension
- **THEN** `LSTK_EXT_CONTEXT.emulators` contains one entry with `type` `aws` and `endpoint` set to the resolved emulator URL

#### Scenario: Multiple emulators provided when several are running

- **WHEN** an AWS emulator and a Snowflake emulator are both running and lstk invokes an extension
- **THEN** `LSTK_EXT_CONTEXT.emulators` contains an entry for each, distinguished by `type`

#### Scenario: Empty array when no emulator running

- **WHEN** no emulator is running and lstk invokes an extension
- **THEN** `LSTK_EXT_CONTEXT.emulators` is an empty array
- **AND** the extension is still executed

#### Scenario: AWS endpoint has no subdomain

- **WHEN** an AWS emulator is running on host port 4566, the resolved host is `localhost.localstack.cloud`, and lstk invokes an extension
- **THEN** the `aws` entry's `endpoint` is `http://localhost.localstack.cloud:4566`

#### Scenario: Snowflake endpoint carries the snowflake subdomain

- **WHEN** a Snowflake emulator is running on host port 4566, the resolved host is `localhost.localstack.cloud`, and lstk invokes an extension
- **THEN** the `snowflake` entry's `endpoint` is `http://snowflake.localhost.localstack.cloud:4566`

#### Scenario: Azure endpoint carries the azure subdomain and https

- **WHEN** an Azure emulator is running on host port 4566, the resolved host is `localhost.localstack.cloud`, and lstk invokes an extension
- **THEN** the `azure` entry's `endpoint` is `https://azure.localhost.localstack.cloud:4566`

#### Scenario: No subdomain is added to an IP host

- **WHEN** a Snowflake or Azure emulator is running on host port 4566, the resolved host is `127.0.0.1`, and lstk invokes an extension
- **THEN** the `snowflake` entry's `endpoint` is `http://127.0.0.1:4566`
- **AND** the `azure` entry's `endpoint` is `https://127.0.0.1:4566`
- **AND** the extension is still executed

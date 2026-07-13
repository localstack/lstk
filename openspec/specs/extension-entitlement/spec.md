# extension-entitlement Specification

## Purpose

Establish that authorization for an extension is the extension's own responsibility, and that lstk's role is limited to conveying the user's auth token so the extension can decide for itself whether the user is entitled. A richer lstk-side mechanism (lstk obtaining a LocalStack-signed entitlement description and passing it for offline verification) is deliberately deferred to future work; this capability records that intent and the security rationale behind it.

## Requirements

### Requirement: lstk conveys the auth token; the extension authorizes

lstk SHALL make the resolved user auth token available to the extension via the runtime context (`LSTK_EXT_AUTH_TOKEN`) and SHALL NOT itself perform any entitlement or license decision for the extension. An extension that wishes to restrict its use SHALL perform its own authorization check — for example, by calling the LocalStack platform with the conveyed token — and SHALL refuse to perform protected work when that check does not pass.

#### Scenario: Token conveyed for the extension to authorize

- **WHEN** a user with a resolved auth token invokes an extension
- **THEN** lstk passes the token to the extension via `LSTK_EXT_AUTH_TOKEN`
- **AND** lstk invokes the extension without making any entitlement decision of its own

#### Scenario: Extension enforces its own authorization

- **WHEN** an extension that gates on entitlement determines the user is not entitled
- **THEN** the extension refuses to perform its protected work
- **AND** this decision is made by the extension, not by lstk

#### Scenario: Unauthenticated invocation still dispatches

- **WHEN** no auth token is available and a user invokes an extension
- **THEN** lstk still resolves and executes the extension (omitting `LSTK_EXT_AUTH_TOKEN`)
- **AND** any requirement for authentication is enforced by the extension itself

### Requirement: Security rests on the extension, not on lstk

Because lstk is open source and can be rebuilt with any check removed, no authorization guarantee SHALL depend on lstk behaving honestly. An extension that needs durable protection SHALL anchor its enforcement in something a modified lstk cannot forge — a server-side check against the LocalStack platform using the conveyed token, and/or verification it performs itself — rather than relying on lstk to gate invocation.

#### Scenario: Modified lstk cannot bypass extension authorization

- **WHEN** a rebuilt lstk skips conveying the token or alters its behavior
- **THEN** an extension that authorizes server-side (or otherwise verifies independently) still refuses unauthorized work
- **AND** the absence of an lstk-side gate does not weaken the extension's protection

### Requirement: Signed-entitlement mechanism is deferred

This change SHALL NOT implement lstk-side entitlement verification, signed grant/entitlement-description issuance, or offline grant verification. These remain future work. lstk SHALL NOT set `LSTK_EXT_GRANT` or `LSTK_EXT_PUBLIC_KEY`; if a future change introduces a LocalStack-signed entitlement description, it will be added as an additive extension to the runtime-context contract under a documented version bump.

#### Scenario: No grant or public key conveyed

- **WHEN** lstk invokes any extension
- **THEN** `LSTK_EXT_GRANT` is not set
- **AND** `LSTK_EXT_PUBLIC_KEY` is not set

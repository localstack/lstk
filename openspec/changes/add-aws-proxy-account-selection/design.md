## Context

Three of lstk's four AWS-family proxies already agree on how a LocalStack account is selected: `--account <12 digits>` in leading position, falling back to the ambient `AWS_ACCESS_KEY_ID`, falling back to `test`. `lstk terraform` bakes the result into the generated override file's `access_key`; `lstk sam` writes it into the subprocess `AWS_ACCESS_KEY_ID`; `lstk cdk` rejects the flag because CDK's STS-round-trip account resolution did not track it. The shared parsing and precedence live at the command boundary in `cmd/iac.go`.

`lstk aws` was never brought into that convention. Extending it there raises questions the other three did not have to answer, because none of them wraps a tool that owns the same flag namespace, and none of them has a credentials source of its own — the `localstack` AWS profile — that can outrank the environment.

## Decisions

### Decision 1: Select the `localstack` profile through `AWS_PROFILE`, not `--profile`

When a `localstack` profile exists, lstk selects it by setting `AWS_PROFILE=localstack` in the subprocess environment instead of passing `--profile localstack` on the command line, as it does today. The profile is never dropped.

**Rationale**: the two spellings select the same profile but resolve credentials differently. botocore removes the environment credential provider from the resolver chain only when the profile arrives as an explicit *instance* variable — which `--profile` sets and `AWS_PROFILE` does not. The environment form therefore lets credentials come from the environment while every non-credential setting still comes from the profile:

```
   --profile localstack (today)            AWS_PROFILE=localstack

 ✗ EnvProvider  ── removed               ✓ EnvProvider  ◀── account lands here
 ✓ SharedCredentialsProvider ◀── wins    ✓ SharedCredentialsProvider (fallback)
 ✓ [profile localstack] config           ✓ [profile localstack] config

 → profile wins credentials;             → environment wins credentials;
   an account cannot be expressed          profile still supplies everything else
```

Measured against `aws-cli/2.35.15` with a profile carrying `region = eu-west-1`, `output = table`, `max_attempts = 7`, and `s3.addressing_style = path`:

| | access_key | region | output / max_attempts / s3 |
|---|---|---|---|
| `--profile` + credentials in env | `test` (profile) | eu-west-1 | from profile |
| `AWS_PROFILE` + credentials in env | **account (env)** | eu-west-1 | from profile |
| `AWS_PROFILE`, no credentials in env | `test` (profile) | eu-west-1 | from profile |

This is documented botocore behavior, not an accident of implementation: the source comments the exact combination of `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` + `AWS_PROFILE` and states that the explicit credentials take precedence.

**Alternatives considered**:

*Keep `--profile` and drop it when an account is selected* (the change's original design). Rejected. `[profile localstack]` is an ordinary AWS config section, and the settings a LocalStack user adds to it are not cosmetic — a `region` naming a different LocalStack partition, `s3.addressing_style = path` for a loopback endpoint, `max_attempts`, `output`. Dropping the profile discards all of them silently, reverting region to `us-east-1` and producing a plausible-looking empty result from the wrong partition. That is the same species of bug this change exists to fix — a user-set value ignored without a word — merely relocated from the account to the region. Trading one silent wrong answer for another is not a fix, and `AWS_PROFILE` means no trade is required.

*Generate a temporary config file that copies `[profile localstack]` with the account substituted, pointed at by `AWS_CONFIG_FILE`.* Rejected: `AWS_CONFIG_FILE` replaces rather than layers, so any other profile the user's command references would disappear; it requires parsing and reproducing arbitrary AWS config syntax; and it earns nothing over the environment form.

*Accept that `--account` does not work after `lstk setup aws`, and document it.* Rejected: that is the status quo for the environment variable, and the status quo is the defect. A flag that is accepted, validated, and then silently ignored depending on whether the user once ran an unrelated setup command is worse than no flag.

**Consequence to document**: a user-supplied `--profile` on the command line re-disables the environment credential provider, so `lstk aws --account 111111111111 --profile mine s3 ls` uses `mine`'s credentials and ignores the account. This is the correct outcome — an explicitly named profile should win — but it must be a stated scenario rather than a surprise.

### Decision 2: When the profile is in play, lstk owns the whole credential triple and seeds nothing else

Two rules follow from Decision 1, and both are silent-failure traps if missed.

**Credentials are all-or-nothing.** Whenever a `localstack` profile is in use, lstk either sets `AWS_ACCESS_KEY_ID` *and* `AWS_SECRET_ACCESS_KEY` (an account was selected), or removes both — along with `AWS_SESSION_TOKEN` — so the profile is the sole source. lstk never leaves a partial pair.

*Why the setting half*: `EnvProvider` raises `PartialCredentialsError` when an access key id is present without a secret. `--profile` shields that today by removing the provider entirely; `AWS_PROFILE` does not, so an ambient lone `AWS_ACCESS_KEY_ID` would turn a working command into a hard error (`aws: [ERROR]: Partial credentials found in env, missing: AWS_SECRET_ACCESS_KEY`).

*Why the removing half*: it is required for correctness, not merely tidiness. With the environment provider back in the chain, ambient credentials would start beating the profile in cases where today they cannot — including the case where the user has written a 12-digit `aws_access_key_id` directly into `[localstack]`, which is a legitimate third way to select an account that lstk already honors. Removing the triple preserves it.

Stripping `AWS_SESSION_TOKEN` also brings `lstk aws` in line with `lstk terraform` and `lstk sam`, which already strip it; a stale token attached to lstk's own credentials serves nothing, and the endpoint is always LocalStack.

**Never seed `AWS_DEFAULT_REGION` (or any other setting) when the profile is in use.** Environment variables outrank config-file values, so seeding `AWS_DEFAULT_REGION=us-east-1` alongside `AWS_PROFILE=localstack` would override the profile's `region` and reintroduce exactly the failure Decision 1 avoids:

```
AWS_PROFILE=localstack (region=eu-west-1) + AWS_DEFAULT_REGION=us-east-1  →  us-east-1  ✗
AWS_PROFILE=localstack (region=eu-west-1)                                 →  eu-west-1  ✓
```

The `test` / `us-east-1` seeding is therefore conditional on there being no profile, rather than unconditional as the code is written today. lstk supplies defaults only where nothing else can.

**Non-goal**: reading the profile to discover whether it *has* a `region` and seeding only if it does not. A profile without a region already fails the same way today under `--profile`, lstk always writes one into the profiles it creates, and parsing the profile to partially fill it in trades a clear failure for a fuzzy one.

### Decision 3: `lstk aws` claims `--account` only, never `--region`

`lstk terraform`, `lstk cdk`, and `lstk sam` consume both `--region` and `--account` in leading position. `lstk aws` consumes only `--account`; `--region` is forwarded to the AWS CLI verbatim in every position, including leading.

**Rationale**: the AWS CLI has its own global `--region` and users already know it. Intercepting it would mean re-emitting it, or translating it into `AWS_DEFAULT_REGION`, to arrive at the same place the AWS CLI would have reached on its own — mechanism in exchange for nothing. Under Decision 2 it would be actively harmful: translating `--region` into an environment variable would override the profile's region, which is the trap that decision exists to avoid, whereas the AWS CLI's own `--region` is command-line tier and correctly outranks the profile. It would also make the leading and non-leading spellings of one flag behave differently for no reason the user can see. The three IaC proxies intercept it because their wrapped tools have no equivalent flag, not because interception is the convention.

`--account` is safe to claim in leading position because the AWS CLI has no *global* flag by that name. It does have real `--account` **parameters**: a sweep of all 425 botocore service definitions found ten operations exposing one — `opensearch` and `es` authorize/revoke-vpc-endpoint-access, `redshift` authorize/revoke-endpoint-access and describe-endpoint-authorization, `events` create/delete-partner-event-source, and `macie2` create-member. Every one of them follows a service *and* an operation, so leading-position-only parsing forwards them untouched. This is why lstk must not simply pull `--account` out of anywhere in the argument list, however tempting that is as a way to make placement order-free.

**Consequence**: the leading-flag parser becomes parameterized rather than fixed, and moves next to the other shared proxy-argument helpers, since it is no longer specific to the IaC commands.

### Decision 4: Neutralize a real-looking ambient access key wherever one is passed through

An ambient `AWS_ACCESS_KEY_ID` beginning with `A` (`AKIA…` long-term, `ASIA…` temporary) has its leading character rewritten to `L` before it reaches the `aws` subprocess — the same `DeactivateAccessKey` safeguard terraform and sam already apply, mirroring tflocal.

**Rationale**: no functional change. LocalStack derives `000000000000` from a real key and from a deactivated one alike, because neither is 12 digits. What changes is that a live credential stops being transmitted to the emulator, where it could be captured in logs or analytics by accident. `lstk aws` is currently the only AWS-family proxy that transmits it, and it is the one most likely to be run from a shell that also has real AWS credentials exported.

This applies on the no-profile path, where the resolved account is written through to the subprocess. On the profile path the stronger rule from Decision 2 takes over — the ambient value is removed outright when the profile supplies credentials, and replaced by the selected account when it does not — so a live key never survives either way.

**Alternatives considered**: stripping the variable on every path rather than deactivating it (rejected for the no-profile path: it diverges from terraform and sam for no gain, and an unset variable is harder to recognize when debugging than a visibly mangled one). Refusing to run when a real key is detected (rejected: the endpoint is always LocalStack, so there is nothing to protect the user from — the key cannot reach real AWS).

### Decision 5: The 12-digit rule moves into `internal/validate`

`validate.AWSAccountID` replaces the inline `regexp.MustCompile(\`^\d{12}$\`)` in `cmd/iac.go`. The user-facing error message is unchanged.

**Rationale**: the project's validation rule requires user-supplied values to be validated through `internal/validate` rather than local regexps, precisely because parallel copies of one rule drift — the pod-name rules forked this way before being re-unified. A single inline regexp with one caller was a borderline case; `lstk aws` becoming a second caller is not. Routing it through `internal/validate` also attaches rule codes (`empty` / `range` / `format`) to the failure, which is what makes the reason machine-classifiable later.

Keeping the message byte-identical is deliberate: the terraform and sam specs already have scenarios asserting that an invalid `--account` is rejected at the boundary, and this change should not perturb them.

### Decision 6: The leading run ends at the wrapped tool's action, not at the first unrecognized argument

The leading-flag parser forwards an argument belonging to the wrapped tool and keeps scanning, rather than stopping at it. lstk's flags are therefore recognized in any order relative to the tool's own.

**Rationale**: the old rule produced a distinction no user could see. `lstk aws --account 5… --region eu-west-1 sqs …` worked; `lstk aws --region eu-west-1 --account 5… sqs …` leaked `--account` to the AWS CLI, which rejects it as an unknown option. Both put both flags before the service name, which is all the help text asks for. `lstk aws` is worst hit because Decision 3 leaves `--region` — the flag most likely to be typed first — deliberately unclaimed, but the defect is in the shared parser: `lstk sam --debug --account 5… build` failed the same way, silently falling back to the default account.

**How the action is located without a per-tool flag table**: a bare argument following a flag that may still take a value (one containing no `=`) is presumed to be that value. At most one bare argument is absorbed per flag, so scanning always halts at or before the second consecutive bare argument.

That bound is load-bearing, not incidental: it is what keeps the ten genuine `--account` parameters from Decision 3 safe, since each follows a service *and* an operation. It also means the parser only ever *removes* lstk's own flags — every other argument is forwarded in its original order — so a wrong guess about whether a bare argument is a value cannot change what the wrapped tool receives.

**Alternatives considered**: a per-tool table of global flags and their arity (rejected: the AWS CLI's ~18 globals would need to stay in sync, and a newly added one would reintroduce the failure). Consuming `--account` from anywhere in the argument list (rejected on evidence — it steals the ten real parameters). Detecting the leaked flag and failing with "put `--account` first" (rejected: it keeps an arbitrary restriction and merely explains it).

**Known limit**: a tool flag whose *value* is literally the string `--account` — `lstk aws --query --account s3 ls` — is read as lstk's flag. No realistic invocation has that shape, and it is documented rather than engineered around.

### Decision 7: `lstk sam --region` is also passed on sam's command line

When the user names a region with `--region`, lstk passes `--region <region>` to `sam` as well as setting `AWS_REGION`/`AWS_DEFAULT_REGION`, for the AWS-contacting subcommands only.

**Rationale**: the environment is not sufficient. SAM injects `samconfig.toml` values as though they had been typed on the command line, so a `region` key there outranks both environment variables. Measured against SAM 1.163.0 by reading the SigV4 credential scope off the wire: with `samconfig.toml` naming `us-east-1` and both environment variables naming `ap-northeast-1`, SAM signs for `us-east-1`; remove the `samconfig.toml` key and it signs for `ap-northeast-1`; pass `--region eu-west-3` on the command line and it signs for `eu-west-3`. The account never suffered this because `samconfig.toml` has no account concept — which is exactly why the bug presented as "account right, region wrong".

**Only when explicitly named**: an ambient `AWS_REGION` or the `us-east-1` default does not trigger it. Forwarding the default would override the region of every project that configured one in `samconfig.toml`, and `AWS_REGION` is commonly exported globally for real-AWS work. This is the same explicit-versus-inherited distinction `resolveAccountSelection` draws for accounts, and it required splitting `resolveRegion` into a variant that reports how the region was chosen.

**Scoped to AWS-contacting subcommands**: `init` and `docs` reject `--region` outright. Determined by executing each subcommand rather than parsing its help output, which gave false negatives for `list resources` and `remote invoke`. An existing `--region` in the forwarded arguments is left alone — that is the user addressing sam directly.

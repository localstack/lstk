## MODIFIED Requirements

### Requirement: Error codes are drawn from a documented enumeration

The enumeration gains one code. Every existing code, its retryability and its category are unchanged; only the row below is added, in the `USAGE` category:

| Code | Meaning | Retryable | Category |
|---|---|---|---|
| `UPDATE_EXTERNALLY_MANAGED` | Another package manager (mise, asdf, Nix, Scoop, Chocolatey) owns the lstk binary, so lstk will not replace it | No | `USAGE` |

The code is added rather than folded into `INTERNAL_ERROR` or `USAGE_ERROR` because the condition is a deliberate, permanent and actionable refusal, not an unclassified failure and not a flag-parsing mistake: a caller can branch on it to route the user to the owning package manager. It is non-retryable — the identical invocation will always be refused — and `USAGE`, because the invocation itself has to change.

#### Scenario: A manager-owned install reports the refusal code

- **WHEN** `lstk update --json` runs from a binary another package manager owns
- **THEN** `error.code` is `"UPDATE_EXTERNALLY_MANAGED"`
- **AND** `error.category` is `"USAGE"` and `error.retryable` is `false`

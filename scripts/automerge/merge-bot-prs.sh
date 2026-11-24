#!/usr/bin/env bash
# Merges open Dependabot and weekly Go toolchain PRs once their required checks
# pass, without a code-owner review. Run by dependency-automerge.yml with a
# GitHub App token: the app may bypass the review ruleset for pull requests,
# but not the required-checks ruleset, so GitHub still refuses a red merge.
#
# Usage: GH_TOKEN=... GH_REPO=owner/repo [DRY_RUN=1] merge-bot-prs.sh
set -euo pipefail

: "${GH_REPO:?set GH_REPO to owner/repo}"
export GH_REPO
dry_run="${DRY_RUN:-0}"
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"

report() {
  echo "$1"
  echo "- $1" >>"$summary"
}

# Prints the major version of a string like v1.31.1, or nothing.
major() {
  if [[ "$1" =~ ^v?([0-9]+)\. ]]; then
    echo "${BASH_REMATCH[1]}"
  fi
}

# Prints why a Dependabot commit is not a minor/patch update, or nothing.
not_minor_or_patch() {
  local message="$1" names types
  names=$(grep -cE '^- dependency-name: ' <<<"$message" || true)
  types=$(grep -E '^ +update-type: ' <<<"$message" || true)

  if [[ "$names" -eq 0 ]]; then
    echo "no Dependabot metadata in the head commit"
  elif grep -q 'semver-major' <<<"$types"; then
    echo "semver-major update, needs a human"
  elif [[ -n "$types" && $(grep -c . <<<"$types") -eq "$names" ]]; then
    return
  elif [[ "$names" -eq 1 && -z "$types" ]]; then
    # pre-commit and SHA-pinned actions omit update-type, so compare the
    # majors in the body's "Bumps [x](url) from X to Y." line instead (the
    # subject gets truncated for long names).
    local bumps from="" to=""
    bumps=$(grep -m1 '^Bumps ' <<<"$message" || true)
    if [[ "$bumps" =~ \ from\ ([^ ]+)\ to\ ([^ ]+) ]]; then
      local old="${BASH_REMATCH[1]}" new="${BASH_REMATCH[2]}"
      from=$(major "$old")
      to=$(major "$new")
    fi
    if [[ -z "$from" || "$from" != "$to" ]]; then
      echo "can't tell it is a minor or patch update"
    fi
  else
    echo "can't tell it is a minor or patch update"
  fi
}

# Prints why a bot PR must not be merged yet, or nothing when it may be.
# Every API failure prints a reason, so an error never lets a PR through.
skip_reason() {
  local number="$1" bot="$2" sha="$3"

  # --json exits 0 even when checks fail, so the buckets decide.
  local checks
  checks=$(gh pr checks "$number" --required --json name,bucket 2>&1) || true
  if ! jq -e 'type == "array" and length > 0' <<<"$checks" >/dev/null 2>&1; then
    echo "no required checks reported (${checks%%$'\n'*})"
    return
  fi
  local not_green
  not_green=$(jq -r '[.[] | select(.bucket != "pass" and .bucket != "skipping")
    | "\(.name) (\(.bucket))"] | join(", ")' <<<"$checks")
  if [[ -n "$not_green" ]]; then
    echo "checks not green: $not_green"
    return
  fi

  # The CI run's actor is whoever pushed the head commit.
  local pushers
  if ! pushers=$(gh api "repos/$GH_REPO/actions/runs?head_sha=$sha&event=pull_request" \
    --jq '[.workflow_runs[] | select(.name == "LSTK CI") | .actor.login] | unique | join(",")'); then
    echo "could not read CI runs"
    return
  fi
  if [[ "$pushers" != "$bot" ]]; then
    echo "head not pushed by $bot (LSTK CI actor: ${pushers:-none})"
    return
  fi

  # Dependabot signs its commits; localstack-bot's are unsigned.
  local verify=false
  [[ "$bot" == "dependabot[bot]" ]] && verify=true
  local foreign
  if ! foreign=$(gh api --paginate "repos/$GH_REPO/pulls/$number/commits" |
    jq -r --arg bot "$bot" --argjson verify "$verify" \
      '.[] | select(.author.login != $bot or ($verify and .commit.verification.verified != true)) | .sha[0:7]'); then
    echo "could not read commits"
    return
  fi
  if [[ -n "$foreign" ]]; then
    echo "commits not authored by $bot: $(tr '\n' ' ' <<<"$foreign")"
    return
  fi

  if [[ "$bot" == "dependabot[bot]" ]]; then
    local message
    if ! message=$(gh api "repos/$GH_REPO/commits/$sha" --jq .commit.message); then
      echo "could not read the head commit"
      return
    fi
    not_minor_or_patch "$message"
  fi
}

prs=$(gh pr list --base main --state open --limit 100 \
  --json number,author,headRefName,headRefOid,isDraft,isCrossRepository \
  --jq '[.[] | select(.author.login == "app/dependabot" or .author.login == "localstack-bot")]
    | sort_by(.number) | .[]
    | [.number, .author.login, .headRefName, .headRefOid, .isDraft, .isCrossRepository] | @tsv')

echo "## Dependency auto-merge" >>"$summary"
[[ -n "$prs" ]] || report "no open bot PRs"

failed=0
while IFS=$'\t' read -r -u 3 number author branch sha draft cross; do
  [[ -n "$number" ]] || continue
  case "$author:$branch" in
    app/dependabot:dependabot/*) bot="dependabot[bot]" ;;
    localstack-bot:chore/weekly-go-upgrade) bot="localstack-bot" ;;
    *)
      report "#$number skipped: unexpected branch $branch for $author"
      continue
      ;;
  esac
  if [[ "$draft" == true || "$cross" == true ]]; then
    report "#$number skipped: draft or from a fork"
    continue
  fi

  if ! reason=$(skip_reason "$number" "$bot" "$sha"); then
    reason="${reason:-unexpected error}"
  fi
  if [[ -n "$reason" ]]; then
    report "#$number skipped: $reason"
  elif [[ "$dry_run" != 0 ]]; then
    report "#$number would be merged"
  elif gh pr merge "$number" --squash --admin --match-head-commit "$sha" </dev/null; then
    report "#$number merged"
  else
    report "#$number merge failed"
    failed=1
  fi
done 3<<<"$prs"

exit "$failed"

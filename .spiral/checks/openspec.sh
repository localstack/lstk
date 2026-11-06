#!/bin/sh
# Stage of .spiral/check.sh: the branch's openspec change. Spiral only adds new
# changes under openspec/changes/<id>/: any edit to an existing change, the
# archive, openspec/specs/ or the config fails. Every new change must pass
# `openspec validate --strict` and have every task ticked. A branch that changes
# non-test Go code under cmd/ or internal/ must add one, unless a commit body
# has an `openspec: none — <reason>` line (a refactor, or a fix that restores
# already-specified behaviour).
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

step "openspec changes on the branch vs $base"
touched=$(git diff --name-status --no-renames "$range" -- openspec/ || true)

violations=$(printf '%s\n' "$touched" | while read -r status path; do
  [ -n "$path" ] || continue
  case $path in
    (openspec/changes/archive/* | openspec/specs/* | openspec/config.yaml) echo "$path"; continue ;;
  esac
  [ "$status" = A ] || { echo "$path"; continue; }
  id=$(echo "$path" | cut -d/ -f3)
  if git cat-file -e "$base:openspec/changes/$id" 2>/dev/null; then echo "$path"; fi
done)
if [ -n "$violations" ]; then
  echo "checks: the branch changes existing openspec files; Spiral only adds new changes under openspec/changes/<id>/:" >&2
  echo "$violations" >&2
  exit 1
fi

changes=$(printf '%s\n' "$touched" | sed -En 's#^A[[:space:]]+openspec/changes/([^/]+)/.*#\1#p' | sort -u)
failed=0
for id in $changes; do
  echo "validating $id"
  openspec validate "$id" --strict --no-interactive || failed=1
  if grep -n -- '- \[ \]' "openspec/changes/$id/tasks.md" 2>/dev/null; then
    echo "checks: openspec/changes/$id/tasks.md has unticked tasks (above); tick each when done, or drop the ones that no longer apply and say why in design.md" >&2
    failed=1
  fi
done
[ "$failed" = 0 ] || exit 1

code=$(git diff --diff-filter=d --name-only "$range" -- 'cmd/*.go' 'internal/*.go' main.go | grep -v '_test\.go$' || true)
if [ -n "$changes" ]; then
  echo "new openspec change(s): $(echo "$changes" | tr '\n' ' ')"
elif [ -z "$code" ]; then
  echo "no non-test Go changes under cmd/ or internal/; no openspec change required"
elif git log --format=%B "$base..HEAD" | grep -q '^openspec: none'; then
  echo "no openspec change; a commit body says why:"
  git log --format=%B "$base..HEAD" | grep '^openspec: none'
else
  echo "checks: the branch changes Go code but adds no openspec change under openspec/changes/<id>/:" >&2
  echo "$code" >&2
  echo "checks: add one (.spiral/agents/implementer.md), or put an 'openspec: none — <reason>' line in a commit body" >&2
  exit 1
fi

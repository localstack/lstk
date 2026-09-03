#!/usr/bin/env bash
#
# Release gate: the descriptions file, the bundled extensions binary and the
# binary's own command list must agree.
#
# LocalStack's bundled extensions ship as one multi-call binary,
# `bundled-extensions`, and lstk learns which commands it provides from
# lstk-extensions.toml (a flat table of `name = "one-line description"`) and
# execs the binary with argv[0] set to `lstk-<name>`. That makes the file
# load-bearing in both directions: a described name the binary does not answer
# to is a command that shows in help and fails when run. The binary's side of
# the story comes from the binary itself: `bundled-extensions list` prints the
# commands it provides, one bare name per line. Asking it is the only
# authoritative answer — the toml is hand-written, and nothing else on disk
# records what the binary answers to.
#
#   * commands described but no binary        -> FAIL (help would list commands
#                                                that cannot run)
#   * binary present but nothing described    -> FAIL (lstk could never reach it;
#                                                the runtime treats this as a
#                                                broken install)
#   * `list` fails or prints nothing           -> FAIL (nothing to verify the
#                                                descriptions against)
#   * described but the bundle lacks it       -> FAIL (lstk would exec the binary
#                                                under a name it does not answer to)
#   * provided but not described              -> warn (unreachable through lstk;
#                                                the bundle's own inconsistency)
#   * neither binary nor descriptions         -> pass (nothing is bundled)
#   * everything agrees                       -> pass; the command names are
#                                                printed for the release log
#
# Only the left-hand names are read from the toml, never the description
# values, so no description string can break this check.
#
# The binary has to run, so this can only be pointed at a platform directory
# the host can execute — the release runner checks bundled/linux_amd64. That
# also means only that platform's binary is interrogated; a bundle whose
# platforms disagree with each other is not something this can see. The toml is
# identical in every archive (the fetch script insists on it), so one directory
# is the practical unit of verification.
#
# Usage:
#   scripts/bundled-extensions/check-descriptions.sh <platform-dir> [descriptions-file]
#
#   <platform-dir>       e.g. bundled/linux_amd64, as staged by
#                        scripts/bundled-extensions/fetch-bundled-extensions.sh
#   [descriptions-file]  defaults to <platform-dir>/../lstk-extensions.toml
set -euo pipefail

BUNDLED_BINARY="bundled-extensions"
DESCRIPTIONS_FILE="lstk-extensions.toml"
# The subcommand the bundle answers with its command list.
LIST_COMMAND="list"
NAME_RULE='^[A-Za-z0-9][A-Za-z0-9_-]*$'

die() {
  echo "check-descriptions: $*" >&2
  exit 1
}

usage() {
  sed -n '2,47p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 1
}

[ $# -ge 1 ] && [ $# -le 2 ] || usage
PLATFORM_DIR="$1"
TOML="${2:-${PLATFORM_DIR}/../${DESCRIPTIONS_FILE}}"

[ -d "${PLATFORM_DIR}" ] || die "no such directory: ${PLATFORM_DIR}"

# The binary, if present, under either spelling.
binary=""
for candidate in "${PLATFORM_DIR}/${BUNDLED_BINARY}" "${PLATFORM_DIR}/${BUNDLED_BINARY}.exe"; do
  if [ -e "${candidate}" ]; then
    binary="${candidate}"
    break
  fi
done

# The described command names: the bare identifier left of the first `=` on
# each non-comment line. Values are never looked at. The name rule is the same
# one lstk applies at load time (validate.ExtensionName), so a file that passes
# here always loads.
names=""
invalid=""
if [ -f "${TOML}" ]; then
  while IFS= read -r line; do
    line="${line%%#*}"
    case "${line}" in *=*) ;; *) continue ;; esac
    key="${line%%=*}"
    key="$(echo "${key}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    [ -n "${key}" ] || continue
    if echo "${key}" | grep -Eq "${NAME_RULE}"; then
      names="${names}${key}
"
    else
      invalid="${invalid}  ${key}
"
    fi
  done < "${TOML}"
fi

if [ -n "${invalid}" ]; then
  echo "check-descriptions: ${TOML} contains invalid command names:" >&2
  printf '%s' "${invalid}" >&2
  die "command names must match ${NAME_RULE}"
fi

# The staging tree holds the bundle binary and nothing else per platform, so an
# lstk-<name> file here is a genuinely standalone extension somebody added. It
# still works — lstk resolves it from its directory — but carries no
# description, so it is flagged rather than failed.
for stray in "${PLATFORM_DIR}"/lstk-*; do
  [ -e "${stray}" ] || [ -L "${stray}" ] || continue
  echo "Warning: standalone extension binary $(basename "${stray}") in ${PLATFORM_DIR} is not part of the bundle and will show name-only in help."
done

if [ -z "${binary}" ]; then
  if [ -n "${names}" ]; then
    echo "check-descriptions: ${TOML} describes commands but ${PLATFORM_DIR} has no ${BUNDLED_BINARY} binary to provide them:" >&2
    printf '%s' "${names}" | sed 's/^/  /' >&2
    die "either add the bundle binary to the release or remove these entries"
  fi
  echo "Nothing bundled: no ${BUNDLED_BINARY} binary and no described commands."
  exit 0
fi

[ -x "${binary}" ] || die "${binary} is not executable"
[ -f "${TOML}" ] || die "${binary} is present but ${TOML} is missing; lstk cannot know which commands the bundle provides without ${DESCRIPTIONS_FILE}"
[ -n "${names}" ] || die "${binary} is present but ${TOML} describes no commands; the bundle would be unreachable"
# Ask the bundle which commands it provides. A non-zero exit is fatal: without
# its answer there is nothing to verify the descriptions against, and shipping
# an unverified pair is exactly what this gate exists to prevent. The most
# likely cause in practice is pointing this at a platform directory the host
# cannot execute (a windows_* dir on Linux, say).
if ! listed="$("${binary}" "${LIST_COMMAND}" 2>&1)"; then
  echo "check-descriptions: ${binary} ${LIST_COMMAND} failed:" >&2
  printf '%s\n' "${listed}" | sed 's/^/  /' >&2
  die "cannot read the bundle's command list; run this against a platform directory this machine can execute"
fi

# One bare command name per line. Anything else is rejected rather than
# best-guess parsed: this list decides what lstk will dispatch, so a format
# change in the bundle must surface here and not as a silently shorter list.
provided=""
malformed=""
while IFS= read -r line; do
  line="$(printf '%s' "${line}" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  [ -n "${line}" ] || continue
  if echo "${line}" | grep -Eq "${NAME_RULE}"; then
    provided="${provided}${line}
"
  else
    malformed="${malformed}  ${line}
"
  fi
done <<EOF
${listed}
EOF

if [ -n "${malformed}" ]; then
  echo "check-descriptions: $(basename "${binary}") ${LIST_COMMAND} printed lines that are not command names:" >&2
  printf '%s' "${malformed}" >&2
  die "expected one bare command name per line, each matching ${NAME_RULE}"
fi
[ -n "${provided}" ] || die "${binary} ${LIST_COMMAND} printed no commands; the bundle declares nothing for lstk to dispatch"

unprovided=""
for name in ${names}; do
  echo "${provided}" | grep -qx -- "${name}" || unprovided="${unprovided}  ${name}
"
done
if [ -n "${unprovided}" ]; then
  echo "check-descriptions: ${TOML} describes commands that $(basename "${binary}") does not provide:" >&2
  printf '%s' "${unprovided}" >&2
  die "lstk would exec the bundle under a name it does not answer to; fix the descriptions file or the bundle"
fi

for name in ${provided}; do
  printf '%s' "${names}" | grep -qx -- "${name}" \
    || echo "Warning: the bundle provides ${name} but ${TOML} does not describe it, so lstk will not expose it."
done

count="$(printf '%s' "${names}" | grep -c . || true)"
echo "Bundle $(basename "${binary}") provides ${count} described command(s): $(printf '%s' "${names}" | tr '\n' ' ')"

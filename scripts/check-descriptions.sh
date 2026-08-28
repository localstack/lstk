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
# the story is bundle-commands.txt, which scripts/fetch-bundled-extensions.sh
# records from the `lstk-<name>` alias entries the bundle archives carry.
#
#   * commands described but no binary        -> FAIL (help would list commands
#                                                that cannot run)
#   * binary present but nothing described    -> FAIL (lstk could never reach it;
#                                                the runtime treats this as a
#                                                broken install)
#   * binary present but no command list      -> FAIL (nothing to verify the
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
# values, so no description string can break this check. Descriptions and the
# command list are identical on every platform, so the release runs this once
# against one platform directory.
#
# Usage:
#   scripts/check-descriptions.sh <platform-dir> [descriptions-file] [commands-file]
#
#   <platform-dir>       e.g. bundled/linux_amd64, as staged by
#                        scripts/fetch-bundled-extensions.sh
#   [descriptions-file]  defaults to <platform-dir>/../lstk-extensions.toml
#   [commands-file]      defaults to <platform-dir>/../bundle-commands.txt
set -euo pipefail

BUNDLED_BINARY="bundled-extensions"
DESCRIPTIONS_FILE="lstk-extensions.toml"
COMMANDS_FILE="bundle-commands.txt"

die() {
  echo "check-descriptions: $*" >&2
  exit 1
}

usage() {
  sed -n '2,42p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 1
}

[ $# -ge 1 ] && [ $# -le 3 ] || usage
PLATFORM_DIR="$1"
TOML="${2:-${PLATFORM_DIR}/../${DESCRIPTIONS_FILE}}"
COMMANDS="${3:-${PLATFORM_DIR}/../${COMMANDS_FILE}}"

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
    if echo "${key}" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9_-]*$'; then
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
  die "command names must match ^[A-Za-z0-9][A-Za-z0-9_-]*$"
fi

# Standalone lstk-<name> files are not how the bundle ships. They still work
# (lstk resolves them from its directory) but carry no description, so flag
# them rather than fail.
for stray in "${PLATFORM_DIR}"/lstk-*; do
  [ -e "${stray}" ] || continue
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
[ -f "${COMMANDS}" ] || die "${binary} is present but its command list ${COMMANDS} is missing; scripts/fetch-bundled-extensions.sh records it from the bundle's lstk-<name> alias entries"

provided="$(grep -v '^[[:space:]]*$' "${COMMANDS}" || true)"
[ -n "${provided}" ] || die "${binary} is present but its command list ${COMMANDS} is empty; the bundle declares no commands"

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

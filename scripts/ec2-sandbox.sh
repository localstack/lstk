#!/usr/bin/env bash
#
# ec2-sandbox.sh - stand up a throwaway Windows or Linux desktop on EC2, reachable
# over RDP, then tear it down again.
#
# Each sandbox gets two logins: a privileged admin account and an unprivileged
# "User" account, with VS Code and Docker preinstalled.
#
# On Windows the container engine is selectable with --containers: 'docker' (the
# default) runs Docker Engine inside WSL2 behind a proxy that rewrites Windows
# bind-mount paths, and 'podman' keeps the older Podman-as-docker.exe arrangement.
#
# One instance per OS per region. Everything the script creates is tagged
# ManagedBy=ec2-sandbox.sh so teardown never touches anything else.
#
# Written for macOS system bash 3.2, so no associative arrays / mapfile / ${var,,}.

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

SCRIPT_NAME=$(basename "$0")
TAG_KEY="ManagedBy"
TAG_VAL="ec2-sandbox.sh"
OS_TAG_KEY="SandboxOS"
ENGINE_TAG_KEY="SandboxContainers"
ARCH_TAG_KEY="SandboxArch"

# Windows needs nested virtualization for WSL2, which rules out t3. Linux runs
# Docker Engine natively, so it stays on the cheap type.
DEFAULT_TYPE_WINDOWS="m8i.large"
DEFAULT_TYPE_LINUX_X64="t3.medium"
# The arm64 counterpart of t3.medium: Graviton2, 2 vCPU, 4 GiB.
DEFAULT_TYPE_LINUX_ARM64="t4g.medium"

# Families that support --cpu-options NestedVirtualization=enabled. The AWS docs
# list 7th gen too, but the CLI's own help restricts it to 8th gen; both agree on
# 8th, which is why that is the Windows default.
NESTED_VIRT_FAMILIES="c8i m8i r8i x8i c8id m8id r8id c8i-flex m8i-flex r8i-flex c7i m7i r7i i7i c7i-flex m7i-flex"

# The Windows AMI ships a 30 GiB root volume, and a Windows sandbox stores the WSL2
# distro's VHD (or the podman machine image) plus whatever container images get
# pulled on top of that, so it needs more headroom than Linux.
MIN_VOLUME_GB_WINDOWS=60
MIN_VOLUME_GB_LINUX=40
# Placeholder so --help can render before set_os_profile picks the real one.
MIN_VOLUME_GB=$MIN_VOLUME_GB_LINUX

RDP_PORT=3389
SSH_PORT=22

# Windows convention is capitalised account names; Linux convention is lowercase.
UNPRIV_USER_WINDOWS="User"
UNPRIV_USER_LINUX="user"
ADMIN_USER_WINDOWS="Administrator"
ADMIN_USER_LINUX="ubuntu"
# Placeholder so --help can render before set_os_profile picks the real one.
UNPRIV_USER="User"

# Windows container engine, selected with --containers. Linux always runs Docker
# Engine natively, so the flag is a no-op there.
DEFAULT_CONTAINERS="docker"

# Pinned so a sandbox built today matches one built last month. Docker's static
# index has no 'latest' alias, so the CLI version has to be spelled out anyway.
DOCKER_CLI_VERSION="29.8.0"
DOCKER_COMPOSE_VERSION="v5.5.1"
DOCKER_BUILDX_VERSION="v0.37.0"

# The rootfs Docker Engine runs in. cloud-images.ubuntu.com stopped publishing WSL
# tarballs in 2025; this is where they live now, and it is the same artifact that
# 'wsl --install Ubuntu-24.04' resolves to through Microsoft's distro manifest.
WSL_ROOTFS_URL="https://releases.ubuntu.com/24.04.4/ubuntu-24.04.4-wsl-amd64.wsl"
WSL_ROOTFS_SHA256="9b2f7730dc68227dd04a9f3e5eab86ad85caf556b8606ad94f1f29ff5c4fd3f5"
WSL_DISTRO="ec2-sandbox-docker"

# Where the path-rewriting proxy listens inside the distro. Loopback only, reached
# from Windows through WSL2's localhost forwarding.
DOCKER_PROXY_PORT=2375

SSM_PARAM_WINDOWS="/aws/service/ami-windows-latest/Windows_Server-2025-English-Full-Base"
# @@AMI_ARCH@@ is amd64 or arm64; set_os_profile fills it in.
SSM_PARAM_LINUX_TEMPLATE="/aws/service/canonical/ubuntu/server/24.04/stable/current/@@AMI_ARCH@@/hvm/ebs-gp3/ami-id"

AMI_FALLBACK_OWNER_WINDOWS="amazon"
AMI_FALLBACK_NAME_WINDOWS="Windows_Server-2025-English-Full-Base-*"
AMI_FALLBACK_OWNER_LINUX="099720109477" # Canonical
AMI_FALLBACK_NAME_LINUX_TEMPLATE="ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-@@AMI_ARCH@@-server-*"

# Instance states that count as "this sandbox already exists". 'terminated' is
# excluded on purpose: terminated instances stay visible to describe-instances for
# about an hour, and including it would block create for that whole window.
LIVE_STATES="pending,running,stopping,stopped"

READY_SENTINEL_LINUX="/var/lib/ec2-sandbox-ready"
READY_SENTINEL_WINDOWS="C:\\ProgramData\\ec2-sandbox\\phase1-ready"

# Windows reboots to enable the WSL2 features, so allow more time than Linux.
BOOTSTRAP_DEADLINE_LINUX=1200
BOOTSTRAP_DEADLINE_WINDOWS=1200

ACTION=""
OS=""
ARCH=""
SOURCE_PATH=""
CONTAINERS=""
REGION=""
INSTANCE_TYPE=""
ASSUME_YES=0
DO_OPEN=0
OPEN_AS="user"

# Track a launched-but-not-finished instance so the exit trap can warn about it.
LAUNCHED_IID=""
CREATE_DONE=0

# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------

if [ -t 1 ]; then
  C_RESET=$'\033[0m'; C_RED=$'\033[31m'; C_GREEN=$'\033[32m'
  C_YELLOW=$'\033[33m'; C_BLUE=$'\033[34m'; C_BOLD=$'\033[1m'
else
  C_RESET=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""; C_BOLD=""
fi

log()  { printf '%s==>%s %s\n' "$C_BLUE" "$C_RESET" "$*" >&2; }
ok()   { printf '%s==>%s %s\n' "$C_GREEN" "$C_RESET" "$*" >&2; }
warn() { printf '%swarning:%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
die()  { printf '%serror:%s %s\n' "$C_RED" "$C_RESET" "$*" >&2; exit 1; }

usage() {
  cat <<EOF
${C_BOLD}${SCRIPT_NAME}${C_RESET} - throwaway Windows or Linux desktop on EC2, over RDP.

${C_BOLD}USAGE${C_RESET}
  ${SCRIPT_NAME} <create|delete|info> <windows|linux> [options]
  ${SCRIPT_NAME} copyto <windows|linux> [options] <path>

${C_BOLD}COMMANDS${C_RESET}
  create <os>    Launch the sandbox and print RDP connection details.
  delete <os>    Terminate the instance and delete its security group.
  info <os>      Re-print connection details for a running sandbox.
  copyto <os> <path>
                 Copy a local file, or a directory and its contents, onto the
                 unprivileged account's desktop -- '${UNPRIV_USER_WINDOWS}' on Windows,
                 '${UNPRIV_USER_LINUX}' on Linux.

${C_BOLD}OPTIONS${C_RESET}
  --region <r>        AWS region. Defaults to \$AWS_REGION, \$AWS_DEFAULT_REGION,
                      then your configured region.
  --instance-type <t> Override the instance type.
  --arch <a>          CPU architecture: 'x64' or 'arm64'. Required for create,
                      which has no default. delete and info work it out from the
                      sandboxes that exist, and only need it to break a tie.
  --containers <e>    Windows container engine: 'docker' (default) or 'podman'.
                      create only; info reads it back from the instance.
  --as <admin|user>   Which account --open connects as (default: user).
  --open              After create/info, launch the RDP client.
  -y, --yes           Skip the delete confirmation prompt.
  -h, --help          Show this help.

${C_BOLD}WHAT YOU GET${C_RESET}
  Two logins per sandbox, both able to RDP:

    admin   Full privileges. 'Administrator' on Windows, 'ubuntu' (sudo) on Linux.
    user    An ordinary account with no admin rights. Named '${UNPRIV_USER_WINDOWS}' on
            Windows and '${UNPRIV_USER_LINUX}' on Linux, following each platform's
            naming convention.

  Preinstalled:

    VS Code   Installed system-wide, so it is on the Start menu / app menu for
              both accounts at first login.
    Firefox   Linux only, from Mozilla's apt repo (not the snap), set as the
              default browser for both accounts.
    Tooling   The AWS CLI v2, Terraform and the AWS SAM CLI, installed
              system-wide for both accounts -- these are what 'lstk aws',
              'lstk terraform' and 'lstk sam' shell out to.
    Docker    Linux: Docker Engine + compose, running natively.
              Windows, --containers=docker (default): Docker Engine inside a
              WSL2 distro, served on \\\\.\pipe\docker_engine -- the same endpoint
              Docker Desktop uses, so docker.exe and lstk need no configuration.
              A proxy rewrites Windows bind-mount paths to /mnt/c/... exactly as
              Desktop's backend does, so 'docker run -v C:\dir:/x' works for the
              CLI and for API clients alike.
              Windows, --containers=podman: Podman, presented as 'docker' on the
              PATH. Its path translation is client-side, so it covers docker.exe
              but not programs that speak the API directly.
              Docker Desktop itself cannot be installed on Windows Server at all.

  Defaults: windows ${DEFAULT_TYPE_WINDOWS}, linux ${DEFAULT_TYPE_LINUX_X64} on x64 and
  ${DEFAULT_TYPE_LINUX_ARM64} on arm64. Root volume ${MIN_VOLUME_GB_LINUX} GiB gp3, or ${MIN_VOLUME_GB_WINDOWS} GiB on Windows,
  which also stores a WSL2 or podman VM image.

${C_BOLD}NOTES${C_RESET}
  * One instance per OS and architecture per region, so a linux x64 and a linux
    arm64 sandbox can run side by side, as can a Windows one.
  * Windows is x64 only -- there is no arm64 Windows Server, on EC2 or anywhere.
  * RDP and SSH are opened only to your current public IP as a /32. If your IP
    changes, re-running create re-authorises it; a running instance is untouched.
  * Key pairs and their .pem are reused across create/delete cycles, not deleted.
  * State lives in ~/.local/state/ec2-sandbox/<region>/<os>-<arch>/
  * Windows defaults to ${DEFAULT_TYPE_WINDOWS} because WSL2 needs nested virtualization,
    which t3 does not support. Overriding to an unsupported type disables Docker.
  * Windows reboots once during setup to enable WSL2, so create takes ~15 minutes.
  * With --containers=docker, Docker is ready when create finishes and is shared
    by both accounts -- there is no first-logon step. The daemon runs in a WSL2
    distro owned by the admin account and is reachable machine-wide, so '${UNPRIV_USER_WINDOWS}'
    gets Docker without a per-user setup. That also means '${UNPRIV_USER_WINDOWS}' can mount
    any part of C: into a container with the admin account's rights; Docker access
    is root-equivalent on every platform, but on Windows it is worth stating.
    If the named pipe ever misbehaves, DOCKER_HOST=tcp://127.0.0.1:${DOCKER_PROXY_PORT} is the
    same daemon without the pipe (and without the path rewriting).
  * With --containers=podman, the Podman machine is instead created the first time
    you log in as '${UNPRIV_USER_WINDOWS}', because podman machines are per-Windows-user and
    cannot be prepared in advance. That first logon downloads ~1 GB and takes
    several minutes; a console window is visible while it runs. The machine is set
    rootful so containers can bind ports below 1024 (LocalStack needs 443).
  * On Linux, '${UNPRIV_USER_LINUX}' is in the 'docker' group so it can run containers.
    Docker group membership is equivalent to root on the host, so it is
    unprivileged in the ordinary sense but is not a security boundary.

${C_BOLD}EXAMPLES${C_RESET}
  ${SCRIPT_NAME} create linux --arch=arm64
  ${SCRIPT_NAME} create windows --arch=x64 --region us-east-1 --open
  ${SCRIPT_NAME} create windows --containers=podman
  ${SCRIPT_NAME} info linux --as admin --open
  ${SCRIPT_NAME} copyto windows ./dist/extension.vsix
  ${SCRIPT_NAME} copyto linux --arch=arm64 ./testdata
  ${SCRIPT_NAME} delete windows -y
EOF
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

parse_args() {
  for arg in "$@"; do
    case "$arg" in
      -h|--help) usage; exit 0 ;;
    esac
  done

  [ $# -gt 0 ] || { usage >&2; die "missing command (create, delete, info or copyto)"; }

  ACTION="$1"; shift
  case "$ACTION" in
    create|delete|info|copyto) ;;
    -*) die "unknown option '$ACTION'; the command must come first (create, delete, info or copyto)" ;;
    *)  die "unknown command '$ACTION'; expected create, delete, info or copyto" ;;
  esac

  [ $# -gt 0 ] || die "missing OS argument; '$ACTION' needs 'windows' or 'linux' (e.g. $SCRIPT_NAME $ACTION linux)"

  OS="$1"; shift
  case "$OS" in
    windows|linux) ;;
    -*) die "missing OS argument; '$ACTION' needs 'windows' or 'linux' before any options" ;;
    *)  die "unknown OS '$OS'; expected 'windows' or 'linux'" ;;
  esac

  while [ $# -gt 0 ]; do
    case "$1" in
      --region)
        [ $# -ge 2 ] || die "--region needs a value"
        REGION="$2"; shift 2 ;;
      --region=*) REGION="${1#*=}"; shift ;;
      --instance-type)
        [ $# -ge 2 ] || die "--instance-type needs a value"
        INSTANCE_TYPE="$2"; shift 2 ;;
      --instance-type=*) INSTANCE_TYPE="${1#*=}"; shift ;;
      --arch)
        [ $# -ge 2 ] || die "--arch needs a value (x64 or arm64)"
        ARCH="$2"; shift 2 ;;
      --arch=*) ARCH="${1#*=}"; shift ;;
      --containers)
        [ $# -ge 2 ] || die "--containers needs a value (docker or podman)"
        CONTAINERS="$2"; shift 2 ;;
      --containers=*) CONTAINERS="${1#*=}"; shift ;;
      --as)
        [ $# -ge 2 ] || die "--as needs a value (admin or user)"
        OPEN_AS="$2"; shift 2 ;;
      --as=*) OPEN_AS="${1#*=}"; shift ;;
      --open) DO_OPEN=1; shift ;;
      -y|--yes) ASSUME_YES=1; shift ;;
      -*) die "unknown option '$1'" ;;
      *)
        [ "$ACTION" = "copyto" ] || die "unexpected argument '$1'; '$ACTION' takes options only"
        [ -z "$SOURCE_PATH" ] || die "copyto takes one path, but got both '$SOURCE_PATH' and '$1'"
        SOURCE_PATH="$1"; shift ;;
    esac
  done

  if [ "$ACTION" = "copyto" ]; then
    [ -n "$SOURCE_PATH" ] || die "copyto needs a path to copy (e.g. $SCRIPT_NAME copyto $OS ./build.zip)"
    [ -e "$SOURCE_PATH" ] || die "no such file or directory: $SOURCE_PATH"
  fi

  case "$OPEN_AS" in
    admin|user) ;;
    *) die "--as must be 'admin' or 'user', not '$OPEN_AS'" ;;
  esac

  case "$ARCH" in
    "") ;;
    x64|arm64) ;;
    *) die "--arch must be 'x64' or 'arm64', not '$ARCH'" ;;
  esac

  # Deliberately no default: an architecture is not something to be guessed on
  # your behalf when it changes which binaries the sandbox can even run.
  if [ "$ACTION" = "create" ] && [ -z "$ARCH" ]; then
    die "missing --arch; pass --arch=x64 or --arch=arm64 (there is no default)"
  fi

  if [ "$OS" = "windows" ] && [ "$ARCH" = "arm64" ]; then
    die "windows is x64 only. AWS publishes no arm64 Windows AMIs, and Windows Server
  2025 has no arm64 release at all; the AWS and SAM CLIs ship no Windows arm64
  builds either. Use --arch=x64 for windows, or --arch=arm64 with linux."
  fi

  if [ -n "$CONTAINERS" ]; then
    case "$CONTAINERS" in
      podman|docker) ;;
      *) die "--containers must be 'docker' or 'podman', not '$CONTAINERS'" ;;
    esac
    # The engine is a property of the instance, recorded as a tag at launch, so
    # delete and info read it back rather than being told.
    if [ "$ACTION" != "create" ]; then
      die "--containers only applies to create; '$ACTION' reads the engine from the instance"
    fi
    if [ "$OS" = "linux" ] && [ "$CONTAINERS" = "podman" ]; then
      die "the linux sandbox runs Docker Engine natively; --containers=podman is not supported"
    fi
  fi
}

# ---------------------------------------------------------------------------
# Environment
# ---------------------------------------------------------------------------

require_cmds() {
  local missing=""
  local c
  for c in "$@"; do
    command -v "$c" >/dev/null 2>&1 || missing="$missing $c"
  done
  [ -z "$missing" ] || die "missing required command(s):$missing"
}

resolve_region() {
  if [ -z "$REGION" ]; then REGION="${AWS_REGION:-}"; fi
  if [ -z "$REGION" ]; then REGION="${AWS_DEFAULT_REGION:-}"; fi
  if [ -z "$REGION" ]; then
    # 'aws configure get region' exits 1 when unset, which set -e would catch.
    REGION=$(aws configure get region 2>/dev/null || true)
  fi
  [ -n "$REGION" ] || die "no region: pass --region, or set AWS_REGION, or run 'aws configure'"
}

# Wrapper so --region can never be forgotten on an API call.
aws_() { command aws --region "$REGION" "$@"; }

preflight_identity() {
  local ident
  if ! ident=$(aws_ sts get-caller-identity --query 'Arn' --output text 2>&1); then
    case "$ident" in
      *sso*|*SSO*|*Token*has*expired*)
        die "AWS credentials are expired. Run 'aws sso login' and try again." ;;
      *ExpiredToken*|*InvalidClientTokenId*|*credentials*)
        die "AWS credentials are invalid or expired. Refresh them and try again." ;;
      *)
        die "could not verify AWS credentials: $ident" ;;
    esac
  fi
  log "Authenticated as ${ident} in ${REGION}"
}

# ---------------------------------------------------------------------------
# Per-OS profile. bash 3.2, so plain variables rather than an associative array.
# ---------------------------------------------------------------------------

set_os_profile() {
  # Architecture is part of a sandbox's identity, so an x64 and an arm64 sandbox of
  # the same OS coexist without colliding on an instance, security group, key pair
  # or state directory.
  local profile="${OS}-${ARCH}"
  INSTANCE_NAME="ec2-sandbox-${profile}"
  SG_NAME="ec2-sandbox-${profile}-sg"
  KEY_NAME="ec2-sandbox-${profile}-key"

  # EC2 and the Ubuntu AMI paths spell it amd64/arm64; the flag spells it x64 to
  # match how the rest of the world names the platform.
  case "$ARCH" in
    arm64) AMI_ARCH="arm64" ;;
    *)     AMI_ARCH="amd64" ;;
  esac

  STATE_DIR="${HOME}/.local/state/ec2-sandbox/${REGION}/${profile}"
  PEM_PATH="${STATE_DIR}/key.pem"
  RDP_ADMIN_PATH="${STATE_DIR}/connect-admin.rdp"
  RDP_USER_PATH="${STATE_DIR}/connect-user.rdp"
  PW_ADMIN_PATH="${STATE_DIR}/password-admin"
  PW_USER_PATH="${STATE_DIR}/password-user"

  case "$OS" in
    windows)
      OS_LABEL="Windows Server 2025"
      ADMIN_USER="$ADMIN_USER_WINDOWS"
      SSH_USER="Administrator"
      SSM_PARAM="$SSM_PARAM_WINDOWS"
      AMI_FALLBACK_OWNER="$AMI_FALLBACK_OWNER_WINDOWS"
      AMI_FALLBACK_NAME="$AMI_FALLBACK_NAME_WINDOWS"
      READY_SENTINEL="$READY_SENTINEL_WINDOWS"
      UNPRIV_USER="$UNPRIV_USER_WINDOWS"
      BOOTSTRAP_DEADLINE="$BOOTSTRAP_DEADLINE_WINDOWS"
      MIN_VOLUME_GB="$MIN_VOLUME_GB_WINDOWS"
      if [ -z "$INSTANCE_TYPE" ]; then INSTANCE_TYPE="$DEFAULT_TYPE_WINDOWS"; fi
      if [ -z "$CONTAINERS" ]; then CONTAINERS="$DEFAULT_CONTAINERS"; fi
      ;;
    linux)
      OS_LABEL="Ubuntu 24.04 LTS + XFCE"
      ADMIN_USER="$ADMIN_USER_LINUX"
      SSH_USER="ubuntu"
      SSM_PARAM="${SSM_PARAM_LINUX_TEMPLATE//@@AMI_ARCH@@/$AMI_ARCH}"
      AMI_FALLBACK_OWNER="$AMI_FALLBACK_OWNER_LINUX"
      AMI_FALLBACK_NAME="${AMI_FALLBACK_NAME_LINUX_TEMPLATE//@@AMI_ARCH@@/$AMI_ARCH}"
      READY_SENTINEL="$READY_SENTINEL_LINUX"
      UNPRIV_USER="$UNPRIV_USER_LINUX"
      BOOTSTRAP_DEADLINE="$BOOTSTRAP_DEADLINE_LINUX"
      MIN_VOLUME_GB="$MIN_VOLUME_GB_LINUX"
      if [ -z "$INSTANCE_TYPE" ]; then
        if [ "$ARCH" = "arm64" ]; then
          INSTANCE_TYPE="$DEFAULT_TYPE_LINUX_ARM64"
        else
          INSTANCE_TYPE="$DEFAULT_TYPE_LINUX_X64"
        fi
      fi
      CONTAINERS="docker"
      ;;
  esac

  # Both OSes need SSH inbound: it is how the script sets the unprivileged
  # account's password without ever putting it in user-data.
  INGRESS_PORTS="$RDP_PORT $SSH_PORT"
}

ensure_state_dir() {
  mkdir -p "$STATE_DIR"
  chmod 700 "$STATE_DIR" 2>/dev/null || true
}

supports_nested_virt() { # $1 = instance type
  local family f
  family="${1%%.*}"
  for f in $NESTED_VIRT_FAMILIES; do
    if [ "$f" = "$family" ]; then return 0; fi
  done
  return 1
}

# ---------------------------------------------------------------------------
# Lookups
# ---------------------------------------------------------------------------

# --output text renders JSON null as the literal string "None", so every scalar
# extraction has to treat that as absent.
is_none() { [ -z "$1" ] || [ "$1" = "None" ]; }

find_instance() { # $1 = comma-separated states
  aws_ ec2 describe-instances \
    --filters "Name=tag:${TAG_KEY},Values=${TAG_VAL}" \
              "Name=tag:${OS_TAG_KEY},Values=${OS}" \
              "Name=tag:${ARCH_TAG_KEY},Values=${ARCH}" \
              "Name=instance-state-name,Values=$1" \
    --query 'Reservations[].Instances[].InstanceId' \
    --output text 2>/dev/null | tr '\t' '\n' | grep -v '^$' | head -1 || true
}

# Lists "<instance-id>\t<arch>" for every sandbox of this OS whatever its
# architecture, so delete and info can work out which one the caller meant.
find_instances_any_arch() {
  aws_ ec2 describe-instances \
    --filters "Name=tag:${TAG_KEY},Values=${TAG_VAL}" \
              "Name=tag:${OS_TAG_KEY},Values=${OS}" \
              "Name=instance-state-name,Values=${LIVE_STATES},shutting-down" \
    --query "Reservations[].Instances[].[InstanceId,Tags[?Key=='${ARCH_TAG_KEY}']|[0].Value]" \
    --output text 2>/dev/null | grep -v '^$' || true
}

# delete and info do not take --arch when it is unambiguous. Leaves ARCH empty
# when there is no sandbox at all; the caller decides what that means.
resolve_arch_from_instances() {
  local rows count
  rows=$(find_instances_any_arch)
  count=$(printf '%s' "$rows" | grep -c . || true)

  if [ "${count:-0}" -eq 0 ]; then
    return 0
  fi

  if [ "${count:-0}" -eq 1 ]; then
    ARCH=$(printf '%s\n' "$rows" | awk '{print $2}')
    if is_none "$ARCH"; then
      die "the ${OS} sandbox in ${REGION} predates --arch and this version cannot manage it.
  Terminate it with an older copy of this script, or by hand:
    aws ec2 terminate-instances --region ${REGION} --instance-ids $(printf '%s\n' "$rows" | awk '{print $1}')"
    fi
    return 0
  fi

  die "more than one ${OS} sandbox in ${REGION}; say which with --arch:
$(printf '%s\n' "$rows" | awk '{printf "    %s  --arch=%s\n", $1, $2}')"
}

instance_field() { # $1 = instance id, $2 = JMESPath under Instances[]
  aws_ ec2 describe-instances --instance-ids "$1" \
    --query "Reservations[0].Instances[0].$2" --output text 2>/dev/null || true
}

resolve_ami() {
  local ami
  ami=$(aws_ ssm get-parameter --name "$SSM_PARAM" \
          --query 'Parameter.Value' --output text 2>/dev/null || true)

  if is_none "$ami"; then
    warn "SSM lookup failed for ${SSM_PARAM}; falling back to describe-images"
    ami=$(aws_ ec2 describe-images --owners "$AMI_FALLBACK_OWNER" \
            --filters "Name=name,Values=${AMI_FALLBACK_NAME}" "Name=state,Values=available" \
            --query 'sort_by(Images,&CreationDate)[-1].ImageId' --output text 2>/dev/null || true)
  fi

  is_none "$ami" && die "could not resolve an AMI for ${OS} in ${REGION}"
  printf '%s\n' "$ami"
}

# Root volume must be >= the AMI snapshot, so read the AMI rather than hardcoding:
# Windows ships at 30 GiB and Ubuntu at 8, and max() covers both.
resolve_root_device() {
  local dev
  dev=$(aws_ ec2 describe-images --image-ids "$1" \
          --query 'Images[0].RootDeviceName' --output text 2>/dev/null || true)
  is_none "$dev" && die "could not read the root device name of AMI $1"
  printf '%s\n' "$dev"
}

resolve_volume_size() { # $1 = ami id, $2 = root device name
  local size
  # Kept as its own call: a JMESPath multiselect with an embedded pipe is fragile.
  size=$(aws_ ec2 describe-images --image-ids "$1" \
           --query "Images[0].BlockDeviceMappings[?DeviceName=='$2'].Ebs.VolumeSize | [0]" \
           --output text 2>/dev/null || true)
  if is_none "$size"; then size=0; fi
  if [ "$size" -gt "$MIN_VOLUME_GB" ]; then
    printf '%s\n' "$size"
  else
    printf '%s\n' "$MIN_VOLUME_GB"
  fi
}

find_default_vpc() {
  local vpc
  vpc=$(aws_ ec2 describe-vpcs --filters "Name=isDefault,Values=true" \
          --query 'Vpcs[0].VpcId' --output text 2>/dev/null || true)
  if is_none "$vpc"; then
    die "no default VPC in ${REGION}.
  Create one with:  aws ec2 create-default-vpc --region ${REGION}"
  fi
  printf '%s\n' "$vpc"
}

# Not every AZ offers every instance type, and run-instances only says "Unsupported".
pick_subnet() { # $1 = vpc id
  local azs subnet az
  azs=$(aws_ ec2 describe-instance-type-offerings \
          --location-type availability-zone \
          --filters "Name=instance-type,Values=${INSTANCE_TYPE}" \
          --query 'InstanceTypeOfferings[].Location' --output text 2>/dev/null || true)
  is_none "$azs" && die "instance type ${INSTANCE_TYPE} is not offered in ${REGION}"

  for az in $azs; do
    subnet=$(aws_ ec2 describe-subnets \
               --filters "Name=vpc-id,Values=$1" "Name=availability-zone,Values=${az}" \
                         "Name=default-for-az,Values=true" \
               --query 'Subnets[0].SubnetId' --output text 2>/dev/null || true)
    if ! is_none "$subnet"; then
      printf '%s\n' "$subnet"
      return 0
    fi
  done
  die "no default subnet in ${REGION} whose AZ offers ${INSTANCE_TYPE}"
}

# Fail closed. Never widen to 0.0.0.0/0 on failure: that is RDP exposed to the whole
# internet on a known-username account, and scanners find it within minutes.
detect_my_cidr() {
  local ip
  ip=$(curl -fsS --max-time 8 https://checkip.amazonaws.com 2>/dev/null || true)
  ip=$(printf '%s' "$ip" | tr -d '\r\n[:space:]')

  if ! printf '%s' "$ip" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$'; then
    die "could not detect your public IPv4 address (checkip.amazonaws.com unreachable).
  Check your network and retry. To authorise an address by hand:
    aws ec2 authorize-security-group-ingress --region ${REGION} \\
      --group-name ${SG_NAME} --protocol tcp --port ${RDP_PORT} --cidr <YOUR.IP>/32"
  fi

  printf '%s/32\n' "$ip"
}

# ---------------------------------------------------------------------------
# Key pair
# ---------------------------------------------------------------------------

# AWS returns the private half exactly once, so a .pem that has drifted from the
# remote key pair is unrecoverable -- it would neither decrypt a Windows password
# nor open an SSH session. Reconcile the two sides rather than assuming.
ensure_key_pair() {
  local remote_exists=0
  if aws_ ec2 describe-key-pairs --key-names "$KEY_NAME" >/dev/null 2>&1; then
    remote_exists=1
  fi

  if [ -f "$PEM_PATH" ] && [ "$remote_exists" -eq 1 ]; then
    log "Reusing key pair ${KEY_NAME}"
    return 0
  fi

  if [ ! -f "$PEM_PATH" ] && [ "$remote_exists" -eq 1 ]; then
    warn "key pair ${KEY_NAME} exists in AWS but ${PEM_PATH} is missing; recreating it"
    aws_ ec2 delete-key-pair --key-name "$KEY_NAME" >/dev/null 2>&1 || true
  elif [ -f "$PEM_PATH" ] && [ "$remote_exists" -eq 0 ]; then
    warn "${PEM_PATH} exists but key pair ${KEY_NAME} is gone from AWS; recreating both"
    rm -f "$PEM_PATH"
  fi

  log "Creating key pair ${KEY_NAME}"
  # umask inside the subshell, before the redirect: a later chmod would leave a
  # window where the private key is world-readable.
  # --key-type rsa is explicit because ED25519 keys are NOT supported for Windows,
  # and the failure is silent -- get-password-data just returns garbage.
  (
    umask 077
    aws_ ec2 create-key-pair --key-name "$KEY_NAME" \
      --key-type rsa --key-format pem \
      --tag-specifications "ResourceType=key-pair,Tags=[{Key=${TAG_KEY},Value=${TAG_VAL}},{Key=${OS_TAG_KEY},Value=${OS}}]" \
      --query 'KeyMaterial' --output text > "$PEM_PATH"
  )
  [ -s "$PEM_PATH" ] || { rm -f "$PEM_PATH"; die "failed to create key pair ${KEY_NAME}"; }
  chmod 600 "$PEM_PATH"
}

# Windows has no equivalent of cloud-init's key injection, so user-data places the
# public half itself. A public key is not secret, unlike a password.
public_key() {
  ssh-keygen -y -f "$PEM_PATH" 2>/dev/null \
    || die "could not derive the public key from ${PEM_PATH}"
}

# ---------------------------------------------------------------------------
# Security group
# ---------------------------------------------------------------------------

find_security_group() { # $1 = vpc id
  aws_ ec2 describe-security-groups \
    --filters "Name=group-name,Values=${SG_NAME}" "Name=vpc-id,Values=$1" \
    --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null || true
}

ensure_security_group() { # $1 = vpc id, $2 = cidr
  local sg_id port err
  sg_id=$(find_security_group "$1")

  if is_none "$sg_id"; then
    log "Creating security group ${SG_NAME}"
    sg_id=$(aws_ ec2 create-security-group \
              --group-name "$SG_NAME" \
              --description "ec2-sandbox.sh ${OS} sandbox access" \
              --vpc-id "$1" \
              --tag-specifications "ResourceType=security-group,Tags=[{Key=${TAG_KEY},Value=${TAG_VAL}},{Key=${OS_TAG_KEY},Value=${OS}}]" \
              --query 'GroupId' --output text)
    is_none "$sg_id" && die "failed to create security group ${SG_NAME}"
  fi

  for port in $INGRESS_PORTS; do
    if err=$(aws_ ec2 authorize-security-group-ingress \
               --group-id "$sg_id" --protocol tcp --port "$port" --cidr "$2" 2>&1); then
      log "Authorised tcp/${port} from ${2}"
    else
      case "$err" in
        *InvalidPermission.Duplicate*) : ;; # rule already present, fine
        *) die "could not authorise tcp/${port} from ${2}: $err" ;;
      esac
    fi
  done

  printf '%s\n' "$sg_id"
}

# ---------------------------------------------------------------------------
# Bootstrap scripts
#
# Neither of these ever contains a password. user-data is readable through IMDS by
# anything on the instance and by anyone holding ec2:DescribeInstanceAttribute, so
# accounts are created with a throwaway secret and the real password is set later
# over SSH.
#
# Both use a quoted heredoc so the local shell expands nothing, with @@NAME@@
# placeholders substituted afterwards. That keeps remote $variables intact.
# ---------------------------------------------------------------------------

linux_user_data() {
  sed -e "s|@@USER@@|${UNPRIV_USER}|g" \
      -e "s|@@ADMIN@@|${ADMIN_USER}|g" \
      -e "s|@@SENTINEL@@|${READY_SENTINEL_LINUX}|g" <<'LINUXEOF'
#!/bin/bash
set -eux
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y xfce4 xfce4-goodies xrdp ca-certificates curl gnupg apt-transport-https unzip

# --- unprivileged account -------------------------------------------------
# Deliberately not in the sudo group. Password is set over SSH afterwards.
if ! id -u '@@USER@@' >/dev/null 2>&1; then
  adduser --disabled-password --gecos "" '@@USER@@'
fi

# --- XFCE session for both accounts ---------------------------------------
for u in '@@ADMIN@@' '@@USER@@'; do
  home=$(getent passwd "$u" | cut -d: -f6)
  echo xfce4-session > "$home/.xsession"
  chown "$u:$u" "$home/.xsession"
done

adduser xrdp ssl-cert

# Log straight in when the RDP client supplies credentials, instead of showing
# xrdp's login dialog. That dialog cannot be pasted into (neutrinolabs/xrdp#816),
# so skipping it is the only way to avoid typing the password by hand.
if grep -q '^autorun=' /etc/xrdp/xrdp.ini; then
  sed -i 's/^autorun=.*/autorun=Xorg/' /etc/xrdp/xrdp.ini
else
  sed -i '/^\[Globals\]/a autorun=Xorg' /etc/xrdp/xrdp.ini
fi

systemctl enable --now xrdp

# --- apt repos for VS Code and Docker -------------------------------------
install -m 0755 -d /etc/apt/keyrings

curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
  -o /etc/apt/keyrings/microsoft.asc
chmod a+r /etc/apt/keyrings/microsoft.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/microsoft.asc] https://packages.microsoft.com/repos/code stable main" \
  > /etc/apt/sources.list.d/vscode.list

# Firefox from Mozilla's own repo rather than Ubuntu's, whose 'firefox' package is
# only a transitional shim onto the snap. The pin is what stops that shim winning.
curl -fsSL https://packages.mozilla.org/apt/repo-signing-key.gpg \
  -o /etc/apt/keyrings/packages.mozilla.org.asc
chmod a+r /etc/apt/keyrings/packages.mozilla.org.asc
echo "deb [signed-by=/etc/apt/keyrings/packages.mozilla.org.asc] https://packages.mozilla.org/apt mozilla main" \
  > /etc/apt/sources.list.d/mozilla.list
printf 'Package: *\nPin: origin packages.mozilla.org\nPin-Priority: 1000\n' \
  > /etc/apt/preferences.d/mozilla

curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" \
  > /etc/apt/sources.list.d/docker.list

# Terraform from HashiCorp's own repo, which carries arm64 for noble. The AWS CLI
# and SAM CLI have no apt package at all and are installed from their zips below.
curl -fsSL https://apt.releases.hashicorp.com/gpg \
  | gpg --dearmor -o /etc/apt/keyrings/hashicorp.gpg
chmod a+r /etc/apt/keyrings/hashicorp.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/hashicorp.gpg] https://apt.releases.hashicorp.com noble main" \
  > /etc/apt/sources.list.d/hashicorp.list

apt-get update
apt-get install -y code firefox docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin terraform

# Make Firefox the default browser, both for xdg-open and for the alternatives
# system, so links opened from VS Code and the desktop actually resolve.
update-alternatives --install /usr/bin/x-www-browser x-www-browser /usr/bin/firefox 200 || true
update-alternatives --set x-www-browser /usr/bin/firefox || true
for u in '@@ADMIN@@' '@@USER@@'; do
  home=$(getent passwd "$u" | cut -d: -f6)
  install -d -m 0755 -o "$u" -g "$u" "$home/.config"
  printf '[Default Applications]\nx-scheme-handler/http=firefox.desktop\nx-scheme-handler/https=firefox.desktop\ntext/html=firefox.desktop\n' \
    > "$home/.config/mimeapps.list"
  chown "$u:$u" "$home/.config/mimeapps.list"
done

# --- AWS CLI and SAM CLI --------------------------------------------------
# Both ship a zip whose ./install writes to /usr/local, so every account gets
# them; the shell installers those vendors also publish default to a per-user
# install and would leave '@@USER@@' without the tools. Note the two disagree on
# how to spell aarch64.
case "$(uname -m)" in
  aarch64) awscli_arch=aarch64; sam_arch=arm64 ;;
  *)       awscli_arch=x86_64;  sam_arch=x86_64 ;;
esac

# Deliberately not fatal, unlike everything above: this script runs under 'set -e'
# and a download that fails here would cost the readiness sentinel, turning a
# missing CLI into a 20-minute create timeout. verify_tools reports what landed.
# Chained with && rather than written as separate lines: calling a function as the
# left side of || switches errexit off for its whole body, so without the chain a
# failed download would carry on into unzip and install and report success.
install_awscli() {
  curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${awscli_arch}.zip" -o /tmp/awscliv2.zip \
    && unzip -q -o /tmp/awscliv2.zip -d /tmp \
    && /tmp/aws/install
}
install_sam() {
  curl -fsSL "https://github.com/aws/aws-sam-cli/releases/latest/download/aws-sam-cli-linux-${sam_arch}.zip" \
      -o /tmp/aws-sam-cli.zip \
    && unzip -q -o /tmp/aws-sam-cli.zip -d /tmp/sam-installation \
    && /tmp/sam-installation/install
}
install_awscli || echo "WARNING: the AWS CLI failed to install" >&2
install_sam    || echo "WARNING: the SAM CLI failed to install" >&2

systemctl enable --now docker
# Both accounts can drive Docker. Note that docker group membership is
# effectively root on the host.
usermod -aG docker '@@ADMIN@@'
usermod -aG docker '@@USER@@'

# --- VS Code launcher on the unprivileged account's desktop ---------------
# Thunar has trusted desktop files only inside XDG_DATA_DIRS since 4.17.4, so a
# launcher written straight into ~/Desktop is refused at click time as being "in
# an insecure location" no matter how its permissions are set. A symlink to a
# launcher that does live in such a directory inherits that trust, which is why
# this is done in two steps rather than one.
#
# Derived from the launcher the VS Code package ships, so the Exec line and icon
# stay whatever the vendor considers correct; only the first Name= is rewritten,
# leaving the right-click actions their own names.
if [ -f /usr/share/applications/code.desktop ]; then
  awk '/^Name=/ && !seen { print "Name=VS Code"; seen = 1; next } { print }' \
    /usr/share/applications/code.desktop > /usr/share/applications/vs-code.desktop
else
  printf '[Desktop Entry]\nType=Application\nName=VS Code\nExec=/usr/bin/code %%F\nIcon=vscode\nTerminal=false\nCategories=Development;IDE;\n' \
    > /usr/share/applications/vs-code.desktop
fi
chmod 0644 /usr/share/applications/vs-code.desktop

user_home=$(getent passwd '@@USER@@' | cut -d: -f6)
install -d -m 0755 -o '@@USER@@' -g '@@USER@@' "$user_home/Desktop"
ln -sfn /usr/share/applications/vs-code.desktop "$user_home/Desktop/VS Code.desktop"
chown -h '@@USER@@:@@USER@@' "$user_home/Desktop/VS Code.desktop"

mkdir -p "$(dirname '@@SENTINEL@@')"
touch '@@SENTINEL@@'
LINUXEOF
}

# Windows bootstrap.
#
# The helper scripts below are generated here and shipped inside user-data as
# base64 blobs. Building them inside nested PowerShell here-strings caused several
# $-interpolation and quoting bugs during testing; base64 removes every layer of
# escaping, and preserves LF endings for the files that bash has to read.

# gzip before base64: user-data is capped at 16 KB and base64 alone inflates by
# a third. Compressing first roughly halves the payload.
b64gz() { gzip -9 -c | base64 | tr -d '\n'; }

# Runs as the unprivileged user at every logon, in podman mode only. Podman machines
# register per Windows user, so SYSTEM cannot create one on their behalf -- this is
# the one part that cannot be prepared ahead of time. Docker mode has no equivalent:
# its daemon is shared machine-wide, so nothing is left to do at first logon.
win_firstlogon_ps1() {
  cat <<'FLEOF'
$ErrorActionPreference = 'Continue'
Start-Transcript -Path "$env:LOCALAPPDATA\ec2-sandbox-firstlogon.log" -Append | Out-Null
$P = 'C:\Program Files\Podman\podman.exe'

if (((& $P machine list --format '{{.Name}}') -join ' ') -notmatch 'podman-machine-default') {
  # First run downloads roughly 1 GB, so this logon takes several minutes.
  & $P machine init
}

# Checked every logon, not just at creation. Rootful is required for ports below
# 1024 -- LocalStack publishes 443, and a rootless machine fails with "Listen
# failed for HOST TCP port ...: Permission denied". The setting only applies while
# the machine is stopped, so a machine left rootless would otherwise stay broken.
if (((& $P machine inspect --format '{{.Rootful}}') -join '') -notmatch 'true') {
  & $P machine stop | Out-Null
  & $P machine set --rootful
}

& $P machine start

# WSL stops a distro once the last wsl.exe client exits, which kills the podman
# machine and its ssh endpoint. This hidden process holds it open for the logon
# session; vmIdleTimeout does not cover this case.
Start-Process -WindowStyle Hidden -FilePath 'wsl.exe' -ArgumentList '-d','podman-machine-default','-u','root','--','sleep','infinity'

Start-Sleep -Seconds 3
"machine running: " + ((& $P machine list --format '{{.Running}}') -join '')
Stop-Transcript | Out-Null
FLEOF
}

win_prepare_wsl_ps1() {
  win_prepare_wsl_head
  if [ "$CONTAINERS" = "podman" ]; then
    win_prepare_wsl_podman_task
  fi
  printf "\nWrite-Output 'WSL_PREP_OK'\n"
}

win_prepare_wsl_head() {
  cat <<'PWEOF'
$ErrorActionPreference = 'Stop'
$base = 'C:\ProgramData\ec2-sandbox'
$ProgressPreference = 'SilentlyContinue'

# wsl.exe writes UTF-16LE, so its output reaches PowerShell as W\0S\0L\0... and a
# plain -match never fires. Every check of wsl output has to go through this.
function Wsl-Text([string[]]$wslArgs) {
  $prev = [Console]::OutputEncoding
  $prevEA = $ErrorActionPreference
  try {
    [Console]::OutputEncoding = [System.Text.Encoding]::Unicode
    # 'Continue' is essential. With ErrorActionPreference=Stop, 2>&1 turns anything
    # wsl.exe writes to stderr into a TERMINATING NativeCommandError -- so on a box
    # where WSL is not yet installed, probing for it threw instead of returning
    # false, and the script died before it could install WSL.
    $ErrorActionPreference = 'Continue'
    return (& wsl.exe @wslArgs 2>&1 | Out-String)
  } finally {
    [Console]::OutputEncoding = $prev
    $ErrorActionPreference = $prevEA
  }
}

function Test-ModernWsl { (Wsl-Text @('--version')) -match 'WSL version:' }

# Both engines need the modern WSL build: podman's machine image needs systemd,
# which only modern WSL provides, and Docker Engine needs its kernel modules.
# 'wsl --install' is a silent no-op on Server (it wants the Microsoft Store), so
# install the MSI directly.
if (-not (Test-ModernWsl)) {
  Write-Output 'installing WSL'
  $msi = "$env:TEMP\wsl.msi"
  if (-not (Test-Path $msi)) {
    Invoke-WebRequest -Uri 'https://github.com/microsoft/WSL/releases/download/2.7.12/wsl.2.7.12.0.x64.msi' -OutFile "$msi.part" -UseBasicParsing
    Move-Item "$msi.part" $msi -Force
  }
  Start-Process msiexec.exe -ArgumentList '/i', $msi, '/quiet', '/norestart' -Wait
}
if (-not (Test-ModernWsl)) { throw 'WSL is still not installed after running the MSI' }
wsl.exe --set-default-version 2 | Out-Null

PWEOF
}

# Only podman needs a per-user step. The machine itself is created at first logon,
# by the account that will use it -- machines are per Windows user and cannot be
# shared. Docker mode's daemon is machine-wide, so it registers nothing here.
win_prepare_wsl_podman_task() {
  sed -e "s|@@USER@@|${UNPRIV_USER_WINDOWS}|g" <<'PWPODMANEOF'

$act = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-ExecutionPolicy Bypass -NoProfile -WindowStyle Hidden -File $base\firstlogon.ps1"
$trg = New-ScheduledTaskTrigger -AtLogOn -User "$env:COMPUTERNAME\@@USER@@"
$prn = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\@@USER@@" -RunLevel Limited
Register-ScheduledTask -TaskName 'ec2-sandbox-podman' -Action $act -Trigger $trg -Principal $prn -Force | Out-Null
PWPODMANEOF
}

# EC2Launch v2 runs <powershell> user-data as SYSTEM, once, on first boot. Enabling
# the WSL2 features needs a reboot, so phase 1 registers a startup task that
# finishes the job on the way back up.
#
# Assembled from three parts rather than one heredoc so the engine-specific middle
# can vary without duplicating the ~150 shared lines around it. Shipping both
# engines' payloads unconditionally would also push user-data close to its 16 KB cap.
windows_user_data() { # $1 = public key
  win_user_data_head "$1"
  if [ "$CONTAINERS" = "podman" ]; then
    win_user_data_podman
  else
    win_user_data_docker
  fi
  win_user_data_tail
}

win_user_data_head() { # $1 = public key
  local b_prep
  b_prep=$(win_prepare_wsl_ps1 | b64gz)

  sed -e "s|@@PUBKEY@@|$1|g" \
      -e "s|@@USER@@|${UNPRIV_USER_WINDOWS}|g" \
      -e "s|@@B64_PREP@@|${b_prep}|g" <<'WINHEADEOF'
<powershell>
# Deliberately 'Continue', not 'Stop'. SSH is the only channel for diagnosing this
# machine, so a failure in a later step must never prevent sshd from coming up --
# on Server 2025 an aborted phase 1 left a box that was reachable only over RDP.
$ErrorActionPreference = 'Continue'
$base = 'C:\ProgramData\ec2-sandbox'
New-Item -ItemType Directory -Force -Path $base | Out-Null
# Whichever engine is installed puts its client here, and the tail adds it to PATH.
$bin = "$base\bin"
New-Item -ItemType Directory -Force -Path $bin | Out-Null
Start-Transcript -Path "$base\phase1.log" -Append

# --- OpenSSH, authorised with the launch key pair's public half ------------
# Two routes: the Windows capability, and failing that the standalone build. The
# capability needs Features-on-Demand to be reachable, which is not guaranteed.
try { Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0 | Out-Null }
catch { Write-Output "Add-WindowsCapability failed: $_" }

if (-not (Get-Service sshd -ErrorAction SilentlyContinue)) {
  Write-Output 'sshd absent after the capability install; using the standalone build'
  try {
    $m = "$env:TEMP\openssh.msi"
    Invoke-WebRequest -Uri 'https://github.com/PowerShell/Win32-OpenSSH/releases/download/10.0.0.0p2-Preview/OpenSSH-Win64-v10.0.0.0.msi' -OutFile "$m.part" -UseBasicParsing
    Move-Item "$m.part" $m -Force
    Start-Process msiexec.exe -ArgumentList '/i', $m, '/quiet', '/norestart' -Wait
  } catch { Write-Output "standalone OpenSSH install failed: $_" }
}

Set-Service -Name sshd -StartupType Automatic -ErrorAction SilentlyContinue
Start-Service sshd -ErrorAction SilentlyContinue

# Open port 22 explicitly. The Server 2022 AMI already had this rule, but Server
# 2025 does not, which left sshd running yet unreachable -- the service looks
# healthy and every connection still times out.
if (-not (Get-NetFirewallRule -Name 'sshd' -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -Name 'sshd' -DisplayName 'OpenSSH Server (sshd)' -Enabled True `
    -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22 | Out-Null
}
Write-Output ("sshd running: " + [bool](Get-Service sshd -ErrorAction SilentlyContinue | Where-Object Status -eq 'Running'))

New-Item -ItemType Directory -Force -Path 'C:\ProgramData\ssh' | Out-Null
$akf = 'C:\ProgramData\ssh\administrators_authorized_keys'
Set-Content -Path $akf -Value '@@PUBKEY@@' -Encoding ascii
# sshd silently ignores this file unless only Administrators and SYSTEM can write it.
icacls $akf /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null

# --- unprivileged account -------------------------------------------------
# Created with a throwaway secret. The real password arrives over SSH later, so it
# never appears in user-data.
$throwaway = ConvertTo-SecureString ([guid]::NewGuid().ToString() + 'aA1!') -AsPlainText -Force
if (-not (Get-LocalUser -Name '@@USER@@' -ErrorAction SilentlyContinue)) {
  New-LocalUser -Name '@@USER@@' -Password $throwaway -PasswordNeverExpires -AccountNeverExpires
}
Add-LocalGroupMember -Group 'Remote Desktop Users' -Member '@@USER@@' -ErrorAction SilentlyContinue

# --- helper scripts, shipped gzip+base64 to dodge nested quoting and stay
# --- inside the 16 KB user-data limit -------------------------------------
function Wr($b, $p) {
  $m = New-Object IO.MemoryStream(, [Convert]::FromBase64String($b))
  $g = New-Object IO.Compression.GZipStream($m, [IO.Compression.CompressionMode]::Decompress)
  $o = New-Object IO.MemoryStream; $g.CopyTo($o); [IO.File]::WriteAllBytes($p, $o.ToArray())
}
Wr '@@B64_PREP@@' "$base\prepare-wsl.ps1"

# Helper the host invokes over SSH. Reads the password from stdin so it never lands
# in a command line or the remote process table.
Set-Content -Path "$base\set-user-password.ps1" -Encoding ascii -Value @'
$pw = [Console]::In.ReadToEnd().Trim()
$sec = ConvertTo-SecureString $pw -AsPlainText -Force
Set-LocalUser -Name "@@USER@@" -Password $sec
'@
WINHEADEOF
}

win_user_data_podman() {
  local b_firstlogon
  b_firstlogon=$(win_firstlogon_ps1 | b64gz)

  sed -e "s|@@B64_FIRSTLOGON@@|${b_firstlogon}|g" <<'WINPODMANEOF'
Wr '@@B64_FIRSTLOGON@@' "$base\firstlogon.ps1"

# --- Podman: the container engine -----------------------------------------
# ALLUSERS=1 matters: the installer defaults to a per-user install, which would
# put podman.exe in SYSTEM's profile where the unprivileged account cannot see it.
$msi = "$env:TEMP\podman.msi"
Invoke-WebRequest -Uri 'https://github.com/podman-container-tools/podman/releases/download/v6.1.0/podman-installer-windows-amd64.msi' -OutFile "$msi.part" -UseBasicParsing
Move-Item "$msi.part" $msi -Force
Start-Process msiexec.exe -ArgumentList '/i', $msi, '/quiet', '/norestart', 'ALLUSERS=1' -Wait

# Present Podman as 'docker'. Tools such as the LocalStack CLI shell out to a
# binary called docker, and Podman -- unlike a plain Linux daemon -- translates
# Windows volume paths (C:\Users\...) client-side, which is what makes bind
# mounts from Windows work at all.
Copy-Item 'C:\Program Files\Podman\podman.exe' "$bin\docker.exe" -Force
WINPODMANEOF
}

win_user_data_docker() {
  sed -e "s|@@CLI_VERSION@@|${DOCKER_CLI_VERSION}|g" \
      -e "s|@@COMPOSE_VERSION@@|${DOCKER_COMPOSE_VERSION}|g" \
      -e "s|@@BUILDX_VERSION@@|${DOCKER_BUILDX_VERSION}|g" \
      -e "s|@@ROOTFS_URL@@|${WSL_ROOTFS_URL}|g" <<'WINDOCKEREOF'
# --- Docker: the Windows client only --------------------------------------
# The daemon itself runs in a WSL2 distro, which phase 2 builds -- WSL cannot
# register a distro from the SYSTEM account that user-data runs under. What phase 1
# can do is everything that needs no WSL: the client, its plugins, and the download.
try {
  $zip = "$env:TEMP\docker-cli.zip"
  Invoke-WebRequest -Uri 'https://download.docker.com/win/static/stable/x86_64/docker-@@CLI_VERSION@@.zip' -OutFile "$zip.part" -UseBasicParsing
  Move-Item "$zip.part" $zip -Force
  # The archive also carries dockerd.exe, the Windows-containers daemon, which is
  # of no use here -- take only the client.
  Expand-Archive -Path $zip -DestinationPath "$env:TEMP\docker-cli" -Force
  Copy-Item "$env:TEMP\docker-cli\docker\docker.exe" "$bin\docker.exe" -Force
} catch { Write-Output "docker CLI install failed: $_" }

# '$env:ProgramFiles\Docker\cli-plugins' is the only system-wide plugin directory
# the Windows CLI searches; '~\.docker\cli-plugins' would only serve one account.
# buildx is not optional: BuildKit is the default builder, so without it
# 'docker build' falls back to a classic builder that is on its way out.
$plugins = "$env:ProgramFiles\Docker\cli-plugins"
New-Item -ItemType Directory -Force -Path $plugins | Out-Null
try {
  Invoke-WebRequest -Uri 'https://github.com/docker/compose/releases/download/@@COMPOSE_VERSION@@/docker-compose-windows-x86_64.exe' -OutFile "$plugins\docker-compose.exe" -UseBasicParsing
} catch { Write-Output "compose plugin install failed: $_" }
try {
  Invoke-WebRequest -Uri 'https://github.com/docker/buildx/releases/download/@@BUILDX_VERSION@@/buildx-@@BUILDX_VERSION@@.windows-amd64.exe' -OutFile "$plugins\docker-buildx.exe" -UseBasicParsing
} catch { Write-Output "buildx plugin install failed: $_" }

# Fetch the distro image now, while phase 2 is still a reboot away. curl.exe is
# in-box on Server 2025 and can resume a partial transfer; Invoke-WebRequest would
# buffer all 390 MB in memory and has to start over on a dropped connection.
try {
  & curl.exe -fL --retry 3 --retry-delay 5 -C - -o "$base\rootfs.wsl.part" '@@ROOTFS_URL@@'
  # Only promote a complete download. A partial file left as .part is what lets
  # phase 2 resume it; renamed, it would just fail the checksum there.
  if ($LASTEXITCODE -eq 0) { Move-Item "$base\rootfs.wsl.part" "$base\rootfs.wsl" -Force }
  else { Write-Output "rootfs download incomplete (curl $LASTEXITCODE); phase 2 will resume it" }
} catch { Write-Output "rootfs download failed: $_" }
WINDOCKEREOF
}

win_user_data_tail() {
  cat <<'WINTAILEOF'

# --- the engine's client on the machine PATH ------------------------------
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
if ($machinePath -notlike "*$bin*") {
  [Environment]::SetEnvironmentVariable('Path', "$machinePath;$bin", 'Machine')
}

# --- AWS CLI, Terraform and SAM CLI ---------------------------------------
# All three system-wide: the MSIs install into Program Files and add themselves to
# the machine PATH, and terraform.exe lands in $bin, which the block above already
# put there. AWS publishes a per-user MSI too (AWSCLIV2-User.msi) -- that one would
# install into SYSTEM's profile and leave both real accounts without the CLI.
try {
  $m = "$env:TEMP\awscliv2.msi"
  Invoke-WebRequest -Uri 'https://awscli.amazonaws.com/AWSCLIV2.msi' -OutFile "$m.part" -UseBasicParsing
  Move-Item "$m.part" $m -Force
  Start-Process msiexec.exe -ArgumentList '/i', $m, '/qn', '/norestart' -Wait
} catch { Write-Output "AWS CLI install failed: $_" }

try {
  $m = "$env:TEMP\sam-cli.msi"
  Invoke-WebRequest -Uri 'https://github.com/aws/aws-sam-cli/releases/latest/download/AWS_SAM_CLI_64_PY3.msi' -OutFile "$m.part" -UseBasicParsing
  Move-Item "$m.part" $m -Force
  Start-Process msiexec.exe -ArgumentList '/i', $m, '/qn', '/norestart' -Wait
  # 'sam build' walks deep dependency trees and trips over MAX_PATH without this.
  New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\FileSystem' -Name 'LongPathsEnabled' -Value 1 -PropertyType DWORD -Force | Out-Null
} catch { Write-Output "SAM CLI install failed: $_" }

# Terraform is a bare zip with no 'latest' alias, so ask HashiCorp's own version
# endpoint which release is current. Reading the release index instead would pick
# up the alpha builds, which sort above the stable one.
try {
  $tfVersion = (Invoke-RestMethod -Uri 'https://checkpoint-api.hashicorp.com/v1/check/terraform').current_version
  $zip = "$env:TEMP\terraform.zip"
  Invoke-WebRequest -Uri "https://releases.hashicorp.com/terraform/$tfVersion/terraform_${tfVersion}_windows_amd64.zip" -OutFile "$zip.part" -UseBasicParsing
  Move-Item "$zip.part" $zip -Force
  Expand-Archive -Path $zip -DestinationPath "$env:TEMP\terraform" -Force
  Copy-Item "$env:TEMP\terraform\terraform.exe" "$bin\terraform.exe" -Force
} catch { Write-Output "Terraform install failed: $_" }

# --- VS Code: system installer, so both accounts get it -------------------
$vs = "$env:TEMP\vscode-setup.exe"
Invoke-WebRequest -Uri 'https://update.code.visualstudio.com/latest/win32-x64/stable' -OutFile $vs -UseBasicParsing
Start-Process -FilePath $vs -Wait -ArgumentList '/VERYSILENT','/NORESTART','/MERGETASKS=!runcode,addcontextmenufiles,addcontextmenufolders,addtopath'

# Desktop shortcut on the PUBLIC desktop, so every account sees it rather than
# just whichever profile happened to run the installer.
$code = 'C:\Program Files\Microsoft VS Code\Code.exe'
if (Test-Path $code) {
  $ws = New-Object -ComObject WScript.Shell
  $lnk = $ws.CreateShortcut("$env:PUBLIC\Desktop\Visual Studio Code.lnk")
  $lnk.TargetPath = $code
  $lnk.WorkingDirectory = Split-Path $code
  $lnk.IconLocation = "$code,0"
  $lnk.Description = 'Visual Studio Code'
  $lnk.Save()
}

# --- WSL2 prerequisites (require a reboot) --------------------------------
Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux -NoRestart -All
Enable-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform -NoRestart -All

# The WSL preparation itself runs later, as Administrator over SSH: WSL cannot
# register a distro from the SYSTEM account that user-data runs under.
New-Item -ItemType File -Force -Path "$base\phase1-ready" | Out-Null

Stop-Transcript
Restart-Computer -Force
</powershell>
WINTAILEOF
}

# ---------------------------------------------------------------------------
# Docker Engine in WSL2 (windows, --containers=docker)
#
# The chain a Windows client talks through:
#
#   lstk / docker.exe -> \\.\pipe\docker_engine   (pipe-relay.ps1, Windows)
#                     -> tcp://127.0.0.1:PORT     (WSL2 localhost forwarding)
#                     -> dockerproxy.py           (in the distro, rewrites paths)
#                     -> /var/run/docker.sock     (dockerd)
#
# The pipe is what Docker Desktop serves, so both docker.exe and lstk find the
# daemon with no configuration, and lstk's npipe branch -- which maps the endpoint
# to /var/run/docker.sock for containers that need the socket bind-mounted -- stays
# correct, which a tcp:// DOCKER_HOST would quietly break.
#
# Everything here is generated on the host, copied over with scp and run over SSH.
# It is deliberately not shipped in user-data: user-data is capped at 16 KB and
# these five files do not fit beside the rest of phase 1.
# ---------------------------------------------------------------------------

# Runs inside the distro as root, once, to turn a stock Ubuntu rootfs into a
# Docker host. Idempotent: re-running it after a dropped SSH session is a no-op.
wsl_provision_sh() {
  cat <<'PROVEOF'
#!/bin/sh
set -eu
export DEBIAN_FRONTEND=noninteractive
base=/mnt/c/ProgramData/ec2-sandbox

if ! command -v dockerd >/dev/null 2>&1; then
  echo "installing docker packages"
  apt-get update
  apt-get install -y ca-certificates curl python3 iptables
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

# Docker Engine speaks iptables, and the WSL kernel ships every netfilter backend
# as a module while Ubuntu 24.04 defaults to the nftables compatibility layer. Which
# of the two actually works depends on what modprobe can load, so probe instead of
# assuming, and fail loudly rather than leaving a daemon that cannot publish a port.
if ! iptables -t nat -nL >/dev/null 2>&1; then
  echo "switching to the legacy iptables backend"
  update-alternatives --set iptables /usr/sbin/iptables-legacy
  update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy
fi
iptables -t nat -nL >/dev/null 2>&1 || { echo "iptables is unusable in this distro"; exit 1; }

# userland-proxy must stay on. WSL forwards a published port to Windows only when
# something in the distro is really listening on it, and with the proxy disabled
# publishing is pure iptables DNAT, which listens on nothing -- ports would simply
# not appear on the Windows side. 'hosts' is deliberately absent: dockerd refuses to
# start when a directive is given both here and as a flag, and the flags win.
mkdir -p /etc/docker
cat > /etc/docker/daemon.json <<'JSON'
{
  "userland-proxy": true
}
JSON

# systemd stays off: dockerd is supervised by the wsl.exe client that keeps this
# distro alive, and enabling systemd hands /etc/resolv.conf to systemd-resolved,
# whose 127.0.0.53 stub dockerd refuses to forward to.
cat > /etc/wsl.conf <<'CONF'
[boot]
systemd=false

[user]
default=root
CONF
# 'wsl --import' keeps the image's own default-user setting, which can name a uid
# that does not exist in a distro nobody ran the first-boot setup for.
rm -f /etc/wsl-distribution.conf

install -m 0755 "$base/dockerproxy.py"        /usr/local/bin/dockerproxy.py
install -m 0755 "$base/dockerd-supervisor.sh" /usr/local/bin/dockerd-supervisor.sh

echo PROVISION_OK
PROVEOF
}

# The scheduled task's foreground process. This wsl.exe client is also what holds
# the distro open -- WSL shuts a distro down once its last client exits -- so it
# must never return.
wsl_supervisor_sh() {
  sed -e "s|@@PORT@@|${DOCKER_PROXY_PORT}|g" <<'SUPEOF'
#!/bin/sh
exec >>/var/log/ec2-sandbox-dockerd.log 2>&1
echo "=== supervisor start $(date -u +%FT%TZ)"

# Each in its own restart loop, so a crash in one does not take the other with it.
( while :; do /usr/bin/dockerd -H unix:///var/run/docker.sock; echo "dockerd exited"; sleep 2; done ) &

while :; do
  /usr/bin/python3 /usr/local/bin/dockerproxy.py --port @@PORT@@
  echo "proxy exited"
  sleep 2
done
SUPEOF
}

wsl_dockerproxy_py() {
  cat <<'PROXYEOF'
#!/usr/bin/env python3
"""Docker API proxy that rewrites Windows bind-mount paths.

'docker run -v C:\\dir:/x' works under Docker Desktop because Desktop's backend
rewrites the mount source before the request reaches dockerd. That is why it works
for API clients -- lstk among them -- and not only for the CLI. Plain Docker Engine
has no such thing: a Linux daemon reads 'C:\\dir' as a volume name and rejects the
request. This proxy supplies that one missing piece and nothing else.

Everything other than the mount sources of POST /containers/create is relayed
byte-for-byte, including hijacked connections (attach, exec, interactive run) and
streaming responses (pull progress, logs -f).
"""

import argparse
import asyncio
import json
import re
import sys

SOCKET = "/var/run/docker.sock"
LIMIT = 4 * 1024 * 1024

# Matches an absolute Windows path, with or without the \\?\ long-path prefix.
_WIN_ABS = re.compile(r"^(?:\\\\\?\\)?([A-Za-z]):[\\/]")
_CREATE = re.compile(r"^(?:/v[0-9.]+)?/containers/create(?:\?|$)")


def to_linux(path):
    m = _WIN_ABS.match(path)
    if not m:
        return path
    return "/mnt/" + m.group(1).lower() + "/" + path[m.end():].replace("\\", "/")


def rewrite_bind(spec):
    """Rewrite the source of a 'src:dst[:opts]' bind spec.

    The source's own drive colon means the first colon is not the separator, so
    scan from the end of the drive prefix instead of splitting naively.
    """
    m = _WIN_ABS.match(spec)
    if not m:
        return spec
    sep = spec.find(":", m.end())
    if sep < 0:
        return spec
    return to_linux(spec[:sep]) + spec[sep:]


def rewrite_body(raw):
    body = json.loads(raw)
    host = body.get("HostConfig")
    if not isinstance(host, dict):
        return raw
    binds = host.get("Binds")
    if isinstance(binds, list):
        host["Binds"] = [rewrite_bind(b) if isinstance(b, str) else b for b in binds]
    mounts = host.get("Mounts")
    if isinstance(mounts, list):
        for mount in mounts:
            if isinstance(mount, dict) and mount.get("Type") == "bind" \
                    and isinstance(mount.get("Source"), str):
                mount["Source"] = to_linux(mount["Source"])
    return json.dumps(body).encode()


def parse_head(head):
    lines = head.split(b"\r\n")
    start = lines[0].decode("latin-1")
    headers = {}
    for line in lines[1:]:
        if not line or b":" not in line:
            continue
        name, _, value = line.partition(b":")
        headers[name.decode("latin-1").lower()] = value.strip().decode("latin-1")
    return start, headers


async def copy_stream(reader, writer):
    try:
        while True:
            chunk = await reader.read(65536)
            if not chunk:
                break
            writer.write(chunk)
            await writer.drain()
    except (ConnectionError, asyncio.IncompleteReadError):
        pass
    finally:
        try:
            writer.close()
        except Exception:
            pass


async def relay_chunked(reader, writer):
    while True:
        line = await reader.readline()
        if not line:
            return
        writer.write(line)
        size = int(line.strip().split(b";")[0] or b"0", 16)
        if size == 0:
            # Trailers, then the blank line that ends the message.
            while True:
                trailer = await reader.readline()
                writer.write(trailer)
                await writer.drain()
                if trailer in (b"\r\n", b"\n", b""):
                    return
        data = await reader.readexactly(size + 2)
        writer.write(data)
        await writer.drain()


async def relay_body(reader, writer, headers):
    if headers.get("transfer-encoding", "").lower() == "chunked":
        await relay_chunked(reader, writer)
    elif headers.get("content-length"):
        remaining = int(headers["content-length"])
        while remaining > 0:
            chunk = await reader.read(min(65536, remaining))
            if not chunk:
                return
            remaining -= len(chunk)
            writer.write(chunk)
            await writer.drain()


async def handle(client_reader, client_writer):
    try:
        up_reader, up_writer = await asyncio.open_unix_connection(SOCKET, limit=LIMIT)
    except OSError as exc:
        # The daemon is still starting, or has crashed and is being restarted.
        client_writer.write(b"HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\n\r\n")
        await client_writer.drain()
        client_writer.close()
        print("upstream unavailable: %s" % exc, file=sys.stderr)
        return

    try:
        while True:
            try:
                head = await client_reader.readuntil(b"\r\n\r\n")
            except (asyncio.IncompleteReadError, asyncio.LimitOverrunError):
                return
            start, headers = parse_head(head)
            parts = start.split(" ")
            path = parts[1] if len(parts) > 2 else ""

            if parts[0] == "POST" and _CREATE.match(path) and headers.get("content-length"):
                raw = await client_reader.readexactly(int(headers["content-length"]))
                try:
                    new = rewrite_body(raw)
                except Exception as exc:
                    print("rewrite skipped: %s" % exc, file=sys.stderr)
                    new = raw
                head = re.sub(
                    rb"(?i)\r\ncontent-length:[^\r\n]*",
                    b"\r\nContent-Length: %d" % len(new),
                    head,
                )
                up_writer.write(head + new)
                await up_writer.drain()
            else:
                up_writer.write(head)
                await up_writer.drain()
                await relay_body(client_reader, up_writer, headers)

            try:
                resp = await up_reader.readuntil(b"\r\n\r\n")
            except (asyncio.IncompleteReadError, asyncio.LimitOverrunError):
                return
            resp_start, resp_headers = parse_head(resp)
            client_writer.write(resp)
            await client_writer.drain()

            status = resp_start.split(" ")[1] if len(resp_start.split(" ")) > 1 else ""
            framed = resp_headers.get("content-length") or \
                resp_headers.get("transfer-encoding", "").lower() == "chunked"
            if status == "101" or not framed:
                # A hijacked connection (attach, exec, interactive run) or a body
                # that ends at EOF. Either way the framing is gone: pump raw bytes
                # both ways until one side hangs up.
                await asyncio.gather(
                    copy_stream(client_reader, up_writer),
                    copy_stream(up_reader, client_writer),
                )
                return
            await relay_body(up_reader, client_writer, resp_headers)
    except (ConnectionError, asyncio.IncompleteReadError):
        pass
    finally:
        for w in (up_writer, client_writer):
            try:
                w.close()
            except Exception:
                pass


async def main():
    global SOCKET
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=2375)
    # Only the tests point this anywhere else, but it is also the quickest way to
    # run the proxy against a second daemon while debugging.
    ap.add_argument("--socket", default=SOCKET)
    args = ap.parse_args()
    SOCKET = args.socket
    # Loopback only. Windows reaches it through WSL2's localhost forwarding; nothing
    # outside the machine can.
    server = await asyncio.start_server(handle, "127.0.0.1", args.port, limit=LIMIT)
    print("listening on 127.0.0.1:%d -> %s" % (args.port, SOCKET), file=sys.stderr)
    async with server:
        await server.serve_forever()


if __name__ == "__main__":
    asyncio.run(main())
PROXYEOF
}

# Serves \\.\pipe\docker_engine on the Windows side and relays to the proxy inside
# the distro. Compiled on the box with Add-Type, which uses the in-box .NET
# Framework compiler, so there is no toolchain to install.
#
# It is a byte relay and nothing more: all the protocol awareness lives in
# dockerproxy.py, where JSON can be parsed properly.
win_pipe_relay_ps1() {
  sed -e "s|@@PORT@@|${DOCKER_PROXY_PORT}|g" <<'RELAYEOF'
$ErrorActionPreference = 'Stop'

Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.IO.Pipes;
using System.Net.Sockets;
using System.Security.AccessControl;
using System.Security.Principal;
using System.Threading;

public class DockerPipeRelay {
  const string PipeName = "docker_engine";
  // Acceptors sit blocked in WaitForConnection, so several can listen at once.
  // Without that there is a window after each accept in which the pipe does not
  // exist and a connecting client fails outright instead of queueing.
  const int Acceptors = 16;
  static int _port;
  static string _log;

  public static void Run(int port, string logPath) {
    _port = port;
    _log = logPath;
    Log("relay starting: \\\\.\\pipe\\" + PipeName + " -> 127.0.0.1:" + port);
    for (int i = 0; i < Acceptors; i++) {
      Thread t = new Thread(new ThreadStart(AcceptLoop));
      t.IsBackground = true;
      t.Start();
    }
    Thread.Sleep(Timeout.Infinite);
  }

  static void Log(string message) {
    try {
      File.AppendAllText(_log, DateTime.UtcNow.ToString("s") + " " + message + Environment.NewLine);
    } catch { }
  }

  static PipeSecurity Security() {
    PipeSecurity ps = new PipeSecurity();
    // Both sandbox accounts have to reach the daemon. Docker access is
    // root-equivalent, which is the same bargain the Linux sandbox makes by
    // putting its unprivileged user in the docker group.
    ps.AddAccessRule(new PipeAccessRule(
      new SecurityIdentifier(WellKnownSidType.AuthenticatedUserSid, null),
      PipeAccessRights.ReadWrite, AccessControlType.Allow));
    ps.AddAccessRule(new PipeAccessRule(
      new SecurityIdentifier(WellKnownSidType.BuiltinAdministratorsSid, null),
      PipeAccessRights.FullControl, AccessControlType.Allow));
    ps.AddAccessRule(new PipeAccessRule(
      new SecurityIdentifier(WellKnownSidType.LocalSystemSid, null),
      PipeAccessRights.FullControl, AccessControlType.Allow));
    return ps;
  }

  static void AcceptLoop() {
    while (true) {
      NamedPipeServerStream pipe = null;
      try {
        pipe = new NamedPipeServerStream(PipeName, PipeDirection.InOut,
          NamedPipeServerStream.MaxAllowedServerInstances, PipeTransmissionMode.Byte,
          PipeOptions.Asynchronous, 65536, 65536, Security());
        pipe.WaitForConnection();
      } catch (Exception e) {
        Log("accept failed: " + e.Message);
        if (pipe != null) { try { pipe.Dispose(); } catch { } }
        Thread.Sleep(1000);
        continue;
      }
      // Hand the connection off so this acceptor goes straight back to listening;
      // an interactive 'docker run -it' holds its connection for the container's
      // whole life and must not occupy an acceptor.
      ThreadPool.QueueUserWorkItem(new WaitCallback(Serve), pipe);
    }
  }

  static void Serve(object state) {
    NamedPipeServerStream pipe = (NamedPipeServerStream)state;
    TcpClient tcp = null;
    try {
      tcp = new TcpClient();
      tcp.Connect("127.0.0.1", _port);
      tcp.NoDelay = true;
      NetworkStream net = tcp.GetStream();
      Stream from = pipe;
      Thread up = new Thread(new ThreadStart(delegate { Copy(from, net); }));
      up.IsBackground = true;
      up.Start();
      Copy(net, pipe);
      up.Join(2000);
    } catch (Exception e) {
      Log("connection failed: " + e.Message);
    } finally {
      try { pipe.Dispose(); } catch { }
      if (tcp != null) { try { tcp.Close(); } catch { } }
    }
  }

  static void Copy(Stream from, Stream to) {
    byte[] buffer = new byte[65536];
    try {
      int n;
      while ((n = from.Read(buffer, 0, buffer.Length)) > 0) {
        to.Write(buffer, 0, n);
        to.Flush();
      }
    } catch { }
    // Close so the peer sees EOF rather than waiting on a half-dead connection.
    try { to.Close(); } catch { }
  }
}
'@

[DockerPipeRelay]::Run(@@PORT@@, 'C:\ProgramData\ec2-sandbox\pipe-relay.log')
RELAYEOF
}

# Phase 2, part one: build the distro. Runs over SSH as Administrator, and every
# step is skipped when it has already happened, because the SSH session can drop
# mid-run and the caller simply reconnects and runs it again.
win_prepare_docker_ps1() {
  sed -e "s|@@DISTRO@@|${WSL_DISTRO}|g" \
      -e "s|@@ROOTFS_URL@@|${WSL_ROOTFS_URL}|g" \
      -e "s|@@ROOTFS_SHA256@@|${WSL_ROOTFS_SHA256}|g" <<'PREPDOCKEREOF'
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$base = 'C:\ProgramData\ec2-sandbox'
Start-Transcript -Path "$base\phase2.log" -Append | Out-Null

# wsl.exe writes UTF-16LE, so its output reaches PowerShell as W\0S\0L\0... and a
# plain -match never fires. Same helper as prepare-wsl.ps1; the two scripts are
# delivered separately and cannot share it.
function Wsl-Text([string[]]$wslArgs) {
  $prev = [Console]::OutputEncoding
  $prevEA = $ErrorActionPreference
  try {
    [Console]::OutputEncoding = [System.Text.Encoding]::Unicode
    $ErrorActionPreference = 'Continue'
    return (& wsl.exe @wslArgs 2>&1 | Out-String)
  } finally {
    [Console]::OutputEncoding = $prev
    $ErrorActionPreference = $prevEA
  }
}

$distro = '@@DISTRO@@'
$rootfs = "$base\rootfs.wsl"
$root = "$base\wsl\docker"

# Phase 1 normally fetched this before the reboot; do it here if that failed.
if (-not (Test-Path $rootfs)) {
  Write-Output 'downloading the distro image'
  & curl.exe -fL --retry 3 --retry-delay 5 -C - -o "$rootfs.part" '@@ROOTFS_URL@@'
  if ($LASTEXITCODE -ne 0) { throw "downloading the distro image failed with $LASTEXITCODE" }
  Move-Item "$rootfs.part" $rootfs -Force
}
$sha = (Get-FileHash -Path $rootfs -Algorithm SHA256).Hash.ToLower()
if ($sha -ne '@@ROOTFS_SHA256@@') { throw "distro image checksum mismatch: $sha" }

# --import rather than 'wsl --install -d Ubuntu-24.04': it pins the image, fixes
# the distro name, skips the first-boot setup and leaves root as the default user.
if ((Wsl-Text @('-l', '-q')) -notmatch [regex]::Escape($distro)) {
  Write-Output 'importing the distro'
  New-Item -ItemType Directory -Force -Path $root | Out-Null
  & wsl.exe --import $distro $root $rootfs --version 2
  if ($LASTEXITCODE -ne 0) { throw "wsl --import failed with $LASTEXITCODE" }
}

Write-Output 'provisioning docker inside the distro'
# 'Continue' while a native command's stderr is redirected: under 'Stop', 2>&1
# turns anything the child writes to stderr into a terminating NativeCommandError,
# so apt's ordinary progress chatter would abort the run. Same trap as Wsl-Text.
$ErrorActionPreference = 'Continue'
$out = (& wsl.exe -d $distro -u root -- /bin/sh /mnt/c/ProgramData/ec2-sandbox/provision-docker.sh 2>&1 | Out-String)
$ErrorActionPreference = 'Stop'
foreach ($line in ($out -split "`n")) {
  if ($line -match '\S') { Write-Output ("  " + $line.Trim()) }
}
if ($out -notmatch 'PROVISION_OK') { throw 'provisioning the distro failed' }

Stop-Transcript | Out-Null
Write-Output 'DOCKER_IMPORT_OK'
PREPDOCKEREOF
}

# Phase 2, part two: register the services and prove the whole chain works. Takes
# the Administrator password on stdin, like set-user-password.ps1, so it never
# reaches a command line or the remote process table.
#
# The tasks are not a convenience for reboots alone. Win32-OpenSSH runs each SSH
# command inside a job object that forbids breakaway, so anything this script
# started directly would be killed the moment the SSH command returned. Task
# Scheduler is what puts the daemon outside that job.
win_register_docker_ps1() {
  sed -e "s|@@DISTRO@@|${WSL_DISTRO}|g" \
      -e "s|@@PORT@@|${DOCKER_PROXY_PORT}|g" <<'REGDOCKEREOF'
$pw = [Console]::In.ReadToEnd().Trim()
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$base = 'C:\ProgramData\ec2-sandbox'
$distro = '@@DISTRO@@'
$docker = "$base\bin\docker.exe"

# The default execution time limit is three days, which would eventually kill the
# daemon; IgnoreNew stops a second instance if the task is ever triggered twice.
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) `
  -MultipleInstances IgnoreNew -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
  -StartWhenAvailable -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)

# HNS and the vmcompute stack are still settling immediately after boot.
function New-StartupTrigger {
  $t = New-ScheduledTaskTrigger -AtStartup
  $t.Delay = 'PT30S'
  return $t
}

# dockerd has to run as the account whose WSL registration owns the distro: those
# live in HKCU, so SYSTEM and Windows services read a different hive and would
# report "no distribution with the supplied name". Task Scheduler loads the user
# profile, which is exactly why this works where a service would not.
$dockerAction = New-ScheduledTaskAction -Execute 'C:\Windows\System32\wsl.exe' `
  -Argument "-d $distro -u root -- /usr/local/bin/dockerd-supervisor.sh"
$admin = "$env:COMPUTERNAME\Administrator"
try {
  # Task Scheduler stores this credential on the box. On a throwaway sandbox it
  # discloses nothing new: the same password is already recoverable from EC2 by
  # anyone holding the launch key pair.
  Register-ScheduledTask -TaskName 'ec2-sandbox-dockerd' -Action $dockerAction `
    -Trigger (New-StartupTrigger) -Settings $settings -RunLevel Highest `
    -User $admin -Password $pw -Force | Out-Null
} catch {
  Write-Output "password registration failed ($_); falling back to S4U"
  $prn = New-ScheduledTaskPrincipal -UserId $admin -LogonType S4U -RunLevel Highest
  Register-ScheduledTask -TaskName 'ec2-sandbox-dockerd' -Action $dockerAction `
    -Trigger (New-StartupTrigger) -Settings $settings -Principal $prn -Force | Out-Null
}

# The relay only needs a named pipe and a loopback connect -- no WSL, so no user
# hive, so no stored credential.
$relayAction = New-ScheduledTaskAction -Execute 'powershell.exe' `
  -Argument "-ExecutionPolicy Bypass -NoProfile -WindowStyle Hidden -File $base\pipe-relay.ps1"
$relayPrincipal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName 'ec2-sandbox-pipe-relay' -Action $relayAction `
  -Trigger (New-StartupTrigger) -Settings $settings -Principal $relayPrincipal -Force | Out-Null

Write-Output 'starting the daemon'
Start-ScheduledTask -TaskName 'ec2-sandbox-dockerd'
Start-ScheduledTask -TaskName 'ec2-sandbox-pipe-relay'

# From here on every command is a native one whose stderr is captured, and under
# 'Stop' a 2>&1 redirect turns that stderr into a terminating NativeCommandError --
# so a daemon that is merely still starting would abort the run instead of being
# retried. The checks below throw explicitly instead.
$ErrorActionPreference = 'Continue'

# Test-Path is unreliable on the pipe filesystem; enumerating it is not.
function Test-DockerPipe {
  try { return [bool](@([System.IO.Directory]::GetFiles('\\.\pipe\')) -match 'docker_engine$') }
  catch { return $false }
}

$deadline = (Get-Date).AddMinutes(5)
while (-not (Test-DockerPipe) -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 3 }
if (-not (Test-DockerPipe)) { throw 'the docker_engine pipe never appeared' }

# One smoke test for the whole chain rather than several for its parts: a
# published port exercises iptables, docker-proxy and WSL's localhost forwarding,
# and a Windows-path mount exercises the rewriting proxy -- which is the entire
# reason this arrangement exists.
Write-Output 'smoke test: talking to the daemon'
$version = ''
$versionDeadline = (Get-Date).AddMinutes(3)
while ((Get-Date) -lt $versionDeadline) {
  $version = (& $docker version --format '{{.Server.Version}}' 2>&1 | Out-String).Trim()
  if ($LASTEXITCODE -eq 0) { break }
  Start-Sleep -Seconds 5
}
if ($LASTEXITCODE -ne 0) { throw "docker version failed: $version" }
Write-Output ("  server " + $version)

& $docker rm -f ec2-sandbox-probe 2>&1 | Out-Null
Write-Output 'smoke test: published port'
& $docker run -d --rm -p 18080:80 --name ec2-sandbox-probe nginx:alpine 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'could not start the probe container' }
$reachable = $false
$portDeadline = (Get-Date).AddMinutes(2)
while (-not $reachable -and (Get-Date) -lt $portDeadline) {
  try {
    Invoke-WebRequest -Uri 'http://127.0.0.1:18080' -UseBasicParsing -TimeoutSec 5 | Out-Null
    $reachable = $true
  } catch { Start-Sleep -Seconds 3 }
}
& $docker rm -f ec2-sandbox-probe 2>&1 | Out-Null
if (-not $reachable) { throw 'a published container port was not reachable from Windows' }

Write-Output 'smoke test: windows path bind mount'
$mounted = (& $docker run --rm -v C:\ProgramData\ec2-sandbox:/probe nginx:alpine ls /probe/pipe-relay.ps1 2>&1 | Out-String)
if ($mounted -notmatch 'pipe-relay') { throw "a Windows path bind mount failed: $mounted" }

Write-Output 'DOCKER_PREP_OK'
REGDOCKEREOF
}

# Drives phase 2 from the host. Split from prepare_windows_wsl because only the
# docker engine needs it, and because the second call has to feed a password in.
prepare_windows_docker() { # $1 = public ip, $2 = admin password
  set_ssh_opts
  local dir="${STATE_DIR}/docker"
  local attempt out

  mkdir -p "$dir"
  chmod 700 "$dir" 2>/dev/null || true
  wsl_provision_sh        > "${dir}/provision-docker.sh"
  wsl_supervisor_sh       > "${dir}/dockerd-supervisor.sh"
  wsl_dockerproxy_py      > "${dir}/dockerproxy.py"
  win_pipe_relay_ps1      > "${dir}/pipe-relay.ps1"
  win_prepare_docker_ps1  > "${dir}/prepare-docker.ps1"
  win_register_docker_ps1 > "${dir}/register-docker-tasks.ps1"

  # scp rather than more base64 blobs: these files run to hundreds of lines, and
  # the Linux ones must keep their LF endings byte for byte.
  log "Copying the Docker Engine helpers to the instance..."
  scp "${SSH_OPTS[@]}" "${dir}"/* "${SSH_USER}@$1:C:/ProgramData/ec2-sandbox/" >/dev/null 2>&1 \
    || die "could not copy the Docker Engine helpers to ${1}"

  for attempt in 1 2 3; do
    if [ "$attempt" -eq 1 ]; then
      log "Building the Docker Engine distro (about 6 minutes)..."
    else
      log "Connection dropped; resuming the distro build (attempt ${attempt}/3)..."
      sleep 20
    fi

    out=$(ssh "${SSH_OPTS[@]}" "${SSH_USER}@$1" \
            'powershell -ExecutionPolicy Bypass -NoProfile -File C:\ProgramData\ec2-sandbox\prepare-docker.ps1' 2>&1 \
          | tr -d '\000\r') || true

    printf '%s\n' "$out" \
      | grep -E '^(downloading|importing|provisioning|installing|switching|DOCKER_IMPORT_OK)' \
      | sed 's/^/    /' >&2 || true

    if printf '%s' "$out" | grep -q 'DOCKER_IMPORT_OK'; then
      ok "Docker Engine installed in the WSL2 distro"
      break
    fi

    if [ "$attempt" -eq 3 ]; then
      warn "the distro build did not succeed after 3 attempts. Last output:"
      printf '%s\n' "$out" | sed 's/^/    /' >&2
      die "could not build the Docker Engine distro on the instance.
  Every step is resumable, so retry by hand:
    ssh -i ${PEM_PATH} ${SSH_USER}@$1 \\
      \"powershell -ExecutionPolicy Bypass -File C:\\ProgramData\\ec2-sandbox\\prepare-docker.ps1\""
    fi
  done

  log "Registering the Docker services and running a smoke test..."
  out=$(printf '%s' "$2" \
        | ssh "${SSH_OPTS[@]}" "${SSH_USER}@$1" \
            'powershell -ExecutionPolicy Bypass -NoProfile -File C:\ProgramData\ec2-sandbox\register-docker-tasks.ps1' 2>&1 \
        | tr -d '\000\r') || true

  printf '%s\n' "$out" \
    | grep -E '^(starting|smoke test|password registration|  )' \
    | sed 's/^/    /' >&2 || true

  if ! printf '%s' "$out" | grep -q 'DOCKER_PREP_OK'; then
    warn "Docker did not pass its smoke test. Last output:"
    printf '%s\n' "$out" | sed 's/^/    /' >&2
    die "Docker did not come up on the instance.
  The sandbox is otherwise usable, and every step is resumable:
    ssh -i ${PEM_PATH} ${SSH_USER}@$1 \\
      \"powershell -ExecutionPolicy Bypass -File C:\\ProgramData\\ec2-sandbox\\prepare-docker.ps1\"
  Logs on the instance: C:\\ProgramData\\ec2-sandbox\\phase2.log and pipe-relay.log"
  fi

  ok "Docker Engine ready on \\\\.\\pipe\\docker_engine"
}

# ---------------------------------------------------------------------------
# Launch
# ---------------------------------------------------------------------------

launch_instance() { # $1 ami, $2 subnet, $3 sg, $4 root dev, $5 vol gb
  local tags vol_tags iid udfile
  local extra=()

  tags="ResourceType=instance,Tags=[{Key=Name,Value=${INSTANCE_NAME}},{Key=${TAG_KEY},Value=${TAG_VAL}},{Key=${OS_TAG_KEY},Value=${OS}},{Key=${ARCH_TAG_KEY},Value=${ARCH}},{Key=${ENGINE_TAG_KEY},Value=${CONTAINERS}}]"
  vol_tags="ResourceType=volume,Tags=[{Key=${TAG_KEY},Value=${TAG_VAL}},{Key=${OS_TAG_KEY},Value=${OS}}]"

  udfile="${STATE_DIR}/user-data"
  if [ "$OS" = "windows" ]; then
    windows_user_data "$(public_key)" > "$udfile"
    if supports_nested_virt "$INSTANCE_TYPE"; then
      extra=(--cpu-options "NestedVirtualization=enabled")
    else
      warn "${INSTANCE_TYPE} does not support nested virtualization, so WSL2 and Docker
  will not work on this instance. Use ${DEFAULT_TYPE_WINDOWS} or another 8th-gen Intel type."
    fi
  else
    linux_user_data > "$udfile"
  fi
  chmod 600 "$udfile"

  # --associate-public-ip-address makes the CLI fold --subnet-id and
  # --security-group-ids into a synthesized network interface, so this must never be
  # combined with an explicit --network-interfaces.
  # The ${extra[@]+...} form is needed because bash 3.2 treats an empty array as
  # unset, which trips set -u.
  iid=$(aws_ ec2 run-instances \
          --image-id "$1" --instance-type "$INSTANCE_TYPE" \
          --subnet-id "$2" --security-group-ids "$3" \
          --key-name "$KEY_NAME" --associate-public-ip-address \
          --block-device-mappings "DeviceName=$4,Ebs={VolumeSize=$5,VolumeType=gp3,DeleteOnTermination=true}" \
          --user-data "file://${udfile}" \
          --tag-specifications "$tags" "$vol_tags" \
          ${extra[@]+"${extra[@]}"} \
          --query 'Instances[0].InstanceId' --output text)

  is_none "$iid" && die "run-instances did not return an instance id"
  printf '%s\n' "$iid"
}

# ---------------------------------------------------------------------------
# Readiness and credentials
# ---------------------------------------------------------------------------

# The waiter budget is 40 attempts x 15s = 10 minutes, but AWS documents waiting up
# to 15, so a single call can time out on a perfectly healthy instance.
wait_for_password() { # $1 = instance id
  local attempt=1
  while [ "$attempt" -le 2 ]; do
    if aws_ ec2 wait password-data-available --instance-id "$1" 2>/dev/null; then
      return 0
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -le 2 ]; then
      log "Still waiting for Windows to generate the password..."
    fi
  done
  die "the Administrator password was not available after ~20 minutes.
  The instance is still running; try '${SCRIPT_NAME} info ${OS} --arch=${ARCH}' shortly."
}

fetch_windows_password() { # $1 = instance id
  aws_ ec2 get-password-data --instance-id "$1" \
    --priv-launch-key "$PEM_PATH" \
    --query 'PasswordData' --output text 2>/dev/null || true
}

# An indexed array, not a string: word-splitting an option string would break on
# any path containing spaces.
set_ssh_opts() {
  SSH_OPTS=(
    -i "$PEM_PATH"
    -o StrictHostKeyChecking=accept-new
    -o "UserKnownHostsFile=${STATE_DIR}/known_hosts"
    -o ConnectTimeout=10
    -o LogLevel=ERROR
    -o BatchMode=yes
    -o ServerAliveInterval=30
    -o ServerAliveCountMax=20
  )
}

# instance-status-ok only means the VM booted. Linux is still running a multi-minute
# apt install, and Windows has a reboot ahead of it, so poll for the sentinel each
# bootstrap writes last. The loop tolerates the connection dropping mid-reboot.
wait_for_bootstrap() { # $1 = public ip
  local waited=0 probe
  set_ssh_opts

  if [ "$OS" = "windows" ]; then
    probe="if exist \"${READY_SENTINEL}\" (exit 0) else (exit 1)"
  else
    probe="test -f ${READY_SENTINEL}"
  fi

  log "Waiting for the sandbox to finish installing (this is the slow part)..."
  while [ "$waited" -lt "$BOOTSTRAP_DEADLINE" ]; do
    if ssh "${SSH_OPTS[@]}" "${SSH_USER}@$1" "$probe" >/dev/null 2>&1; then
      return 0
    fi
    sleep 15
    waited=$((waited + 15))
    if [ $((waited % 120)) -eq 0 ]; then
      log "Still installing (${waited}s elapsed)..."
    fi
  done

  if [ "$OS" = "windows" ]; then
    die "setup did not finish within $((BOOTSTRAP_DEADLINE / 60)) minutes.
  Check the log:  ssh -i ${PEM_PATH} ${SSH_USER}@$1 \"type C:\\ProgramData\\ec2-sandbox\\phase1.log\""
  fi
  die "setup did not finish within $((BOOTSTRAP_DEADLINE / 60)) minutes.
  Check the log:  ssh -i ${PEM_PATH} ${SSH_USER}@$1 'sudo tail -50 /var/log/cloud-init-output.log'"
}

# WSL installation, driven from here rather than from a scheduled task, because WSL
# refuses to register a distro from the SYSTEM account that user-data runs under.
# Shared by both engines; docker mode continues in prepare_windows_docker.
prepare_windows_wsl() { # $1 = public ip
  set_ssh_opts
  local attempt out
  # Installing the WSL kernel MSI restarts services and can reset the SSH session
  # mid-run. Every step in prepare-wsl.ps1 is resumable, so simply reconnecting and
  # running it again picks up where it left off rather than starting over.
  for attempt in 1 2 3; do
    if [ "$attempt" -eq 1 ]; then
      log "Preparing WSL2 and Docker (about 8 minutes)..."
    else
      log "Connection dropped; resuming WSL preparation (attempt ${attempt}/3)..."
      sleep 20
    fi

    out=$(ssh "${SSH_OPTS[@]}" "${SSH_USER}@$1" \
            'powershell -ExecutionPolicy Bypass -NoProfile -File C:\ProgramData\ec2-sandbox\prepare-wsl.ps1' 2>&1 \
          | tr -d '\000\r') || true

    # '|| true' matters: under pipefail a grep with no matches fails the whole
    # pipeline, and set -e would kill the script here -- skipping the retries and
    # swallowing the real error.
    printf '%s\n' "$out" \
      | grep -E '^(installing|resolving|downloading|importing|exporting|WSL_PREP_OK)' \
      | sed 's/^/    /' >&2 || true

    if printf '%s' "$out" | grep -q 'WSL_PREP_OK'; then
      ok "WSL2 + Docker image prepared"
      return 0
    fi
  done

  warn "WSL preparation did not succeed after 3 attempts. Last output:"
  printf '%s\n' "$out" | sed 's/^/    /' >&2
  die "could not prepare WSL2/Docker on the instance.
  The instance is otherwise usable, and the step is resumable, so retry by hand:
    ssh -i ${PEM_PATH} ${SSH_USER}@$1 \\
      \"powershell -ExecutionPolicy Bypass -File C:\\ProgramData\\ec2-sandbox\\prepare-wsl.ps1\""
}

# Reports which of the tools lstk shells out to actually landed. Deliberately a
# warning rather than a failure: one flaky download should not throw away an
# otherwise working sandbox, and the bootstrap treats these installs the same way.
verify_tools() { # $1 = public ip
  set_ssh_opts
  local probe out missing

  if [ "$OS" = "windows" ]; then
    probe="powershell -NoProfile -Command \"foreach (\$t in 'aws','terraform','sam') { if (Get-Command \$t -ea SilentlyContinue) { 'TOOL ' + \$t + ' ok' } else { 'TOOL ' + \$t + ' missing' } }\""
  else
    probe='for t in aws terraform sam; do if command -v $t >/dev/null 2>&1; then echo "TOOL $t ok"; else echo "TOOL $t missing"; fi; done'
  fi

  out=$(ssh "${SSH_OPTS[@]}" "${SSH_USER}@$1" "$probe" 2>&1 | tr -d '\000\r') || true
  missing=$(printf '%s\n' "$out" | awk '/^TOOL .* missing$/ { print $2 }' | tr '\n' ' ' | sed 's/ *$//')

  if [ -n "$missing" ]; then
    warn "not installed: ${missing}
  The sandbox is otherwise usable. Re-run that installer on the box, or recreate it."
  else
    ok "AWS CLI, Terraform and SAM CLI are installed"
  fi
}

# A full 32 bits from urandom. $RANDOM is only 15 bits, which is smaller than the
# word list, and awk's srand() seeds from the clock -- two calls in the same second
# would return the same password.
rand_int() { od -An -N4 -tu4 < /dev/urandom | tr -d ' \n'; }

# xrdp's login dialog cannot be pasted into (neutrinolabs/xrdp#816: a paste yields a
# single character), so on Linux the password has to be typed by hand. Three
# dictionary words plus two digits is far easier to type than a random string, and
# still around 53 bits of entropy. The mixed case, digits and hyphen also satisfy
# Windows' local password complexity policy.
generate_password() {
  local wl=/usr/share/dict/words
  local list count i idx word out=""
  if [ -r "$wl" ]; then
    # The system dictionary contains profane and anatomical entries; these get
    # printed to the terminal and pasted into tickets, so screen them out.
    list=$(LC_ALL=C grep -E '^[a-z]{4,6}$' "$wl" \
           | LC_ALL=C grep -viE 'ass|anal|anus|arse|ball|bast|boob|bugg|bull|clit|cock|coon|cram|crap|cunt|dago|damn|dick|dike|dild|dong|douc|dyke|erot|fag|fart|feck|fuck|gash|gook|hell|hoar|homo|hymen|hore|jism|jizz|kike|knob|labi|muff|nazi|negr|nigg|nude|orga|orgy|paki|pecke|pedo|peni|piss|poop|porn|prick|pube|puss|queer|racy|rape|rect|scat|scro|semen|sex|shag|shit|slag|slut|smeg|sperm|spic|suck|teat|test|tit|toss|turd|twat|urin|vagi|vulv|wank|whor|wop' \
           || true)
    count=$(printf '%s\n' "$list" | grep -c . || true)
    if [ "${count:-0}" -ge 1000 ]; then
      for i in 1 2 3; do
        idx=$(( $(rand_int) % count + 1 ))
        word=$(printf '%s\n' "$list" | sed -n "${idx}p")
        out="${out}$(printf '%s' "$word" | awk '{printf "%s%s", toupper(substr($0,1,1)), substr($0,2)}')-"
      done
      printf '%s%02d\n' "$out" "$(( $(rand_int) % 90 + 10 ))"
      return 0
    fi
  fi
  # No word list: fall back to random, excluding characters that look alike.
  # cut, not head: head closes the pipe early and SIGPIPEs tr, which trips pipefail.
  LC_ALL=C openssl rand -base64 48 \
    | tr -dc 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789' | cut -c1-16
}

# Piped through stdin rather than passed as an argument, so the password never
# appears in the remote process table.
set_remote_password() { # $1 = public ip, $2 = account, $3 = password
  set_ssh_opts
  if [ "$OS" = "windows" ]; then
    printf '%s' "$3" \
      | ssh "${SSH_OPTS[@]}" "${SSH_USER}@$1" \
          'powershell -ExecutionPolicy Bypass -NoProfile -File C:\ProgramData\ec2-sandbox\set-user-password.ps1' \
      || die "could not set the $2 password over SSH"
  else
    printf '%s:%s\n' "$2" "$3" \
      | ssh "${SSH_OPTS[@]}" "${SSH_USER}@$1" 'sudo chpasswd' \
      || die "could not set the $2 password over SSH"
  fi
}

cache_password() { # $1 = path, $2 = password
  ( umask 077; printf '%s\n' "$2" > "$1" )
  chmod 600 "$1"
}

read_cached_password() { # $1 = path
  if [ -f "$1" ]; then cat "$1"; fi
}

# ---------------------------------------------------------------------------
# Connection details
# ---------------------------------------------------------------------------

# The rdp:// URI scheme cannot carry parameters on macOS -- only Windows mstsc
# supports the query-string form -- so write .rdp files instead. One per account, so
# switching between them is just opening a different file.
# authentication level:i:0 suppresses the self-signed certificate warning that both
# EC2 Windows and xrdp always produce.
write_rdp_file() { # $1 = path, $2 = ip, $3 = username
  cat > "$1" <<EOF
full address:s:$2:${RDP_PORT}
username:s:$3
screen mode id:i:2
authentication level:i:0
redirectclipboard:i:1
EOF
}

print_connection_info() { # $1 = ip, $2 = admin pw, $3 = user pw
  printf '\n'
  printf '%s%s-%s sandbox%s - %s on %s\n' "$C_BOLD" "$OS" "$ARCH" "$C_RESET" "$OS_LABEL" "$INSTANCE_TYPE"
  printf '  Region      %s\n' "$REGION"
  printf '  Address     %s:%s\n' "$1" "$RDP_PORT"
  printf '\n'
  printf '  %sadmin%s     %s / %s\n' "$C_BOLD" "$C_RESET" "$ADMIN_USER" "${2:-<unavailable>}"
  printf '            open %s\n' "$RDP_ADMIN_PATH"
  printf '  %suser%s      %s / %s\n' "$C_BOLD" "$C_RESET" "$UNPRIV_USER" "${3:-<unavailable>}"
  printf '            open %s\n' "$RDP_USER_PATH"
  printf '\n'
  printf '  SSH         ssh -i %s %s@%s\n' "$PEM_PATH" "$SSH_USER" "$1"
  if [ "$OS" = "windows" ] && [ "$CONTAINERS" = "podman" ]; then
    printf '  Docker      Podman, as docker.exe. Finishes installing on first login as\n'
    printf '              %s, since podman machines are per-user; a console window\n' "$UNPRIV_USER"
    printf '              runs for a few minutes.\n'
  elif [ "$OS" = "windows" ]; then
    printf '  Docker      Docker Engine in WSL2, on \\\\.\\pipe\\docker_engine. Ready now, and\n'
    printf '              shared by both accounts. Windows paths in bind mounts are\n'
    printf '              rewritten to /mnt/c/... the way Docker Desktop does them.\n'
  else
    printf '  Docker      ready for both accounts: docker run hello-world\n'
  fi
  printf '\n'
}

maybe_open_rdp() {
  local target
  [ "$DO_OPEN" -eq 1 ] || return 0
  if [ "$OPEN_AS" = "admin" ]; then target="$RDP_ADMIN_PATH"; else target="$RDP_USER_PATH"; fi

  if [ -d "/Applications/Windows App.app" ]; then
    open -a "Windows App" "$target"
  elif [ -d "/Applications/Microsoft Remote Desktop.app" ]; then
    open -a "Microsoft Remote Desktop" "$target"
  else
    warn "no RDP client found. Install 'Windows App' from the Mac App Store, then: open ${target}"
  fi
}

print_cost_note() {
  local extra=""
  if [ "$OS" = "windows" ]; then
    extra=", plus a per-vCPU Windows licence charge"
  fi
  cat >&2 <<EOF
${C_YELLOW}Reminder:${C_RESET} this instance bills by the hour (${INSTANCE_TYPE} plus ${MIN_VOLUME_GB} GiB gp3${extra}).
Run '${SCRIPT_NAME} delete ${OS} --arch=${ARCH}' when you are done with it.
EOF
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------

create_exit_trap() {
  if [ -n "$LAUNCHED_IID" ] && [ "$CREATE_DONE" -eq 0 ]; then
    warn "instance ${LAUNCHED_IID} was launched but setup did not finish; it is still billing.
  Retry details:  ${SCRIPT_NAME} info ${OS} --arch=${ARCH} --region ${REGION}
  Or remove it:   ${SCRIPT_NAME} delete ${OS} --arch=${ARCH} --region ${REGION}"
  fi
}

cmd_create() {
  local existing ami root_dev vol_gb vpc subnet cidr sg iid ip admin_pw user_pw

  existing=$(find_instance "$LIVE_STATES")
  if [ -n "$existing" ]; then
    die "a ${OS}-${ARCH} sandbox already exists in ${REGION} (${existing}).
  Connection details:  ${SCRIPT_NAME} info ${OS} --arch=${ARCH}
  Remove it first:     ${SCRIPT_NAME} delete ${OS} --arch=${ARCH}"
  fi

  ensure_state_dir

  log "Resolving the ${OS_LABEL} AMI"
  ami=$(resolve_ami)
  root_dev=$(resolve_root_device "$ami")
  vol_gb=$(resolve_volume_size "$ami" "$root_dev")
  log "AMI ${ami}, root ${root_dev}, ${vol_gb} GiB gp3"

  vpc=$(find_default_vpc)
  subnet=$(pick_subnet "$vpc")
  cidr=$(detect_my_cidr)
  log "Default VPC ${vpc}, subnet ${subnet}, access restricted to ${cidr}"

  ensure_key_pair
  sg=$(ensure_security_group "$vpc" "$cidr")

  log "Launching ${INSTANCE_TYPE}..."
  iid=$(launch_instance "$ami" "$subnet" "$sg" "$root_dev" "$vol_gb")
  ok "Launched ${iid}"

  # From here on a failure leaves a real instance behind, so make sure we say so.
  # This is an EXIT trap, not ERR: ERR traps are not inherited by shell functions
  # without set -E, and die() exits rather than returning non-zero, so an ERR trap
  # would almost never fire.
  LAUNCHED_IID="$iid"
  trap create_exit_trap EXIT

  log "Waiting for the instance to start..."
  aws_ ec2 wait instance-running --instance-ids "$iid"

  ip=$(instance_field "$iid" "PublicIpAddress")
  is_none "$ip" && die "instance ${iid} has no public IP address"

  if [ "$OS" = "windows" ]; then
    log "Waiting for Windows to generate the Administrator password (several minutes)..."
    wait_for_password "$iid"
    admin_pw=$(fetch_windows_password "$iid")
    if is_none "$admin_pw"; then
      die "could not decrypt the Administrator password with ${PEM_PATH} (key mismatch?)"
    fi
    wait_for_bootstrap "$ip"
    prepare_windows_wsl "$ip"
    if [ "$CONTAINERS" = "docker" ]; then
      prepare_windows_docker "$ip" "$admin_pw"
    fi
  else
    log "Waiting for the instance to pass its status checks..."
    aws_ ec2 wait instance-status-ok --instance-ids "$iid"
    wait_for_bootstrap "$ip"
    admin_pw=$(generate_password)
    set_remote_password "$ip" "$ADMIN_USER" "$admin_pw"
    cache_password "$PW_ADMIN_PATH" "$admin_pw"
  fi

  verify_tools "$ip"

  user_pw=$(generate_password)
  set_remote_password "$ip" "$UNPRIV_USER" "$user_pw"
  cache_password "$PW_USER_PATH" "$user_pw"
  ok "Both accounts configured"

  CREATE_DONE=1

  write_rdp_file "$RDP_ADMIN_PATH" "$ip" "$ADMIN_USER"
  write_rdp_file "$RDP_USER_PATH" "$ip" "$UNPRIV_USER"
  print_connection_info "$ip" "$admin_pw" "$user_pw"
  print_cost_note
  maybe_open_rdp
}

cmd_info() {
  local iid state ip admin_pw user_pw engine
  iid=$(find_instance "$LIVE_STATES")
  [ -n "$iid" ] || die "no ${OS}-${ARCH} sandbox in ${REGION}. Create one with: ${SCRIPT_NAME} create ${OS} --arch=${ARCH}"

  state=$(instance_field "$iid" "State.Name")
  ip=$(instance_field "$iid" "PublicIpAddress")
  INSTANCE_TYPE=$(instance_field "$iid" "InstanceType")

  # Sandboxes created before --containers existed carry no engine tag, and every
  # one of those is podman.
  engine=$(instance_field "$iid" "Tags[?Key=='${ENGINE_TAG_KEY}']|[0].Value")
  if is_none "$engine"; then engine="podman"; fi
  if [ "$OS" = "windows" ]; then CONTAINERS="$engine"; fi

  printf '  Instance    %s (%s)\n' "$iid" "$state" >&2
  is_none "$ip" && die "instance ${iid} is ${state} and has no public IP address"

  ensure_state_dir

  admin_pw=""
  if [ "$OS" = "windows" ]; then
    if [ -f "$PEM_PATH" ]; then
      admin_pw=$(fetch_windows_password "$iid")
      if is_none "$admin_pw"; then admin_pw=""; fi
    else
      warn "${PEM_PATH} is missing, so the Administrator password cannot be decrypted"
    fi
  else
    admin_pw=$(read_cached_password "$PW_ADMIN_PATH")
  fi
  user_pw=$(read_cached_password "$PW_USER_PATH")

  write_rdp_file "$RDP_ADMIN_PATH" "$ip" "$ADMIN_USER"
  write_rdp_file "$RDP_USER_PATH" "$ip" "$UNPRIV_USER"
  print_connection_info "$ip" "$admin_pw" "$user_pw"
  maybe_open_rdp
}

# Copies a local file or directory onto the desktop of the sandbox's unprivileged
# account, where it shows up immediately in that account's RDP session.
#
# On Linux that account is not the one SSH logs in as, and its home directory is
# not writable by the admin account, so the payload lands in a staging directory
# first and is moved into place with sudo.
cmd_copyto() {
  # Two spellings of the same directory: scp wants forward slashes (a backslash
  # would be eaten as an escape before it ever reaches the far side), while cmd
  # and PowerShell want backslashes.
  local iid ip dest shown name local_size remote_size stage target

  iid=$(find_instance "$LIVE_STATES")
  [ -n "$iid" ] || die "no ${OS}-${ARCH} sandbox in ${REGION}. Create one with: ${SCRIPT_NAME} create ${OS} --arch=${ARCH}"

  ip=$(instance_field "$iid" "PublicIpAddress")
  is_none "$ip" && die "instance ${iid} has no public IP address"
  [ -f "$PEM_PATH" ] || die "${PEM_PATH} is missing, so ${iid} cannot be reached over SSH"

  set_ssh_opts
  name=$(basename "$SOURCE_PATH")

  if [ "$OS" = "windows" ]; then
    dest="C:/Users/${UNPRIV_USER_WINDOWS}/Desktop"
    shown="C:\\Users\\${UNPRIV_USER_WINDOWS}\\Desktop"
    # Creating the profile directory by hand is worse than failing: Windows would
    # then build the real profile as ${UNPRIV_USER_WINDOWS}.<HOSTNAME> at the next logon.
    if ! ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" \
           "if exist \"${shown}\" (exit 0) else (exit 1)" >/dev/null 2>&1; then
      die "${UNPRIV_USER_WINDOWS} has no profile on ${iid} yet, so there is no desktop to copy to.
  Log in as ${UNPRIV_USER_WINDOWS} once, then retry:
    ${SCRIPT_NAME} info ${OS} --arch=${ARCH} --open"
    fi
  else
    # The home directory comes from the box rather than being assumed, and the
    # desktop itself has to be created: XFCE only makes ~/Desktop at first login,
    # and this may well run before anyone has logged in.
    dest=$(ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" \
             "home=\$(getent passwd '${UNPRIV_USER_LINUX}' | cut -d: -f6) \
              && [ -n \"\$home\" ] \
              && sudo install -d -m 0755 -o '${UNPRIV_USER_LINUX}' -g '${UNPRIV_USER_LINUX}' \"\$home/Desktop\" \
              && printf %s \"\$home/Desktop\"" 2>/dev/null) \
      || die "could not prepare ${UNPRIV_USER_LINUX}'s desktop on ${iid}"
    shown="$dest"

    # scp runs as the admin account, which cannot write into the unprivileged
    # account's home, so land it somewhere writable and move it across below.
    stage=$(ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" \
              'mktemp -d /tmp/ec2-sandbox-copyto.XXXXXX' 2>/dev/null) \
      || die "could not create a staging directory on ${iid}"
  fi

  if [ "$OS" = "windows" ]; then target="$dest"; else target="$stage"; fi

  log "Copying ${name} to ${shown} on ${iid}..."
  if [ -d "$SOURCE_PATH" ]; then
    scp -r "${SSH_OPTS[@]}" "$SOURCE_PATH" "${SSH_USER}@${ip}:${target}/" >/dev/null \
      || die "could not copy ${SOURCE_PATH} to ${iid}"
  else
    scp "${SSH_OPTS[@]}" "$SOURCE_PATH" "${SSH_USER}@${ip}:${target}/" >/dev/null \
      || die "could not copy ${SOURCE_PATH} to ${iid}"
  fi

  if [ "$OS" != "windows" ]; then
    # Replaced rather than merged: 'mv' onto an existing directory of the same
    # name would nest the new copy inside the old one instead of overwriting it.
    ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" \
      "sudo rm -rf \"${dest}/${name}\" \
       && sudo mv \"${stage}/${name}\" \"${dest}/\" \
       && sudo chown -R '${UNPRIV_USER_LINUX}:${UNPRIV_USER_LINUX}' \"${dest}/${name}\" \
       && rm -rf \"${stage}\"" >/dev/null 2>&1 \
      || die "could not move ${name} into ${dest} on ${iid}"
  fi

  # scp reports success even when the far side wrote a short file, so check the
  # bytes really arrived rather than trusting the exit code.
  if [ -f "$SOURCE_PATH" ]; then
    local_size=$(wc -c < "$SOURCE_PATH" | tr -d '[:space:]')
    if [ "$OS" = "windows" ]; then
      remote_size=$(ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" \
        "powershell -NoProfile -Command \"(Get-Item -LiteralPath '${shown}\\${name}').Length\"" 2>/dev/null | tr -d '[:space:]')
    else
      # Via sudo and stat, not a redirect into wc: Ubuntu creates home
      # directories 0750, so the admin account cannot even traverse this path.
      remote_size=$(ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" \
        "sudo stat -c %s \"${dest}/${name}\"" 2>/dev/null | tr -d '[:space:]')
    fi
    [ "$remote_size" = "$local_size" ] \
      || die "${name} did not arrive intact: ${local_size} bytes locally, ${remote_size:-none} on ${iid}"
    ok "Copied ${name} (${local_size} bytes) to ${shown}"
  else
    if [ "$OS" = "windows" ]; then
      ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" "if exist \"${shown}\\${name}\" (exit 0) else (exit 1)" >/dev/null 2>&1 \
        || die "${name} is not on ${iid} after the copy"
    else
      ssh "${SSH_OPTS[@]}" "${SSH_USER}@${ip}" "sudo test -d \"${dest}/${name}\"" >/dev/null 2>&1 \
        || die "${name} is not on ${iid} after the copy"
    fi
    ok "Copied ${name}/ to ${shown}"
  fi
}

confirm_delete() {
  local reply
  [ "$ASSUME_YES" -eq 1 ] && return 0
  if [ ! -t 0 ]; then
    die "refusing to delete without confirmation; pass -y to proceed non-interactively"
  fi
  printf 'Delete the %s-%s sandbox in %s? [y/N] ' "$OS" "$ARCH" "$REGION" >&2
  read -r reply
  case "$reply" in
    y|Y|yes|YES) return 0 ;;
    *) log "Aborted."; exit 0 ;;
  esac
}

terminate_instance() { # $1 = instance id
  local i
  log "Terminating ${1}..."
  aws_ ec2 terminate-instances --instance-ids "$1" >/dev/null

  # instance-terminated has 'pending' and 'stopping' as *failure* acceptors, so a
  # freshly-launched instance can fail the waiter outright rather than be waited on.
  for i in 1 2 3; do
    if aws_ ec2 wait instance-terminated --instance-ids "$1" 2>/dev/null; then
      ok "Terminated ${1}"
      return 0
    fi
    [ "$i" -eq 3 ] && die "instance ${1} did not reach the terminated state"
    sleep 10
  done
}

delete_security_group() { # $1 = vpc id
  local sg_id err i
  sg_id=$(find_security_group "$1")
  is_none "$sg_id" && return 0

  log "Deleting security group ${SG_NAME}..."
  # ENI teardown lags instance termination, so the first attempt after a clean
  # waiter routinely fails with DependencyViolation.
  for i in $(seq 1 24); do
    if err=$(aws_ ec2 delete-security-group --group-id "$sg_id" 2>&1); then
      ok "Deleted security group ${sg_id}"
      return 0
    fi
    case "$err" in
      *InvalidGroup.NotFound*) return 0 ;;
      *DependencyViolation*)
        if [ "$i" -eq 1 ]; then log "Waiting for the network interface to detach..."; fi
        sleep 10 ;;
      *) die "could not delete security group ${sg_id}: $err" ;;
    esac
  done
  warn "security group ${sg_id} still has dependencies; delete it by hand later"
}

# Removes the instance, security group and cached state of whichever OS/arch
# profile set_os_profile currently holds. Prints 1 to stdout when it removed
# something, 0 when there was nothing to do -- every other message goes to stderr.
delete_profile() { # $1 = vpc id
  local iid sg_id did=0

  iid=$(find_instance "${LIVE_STATES},shutting-down")
  if [ -n "$iid" ]; then
    confirm_delete
    terminate_instance "$iid"
    did=1
  fi

  # Sweep an orphaned security group even when no instance exists, e.g. after a
  # create that failed partway through.
  if ! is_none "$1"; then
    sg_id=$(find_security_group "$1")
    if ! is_none "$sg_id"; then
      if [ "$did" -eq 0 ]; then confirm_delete; fi
      delete_security_group "$1"
      did=1
    fi
  fi

  rm -f "$RDP_ADMIN_PATH" "$RDP_USER_PATH" "$PW_ADMIN_PATH" "$PW_USER_PATH" \
        "${STATE_DIR}/user-data" 2>/dev/null || true
  rm -rf "${STATE_DIR}/docker" 2>/dev/null || true

  if [ "$did" -eq 1 ]; then
    ok "Deleted the ${OS}-${ARCH} sandbox. Key pair ${KEY_NAME} and ${PEM_PATH} were kept for reuse."
  fi
  printf '%s\n' "$did"
}

cmd_delete() {
  local vpc did_something=0 a
  vpc=$(aws_ ec2 describe-vpcs --filters "Name=isDefault,Values=true" \
          --query 'Vpcs[0].VpcId' --output text 2>/dev/null || true)

  if [ -n "$ARCH" ]; then
    set_os_profile
    did_something=$(delete_profile "$vpc")
  else
    # No instance of this OS exists in any architecture, so there is nothing to
    # read an architecture off. Sweep both rather than guess which left leftovers.
    log "No ${OS} instance in ${REGION}; checking both architectures for leftovers."
    for a in x64 arm64; do
      ARCH="$a"
      set_os_profile
      if [ "$(delete_profile "$vpc")" -eq 1 ]; then did_something=1; fi
    done
  fi

  if [ "$did_something" -eq 0 ]; then
    log "Nothing to delete for ${OS} in ${REGION}."
  fi
}

# ---------------------------------------------------------------------------

main() {
  parse_args "$@"
  require_cmds aws curl ssh scp ssh-keygen openssl
  resolve_region
  preflight_identity

  # create is always told the architecture; delete and info work it out from the
  # sandboxes that exist, and cmd_delete handles "none at all" by sweeping both.
  if [ "$ACTION" != "create" ] && [ -z "$ARCH" ]; then
    resolve_arch_from_instances
  fi

  case "$ACTION" in
    create) set_os_profile; cmd_create ;;
    delete) cmd_delete ;;
    info)
      [ -n "$ARCH" ] || die "no ${OS} sandbox in ${REGION}. Create one with: ${SCRIPT_NAME} create ${OS} --arch=x64"
      set_os_profile
      cmd_info ;;
    copyto)
      [ -n "$ARCH" ] || die "no ${OS} sandbox in ${REGION}. Create one with: ${SCRIPT_NAME} create ${OS} --arch=x64"
      set_os_profile
      cmd_copyto ;;
  esac
}

main "$@"

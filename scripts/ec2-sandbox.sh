#!/usr/bin/env bash
#
# ec2-sandbox.sh - stand up a throwaway Windows or Linux desktop on EC2, reachable
# over RDP, then tear it down again.
#
# Each sandbox gets two logins: a privileged admin account and an unprivileged
# "User" account, with VS Code and Docker preinstalled.
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

# Windows needs nested virtualization for WSL2, which rules out t3. Linux runs
# Docker Engine natively, so it stays on the cheap type.
DEFAULT_TYPE_WINDOWS="m8i.large"
DEFAULT_TYPE_LINUX="t3.medium"

# Families that support --cpu-options NestedVirtualization=enabled. The AWS docs
# list 7th gen too, but the CLI's own help restricts it to 8th gen; both agree on
# 8th, which is why that is the Windows default.
NESTED_VIRT_FAMILIES="c8i m8i r8i x8i c8id m8id r8id c8i-flex m8i-flex r8i-flex c7i m7i r7i i7i c7i-flex m7i-flex"

MIN_VOLUME_GB=40
RDP_PORT=3389
SSH_PORT=22

# Windows convention is capitalised account names; Linux convention is lowercase.
UNPRIV_USER_WINDOWS="User"
UNPRIV_USER_LINUX="user"
# Placeholder so --help can render before set_os_profile picks the real one.
UNPRIV_USER="User"

SSM_PARAM_WINDOWS="/aws/service/ami-windows-latest/Windows_Server-2025-English-Full-Base"
SSM_PARAM_LINUX="/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id"

AMI_FALLBACK_OWNER_WINDOWS="amazon"
AMI_FALLBACK_NAME_WINDOWS="Windows_Server-2025-English-Full-Base-*"
AMI_FALLBACK_OWNER_LINUX="099720109477" # Canonical
AMI_FALLBACK_NAME_LINUX="ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"

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

${C_BOLD}COMMANDS${C_RESET}
  create <os>    Launch the sandbox and print RDP connection details.
  delete <os>    Terminate the instance and delete its security group.
  info <os>      Re-print connection details for a running sandbox.

${C_BOLD}OPTIONS${C_RESET}
  --region <r>        AWS region. Defaults to \$AWS_REGION, \$AWS_DEFAULT_REGION,
                      then your configured region.
  --instance-type <t> Override the instance type.
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
    Docker    Linux: Docker Engine + compose, running natively.
              Windows: Podman, presented as 'docker' on the PATH. Podman
              translates Windows volume paths, so 'docker run -v C:\dir:/x'
              works -- a plain Linux daemon rejects those outright, which is why
              Docker Engine is not used here. Docker Desktop cannot be installed
              on Windows Server at all.

  Defaults: windows ${DEFAULT_TYPE_WINDOWS}, linux ${DEFAULT_TYPE_LINUX}, ${MIN_VOLUME_GB} GiB gp3 root volume.

${C_BOLD}NOTES${C_RESET}
  * One instance per OS per region. A Windows and a Linux sandbox can run at once.
  * RDP and SSH are opened only to your current public IP as a /32. If your IP
    changes, re-running create re-authorises it; a running instance is untouched.
  * Key pairs and their .pem are reused across create/delete cycles, not deleted.
  * State lives in ~/.local/state/ec2-sandbox/<region>/<os>/
  * Windows defaults to ${DEFAULT_TYPE_WINDOWS} because WSL2 needs nested virtualization,
    which t3 does not support. Overriding to an unsupported type disables Docker.
  * Windows reboots once during setup to enable WSL2, so create takes ~15 minutes.
  * On Windows the Podman machine is created the first time you log in as
    '${UNPRIV_USER_WINDOWS}', because podman machines are per-Windows-user and cannot be
    prepared in advance. That first logon downloads ~1 GB and takes several
    minutes; a console window is visible while it runs. The machine is set
    rootful so containers can bind ports below 1024 (LocalStack needs 443).
  * On Linux, '${UNPRIV_USER_LINUX}' is in the 'docker' group so it can run containers.
    Docker group membership is equivalent to root on the host, so it is
    unprivileged in the ordinary sense but is not a security boundary.

${C_BOLD}EXAMPLES${C_RESET}
  ${SCRIPT_NAME} create linux
  ${SCRIPT_NAME} create windows --region us-east-1 --open
  ${SCRIPT_NAME} info linux --as admin --open
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

  [ $# -gt 0 ] || { usage >&2; die "missing command (create, delete or info)"; }

  ACTION="$1"; shift
  case "$ACTION" in
    create|delete|info) ;;
    -*) die "unknown option '$ACTION'; the command must come first (create, delete or info)" ;;
    *)  die "unknown command '$ACTION'; expected create, delete or info" ;;
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
      --as)
        [ $# -ge 2 ] || die "--as needs a value (admin or user)"
        OPEN_AS="$2"; shift 2 ;;
      --as=*) OPEN_AS="${1#*=}"; shift ;;
      --open) DO_OPEN=1; shift ;;
      -y|--yes) ASSUME_YES=1; shift ;;
      *) die "unknown option '$1'" ;;
    esac
  done

  case "$OPEN_AS" in
    admin|user) ;;
    *) die "--as must be 'admin' or 'user', not '$OPEN_AS'" ;;
  esac
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
  INSTANCE_NAME="ec2-sandbox-${OS}"
  SG_NAME="ec2-sandbox-${OS}-sg"
  KEY_NAME="ec2-sandbox-${OS}-key"

  STATE_DIR="${HOME}/.local/state/ec2-sandbox/${REGION}/${OS}"
  PEM_PATH="${STATE_DIR}/key.pem"
  RDP_ADMIN_PATH="${STATE_DIR}/connect-admin.rdp"
  RDP_USER_PATH="${STATE_DIR}/connect-user.rdp"
  PW_ADMIN_PATH="${STATE_DIR}/password-admin"
  PW_USER_PATH="${STATE_DIR}/password-user"

  case "$OS" in
    windows)
      OS_LABEL="Windows Server 2025"
      ADMIN_USER="Administrator"
      SSH_USER="Administrator"
      SSM_PARAM="$SSM_PARAM_WINDOWS"
      AMI_FALLBACK_OWNER="$AMI_FALLBACK_OWNER_WINDOWS"
      AMI_FALLBACK_NAME="$AMI_FALLBACK_NAME_WINDOWS"
      READY_SENTINEL="$READY_SENTINEL_WINDOWS"
      UNPRIV_USER="$UNPRIV_USER_WINDOWS"
      BOOTSTRAP_DEADLINE="$BOOTSTRAP_DEADLINE_WINDOWS"
      if [ -z "$INSTANCE_TYPE" ]; then INSTANCE_TYPE="$DEFAULT_TYPE_WINDOWS"; fi
      ;;
    linux)
      OS_LABEL="Ubuntu 24.04 LTS + XFCE"
      ADMIN_USER="ubuntu"
      SSH_USER="ubuntu"
      SSM_PARAM="$SSM_PARAM_LINUX"
      AMI_FALLBACK_OWNER="$AMI_FALLBACK_OWNER_LINUX"
      AMI_FALLBACK_NAME="$AMI_FALLBACK_NAME_LINUX"
      READY_SENTINEL="$READY_SENTINEL_LINUX"
      UNPRIV_USER="$UNPRIV_USER_LINUX"
      BOOTSTRAP_DEADLINE="$BOOTSTRAP_DEADLINE_LINUX"
      if [ -z "$INSTANCE_TYPE" ]; then INSTANCE_TYPE="$DEFAULT_TYPE_LINUX"; fi
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
              "Name=instance-state-name,Values=$1" \
    --query 'Reservations[].Instances[].InstanceId' \
    --output text 2>/dev/null | tr '\t' '\n' | grep -v '^$' | head -1 || true
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
apt-get install -y xfce4 xfce4-goodies xrdp ca-certificates curl gnupg apt-transport-https

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
echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/microsoft.asc] https://packages.microsoft.com/repos/code stable main" \
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
echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" \
  > /etc/apt/sources.list.d/docker.list

apt-get update
apt-get install -y code firefox docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

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

systemctl enable --now docker
# Both accounts can drive Docker. Note that docker group membership is
# effectively root on the host.
usermod -aG docker '@@ADMIN@@'
usermod -aG docker '@@USER@@'

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

# Runs inside the WSL distro, as root, while the image is being prepared.
# Runs as the unprivileged user at every logon. Podman machines register per
# Windows user, so SYSTEM cannot create one on their behalf -- this is the one
# part that cannot be prepared ahead of time.
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
  sed -e "s|@@USER@@|${UNPRIV_USER_WINDOWS}|g" <<'PWEOF'
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

# Podman's machine image needs systemd, which only the modern WSL build provides;
# the inbox WSL on Server 2022 could not run it at all. 'wsl --install' is a silent
# no-op on Server (it wants the Microsoft Store), so install the MSI directly.
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

# The podman machine itself is created at first logon, by the account that will
# use it -- machines are per Windows user and cannot be shared.
$act = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-ExecutionPolicy Bypass -NoProfile -WindowStyle Hidden -File $base\firstlogon.ps1"
$trg = New-ScheduledTaskTrigger -AtLogOn -User "$env:COMPUTERNAME\@@USER@@"
$prn = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\@@USER@@" -RunLevel Limited
Register-ScheduledTask -TaskName 'ec2-sandbox-podman' -Action $act -Trigger $trg -Principal $prn -Force | Out-Null

Write-Output 'WSL_PREP_OK'
PWEOF
}

# EC2Launch v2 runs <powershell> user-data as SYSTEM, once, on first boot. Enabling
# the WSL2 features needs a reboot, so phase 1 registers a startup task that
# finishes the job on the way back up.
windows_user_data() { # $1 = public key
  local b_prep b_firstlogon
  b_prep=$(win_prepare_wsl_ps1 | b64gz)
  b_firstlogon=$(win_firstlogon_ps1 | b64gz)

  sed -e "s|@@PUBKEY@@|$1|g" \
      -e "s|@@USER@@|${UNPRIV_USER_WINDOWS}|g" \
      -e "s|@@B64_PREP@@|${b_prep}|g" \
      -e "s|@@B64_FIRSTLOGON@@|${b_firstlogon}|g" <<'WINEOF'
<powershell>
# Deliberately 'Continue', not 'Stop'. SSH is the only channel for diagnosing this
# machine, so a failure in a later step must never prevent sshd from coming up --
# on Server 2025 an aborted phase 1 left a box that was reachable only over RDP.
$ErrorActionPreference = 'Continue'
$base = 'C:\ProgramData\ec2-sandbox'
New-Item -ItemType Directory -Force -Path $base | Out-Null
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
Wr '@@B64_PREP@@'       "$base\prepare-wsl.ps1"
Wr '@@B64_FIRSTLOGON@@' "$base\firstlogon.ps1"

# Helper the host invokes over SSH. Reads the password from stdin so it never lands
# in a command line or the remote process table.
Set-Content -Path "$base\set-user-password.ps1" -Encoding ascii -Value @'
$pw = [Console]::In.ReadToEnd().Trim()
$sec = ConvertTo-SecureString $pw -AsPlainText -Force
Set-LocalUser -Name "@@USER@@" -Password $sec
'@

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
$bin = "$base\bin"
New-Item -ItemType Directory -Force -Path $bin | Out-Null
Copy-Item 'C:\Program Files\Podman\podman.exe' "$bin\docker.exe" -Force

$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
if ($machinePath -notlike "*$bin*") {
  [Environment]::SetEnvironmentVariable('Path', "$machinePath;$bin", 'Machine')
}

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
WINEOF
}

# ---------------------------------------------------------------------------
# Launch
# ---------------------------------------------------------------------------

launch_instance() { # $1 ami, $2 subnet, $3 sg, $4 root dev, $5 vol gb
  local tags vol_tags iid udfile
  local extra=()

  tags="ResourceType=instance,Tags=[{Key=Name,Value=${INSTANCE_NAME}},{Key=${TAG_KEY},Value=${TAG_VAL}},{Key=${OS_TAG_KEY},Value=${OS}}]"
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
  The instance is still running; try '${SCRIPT_NAME} info ${OS}' shortly."
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
  Check the log:  ssh -i ${PEM_PATH} ${SSH_USER}@$1 \"type C:\\ProgramData\\ec2-sandbox\\phase2.log\""
  fi
  die "setup did not finish within $((BOOTSTRAP_DEADLINE / 60)) minutes.
  Check the log:  ssh -i ${PEM_PATH} ${SSH_USER}@$1 'sudo tail -50 /var/log/cloud-init-output.log'"
}

# WSL preparation, driven from here rather than from a scheduled task, because WSL
# refuses to register a distro from the SYSTEM account that user-data runs under.
# Takes roughly 8 minutes: kernel MSI, ~370 MB image, Docker install, then export.
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
  printf '%s%s sandbox%s - %s on %s\n' "$C_BOLD" "$OS" "$C_RESET" "$OS_LABEL" "$INSTANCE_TYPE"
  printf '  Region      %s\n' "$REGION"
  printf '  Address     %s:%s\n' "$1" "$RDP_PORT"
  printf '\n'
  printf '  %sadmin%s     %s / %s\n' "$C_BOLD" "$C_RESET" "$ADMIN_USER" "${2:-<unavailable>}"
  printf '            open %s\n' "$RDP_ADMIN_PATH"
  printf '  %suser%s      %s / %s\n' "$C_BOLD" "$C_RESET" "$UNPRIV_USER" "${3:-<unavailable>}"
  printf '            open %s\n' "$RDP_USER_PATH"
  printf '\n'
  printf '  SSH         ssh -i %s %s@%s\n' "$PEM_PATH" "$SSH_USER" "$1"
  if [ "$OS" = "windows" ]; then
    printf '  Docker      finishes installing on first login as %s, since WSL distros\n' "$UNPRIV_USER"
    printf '              are per-user; a console window runs for a few minutes.\n'
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
Run '${SCRIPT_NAME} delete ${OS}' when you are done with it.
EOF
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------

create_exit_trap() {
  if [ -n "$LAUNCHED_IID" ] && [ "$CREATE_DONE" -eq 0 ]; then
    warn "instance ${LAUNCHED_IID} was launched but setup did not finish; it is still billing.
  Retry details:  ${SCRIPT_NAME} info ${OS} --region ${REGION}
  Or remove it:   ${SCRIPT_NAME} delete ${OS} --region ${REGION}"
  fi
}

cmd_create() {
  local existing ami root_dev vol_gb vpc subnet cidr sg iid ip admin_pw user_pw

  existing=$(find_instance "$LIVE_STATES")
  if [ -n "$existing" ]; then
    die "a ${OS} sandbox already exists in ${REGION} (${existing}).
  Connection details:  ${SCRIPT_NAME} info ${OS}
  Remove it first:     ${SCRIPT_NAME} delete ${OS}"
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
  else
    log "Waiting for the instance to pass its status checks..."
    aws_ ec2 wait instance-status-ok --instance-ids "$iid"
    wait_for_bootstrap "$ip"
    admin_pw=$(generate_password)
    set_remote_password "$ip" "$ADMIN_USER" "$admin_pw"
    cache_password "$PW_ADMIN_PATH" "$admin_pw"
  fi

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
  local iid state ip admin_pw user_pw
  iid=$(find_instance "$LIVE_STATES")
  [ -n "$iid" ] || die "no ${OS} sandbox in ${REGION}. Create one with: ${SCRIPT_NAME} create ${OS}"

  state=$(instance_field "$iid" "State.Name")
  ip=$(instance_field "$iid" "PublicIpAddress")
  INSTANCE_TYPE=$(instance_field "$iid" "InstanceType")

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

confirm_delete() {
  local reply
  [ "$ASSUME_YES" -eq 1 ] && return 0
  if [ ! -t 0 ]; then
    die "refusing to delete without confirmation; pass -y to proceed non-interactively"
  fi
  printf 'Delete the %s sandbox in %s? [y/N] ' "$OS" "$REGION" >&2
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

cmd_delete() {
  local iid vpc sg_id did_something=0

  iid=$(find_instance "${LIVE_STATES},shutting-down")
  vpc=$(aws_ ec2 describe-vpcs --filters "Name=isDefault,Values=true" \
          --query 'Vpcs[0].VpcId' --output text 2>/dev/null || true)

  if [ -n "$iid" ]; then
    confirm_delete
    terminate_instance "$iid"
    did_something=1
  else
    log "No ${OS} instance in ${REGION}; checking for leftovers."
  fi

  # Sweep an orphaned security group even when no instance exists, e.g. after a
  # create that failed partway through.
  if ! is_none "$vpc"; then
    sg_id=$(find_security_group "$vpc")
    if ! is_none "$sg_id"; then
      if [ "$did_something" -eq 0 ]; then confirm_delete; fi
      delete_security_group "$vpc"
      did_something=1
    fi
  fi

  rm -f "$RDP_ADMIN_PATH" "$RDP_USER_PATH" "$PW_ADMIN_PATH" "$PW_USER_PATH" \
        "${STATE_DIR}/user-data" 2>/dev/null || true

  if [ "$did_something" -eq 1 ]; then
    ok "Deleted the ${OS} sandbox. Key pair ${KEY_NAME} and ${PEM_PATH} were kept for reuse."
  else
    log "Nothing to delete for ${OS} in ${REGION}."
  fi
}

# ---------------------------------------------------------------------------

main() {
  parse_args "$@"
  require_cmds aws curl ssh ssh-keygen openssl
  resolve_region
  set_os_profile
  preflight_identity

  case "$ACTION" in
    create) cmd_create ;;
    delete) cmd_delete ;;
    info)   cmd_info ;;
  esac
}

main "$@"

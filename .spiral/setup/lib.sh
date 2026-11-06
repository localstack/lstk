# shellcheck shell=sh disable=SC2034 # defines variables for the scripts that source it
# Shared by .spiral/setup.sh and the pieces under .spiral/setup/, as
#   . "$(dirname "$0")/lib.sh"
# Leaves the caller at the repo root with the fixed install dirs on PATH. POSIX sh.
set -eu

root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$root"
grep -qx 'module github.com/localstack/lstk' go.mod 2>/dev/null || {
  echo "setup: not at the lstk repo root ($root)" >&2
  exit 1
}

log() { echo "setup: $*"; }

# The repo image bake marks its build instance (spiral: images/repo/image.pkr.hcl).
# The bake has open egress; a task boot reaches only Spiral's proxy allowlist,
# which has proxy.golang.org and github.com but not go.dev.
bake=0
[ -f /etc/spiral/image-build ] && bake=1

# Fixed locations, captured in the AMI. checks/lib.sh and the /etc/profile.d
# snippet toolchain.sh writes put them back on the PATH of later processes.
go_dir=/usr/local/go
bin_dir=/usr/local/bin
export PATH="$go_dir/bin:$bin_dir:$PATH"

case "$(uname -m)" in
  aarch64 | arm64) arch=arm64 ;;
  x86_64) arch=amd64 ;;
  *) echo "setup: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

#!/bin/sh
# Piece of .spiral/setup.sh: the Go toolchain go.mod names, and the tools the
# repo pins elsewhere: mockgen (go.mod), golangci-lint (.tool-versions; `make
# lint` refuses any other), gitleaks (.pre-commit-config.yaml), and the OpenSpec CLI.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

# --- Go ------------------------------------------------------------------------
# Installed only at bake, or when the image has no Go at all: go.dev is closed
# at boot, and GOTOOLCHAIN=auto (the default) fetches a newer go.mod toolchain
# through proxy.golang.org, so the weekly Go upgrade needs no rebake to work.
want_go=$(sed -n 's/^toolchain go\([0-9][0-9.]*\)$/\1/p' go.mod)
[ -n "$want_go" ] || want_go=$(sed -n 's/^go \([0-9][0-9.]*\)$/\1/p' go.mod)
have_go=$(go version 2>/dev/null | sed -n 's/^go version go\([0-9][0-9.]*\) .*/\1/p')
if [ -z "$have_go" ] || { [ "$bake" = 1 ] && [ "$have_go" != "$want_go" ]; }; then
  log "installing go$want_go linux/$arch to $go_dir (have: ${have_go:-none})"
  tmp=$(mktemp -d)
  curl -fsSL --retry 3 "https://go.dev/dl/go$want_go.linux-$arch.tar.gz" -o "$tmp/go.tgz"
  rm -rf "$go_dir"
  tar -C "$(dirname "$go_dir")" -xzf "$tmp/go.tgz"
  rm -rf "$tmp"
fi
go version

# --- mockgen -------------------------------------------------------------------------
# `make mock-generate` runs go:generate directives that call mockgen from PATH.
want_mockgen=$(awk '$1 == "go.uber.org/mock" {print $2}' go.mod)
have_mockgen=$(mockgen -version 2>/dev/null || true)
if [ -n "$want_mockgen" ] && [ "$have_mockgen" != "$want_mockgen" ]; then
  log "installing mockgen $want_mockgen to $bin_dir (have: ${have_mockgen:-none})"
  GOBIN="$bin_dir" go install "go.uber.org/mock/mockgen@$want_mockgen"
fi

# --- golangci-lint -----------------------------------------------------------------
# GitHub release assets are on the boot allowlist, so a bump installs at boot too.
want_lint=$(awk '/^golangci-lint/ {print $2}' .tool-versions)
have_lint=$(golangci-lint version --short 2>/dev/null | sed 's/^v//' || true)
if [ "$have_lint" != "$want_lint" ]; then
  log "installing golangci-lint $want_lint linux/$arch to $bin_dir (have: ${have_lint:-none})"
  name="golangci-lint-$want_lint-linux-$arch"
  base="https://github.com/golangci/golangci-lint/releases/download/v$want_lint"
  tmp=$(mktemp -d)
  curl -fsSL --retry 3 "$base/$name.tar.gz" -o "$tmp/$name.tar.gz"
  curl -fsSL --retry 3 "$base/golangci-lint-$want_lint-checksums.txt" -o "$tmp/checksums.txt"
  (cd "$tmp" && grep " $name.tar.gz\$" checksums.txt | sha256sum -c -)
  tar -C "$tmp" -xzf "$tmp/$name.tar.gz"
  install -m 0755 "$tmp/$name/golangci-lint" "$bin_dir/golangci-lint"
  rm -rf "$tmp"
fi
golangci-lint version --short

# --- gitleaks ------------------------------------------------------------------------
# The secret scan developers get from the pre-commit hook, at the rev
# .pre-commit-config.yaml pins; CI does not run it, so checks/gitleaks.sh does.
want_gitleaks=$(awk '/gitleaks\/gitleaks$/ {getline; sub(/^ *rev: *v/, ""); print; exit}' .pre-commit-config.yaml)
have_gitleaks=$(gitleaks version 2>/dev/null | sed 's/^v//' || true)
if [ "$have_gitleaks" != "$want_gitleaks" ]; then
  log "installing gitleaks $want_gitleaks to $bin_dir (have: ${have_gitleaks:-none})"
  case $arch in arm64) gl_arch=arm64 ;; amd64) gl_arch=x64 ;; esac
  name="gitleaks_${want_gitleaks}_linux_$gl_arch.tar.gz"
  base="https://github.com/gitleaks/gitleaks/releases/download/v$want_gitleaks"
  tmp=$(mktemp -d)
  curl -fsSL --retry 3 "$base/$name" -o "$tmp/$name"
  curl -fsSL --retry 3 "$base/gitleaks_${want_gitleaks}_checksums.txt" -o "$tmp/checksums.txt"
  (cd "$tmp" && grep " $name\$" checksums.txt | sha256sum -c -)
  tar -C "$tmp" -xzf "$tmp/$name" gitleaks
  install -m 0755 "$tmp/gitleaks" "$bin_dir/gitleaks"
  rm -rf "$tmp"
fi
gitleaks version

# --- OpenSpec CLI ------------------------------------------------------------------
# For writing and validating openspec/changes/. registry.npmjs.org is on the
# boot allowlist, so a bump installs at boot too. Node comes with the base image.
want_openspec=1.15.0
have_openspec=$(openspec --version 2>/dev/null || true)
if [ "$have_openspec" != "$want_openspec" ]; then
  log "installing @fission-ai/openspec@$want_openspec (have: ${have_openspec:-none})"
  npm install -g --no-fund --no-audit "@fission-ai/openspec@$want_openspec"
fi
OPENSPEC_TELEMETRY=0 openspec config set telemetry.enabled false >/dev/null
openspec --version

# Login shells an agent opens get a fresh environment; put Go on their PATH.
if [ -d /etc/profile.d ]; then
  cat >/etc/profile.d/20-spiral-go.sh <<PROFILE
# Written by .spiral/setup.sh: the Go toolchain on the login PATH.
case ":\$PATH:" in
  *":$go_dir/bin:"*) ;;
  *) export PATH="$go_dir/bin:\$HOME/go/bin:\$PATH" ;;
esac
PROFILE
  chmod 644 /etc/profile.d/20-spiral-go.sh
fi

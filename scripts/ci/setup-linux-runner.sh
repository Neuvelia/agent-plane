#!/usr/bin/env bash
# Prepares an Ubuntu x64 machine to run this repository's Linux CI jobs as
# self-hosted GitHub Actions runners (docs/development-workflow.md,
# "Self-hosted runners"). Run it once, as root, from a checkout of this
# repository:
#
#   sudo RUNNER_TOKEN=<registration token> scripts/ci/setup-linux-runner.sh
#
# The registration token comes from GitHub: Settings -> Actions -> Runners ->
# New self-hosted runner (valid for one hour; one token registers all
# instances). Optional variables:
#
#   RUNNER_COUNT   runner instances on this machine, one job each (default 3)
#   RUNNER_USER    unprivileged account the runners run as (default ghrunner)
#   REPO_URL       repository or organization URL (default this repository)
#   RUNNER_GROUP   runner group, for an organization URL only
#
# What it does: installs the build tools CI needs (git, make, a C compiler for
# `go test -race`, jq, xvfb), the system libraries for Playwright Chromium and
# Electron (so jobs need no sudo), creates RUNNER_USER without sudo, and
# installs RUNNER_COUNT runner services labelled `self-hosted,linux,x64`.
# Go and Node are not installed system-wide: setup-go and setup-node fetch the
# versions each job asks for. Runners must not run as root: several tests skip
# as root, and Electron then needs --no-sandbox.
set -euo pipefail

RUNNER_VERSION=2.338.2
RUNNER_SHA256=baebf204307bac6193480852732f0aba2ae97b6c5597c244f4d5558390147c40
# Used once, only to run `playwright install-deps`; jobs use setup-node.
NODE_VERSION=24.22.3
NODE_SHA256=4f2b175a6d53a5c617a80dba2676238a0688cef28c6da4eba35c7d844e66fcfb

RUNNER_COUNT=${RUNNER_COUNT:-3}
RUNNER_USER=${RUNNER_USER:-ghrunner}
REPO_URL=${REPO_URL:-https://github.com/Neuvelia/agent-plane}
RUNNER_GROUP=${RUNNER_GROUP:-}
RUNNER_ROOT=/opt/actions-runner

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

fail() {
  echo "setup-linux-runner: $*" >&2
  exit 1
}

check_host() {
  [[ $(id -u) -eq 0 ]] || fail "run as root (sudo)"
  [[ $(uname -m) == x86_64 ]] || fail "only x86_64 is supported, found $(uname -m)"
  grep -q '^ID=ubuntu' /etc/os-release || fail "only Ubuntu is supported"
  [[ -n ${RUNNER_TOKEN:-} ]] || fail "set RUNNER_TOKEN to a runner registration token"
  [[ $RUNNER_COUNT =~ ^[1-9][0-9]?$ ]] || fail "RUNNER_COUNT must be 1-99"
  [[ $RUNNER_USER =~ ^[a-z_][a-z0-9_-]*$ ]] || fail "RUNNER_USER is not a valid user name"
  [[ $RUNNER_USER != root ]] || fail "RUNNER_USER must not be root"
}

# Installs the first package of a list that this Ubuntu release has: library
# packages were renamed (for example libgtk-3-0 -> libgtk-3-0t64 in 24.04).
install_first_available() {
  local package
  for package in "$@"; do
    if apt-cache show "$package" >/dev/null 2>&1; then
      apt-get install -y --no-install-recommends "$package"
      return
    fi
  done
  fail "none of these packages exist: $*"
}

install_packages() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y --no-install-recommends \
    ca-certificates curl git make gcc libc6-dev jq tar xz-utils unzip xvfb xauth
  # Electron needs GTK and its dependencies beyond what Playwright Chromium lists.
  install_first_available libgtk-3-0t64 libgtk-3-0
  install_first_available libnss3
  install_first_available libxss1
}

# Downloads a file and refuses it unless its SHA-256 matches.
download_verified() {
  local url=$1 sha256=$2 target=$3
  curl -fsSL --retry 3 -o "$target" "$url"
  echo "$sha256  $target" | sha256sum --check --quiet - || fail "checksum mismatch for $url"
}

install_playwright_deps() {
  local version node_dir
  version=$(jq -r '.devDependencies.playwright // .dependencies.playwright // empty' \
    "$repo_root/desktop/package.json")
  [[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "desktop/package.json must pin an exact playwright version, found '$version'"
  download_verified \
    "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-linux-x64.tar.xz" \
    "$NODE_SHA256" "$work_dir/node.tar.xz"
  node_dir="$work_dir/node"
  mkdir -p "$node_dir"
  tar -xJf "$work_dir/node.tar.xz" -C "$node_dir" --strip-components=1
  PATH="$node_dir/bin:$PATH" npm_config_cache="$work_dir/npm" \
    npx --yes "playwright@$version" install-deps chromium
}

create_user() {
  if id "$RUNNER_USER" >/dev/null 2>&1; then
    if id -nG "$RUNNER_USER" | grep -Eqw 'sudo|admin|wheel'; then
      fail "$RUNNER_USER can use sudo; use an account without it"
    fi
    return
  fi
  useradd --create-home --shell /bin/bash "$RUNNER_USER"
}

install_runner() {
  local index=$1 dir name
  name="$(hostname -s)-$index"
  dir="$RUNNER_ROOT/$name"
  if [[ -f $dir/.runner ]]; then
    echo "runner $name is already configured; skipping"
    return
  fi
  mkdir -p "$dir"
  tar -xzf "$work_dir/runner.tar.gz" -C "$dir"
  chown -R "$RUNNER_USER:$RUNNER_USER" "$dir"
  # shellcheck disable=SC2054 # "linux,x64" is one --labels value, not two elements.
  local config=(./config.sh --unattended --replace --url "$REPO_URL" --token "$RUNNER_TOKEN"
    --name "$name" --labels linux,x64 --work _work)
  [[ -z $RUNNER_GROUP ]] || config+=(--runnergroup "$RUNNER_GROUP")
  (cd "$dir" && runuser -u "$RUNNER_USER" -- "${config[@]}")
  (cd "$dir" && ./svc.sh install "$RUNNER_USER" && ./svc.sh start)
}

main() {
  check_host
  install_packages
  install_playwright_deps
  create_user
  download_verified \
    "https://github.com/actions/runner/releases/download/v$RUNNER_VERSION/actions-runner-linux-x64-$RUNNER_VERSION.tar.gz" \
    "$RUNNER_SHA256" "$work_dir/runner.tar.gz"
  # The runner is a .NET program; its own script installs the libraries it needs.
  mkdir -p "$work_dir/runner"
  tar -xzf "$work_dir/runner.tar.gz" -C "$work_dir/runner"
  "$work_dir/runner/bin/installdependencies.sh"
  local index
  for index in $(seq 1 "$RUNNER_COUNT"); do
    install_runner "$index"
  done
  echo "setup-linux-runner: $RUNNER_COUNT runner(s) installed for $REPO_URL as $RUNNER_USER."
  echo "Next: set the repository variable CI_RUNNER_LINUX to [\"self-hosted\",\"linux\",\"x64\"]."
}

main "$@"

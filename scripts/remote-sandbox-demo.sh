#!/usr/bin/env bash
set -euo pipefail

host="${TORQUE_SANDBOX_DEMO_HOST:-root@188.124.37.233}"
remote_dir="${TORQUE_SANDBOX_DEMO_REMOTE_DIR:-}"
policy_rel="${TORQUE_SANDBOX_DEMO_POLICY_REL:-sandbox/linux-ci.cfg}"

red() { printf "\033[31m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    red "Не найдено: $1"
    exit 2
  fi
}

need ssh
need tar

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ -z "$remote_dir" ]]; then
  remote_dir="/root/torque-sandbox-demo-$(date +%s)"
fi

echo ">> building torque for linux/amd64 locally"
make build-linux-amd64 >/dev/null

echo ">> staging repo to ${host}:${remote_dir}"
ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new "$host" "mkdir -p '$remote_dir'"

( tar czf - \
  --exclude-vcs \
  --exclude='./bin' \
  --exclude='./dist' \
  --exclude='./torque-capture-*.sqlite' \
  . ) | ssh "$host" "tar xzf - -C '$remote_dir'"

echo ">> uploading linux torque binary"
ssh "$host" "mkdir -p '$remote_dir/bin'"
cat "./bin/torque-linux-amd64" | ssh "$host" "cat > '$remote_dir/bin/torque' && chmod +x '$remote_dir/bin/torque'"

echo ">> running sandbox demo on remote"
ssh "$host" "cd '$remote_dir' && export TORQUE_BIN='./bin/torque' && export TORQUE_SANDBOX_CONFIG='$remote_dir/$policy_rel' && ./scripts/sandbox-demo.sh"

yellow "Note: remote repo directory left at $remote_dir (set TORQUE_SANDBOX_DEMO_REMOTE_DIR to reuse/cleanup)."

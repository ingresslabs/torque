#!/usr/bin/env bash
set -euo pipefail

# Real-cluster e2e verification for `torque stack` verify (Kubernetes-only health gates).
#
# This suite is separate from scripts/stack-e2e-real.sh because it intentionally
# creates real workloads (Deployments/Pods), not just ConfigMaps.
#
# Required:
#   KUBECONFIG_PATH=/path/to/kubeconfig
#   TORQUE_STACK_VERIFY_E2E_CONFIRM=1
#
# Optional:
#   KUBE_CONTEXT=...
#   TORQUE_STACK_VERIFY_E2E_NAMESPACE=torque-stack-verify-e2e
#

ROOT_BASE="${ROOT_BASE:-testdata/stack/verify-e2e-real}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
NAMESPACE="${TORQUE_STACK_VERIFY_E2E_NAMESPACE:-torque-stack-verify-e2e}"

if [[ "${TORQUE_STACK_VERIFY_E2E_CONFIRM:-}" != "1" ]]; then
  echo "Refusing to run without TORQUE_STACK_VERIFY_E2E_CONFIRM=1" >&2
  echo "This script talks to a real cluster and will create Deployments/Pods." >&2
  exit 2
fi

if [[ -z "${KUBECONFIG_PATH}" ]]; then
  echo "missing KUBECONFIG_PATH" >&2
  exit 2
fi
if [[ ! -f "${KUBECONFIG_PATH}" ]]; then
  echo "missing kubeconfig: ${KUBECONFIG_PATH}" >&2
  exit 2
fi
if ! command -v kubectl >/dev/null 2>&1; then
  echo "missing kubectl in PATH" >&2
  exit 2
fi

file_matches() {
  local pattern="$1"
  local file="$2"
  if command -v rg >/dev/null 2>&1; then
    rg -n "${pattern}" "${file}" >/dev/null
  else
    grep -En "${pattern}" "${file}" >/dev/null
  fi
}

echo "torque stack verify real-cluster e2e"
echo "  fixtures:    ${ROOT_BASE}"
echo "  kubeconfig:  ${KUBECONFIG_PATH}"
if [[ -n "${KUBE_CONTEXT}" ]]; then
  echo "  context:     ${KUBE_CONTEXT}"
fi
echo "  namespace:   ${NAMESPACE}"
echo

make -s build

kubectl_args=(--kubeconfig "${KUBECONFIG_PATH}")
torque_args=(--kubeconfig "${KUBECONFIG_PATH}")
if [[ -n "${KUBE_CONTEXT}" ]]; then
  kubectl_args+=(--context "${KUBE_CONTEXT}")
  torque_args+=(--context "${KUBE_CONTEXT}")
fi

echo ">> ensure namespace ${NAMESPACE}"
kubectl "${kubectl_args[@]}" get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl "${kubectl_args[@]}" create ns "${NAMESPACE}"

tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-stack-verify-e2e-real.XXXXXX")"
cleanup() { rm -rf "${tmp_root}"; }
trap cleanup EXIT

copy_fixture_tree() {
  local dst="$1"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete --exclude ".torque" "${ROOT_BASE}/" "${dst}/"
  else
    mkdir -p "${dst}"
    cp -R "${ROOT_BASE}/." "${dst}/"
    rm -rf "${dst}/"*/.torque || true
  fi
}

rewrite_fixture_yaml() {
  local root="$1"
  python3 - "${root}" "${KUBECONFIG_PATH}" "${NAMESPACE}" <<'PY'
import os
import sys

root = sys.argv[1]
kubeconfig = sys.argv[2]
namespace = sys.argv[3]

def rewrite(path: str) -> None:
    with open(path, "r", encoding="utf-8") as f:
        lines = f.read().splitlines(True)

    out = []
    for line in lines:
        line = line.replace("kubeconfig: ~/.kube/config", f"kubeconfig: {kubeconfig}")
        line = line.replace("namespace: torque-stack-verify-e2e", f"namespace: {namespace}")
        out.append(line)

    with open(path, "w", encoding="utf-8") as f:
        f.write("".join(out))

for base, dirs, files in os.walk(root):
    for name in files:
        if name in ("stack.yaml", "release.yaml"):
            rewrite(os.path.join(base, name))
PY
}

must_fail() {
  local desc="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "expected failure but succeeded: ${desc}" >&2
    return 1
  fi
  return 0
}

echo ">> staging fixtures into temp dir"
work="${tmp_root}/fixtures"
copy_fixture_tree "${work}"
rewrite_fixture_yaml "${work}"

root="${work}/01-deploy-not-ready"

echo ">> plan (${root})"
./bin/torque "${torque_args[@]}" stack plan --config "${root}" --output table >/dev/null

echo ">> apply expect verify failure (${root})"
must_fail "verify should fail" ./bin/torque "${torque_args[@]}" stack apply --config "${root}" --yes --retry 1

echo ">> status shows verify failed (${root})"
status_raw_out="${root}/.status.raw.jsonl"
./bin/torque "${torque_args[@]}" stack status --config "${root}" --format raw --tail 200 >"${status_raw_out}"
file_matches '"phase"[[:space:]]*:[[:space:]]*"verify"' "${status_raw_out}"
file_matches '"status"[[:space:]]*:[[:space:]]*"failed"' "${status_raw_out}"

echo ">> fix image and re-apply (${root})"
python3 - "${root}" <<'PY'
import os,sys
root=sys.argv[1]
path=os.path.join(root,"stack.yaml")
data=open(path,"r",encoding="utf-8").read()
data=data.replace('image: \"example.invalid/does-not-exist:0\"','image: \"busybox:1\"')
data=data.replace('failOnWarnings: true','failOnWarnings: false')
open(path,"w",encoding="utf-8").write(data)
PY

./bin/torque "${torque_args[@]}" stack apply --config "${root}" --yes --retry 5 >/dev/null

echo ">> delete cleanup (${root})"
./bin/torque "${torque_args[@]}" stack delete --config "${root}" --yes --retry 2 >/dev/null

echo "All stack verify e2e checks passed"

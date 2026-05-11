#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KUBECONFIG_PATH="${KUBECONFIG:-}"
NAMESPACE="${E2E_NAMESPACE:-default}"
RELEASE="${E2E_RELEASE:-gi$(date +%s)}"
OUT_DIR="${E2E_OUT_DIR:-$(mktemp -d)}"
CHART="${ROOT_DIR}/testdata/charts/torque-guardian-incident-e2e"
BIN_DIR="$(mktemp -d)"
BIN="${TORQUE_BIN:-${BIN_DIR}/torque}"
PLACEHOLDER_SECRET="test-fixture-secret-placeholder"

if [[ -z "${KUBECONFIG_PATH}" ]]; then
  echo "KUBECONFIG is required" >&2
  exit 2
fi

cleanup() {
  set +e
  "${BIN}" --kubeconfig "${KUBECONFIG_PATH}" delete --release "${RELEASE}" -n "${NAMESPACE}" --yes --wait=false --timeout 2m >/dev/null 2>&1 || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n torque-system delete configmap torque-guardian serviceaccount torque-guardian --ignore-not-found >/dev/null 2>&1 || true
  kubectl --kubeconfig "${KUBECONFIG_PATH}" delete clusterrolebinding torque-guardian clusterrole torque-guardian --ignore-not-found >/dev/null 2>&1 || true
  rm -rf "${BIN_DIR}"
}
trap cleanup EXIT

if [[ -z "${TORQUE_BIN:-}" ]]; then
  (cd "${ROOT_DIR}" && go build -o "${BIN}" ./cmd/torque)
fi

mkdir -p "${OUT_DIR}"

"${BIN}" --kubeconfig "${KUBECONFIG_PATH}" guardian install --namespace torque-system --mode observe --dry-run > "${OUT_DIR}/guardian-install.yaml"
rg 'verbs: \["get", "list", "watch"\]' "${OUT_DIR}/guardian-install.yaml" >/dev/null
if rg '"(create|update|patch|delete)"' "${OUT_DIR}/guardian-install.yaml" >/dev/null; then
  echo "guardian install dry-run exposed a mutation verb" >&2
  exit 1
fi

"${BIN}" --kubeconfig "${KUBECONFIG_PATH}" guardian install --namespace torque-system --mode observe > "${OUT_DIR}/guardian-install.txt"
kubectl --kubeconfig "${KUBECONFIG_PATH}" auth can-i --as=system:serviceaccount:torque-system:torque-guardian watch pods --all-namespaces > "${OUT_DIR}/guardian-can-watch-pods.txt" || true
kubectl --kubeconfig "${KUBECONFIG_PATH}" auth can-i --as=system:serviceaccount:torque-system:torque-guardian patch configmaps -n "${NAMESPACE}" > "${OUT_DIR}/guardian-can-patch-configmaps.txt" || true
test "$(cat "${OUT_DIR}/guardian-can-watch-pods.txt")" = "yes"
test "$(cat "${OUT_DIR}/guardian-can-patch-configmaps.txt")" = "no"

"${BIN}" --kubeconfig "${KUBECONFIG_PATH}" apply --chart "${CHART}" --release "${RELEASE}" -n "${NAMESPACE}" --wait=false --atomic=false --timeout 2m --yes > "${OUT_DIR}/apply.stdout" 2> "${OUT_DIR}/apply.stderr"

FULLNAME="${RELEASE}-torque-guardian-incident-e2e"
kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" patch configmap "${FULLNAME}" --type merge -p '{"data":{"mode":"drifted-by-e2e","password":"test-fixture-secret-placeholder"}}' > "${OUT_DIR}/configmap-patch.txt"

for _ in $(seq 1 30); do
  if kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get pods -l "app.kubernetes.io/instance=${RELEASE}" -o json | jq -e '.items | length > 0' >/dev/null; then
    break
  fi
  sleep 1
done

kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" get pods -l "app.kubernetes.io/instance=${RELEASE}" -o wide > "${OUT_DIR}/pods.txt" || true
kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${NAMESPACE}" describe deploy "${FULLNAME}" > "${OUT_DIR}/deployment.describe.txt" || true

"${BIN}" --kubeconfig "${KUBECONFIG_PATH}" apply simulate --chart "${CHART}" --release "${RELEASE}" -n "${NAMESPACE}" --out "${OUT_DIR}/torque-sim-proof" --format json > "${OUT_DIR}/simulate.stdout.json"
"${BIN}" --kubeconfig "${KUBECONFIG_PATH}" guardian diff --source "${OUT_DIR}/torque-sim-proof" --live --out "${OUT_DIR}/drift-proof.json" --format json > "${OUT_DIR}/guardian-diff.stdout.json"
"${BIN}" --kubeconfig "${KUBECONFIG_PATH}" guardian diff --source "${OUT_DIR}/torque-sim-proof" --live --out "${OUT_DIR}/torque-runtime-proof" > "${OUT_DIR}/guardian-diff-dir.txt"

"${BIN}" --kubeconfig "${KUBECONFIG_PATH}" incident capture --release "${RELEASE}" -n "${NAMESPACE}" --since 30m --out "${OUT_DIR}/incident.torque" --format json > "${OUT_DIR}/incident-capture.stdout.json"
"${BIN}" incident replay "${OUT_DIR}/incident.torque" --lab k3s --out "${OUT_DIR}/incident-replay-proof" --format json > "${OUT_DIR}/incident-replay.stdout.json"
"${BIN}" incident explain --from "${OUT_DIR}/incident-replay-proof" --out "${OUT_DIR}/root-cause.json" --format json > "${OUT_DIR}/incident-explain.stdout.json"
"${BIN}" incident pr --from "${OUT_DIR}/root-cause.json" --branch "fix/${RELEASE}-incident" --out "${OUT_DIR}/incident-fix" --format json > "${OUT_DIR}/incident-pr.stdout.json"

"${BIN}" contract synthesize --from "${OUT_DIR}/incident-replay-proof" --guardian "${OUT_DIR}/drift-proof.json" --out "${OUT_DIR}/torque-contract.yaml" --format json > "${OUT_DIR}/contract-synthesize.stdout.json"
"${BIN}" contract test --contract "${OUT_DIR}/torque-contract.yaml" --from "${OUT_DIR}/incident-replay-proof" --guardian "${OUT_DIR}/drift-proof.json" --out "${OUT_DIR}/contract-proof.json" --format json > "${OUT_DIR}/contract-test.stdout.json"
if "${BIN}" contract test --contract "${OUT_DIR}/torque-contract.yaml" --from "${OUT_DIR}/incident-replay-proof" --guardian "${OUT_DIR}/drift-proof.json" --fail-on-blocked > "${OUT_DIR}/contract-test-fail.stdout" 2> "${OUT_DIR}/contract-test-fail.stderr"; then
  echo "contract test --fail-on-blocked unexpectedly passed broken evidence" >&2
  exit 1
fi
"${BIN}" contract pr --contract "${OUT_DIR}/torque-contract.yaml" --proof "${OUT_DIR}/contract-proof.json" --branch "add/${RELEASE}-runtime-contract" --out "${OUT_DIR}/contract-fix" --format json > "${OUT_DIR}/contract-pr.stdout.json"

jq -e '.status == "drifted" and .blocked == true and .summary.changed == 1 and .summary.runtimeBoundaryFindings >= 1' "${OUT_DIR}/drift-proof.json" >/dev/null
jq -e '.blocked == true and .primaryCause == "image_pull_failure"' "${OUT_DIR}/root-cause.json" >/dev/null
jq -e '.observeOnly == true and .summary.resources >= 3 and .summary.boundaryFindings >= 1' "${OUT_DIR}/incident.torque" >/dev/null
jq -e '.kind == "RuntimeContract" and (.spec.invariants | length) >= 5' "${OUT_DIR}/contract-synthesize.stdout.json" >/dev/null
jq -e '.blocked == true and .summary.failed >= 5 and .summary.criticalFailures >= 1' "${OUT_DIR}/contract-proof.json" >/dev/null
rg 'kind: RuntimeContract' "${OUT_DIR}/torque-contract.yaml" >/dev/null

test -s "${OUT_DIR}/torque-runtime-proof/manifest.json"
test -s "${OUT_DIR}/torque-runtime-proof/fix/pr.md"
test -s "${OUT_DIR}/incident-replay-proof/manifest.json"
test -s "${OUT_DIR}/incident-replay-proof/root-cause.json"
test -s "${OUT_DIR}/incident-replay-proof/fix/pr.md"
test -s "${OUT_DIR}/incident-fix/incident-fix.patch"
test -s "${OUT_DIR}/incident-fix/pr.md"
test -s "${OUT_DIR}/torque-contract.yaml"
test -s "${OUT_DIR}/contract-proof.json"
test -s "${OUT_DIR}/contract-fix/runtime-contract.patch"
test -s "${OUT_DIR}/contract-fix/pr.md"

if rg "${PLACEHOLDER_SECRET}" "${OUT_DIR}/drift-proof.json" "${OUT_DIR}/torque-runtime-proof" "${OUT_DIR}/incident.torque" "${OUT_DIR}/incident-replay-proof" "${OUT_DIR}/incident-fix" "${OUT_DIR}/torque-contract.yaml" "${OUT_DIR}/contract-proof.json" "${OUT_DIR}/contract-fix" >/dev/null; then
  echo "E2E output leaked placeholder secret" >&2
  exit 1
fi

echo "OUT_DIR=${OUT_DIR}"
echo "release=${RELEASE}"
echo "guardian_status=$(jq -r '.status' "${OUT_DIR}/drift-proof.json")"
echo "guardian_changed=$(jq -r '.summary.changed' "${OUT_DIR}/drift-proof.json") guardian_boundary=$(jq -r '.summary.runtimeBoundaryFindings' "${OUT_DIR}/drift-proof.json")"
echo "incident_cause=$(jq -r '.primaryCause' "${OUT_DIR}/root-cause.json") incident_blocked=$(jq -r '.blocked' "${OUT_DIR}/root-cause.json")"
echo "contract_failed=$(jq -r '.summary.failed' "${OUT_DIR}/contract-proof.json") contract_critical=$(jq -r '.summary.criticalFailures' "${OUT_DIR}/contract-proof.json")"
echo "guardian_can_watch=$(cat "${OUT_DIR}/guardian-can-watch-pods.txt") guardian_can_patch=$(cat "${OUT_DIR}/guardian-can-patch-configmaps.txt")"

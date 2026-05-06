#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-testdata/stack/smoke/basic}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-$HOME/.kube/config}"
NAMESPACE="${TORQUE_STACK_SMOKE_NAMESPACE:-torque-stack-smoke}"

echo ">> build"
make -s build

echo ">> ensure namespace ${NAMESPACE}"
kubectl --kubeconfig "${KUBECONFIG_PATH}" get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl --kubeconfig "${KUBECONFIG_PATH}" create ns "${NAMESPACE}"

echo ">> plan"
./bin/torque --kubeconfig "${KUBECONFIG_PATH}" stack plan --config "${ROOT}" --output table

echo ">> graph"
./bin/torque --kubeconfig "${KUBECONFIG_PATH}" stack graph --config "${ROOT}" --format dot >/dev/null

echo ">> apply"
./bin/torque --kubeconfig "${KUBECONFIG_PATH}" stack apply --config "${ROOT}" --concurrency 2 --yes --retry 2

echo ">> resume (noop)"
./bin/torque --kubeconfig "${KUBECONFIG_PATH}" stack apply --config "${ROOT}" --resume --yes --retry 2

echo ">> delete"
./bin/torque --kubeconfig "${KUBECONFIG_PATH}" stack delete --config "${ROOT}" --concurrency 2 --yes --retry 2

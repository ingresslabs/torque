#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

torque="${TORQUE_BIN:-./bin/torque}"
if [[ ! -x "$torque" ]]; then
  echo "torque binary not found at $torque; building..." >&2
  make build >/dev/null
fi

say() { printf "\n===== %s =====\n" "$*"; }
pause() { sleep "${1:-2}"; }

os="$(uname -s | tr '[:upper:]' '[:lower:]')"

say "Demo starts (target ~5 minutes)"
echo "Repo: $repo_root"
echo "torque:  $torque"
echo "OS:   $os"
pause 2

say "1) Build (multistage) + SBOM + attest-dir export"
rm -rf /tmp/torque-demo-attest >/dev/null 2>&1 || true
mkdir -p /tmp/torque-demo-attest
"$torque" build testdata/build/dockerfiles/multistage --sbom --attest-dir /tmp/torque-demo-attest
echo "Attestations: /tmp/torque-demo-attest"
pause 4

say "2) Capture build session to SQLite (--capture)"
rm -f /tmp/torque-demo-build.sqlite >/dev/null 2>&1 || true
"$torque" build testdata/build/dockerfiles/multistage --sbom --capture /tmp/torque-demo-build.sqlite --capture-tag demo=1
echo "Capture DB: /tmp/torque-demo-build.sqlite"
pause 4

say "3) torque apply plan --visualize (HTML output)"
rm -f /tmp/torque-demo-plan.html >/dev/null 2>&1 || true
if [[ -d testdata/charts ]] && [[ -d testdata/charts/hello || -d testdata/charts/hello-world ]]; then
  chart="./testdata/charts/hello"
  [[ -d "$chart" ]] || chart="./testdata/charts/hello-world"
  "$torque" plan --visualize --chart "$chart" --release torque-demo --output /tmp/torque-demo-plan.html || true
  echo "Plan HTML: /tmp/torque-demo-plan.html"
else
  echo "SKIP: no demo chart found under testdata/charts/"
fi
pause 6

say "4) torque apply --dry-run + --ui (live viewer)"
if [[ -z "${KUBECONFIG:-}" ]]; then
  echo "SKIP: KUBECONFIG not set; apply demo needs a reachable cluster"
else
  chart="./testdata/charts/hello"
  [[ -d "$chart" ]] || chart="./testdata/charts/hello-world"
  if [[ -d "$chart" ]]; then
    echo "Starting UI on :8080 for ~45s; open it in a browser if you want."
    # run in background so the script returns; kill after a short window
    "$torque" apply --dry-run --ui :8080 --chart "$chart" --release torque-demo --namespace default >/tmp/torque-demo-apply.out 2>&1 &
    pid=$!
    sleep 45
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
    echo "Apply output captured at /tmp/torque-demo-apply.out"
  else
    echo "SKIP: no demo chart found under testdata/charts/"
  fi
fi
pause 4

say "5) Build stream over WebSocket (--ws-listen)"
echo "Starting build stream on ws://localhost:9085 for ~20s..."
echo "Tip: connect with any WS client to watch raw BuildKit events."
"$torque" build testdata/build/dockerfiles/multistage --ws-listen :9085 >/tmp/torque-demo-ws.out 2>&1 &
pid=$!
sleep 20
kill "$pid" >/dev/null 2>&1 || true
wait "$pid" >/dev/null 2>&1 || true
echo "Build output captured at /tmp/torque-demo-ws.out"
pause 4

say "6) Sandbox build demo (Linux-only)"
if [[ "$os" != "linux" ]]; then
  echo "SKIP: sandbox runtime demo requires Linux + nsjail (see scripts/remote-sandbox-demo.sh)"
else
  export TORQUE_SANDBOX_CONFIG="${TORQUE_SANDBOX_CONFIG:-$repo_root/sandbox/linux-ci.cfg}"
  "$torque" build testdata/build/dockerfiles/sandbox-strict --sandbox --sandbox-logs
fi
pause 2

say "Wrap-up"
echo "Attestations: /tmp/torque-demo-attest/"
echo "Capture DB:   /tmp/torque-demo-build.sqlite"
echo "Plan HTML:    /tmp/torque-demo-plan.html (if created)"
echo "Apply output: /tmp/torque-demo-apply.out (if created)"
echo "WS output:    /tmp/torque-demo-ws.out"
echo "Done."

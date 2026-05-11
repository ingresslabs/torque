#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
boole_host="${BOOLE_HOST:-root@87.228.57.103}"
boole_identity="${BOOLE_IDENTITY:-$HOME/.ssh/id_ed25519}"
runs="${RUNS:-100}"
remote_root="${REMOTE_ROOT:-/tmp/torque-proof-e2e}"
torque_bin="${TORQUE_BIN:-}"

ssh_opts=(-o BatchMode=yes -o StrictHostKeyChecking=accept-new)
if [[ -f "$boole_identity" ]]; then
  ssh_opts+=(-i "$boole_identity")
fi
if [[ -n "${BOOLE_SSH_OPTS:-}" ]]; then
  # shellcheck disable=SC2206
  extra_opts=(${BOOLE_SSH_OPTS})
  ssh_opts+=("${extra_opts[@]}")
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

if [[ -z "$torque_bin" ]]; then
  torque_bin="$tmpdir/torque"
  (
    cd "$repo_root"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$torque_bin" ./cmd/torque
  )
fi

source_commit="$(git -C "$repo_root" rev-parse HEAD)"
binary_sha="$(shasum -a 256 "$torque_bin" | awk '{print $1}')"

ssh "${ssh_opts[@]}" "$boole_host" "rm -rf '$remote_root' && mkdir -p '$remote_root/bin' '$remote_root/work'"
rsync -az -e "ssh ${ssh_opts[*]}" "$torque_bin" "$boole_host:$remote_root/bin/torque"

remote_sha="$(ssh "${ssh_opts[@]}" "$boole_host" "chmod +x '$remote_root/bin/torque' && sha256sum '$remote_root/bin/torque' | awk '{print \$1}'")"
if [[ "$remote_sha" != "$binary_sha" ]]; then
  printf 'binary checksum mismatch: local=%s remote=%s\n' "$binary_sha" "$remote_sha" >&2
  exit 1
fi

ssh "${ssh_opts[@]}" "$boole_host" \
  "RUNS='$runs' SOURCE_COMMIT='$source_commit' TORQUE_SHA='$binary_sha' REMOTE_ROOT='$remote_root' bash -se" <<'REMOTE'
set -euo pipefail

torque="$REMOTE_ROOT/bin/torque"
root="$REMOTE_ROOT/work/release-proof-graph"
rm -rf "$root"
mkdir -p "$root/evidence" "$root/fixes" "$root/chart" "$root/runs" "$root/.torque/keys"
cd "$root"

git init -q
git config user.email e2e@torque.local
git config user.name "Torque E2E"
printf 'release proof graph e2e for %s\n' "$SOURCE_COMMIT" > README.md
git add README.md
git commit -q -m "seed release proof e2e"
fixture_commit="$(git rev-parse HEAD)"

printf '{"passed":true,"status":"passed","summary":{"critical":0,"high":0,"medium":0,"low":0}}\n' > evidence/verifier.report.json
printf 'SQLite format 3\nbuildkit capture for ghcr.io/acme/api@sha256:current\n' > evidence/buildkit-capture.sqlite
printf 'SQLite format 3\nrollout capture api prod\n' > evidence/apply.sqlite
printf 'SQLite format 3\nrollout logs api prod\n' > evidence/rollout-logs.sqlite
cat > evidence/slo.yaml <<'YAML'
minReadyPercent: 100
maxFailedResources: 0
YAML
cat > evidence/sbom.cdx.json <<'JSON'
{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{"name":"api","version":"0.1.0"}},"components":[]}
JSON
cat > evidence/provenance.intoto.jsonl <<'JSONL'
{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://slsa.dev/provenance/v1","subject":[{"name":"ghcr.io/acme/api","digest":{"sha256":"current"}}],"predicate":{"buildType":"torque-e2e"}}
JSONL
cat > evidence/server-dry-run.json <<'JSON'
{"version":"v1","status":"passed","dryRun":true,"summary":{"total":1,"failed":0},"results":[{"resource":{"version":"apps/v1","kind":"Deployment","namespace":"prod","name":"api"},"operation":"server-side-apply","status":"passed","dryRun":true}]}
JSON
cat > drift-proof.json <<'JSON'
{"version":"v1","tool":"torque-guardian","generatedAt":"2026-05-11T18:00:00Z","source":"./torque-sim-proof","release":"api","namespace":"prod","chart":"./chart","renderedManifestSha256":"sha256:rendered-current","clusterHost":"boole","status":"passed","blocked":false,"summary":{"resources":1,"unchanged":1,"changed":0,"missing":0,"fetchErrors":0,"managedFieldOwners":0,"runtimeBoundaryFindings":0,"warningEvents":0,"aftercareIssues":0},"predictedVsLive":{"version":"v1","passed":true},"managedFields":{"version":"v1"},"driftTimeline":{"version":"v1"},"eventsTimeline":{"version":"v1"},"runtimeSecretBoundary":{"version":"v1","passed":true},"rolloutAftercare":{"version":"v1","passed":true}}
JSON
cat > runtime-events.json <<'JSON'
{"version":"v1","tool":"torque-guardian","generatedAt":"2026-05-11T18:00:00Z","clusterHost":"boole","namespace":"prod","since":"5m","startedAt":"2026-05-11T17:55:00Z","eventsTimeline":{"version":"v1","events":[{"time":"2026-05-11T18:00:00Z","type":"Normal","reason":"Ready","message":"deployment available","resource":{"version":"apps/v1","kind":"Deployment","namespace":"prod","name":"api"},"count":1,"source":"e2e"}]},"summary":{"events":1,"warnings":0}}
JSON
cat > fixes/pr.md <<'MD'
# Repair PR

Evidence-linked repair body for release proof graph e2e.
MD
cat > fixes/fix.patch <<'PATCH'
diff --git a/chart/values.yaml b/chart/values.yaml
--- a/chart/values.yaml
+++ b/chart/values.yaml
@@ -1 +1 @@
-image: old
+image: current
PATCH
cat > apply-proof.json <<'JSON'
{
  "version": 1,
  "generatedAt": "2026-05-11T18:00:00Z",
  "startedAt": "2026-05-11T17:59:00Z",
  "finishedAt": "2026-05-11T18:00:00Z",
  "command": ["torque", "apply", "./chart", "--release", "api", "--namespace", "prod", "--proof-bundle", "./apply-proof.json"],
  "release": "api",
  "namespace": "prod",
  "chart": "./chart",
  "chartVersion": "0.1.0",
  "status": "failed",
  "dryRun": true,
  "prediction": {
    "version": 1,
    "generatedAt": "2026-05-11T17:59:10Z",
    "release": "api",
    "namespace": "prod",
    "chart": "./chart",
    "risk": "High",
    "summary": {"creates": 0, "updates": 1, "deletes": 0, "unchanged": 0, "images": 1, "unpinnedImages": 0, "missingDependencies": 0, "quotaFails": 0, "quotaWarnings": 0, "restartingWorkloads": 1},
    "images": [{"resource": "Deployment/prod/api", "container": "api", "image": "ghcr.io/acme/api@sha256:current", "digest": "sha256:current", "pinned": true}],
    "rollback": {"available": true, "revision": 7, "status": "deployed", "confidence": "high"},
    "renderedSha256": "sha256:rendered-current"
  },
  "plan": {
    "release": "api",
    "namespace": "prod",
    "chartVersion": "0.1.0",
    "chartReference": "./chart",
    "renderedSha256": "sha256:rendered-current",
    "images": [{"resource": "Deployment/prod/api", "container": "api", "image": "ghcr.io/acme/api@sha256:current", "digest": "sha256:current", "pinned": true}],
    "verifyReports": [{"path": "evidence/verifier.report.json", "tool": "torque verifier", "passed": true, "blocked": false, "renderedSha256": "sha256:rendered-current", "renderedSha256Matches": true}],
    "buildProvenance": [{"source": "evidence/buildkit-capture.sqlite", "digest": "sha256:current", "tags": ["ghcr.io/acme/api:e2e"], "platforms": ["linux/amd64"], "attestations": [{"type": "slsa-provenance", "path": "evidence/provenance.intoto.jsonl"}, {"type": "sbom", "path": "evidence/sbom.cdx.json"}], "referencedByPlan": true, "verdict": "safe"}],
    "changes": [{"resource": {"kind": "Deployment", "namespace": "prod", "name": "api"}, "change": "update", "diff": "image changed"}],
    "summary": {"creates": 0, "updates": 1, "deletes": 0, "unchanged": 0},
    "generatedAt": "2026-05-11T17:59:00Z"
  },
  "resourceSnapshot": [{"kind": "Deployment", "namespace": "prod", "name": "api", "action": "update", "status": "Failed", "reason": "SLOFailed", "message": "0/1 pods ready"}],
  "rollbackProof": {
    "version": 1,
    "generatedAt": "2026-05-11T18:00:01Z",
    "release": "api",
    "namespace": "prod",
    "chart": "./chart",
    "chartVersion": "0.1.0",
    "mode": "helm-rollback",
    "outcome": "rolled-back",
    "trigger": {"source": "slo", "reason": "SLO failed", "startedAt": "2026-05-11T17:59:00Z", "failedAt": "2026-05-11T18:00:00Z"},
    "slo": {"path": "evidence/slo.yaml", "minReadyPercent": 100, "maxFailedResources": 0},
    "rolledBackToRevision": 7,
    "resourceSnapshot": [{"kind": "Deployment", "namespace": "prod", "name": "api", "action": "rollback", "status": "Ready", "message": "1/1 pods ready"}],
    "rollbackCommand": "helm rollback api 7 -n prod",
    "evidence": ["evidence/slo.yaml"]
  },
  "capturePath": "evidence/apply.sqlite",
  "phaseDurations": {"plan": "1s", "dry-run": "1s", "rollback": "1s"},
  "evidence": ["evidence/server-dry-run.json", "drift-proof.json", "runtime-events.json", "evidence/rollout-logs.sqlite", "evidence/sbom.cdx.json", "evidence/provenance.intoto.jsonl", "fixes/pr.md"]
}
JSON
sed -e 's/sha256:current/sha256:previous/g' -e 's/rendered-current/rendered-previous/g' apply-proof.json > previous-apply-proof.json
cat > agent-request.json <<'JSON'
{
  "version": "v1",
  "actor": "codex-e2e",
  "operation": "apply",
  "command": ["torque", "apply", "--chart", "./chart", "--release", "api", "--namespace", "prod"],
  "release": "api",
  "namespace": "prod",
  "reason": "proof-backed release exercise"
}
JSON

"$torque" stack keygen --out .torque/keys/proof-ed25519.json >/dev/null
attach=(--attach drift-proof.json --attach runtime-events.json --attach evidence/server-dry-run.json --attach evidence/rollout-logs.sqlite --attach evidence/sbom.cdx.json --attach evidence/provenance.intoto.jsonl --attach fixes/pr.md)
"$torque" proof graph ./previous-apply-proof.json "${attach[@]}" --out previous-proof.graph.json --html previous-proof.html --key .torque/keys/proof-ed25519.json >/dev/null
"$torque" proof graph ./apply-proof.json "${attach[@]}" --out proof.graph.json --html proof.html --key .torque/keys/proof-ed25519.json >/dev/null

test -s proof.graph.json
test -s proof.html
grep -q "Torque Proof Graph" proof.html
jq -e '.apiVersion == "torque.dev/proof-graph/v1" and .kind == "ProofGraph" and .signature.algorithm == "ed25519"' proof.graph.json >/dev/null
jq -e '([.artifacts[].type] | index("git-commit") and index("image-digest") and index("build-capture") and index("supply-chain-provenance") and index("sbom") and index("helm-render") and index("verifier-report") and index("server-dry-run") and index("runtime-drift") and index("rollout-events") and index("logs-capture") and index("slo-outcome") and index("rollback-proof") and index("repair-pr"))' proof.graph.json >/dev/null
"$torque" proof verify proof.graph.json --require-signature --pub .torque/keys/proof-ed25519.json --format json > verify.json
jq -e '.passed == true and .signature.verified == true and (.artifacts.checked >= 10) and ((.artifacts.failed // 0) == 0)' verify.json >/dev/null
"$torque" proof verify ./apply-proof.json --format json > verify-source.json
jq -e '.passed == true and .sourceKind == "apply-proof"' verify-source.json >/dev/null
"$torque" proof diff previous-proof.graph.json proof.graph.json --format json > diff.json
jq -e '.changed == true and (.summary.added >= 1 or .summary.removed >= 1 or .summary.changed >= 1)' diff.json >/dev/null
"$torque" proof gate proof.graph.json --out gate.json --format json >/dev/null
jq -e '.passed == true and .summary.failed == 0' gate.json >/dev/null
"$torque" proof attest proof.graph.json --release "e2e-$SOURCE_COMMIT" --key .torque/keys/proof-ed25519.json --out release.attestation.json --format json >/dev/null
jq -e '.verified == true and .signature.algorithm == "ed25519" and .artifacts >= 10' release.attestation.json >/dev/null
"$torque" agent policy check agent-request.json --proof proof.graph.json --allow apply --require-gate --out agent-policy.json --format json >/dev/null
jq -e '.allowed == true and .gate.passed == true' agent-policy.json >/dev/null
"$torque" agent run agent-request.json --proof proof.graph.json --allow apply --require-gate --out agent-run.json --format json >/dev/null
jq -e '.authorized == true and .executed == false' agent-run.json >/dev/null
"$torque" release score proof.graph.json --out release-score.json --format json >/dev/null
jq -e '.score >= 80 and .gatePassed == true and .verified == true' release-score.json >/dev/null
"$torque" flight record proof.graph.json --out release.flight.torque --format json >/dev/null
jq -e '.apiVersion == "torque.dev/release-flight/v1" and (.timeline | length) >= 10 and .score >= 80' release.flight.torque >/dev/null
"$torque" flight replay release.flight.torque --format json > flight-replay.json
jq -e '.passed == true and .events >= 10' flight-replay.json >/dev/null
"$torque" flight explain release.flight.torque --format json > flight-explain.json
jq -e '.summary != "" and (.phases | length) >= 5' flight-explain.json >/dev/null
"$torque" release autopilot proof.graph.json --key .torque/keys/proof-ed25519.json --allow apply --fail-below 80 --out-dir autopilot --format json > release-autopilot.json
jq -e '.passed == true and .gate.passed == true and .score.score >= 80 and .replay.passed == true and .agentPolicy.allowed == true and .attestation.verified == true' release-autopilot.json >/dev/null

cp evidence/verifier.report.json evidence/verifier.report.json.bak
printf '{"passed":false,"status":"failed","tampered":true}\n' > evidence/verifier.report.json
set +e
"$torque" proof verify proof.graph.json --require-signature --format json > tamper.json 2>tamper.err
tamper_code=$?
set -e
test "$tamper_code" -ne 0
jq -e '.passed == false and (.artifacts.mismatched | index("evidence/verifier.report.json"))' tamper.json >/dev/null
mv evidence/verifier.report.json.bak evidence/verifier.report.json

start="$(date +%s)"
for i in $(seq 1 "$RUNS"); do
  graph="runs/proof-${i}.graph.json"
  html="runs/proof-${i}.html"
  verify="runs/verify-${i}.json"
  diff="runs/diff-${i}.json"
  gate="runs/gate-${i}.json"
  attest="runs/attest-${i}.json"
  agent_policy="runs/agent-policy-${i}.json"
  agent_run="runs/agent-run-${i}.json"
  score="runs/release-score-${i}.json"
  flight="runs/release-${i}.flight.torque"
  flight_replay="runs/flight-replay-${i}.json"
  flight_explain="runs/flight-explain-${i}.json"
  autopilot="runs/release-autopilot-${i}.json"
  autopilot_dir="runs/autopilot-${i}"
  "$torque" proof graph ./apply-proof.json "${attach[@]}" --out "$graph" --html "$html" --key .torque/keys/proof-ed25519.json >/dev/null
  "$torque" proof verify "$graph" --require-signature --format json > "$verify"
  jq -e '.passed == true and .signature.verified == true and ((.artifacts.failed // 0) == 0)' "$verify" >/dev/null
  "$torque" proof diff previous-proof.graph.json "$graph" --format json > "$diff"
  jq -e '.version == "v1"' "$diff" >/dev/null
  "$torque" proof gate "$graph" --format json > "$gate"
  jq -e '.passed == true' "$gate" >/dev/null
  "$torque" proof attest "$graph" --release "e2e-$SOURCE_COMMIT" --key .torque/keys/proof-ed25519.json --format json > "$attest"
  jq -e '.verified == true and .signature.algorithm == "ed25519"' "$attest" >/dev/null
  "$torque" agent policy check agent-request.json --proof "$graph" --allow apply --require-gate --format json > "$agent_policy"
  jq -e '.allowed == true and .gate.passed == true' "$agent_policy" >/dev/null
  "$torque" agent run agent-request.json --proof "$graph" --allow apply --require-gate --format json > "$agent_run"
  jq -e '.authorized == true and .executed == false' "$agent_run" >/dev/null
  "$torque" release score "$graph" --format json > "$score"
  jq -e '.score >= 80 and .gatePassed == true' "$score" >/dev/null
  "$torque" flight record "$graph" --out "$flight" --format json >/dev/null
  jq -e '(.timeline | length) >= 10 and .score >= 80' "$flight" >/dev/null
  "$torque" flight replay "$flight" --format json > "$flight_replay"
  jq -e '.passed == true' "$flight_replay" >/dev/null
  "$torque" flight explain "$flight" --format json > "$flight_explain"
  jq -e '.summary != ""' "$flight_explain" >/dev/null
  "$torque" release autopilot "$graph" --key .torque/keys/proof-ed25519.json --allow apply --fail-below 80 --out-dir "$autopilot_dir" --format json > "$autopilot"
  jq -e '.passed == true and .gate.passed == true and .score.score >= 80 and .replay.passed == true and .agentPolicy.allowed == true and .attestation.verified == true' "$autopilot" >/dev/null
  grep -q "Torque Proof Graph" "$html"
  if [ $((i % 20)) -eq 0 ]; then
    echo "loop ${i}/${RUNS} ok" >&2
  fi
done
end="$(date +%s)"

jq -cn \
  --arg host "$(hostname)" \
  --arg sourceCommit "$SOURCE_COMMIT" \
  --arg fixtureCommit "$fixture_commit" \
  --arg binarySha256 "$TORQUE_SHA" \
  --arg graphSha256 "$(jq -r '.signature.graphSha256' proof.graph.json)" \
  --arg attestationSha256 "$(jq -r '.signature.attestationSha256' release.attestation.json)" \
  --argjson runs "$RUNS" \
  --argjson seconds "$((end-start))" \
  --argjson artifacts "$(jq '.artifacts | length' proof.graph.json)" \
  --argjson checked "$(jq '.artifacts.checked' verify.json)" \
  --argjson gateChecks "$(jq '.summary.checks' gate.json)" \
  --argjson agentChecks "$(jq '.checks | length' agent-policy.json)" \
  --argjson releaseScore "$(jq '.score' release-score.json)" \
  --argjson flightEvents "$(jq '.timeline | length' release.flight.torque)" \
  --argjson autopilotPassed "$(jq '.passed' release-autopilot.json)" \
  --argjson diffAdded "$(jq '.summary.added' diff.json)" \
  --argjson diffRemoved "$(jq '.summary.removed' diff.json)" \
  --argjson diffChanged "$(jq '.summary.changed' diff.json)" \
  '{ok:true,host:$host,sourceCommit:$sourceCommit,fixtureCommit:$fixtureCommit,binarySha256:$binarySha256,runs:$runs,seconds:$seconds,artifacts:$artifacts,checkedFiles:$checked,gateChecks:$gateChecks,agentChecks:$agentChecks,releaseScore:$releaseScore,flightEvents:$flightEvents,autopilotPassed:$autopilotPassed,diff:{added:$diffAdded,removed:$diffRemoved,changed:$diffChanged},graphSha256:$graphSha256,attestationSha256:$attestationSha256,tamperFailureVerified:true}'
REMOTE

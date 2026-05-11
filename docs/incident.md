# Torque Incident

Torque Incident is the observe-only incident time machine. It captures runtime
evidence for a release, validates the portable bundle in a lab replay, explains
the likely root cause, and writes PR-ready repair artifacts without mutating the
cluster.

```bash
torque incident capture \
  --release api \
  -n prod \
  --since 1h \
  --out incident.torque

torque incident replay \
  incident.torque \
  --lab k3s \
  --out incident-replay-proof/

torque incident explain \
  --from incident-replay-proof/ \
  --out root-cause.json

torque incident pr \
  --from root-cause.json \
  --branch fix/api-incident
```

## Observe-Only Capture

`incident capture` reads release-labeled Kubernetes state and writes a single
JSON bundle. It does not create, update, patch, delete, roll back, or repair
cluster resources.

Captured evidence includes:

- release-labeled workload, service, config, RBAC, HPA, PDB, and pod objects;
- resource readiness/status snapshots;
- Kubernetes events for the capture window;
- bounded pod log snippets;
- managed-field owners;
- runtime Secret/ConfigMap/env boundary findings;
- rollout aftercare findings;
- a causal timeline and initial root-cause guess.

Secret-like strings are redacted before output is written. Kubernetes Secret
`data` and `stringData` are never copied into the bundle.

## Replay Proof

`incident replay` validates that the bundle is complete enough to explain in a
lab workflow and writes a proof directory:

```text
incident-replay-proof/
  manifest.json
  capture.bundle.json
  replay.result.json
  causal.timeline.json
  events.timeline.json
  logs.redacted.json
  managed-fields.owners.json
  secret.boundary.json
  root-cause.json
  fix/
    incident-fix.patch
    pr.md
```

Replay is intentionally read-only in this first implementation. The `--lab k3s`
label records which validation profile was used; it does not apply resources to
the lab cluster.

## Root Cause And PR

`incident explain` loads either an incident bundle or replay proof directory and
writes a root-cause JSON file. `incident pr` turns that explanation into a
review-ready Markdown body and an evidence patch under `.torque/incidents/`.

The first root-cause heuristics prioritize:

- image pull failures;
- crash loops;
- runtime secret-boundary violations;
- high-severity rollout aftercare and Warning events.

Use Guardian with Incident when you need both source-to-live drift proof and
incident replay evidence:

```bash
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
torque incident capture --release api -n prod --since 1h --out incident.torque
torque incident replay incident.torque --lab k3s --out incident-replay-proof/
```

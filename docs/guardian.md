# Torque Guardian

Torque Guardian is the observe-only runtime proof layer. It connects a
`torque apply simulate` proof to live Kubernetes objects, managed fields,
events, runtime secret-boundary checks, rollout aftercare, and PR-ready repair
artifacts.

```bash
torque guardian install --namespace torque-system --mode observe

torque guardian report --since 24h --out runtime-proof.json

torque guardian diff \
  --source ./torque-sim-proof \
  --live \
  --out drift-proof.json

torque guardian pr \
  --from drift-proof.json \
  --branch fix/runtime-drift
```

## Observe-Only Install

`guardian install` creates read-only RBAC and a config marker. It does not
install a mutating controller and does not apply fixes.

```bash
torque guardian install --namespace torque-system --mode observe
torque guardian install --namespace torque-system --mode observe --dry-run
```

The installed ClusterRole grants only `get`, `list`, and `watch` verbs.

## Runtime Proof

`guardian report` writes a runtime event proof for a time window:

```bash
torque guardian report --since 24h --out runtime-proof.json
torque guardian report --since 30m --all-namespaces --format json
```

The report redacts secret-like strings in event messages before writing output.

## Drift Proof

`guardian diff` compares `predicted-live-state.json` from a simulation proof
against live objects:

```bash
torque apply simulate --chart ./chart --release api -n prod --out ./torque-sim-proof
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
```

It catches:

- live object drift from source/rendered state;
- resources missing after simulation;
- `kubectl edit` or suspicious managed-field owners;
- webhook/controller/HPA field ownership changes;
- Warning events in the aftercare window;
- Deployment availability regressions;
- secret-like values copied into ConfigMaps, metadata, or env values.

The comparison normalizes Helm ownership metadata and common Kubernetes API
defaults when those fields were absent from the desired object, so the proof
stays focused on meaningful runtime drift.

If `--out` ends in `.json`, Guardian writes one JSON drift proof. If `--out` is
a directory, Guardian writes the full runtime proof bundle:

```text
torque-runtime-proof/
  manifest.json
  predicted-vs-live.diff.json
  managed-fields.owners.json
  drift.timeline.json
  events.timeline.json
  runtime.secret.boundary.json
  rollout.aftercare.json
  fix/
    drift-fix.patch
    pr.md
```

## PR Artifacts

`guardian pr` turns a drift proof into a review-ready Markdown body and a patch
that adds a `.torque/guardian/*-drift.md` evidence note:

```bash
torque guardian pr --from drift-proof.json --branch fix/runtime-drift
torque guardian pr --from ./torque-runtime-proof --out ./fix
```

The patch is intentionally conservative: Guardian does not decide whether the
source chart or the live object is authoritative. It records proof and
validation commands so the repair happens through review.

## Incident Replay

Use Incident beside Guardian when drift proof is not enough and you need a
portable failure timeline:

```bash
torque incident capture --release api -n prod --since 1h --out incident.torque
torque incident replay incident.torque --lab k3s --out incident-replay-proof/
```

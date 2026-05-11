# Apply Simulate

`torque apply simulate` is the Live Apply Twin workflow. It renders a Helm
release, compares it with live cluster state, runs Kubernetes server-side
apply dry-run checks, scores rollout risk, and writes a replayable proof bundle
without applying changes.

```bash
torque apply simulate \
  --chart ./chart \
  --release api \
  --namespace prod \
  --from-live \
  --slo ./slo.yaml \
  --security-evidence ./torque-security-evidence \
  --out ./torque-sim-proof
```

The bundle is designed for PRs, CI gates, and incident review:

```text
torque-sim-proof/
  manifest.json
  predicted-live-state.json
  server-dry-run.json
  admission.results.json
  field-ownership.conflicts.json
  quota.capacity.risk.json
  rollout.prediction.json
  verifier.report.json
  apply.proof.json
  fixes/
    fix.patch
    pr.md
```

## What It Proves

- objects that will be created, updated, deleted, or left unchanged;
- server-side apply dry-run results from the Kubernetes API server;
- admission webhook denials and API validation failures;
- field ownership conflicts;
- immutable-field failures;
- namespace/API mapping skips;
- quota capacity failures and warnings;
- rollout prediction risk, missing dependencies, restarts, and rollback
  confidence;
- attached verifier/security evidence when `--security-evidence` is provided;
- repair artifacts generated from the same proof.

## Replay

Use `torque replay` to validate that a proof bundle is complete and replayable:

```bash
torque replay ./torque-sim-proof --lab k3s
torque replay ./torque-sim-proof --lab k3s --format json
```

`--fail-on-blocked` makes replay suitable for CI gates:

```bash
torque replay ./torque-sim-proof --lab k3s --fail-on-blocked
```

Use Guardian after simulation when you need to prove what happened in the live
cluster:

```bash
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
```

## Repair

`torque fix` is an alias for `torque repair`, so simulation bundles can feed the
same repair workflow used by failed apply proof:

```bash
torque fix --from ./torque-sim-proof --chart ./chart \
  --branch fix/api-sim --apply --pr-body ./repair-pr.md --yes
```

The simulation bundle also writes `fixes/pr.md` and `fixes/fix.patch` for
review even when you do not apply repairs automatically.

## Security Notes

`predicted-live-state.json` redacts Kubernetes `Secret.data` and
`Secret.stringData` values and masks common credential fields before writing
the proof. Attach `verifier --security-evidence` output when you need the full
secret flow graph, boundary matrix, and redaction proof beside the simulation.

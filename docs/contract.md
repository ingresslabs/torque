# Torque Runtime Contract

Runtime Contract turns proof from Guardian and Incident into observe-only
recurrence rules. It lets a team say "this failure must not happen again" in a
machine-checkable file, then prove future evidence satisfies that contract.

```bash
torque contract synthesize \
  --from incident-replay-proof/ \
  --guardian drift-proof.json \
  --out torque-contract.yaml

torque contract test \
  --contract torque-contract.yaml \
  --from incident-replay-proof/ \
  --guardian drift-proof.json \
  --out contract-proof.json

torque contract pr \
  --contract torque-contract.yaml \
  --proof contract-proof.json \
  --branch add/api-runtime-contract
```

The first implementation is strictly observe-only. It reads proof files and
writes contract/proof/PR artifacts. It does not install an admission webhook,
patch Kubernetes objects, or mutate source files.

## Contract Synthesis

`contract synthesize` reads one or both evidence sources:

- Incident bundle, root-cause JSON, or incident replay proof directory;
- Guardian drift proof JSON or full runtime proof directory.

It writes a `RuntimeContract` YAML file with deterministic invariants derived
from the evidence. Current invariant families include:

- incident replay proof must be complete;
- blocked incident root cause must be absent;
- image pull failures must not recur;
- runtime secret-boundary findings must be zero;
- rollout aftercare must pass;
- specific unhealthy resources must be available;
- predicted live state must match Guardian simulation proof;
- Warning event reasons must be absent;
- suspicious managed-field owners must be absent;
- captured log failure signals must be absent.

If the source proof contains no concrete failure signal, Torque writes a
fallback `runtime.proof.clean` invariant that requires clean runtime proof.

## Contract Test Proof

`contract test` evaluates the contract against fresh proof artifacts and writes
JSON:

```json
{
  "tool": "torque-contract",
  "passed": false,
  "blocked": true,
  "summary": {
    "invariants": 8,
    "passed": 1,
    "failed": 7,
    "missingEvidence": 0,
    "criticalFailures": 2
  }
}
```

Missing evidence is a failure. For example, a contract that requires Guardian
drift proof fails if `--guardian` is omitted. Use `--fail-on-blocked` in CI when
the contract should gate a release.

All evidence messages are redacted before they are copied into the contract
proof or PR body.

## PR Artifacts

`contract pr` turns a contract test proof into review-ready artifacts:

```text
fix/
  runtime-contract.patch
  pr.md
```

The patch records a `.torque/contracts/*` evidence note. The PR body includes
contract status, failed invariants, missing evidence count, and a copy/paste
validation command.

## Recommended Flow

Use Runtime Contract after a real incident has already been captured:

```bash
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
torque incident capture --release api -n prod --since 1h --out incident.torque
torque incident replay incident.torque --lab k3s --out incident-replay-proof/

torque contract synthesize \
  --from incident-replay-proof/ \
  --guardian drift-proof.json \
  --out torque-contract.yaml

torque contract test \
  --contract torque-contract.yaml \
  --from incident-replay-proof/ \
  --guardian drift-proof.json \
  --out contract-proof.json
```

The first test against the original broken proof should usually fail; that is
the point. The same contract should pass only after fresh Guardian/Incident
proof shows the root cause, drift, warning events, secret-boundary findings, and
aftercare issues are gone.

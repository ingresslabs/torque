# Release Proof Graph

`torque proof` turns existing Torque evidence into one reviewable graph:
commit, image digest, BuildKit capture, verifier findings, Helm render,
server-side dry-run, runtime drift, rollout capture, SLO outcome, rollback, and
repair artifacts.

```bash
torque proof graph ./apply-proof.json \
  --attach drift-proof.json \
  --attach repair-pr.md \
  --out proof.graph.json \
  --html proof.html
```

Sign the graph with the same ed25519 key format used by `torque stack keygen`:

```bash
torque stack keygen --out .torque/keys/proof-ed25519.json
torque proof graph ./apply-proof.json \
  --out proof.graph.json \
  --html proof.html \
  --key .torque/keys/proof-ed25519.json
```

## Verify

`proof verify` checks graph signatures and re-hashes every referenced file that
was present when the graph was built:

```bash
torque proof verify proof.graph.json --require-signature
torque proof verify proof.graph.json \
  --pub .torque/keys/proof-ed25519.json \
  --require-signature \
  --format json
```

You can also point verification directly at a proof source. Torque builds the
graph in memory and verifies the reachable file hashes:

```bash
torque proof verify ./torque-sim-proof
torque proof verify ./apply-proof.json
```

## Diff

Compare two graphs or proof sources before a release is promoted:

```bash
torque proof diff previous-proof.graph.json current-proof.graph.json
torque proof diff previous-proof.graph.json current-proof.graph.json --format json
```

The diff reports added, removed, and changed evidence artifacts, plus release
status changes such as `succeeded` to `failed`.

## Supported Inputs

- `torque apply --proof-bundle` JSON;
- `torque apply simulate --out` proof directories;
- Guardian drift and runtime proof JSON;
- Incident replay proof JSON;
- Runtime Contract proof JSON;
- extra files or directories supplied through repeated `--attach` flags.

The graph JSON is intentionally file-first. It stores paths, SHA256 hashes,
artifact types, status, image digests, and an optional ed25519 signature. It
does not copy raw logs, manifests, secret values, or SQLite capture contents
into the graph.

# Torque Usage Workflows

Use these recipes when a user asks how to operate Torque. Prefer proof-producing
commands before mutating commands.

## Local Build And Help

```bash
make build
./bin/torque --help
./bin/torque --help --ui
```

Use `go run ./cmd/torque ...` only when a binary has not been built yet.

## Apply Planning

```bash
torque apply plan --chart ./chart --release api -n prod --output plan.md
torque apply plan --chart ./chart --release api -n prod --visualize --output plan.html
```

For execution, require a real kubeconfig/context and explicit approval:

```bash
torque apply --chart ./chart --release api -n prod --capture ./apply.sqlite --yes
```

## Proof Graph And Release Gate

```bash
torque stack keygen --out .torque/keys/proof-ed25519.json
torque proof graph ./apply-proof.json \
  --attach drift-proof.json \
  --attach repair-pr.md \
  --out proof.graph.json \
  --html proof.html \
  --key .torque/keys/proof-ed25519.json
torque proof verify proof.graph.json --require-signature
torque proof gate proof.graph.json --out proof.gate.json
torque proof diff previous-proof.graph.json proof.graph.json
```

## Release Score And Flight

```bash
torque release score proof.graph.json --fail-below 90 --out release-score.json
torque flight record proof.graph.json --out release.flight.torque
torque flight replay release.flight.torque
torque flight explain release.flight.torque
```

## Release Autopilot

```bash
torque release autopilot proof.graph.json \
  --key .torque/keys/proof-ed25519.json \
  --policy release-policy.yaml \
  --fail-below 90 \
  --out-dir release-autopilot
```

Use `--execute --yes` only when the user asks for a live apply path.

## Progressive Promotion

Plan a canary:

```bash
torque release promote proof.graph.json \
  --strategy canary \
  --steps 5,25,50,100 \
  --slo slo.yaml \
  --rollback-on-fail \
  --out-dir release-promote-canary
```

Plan blue/green:

```bash
torque release promote proof.graph.json \
  --strategy blue-green \
  --preview \
  --smoke smoke.json \
  --switch-traffic \
  --out-dir release-promote-blue-green
```

Deterministic rehearsal provider:

```bash
torque release promote proof.graph.json \
  --strategy blue-green \
  --preview --smoke smoke.json --switch-traffic \
  --provider file --state-out traffic-state.json \
  --execute --yes
```

## Agent-Safe Operations

```bash
torque agent policy check agent-request.json \
  --proof proof.graph.json --allow apply --require-gate \
  --out agent-policy.json
torque agent run agent-request.json \
  --proof proof.graph.json --allow apply --require-gate \
  --out agent-run.json
```

`agent run` is non-mutating; it records authorization.

## Logs And Incident Evidence

```bash
torque logs 'checkout-.*' -n prod-payments \
  --events --highlight 'ERROR|WARN' --capture ./logs.sqlite --tail 100
torque incident capture --namespace prod-payments --out incident-proof
torque incident replay incident-proof --format json
```

Do not commit real captured logs unless they are sanitized fixtures.

# Demos

Text-only companion commands for the landing page demos. The animated demos live
on the landing page; docs stay copy/paste friendly and free of GIF assets.

<details open>
<summary>Complex DAG stack orchestration</summary>

```bash
torque stack plan --config testdata/stack/e2e/10-large-graph \
  --bundle ./dist/stack-large-graph.tgz
torque stack plan --config testdata/stack/e2e/10-large-graph --output json
torque stack status --config ./stacks/prod --follow
```

Plans a dependency-ordered stack, seals the review bundle, and follows rollout
status in dependency waves.

</details>

<details open>
<summary>Ship subcommand release flow</summary>

```bash
torque ship --chart ./chart --release api -n prod \
  --build . --tag ghcr.io/acme/api:dev --yes
torque ship --chart ./chart --release api -n prod \
  --build . --tag ghcr.io/acme/api:dev --plan-only
torque explain ./dist/torque-ship-api/apply.sqlite --format markdown
```

Runs the focused build-to-deploy path as one command, writing build/apply
captures, verifier output, plan output, explain output, and `ship.json` into a
portable evidence directory.

</details>

<details open>
<summary>DAG performance scheduling</summary>

```bash
torque stack plan --config testdata/stack/e2e/02-fanout --output json
torque stack plan --config testdata/stack/e2e/03-fanin --output json
torque stack plan --config testdata/stack/e2e/10-large-graph --output json
```

Shows how shared roots, joins, and larger graphs are reduced into deterministic
waves before anything is applied.

</details>

<details open>
<summary>Sandboxed builds and secrets</summary>

```bash
torque build sandbox doctor --sandbox-config sandbox/linux-ci.cfg
torque build . --sandbox --sandbox-config sandbox/linux-ci.cfg \
  --capture ./build.sqlite
```

Checks the sandbox profile, then runs the build inside the constrained builder
while writing portable build evidence.

</details>

<details open>
<summary>Helmer archives and verifier gates</summary>

```bash
helmer archive ./chart --output ./chart.tgz
verifier --chart ./chart --release api -n prod --format json --report verify.json
torque apply plan --chart ./chart --release api -n prod \
  --verify-report verify.json --output plan.md
```

Keeps the chart archive, rendered manifests, verifier report, and release plan
bound together for review.

</details>

<details open>
<summary>Helmer HTML plan reports</summary>

```bash
helmer report ./chart --output ./plan.html
torque apply plan --chart ./chart --release api -n prod \
  --visualize --output ./plan.html
torque apply plan --chart ./chart --release api -n prod \
  --build-capture ./build.sqlite --format html --output ./plan.html
```

Produces a reviewable HTML plan report while attaching the same portable build
evidence reviewers use for release decisions.

</details>

<details open>
<summary>Kubernetes logs and evidence capture</summary>

```bash
torque logs 'checkout-.*' -n prod-payments \
  --events --highlight 'ERROR|WARN' --capture ./logs.sqlite --tail 100
torque logs deploy/checkout -n prod-payments \
  --deploy-mode stable+canary --ws-listen :9090
torque explain ./logs.sqlite --format markdown
```

Tails matching pods with events and highlighted failure signals, mirrors the
same stream over WebSocket for live review, and stores the session as portable
SQLite evidence for later explanation.

</details>

<details open>
<summary>Remote agent mirror sessions</summary>

```bash
export TORQUE_REMOTE_TOKEN="$(openssl rand -hex 24)"
torque-agent -listen :7443 -http-listen :8081 \
  -token "$TORQUE_REMOTE_TOKEN" -mirror-store ~/.torque/agent/mirror.sqlite
torque --remote-agent 127.0.0.1:7443 --remote-token "$TORQUE_REMOTE_TOKEN" \
  logs 'checkout-.*' -n prod-payments
curl -H "authorization: Bearer $TORQUE_REMOTE_TOKEN" \
  "http://127.0.0.1:8081/api/v1/mirror/sessions?limit=20"
```

Runs log streaming through `torque-agent`, stores replayable MirrorService
frames in SQLite, and exposes the same sessions over the HTTP/SSE gateway for
browser tools or reviewers.

</details>

<details open>
<summary>Capture explain drilldown</summary>

```bash
torque apply --chart ./chart --release checkout -n prod-payments \
  --capture ./apply.sqlite --yes
torque logs deploy/checkout -n prod-payments \
  --events --capture ./apply.sqlite --tail 100
torque explain ./apply.sqlite --session <session-id> \
  --format markdown --max-hints 12
```

Turns a captured deploy/log session into a compact cause summary with resource
hints, failure signals, captured artifacts, and next commands.

</details>

<details open>
<summary>Secret-safe build and log evidence</summary>

```bash
export NPM_TOKEN="<token from CI secret store>"
torque build . --secret NPM_TOKEN \
  --secrets block --secrets-report ./secrets.json --capture ./build.sqlite
torque apply plan --chart ./chart --release checkout -n prod-payments \
  --secret-provider vault --output plan.md
torque logs 'checkout-.*' -n prod-payments --capture ./logs.sqlite --tail 100
```

Keeps build secrets on BuildKit secret mounts, blocks secret-like build inputs
before publishing, records machine-readable guardrail results, and captures logs
as evidence without embedding secret values.

</details>

<details open>
<summary>Drift and plan comparison</summary>

```bash
torque apply plan --chart ./chart --release checkout -n prod-payments \
  --baseline ./plan.json
torque apply plan --chart ./chart --release checkout -n prod-payments \
  --compare-to ./plan.json --github-comment --output plan.md
verifier verify.yaml --compare-to ./verify-baseline.json
```

Writes a known-good plan baseline, compares the next render against it, and
surfaces new resources, risky diffs, and verifier regressions in review output.

</details>

<details open>
<summary>Stack resume and rerun failed</summary>

```bash
torque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite
torque stack status --config ./stacks/prod --follow
torque stack rerun-failed --config ./stacks/prod --yes --retry 2
torque stack apply --config ./stacks/prod --resume --yes
```

Loads the frozen stack run state, skips releases that already succeeded, and
reschedules only failed nodes after the underlying issue is fixed.

</details>

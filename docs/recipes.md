# Recipes (golden paths)

Copy/paste workflows that cover the common “happy paths” for `torque`.

## Zero-conf onboarding

```bash
# Initialize repo defaults and detect your kubecontext
torque init

# Generate a starter stack.yaml from existing cluster state
torque init from-cluster
torque init from-cluster --all-namespaces --dry-run
# Exports installed Helm chart archives by default; add current values too
torque init from-cluster --all-namespaces --write-values

# Run the interactive setup wizard
torque init --interactive

# Scaffold chart/ and values/ plus gitignore entries
torque init --layout --gitignore

# Use an opinionated preset
torque init --preset prod

# Apply an init template (built-in or URL)
torque init --template platform
torque init --template https://example.com/torque-init.yaml

# Scaffold a Vault secrets provider
torque init --secrets-provider vault

# Preview the config without writing
torque init --dry-run

# Generate a replayable init plan
torque init --plan --plan-output .torque/init-plan.json

# Apply a saved init plan
torque init --apply-plan .torque/init-plan.json

# Launch the interactive help UI
torque --help --ui
```

## Apply a chart (with and without the UI)

```bash
# Preview what will change
torque apply plan --chart ./chart --release foo -n default

# Write a PR-ready Markdown summary
torque apply plan --chart ./chart --release foo -n default --github-comment --output plan.md

# Attach verifier and build evidence to the PR summary
verifier --chart ./chart --release foo -n default \
  --security-evidence ./torque-security-evidence \
  --format json --report verify.json
torque build . --tag ghcr.io/acme/foo:dev --capture ./build.sqlite
torque apply plan --chart ./chart --release foo -n default \
  --verify-report verify.json --build-capture ./build.sqlite \
  --github-comment --output plan.md

# Simulate live API behavior and write a replayable proof directory
torque apply simulate --chart ./chart --release foo -n default \
  --security-evidence ./torque-security-evidence \
  --out ./torque-sim-proof
torque replay ./torque-sim-proof --lab k3s

# Prove runtime drift after simulation
torque guardian install --namespace torque-system --mode observe
torque guardian report --since 24h --out runtime-proof.json
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
torque guardian pr --from drift-proof.json --branch fix/runtime-drift

# Capture and replay an incident window
torque incident capture --release foo -n default --since 1h --out incident.torque
torque incident replay incident.torque --lab k3s --out incident-replay-proof
torque incident explain --from incident-replay-proof --out root-cause.json
torque incident pr --from root-cause.json --branch fix/foo-incident

# Deploy
torque apply --chart ./chart --release foo -n default

# Deploy with the viewer UI
torque apply --chart ./chart --release foo -n default --ui

# Predict rollout risk and write a portable proof bundle
torque apply --chart ./chart --release foo -n default \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes

# Deploy with auto rollback proof and rollout SLO gates
cat > slo.yaml <<'YAML'
apiVersion: torque.ingresslabs.dev/v1alpha1
kind: RolloutSLO
spec:
  minReadyPercent: 100
  maxFailedResources: 0
  maxPendingResources: 0
YAML
torque apply --chart ./chart --release foo -n default \
  --auto-rollback --slo ./slo.yaml \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes

# Turn a failed proof bundle into a repair branch and PR body
torque repair --from ./apply-proof.json --chart ./chart \
  --branch fix/foo-rollout --apply --pr-body ./repair-pr.md --yes
```

## Build → verify → plan → apply

```bash
# Build the image and capture build evidence.
torque build . --tag ghcr.io/acme/foo:dev --capture ./build.sqlite

# Verify the rendered chart.
verifier --chart ./chart --release foo -n default --format json --report verify.json

# Verify with evidence-first secret flow checks and a redaction proof bundle.
verifier --chart ./chart --release foo -n default \
  --security-profile enterprise \
  --security-boundary-matrix \
  --secrets-report secrets.json \
  --security-evidence ./torque-security-evidence \
  --format json --report verify.json

# Write a PR-ready plan with verifier and build evidence attached.
torque apply plan --chart ./chart --release foo -n default \
  --verify-report verify.json --build-capture ./build.sqlite \
  --github-comment --output plan.md

# Apply with the verify report enforced, capture the rollout, and explain it.
torque apply --chart ./chart --release foo -n default \
  --require-verified verify.json \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes
torque repair --from ./apply-proof.json --chart ./chart --pr-body ./repair-pr.md
torque explain ./apply.sqlite --format markdown
```

## Agent MCP bridge

```bash
# Run the MCP server over stdio for an IDE or agent host.
torque-mcp --stdio

# Route agent tool calls to a remote Torque node over gRPC.
export TORQUE_REMOTE_TOKEN="$(openssl rand -hex 24)"
torque-agent -listen :7443 -token "$TORQUE_REMOTE_TOKEN"
torque-mcp --stdio \
  --remote-agent 127.0.0.1:7443 \
  --remote-token "$TORQUE_REMOTE_TOKEN"
```

## Durable Linux agent host

```bash
# Install torque, torque-agent, torque-mcp, and systemd units.
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh -s -- --mode systemd-daemon

# Inspect generated tokens and verify authenticated HTTP MCP.
. /etc/torque/agent.env
systemctl status torque-agent.service torque-mcp.service
curl -fsS -H "authorization: Bearer $TORQUE_MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  http://127.0.0.1:7331/mcp
```

Enterprise remote bridge with mTLS:

```bash
export TORQUE_REMOTE_TOKEN="<from secret manager>"
torque-agent -listen 0.0.0.0:7443 -token "$TORQUE_REMOTE_TOKEN" \
  -tls-cert /etc/torque/tls/agent.crt \
  -tls-key /etc/torque/tls/agent.key \
  -tls-client-ca /etc/torque/tls/client-ca.crt \
  -mirror-store /var/lib/torque/agent/mirror.sqlite

torque-mcp --stdio --remote-agent torque-agent.prod.internal:7443 \
  --remote-tls --remote-tls-ca /etc/torque/tls/ca.crt \
  --remote-tls-client-cert /etc/torque/tls/client.crt \
  --remote-tls-client-key /etc/torque/tls/client.key \
  --remote-tls-server-name torque-agent.prod.internal \
  --enable-write
```

## Build and ship with S3 cache

```bash
# Build with native BuildKit S3 cache import/export.
torque build . --tag ghcr.io/acme/foo:dev \
  --s3-cache s3://acme-build-cache/torque/main \
  --s3-cache-region us-east-1

# Forward the same cache settings through the full ship workflow.
torque ship --chart ./chart --release foo -n default --build . \
  --tag ghcr.io/acme/foo:dev \
  --s3-cache s3://acme-build-cache/torque/main \
  --s3-cache-region us-east-1 --yes
```

For MCP agents, use first-class cache tools before build fanout:

```json
{
  "tool": "torque.cache.plan",
  "arguments": {
    "contextDir": ".",
    "dockerfile": "Dockerfile",
    "tags": ["ghcr.io/acme/foo:dev"],
    "changedPaths": ["go.mod", "cmd/foo/main.go"],
    "s3Cache": "s3://acme-build-cache/torque/main",
    "s3CacheRegion": "us-east-1",
    "s3CacheName": "foo-main"
  }
}
```

Warm writes cache exports, so it requires `torque-mcp --enable-write` and
`"safety": {"confirm": true}`. AWS credentials stay on the BuildKit daemon or
workload identity, not in MCP arguments.

## 5-minute demo (public chart)

Do this:
```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

torque apply plan --chart bitnami/nginx --release demo-nginx -n default --visualize
torque apply --chart bitnami/nginx --release demo-nginx -n default --yes
torque delete --release demo-nginx -n default --yes
```

## Recommended `.torque.yaml` layout

Do this:
```yaml
build:
  profile: ci

secrets:
  defaultProvider: local
  providers:
    local:
      type: file
      path: ./secrets.dev.yaml
```

## Apply with secret references

```bash
# Define providers in .torque.yaml (or pass --secret-config)
cat > .torque.yaml <<'YAML'
secrets:
  defaultProvider: local
  providers:
    local:
      type: file
      path: ./secrets.dev.yaml
YAML

# Use secret:// references in values
cat > values.dev.yaml <<'YAML'
db:
  password: secret://local/db/password
YAML

torque apply plan --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider local
torque apply --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider local
torque stack apply --config ./stacks/prod --secret-provider local --yes
```

## Vault-backed secrets

```bash
cat > .torque.yaml <<'YAML'
secrets:
  defaultProvider: vault
  providers:
    vault:
      type: vault
      address: https://vault.example.com
      authMethod: approle
      authMount: approle
      roleId: 00000000-0000-0000-0000-000000000000
      secretId: s.0000000000000000000000
      mount: secret
      kvVersion: 2
      key: value
      # kubernetesRole: torque
      # kubernetesTokenPath: /var/run/secrets/kubernetes.io/serviceaccount/token
      # awsRole: torque
      # awsRegion: us-east-1
      # awsHeaderValue: vault.example.com
YAML

cat > values.dev.yaml <<'YAML'
db:
  password: secret://vault/app/db#password
YAML

torque apply plan --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider vault
torque apply --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider vault
torque stack apply --config ./stacks/prod --secret-provider vault --yes
```

Inspect providers and references:
```bash
torque secrets test --secret-provider vault --ref secret://vault/app/db#password
torque secrets list --secret-provider vault --path app --format json
```

Minimal CLI workflow (sanity check before apply):
```bash
torque secrets test --secret-provider vault --ref secret://vault/app/db#password
torque secrets list --secret-provider vault --path app
torque secrets scan --scope repo --report secrets.json --mode block --flow-graph
torque secrets scan --scope render --manifest ./rendered.yaml --report render-secrets.json --mode block --flow-graph
torque security benchmark --corpus ./testdata/security --report benchmark.json
```

## Regression-proof plans

Do this:
```bash
torque apply plan --chart ./chart --release foo -n default --baseline ./plan.json
torque apply plan --chart ./chart --release foo -n default --compare-to ./plan.json
```

## Regression-proof verifier

Do this:
```bash
verifier verify.yaml --baseline ./baseline.json
verifier verify.yaml --compare-to ./baseline.json
```

## Share an `apply plan` visualization

```bash
torque apply plan --visualize --chart ./chart --release foo -n default
```

## Stack: minimal-flags workflow (plan → apply)

```bash
export TORQUE_STACK_ROOT=./stacks/prod

# Read-only plan (default `torque stack` behaves like `torque stack plan`)
torque stack

# Execute (DAG order)
torque stack apply --yes

# Capture the full stack run evidence bundle
torque stack apply --yes --capture ./stack.sqlite
```

## Stack: resume / rerun failed

```bash
export TORQUE_STACK_ROOT=./stacks/prod

# Resume the most recent run (frozen plan unless --replan is set)
torque stack apply --resume --yes

# Convenience: resume and schedule only failed nodes
torque stack rerun-failed --yes
```

## Stack: inspect runs

```bash
export TORQUE_STACK_ROOT=./stacks/prod

torque stack runs --limit 50
torque stack status --follow
torque stack audit --output html > stack-audit.html
```

## Build: share the build stream over WebSocket

```bash
torque build . --tag ghcr.io/acme/app:dev --ws-listen :9085
```

## Capture: record deploy/build/log evidence

```bash
# Record a deploy evidence file
torque apply --chart ./chart --release foo -n default --capture ./apply.sqlite --capture-tag change=CHG-1234

# Record a stack evidence file
torque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite

# Save it as a CI/review artifact
tar -czf torque-evidence.tgz ./apply.sqlite

# Explain a captured session locally or in CI logs
torque explain ./apply.sqlite
torque explain ./apply.sqlite --format markdown
```

## Verifier: validate a chart render in CI

```bash
cat > verify-chart-render.yaml <<'YAML'
version: v1

target:
  kind: chart
  chart:
    chart: ./chart
    release: foo
    namespace: default
    useCluster: false

verify:
  mode: block
  failOn: high

output:
  format: table
  report: "-"
YAML

verifier verify-chart-render.yaml

# Package a chart then verify the archive
torque-package ./chart --output dist/chart.sqlite
torque-package --verify dist/chart.sqlite
```

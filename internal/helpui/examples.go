package helpui

// curatedExamples supplements Cobra's .Example fields with task-based golden paths.
// Keys are Cobra command paths (CommandPath()).
var curatedExamples = map[string][]string{
	"torque": {
		"# Launch the built-in interactive help UI\ntorque --help --ui",
		"# Run the MCP bridge over stdio for an agent host\ntorque-mcp --stdio",
		"# Bridge MCP calls to a remote torque-agent over gRPC\ntorque-mcp --stdio --remote-agent 127.0.0.1:7443 --remote-token \"$TORQUE_REMOTE_TOKEN\"",
		"# Bridge MCP calls to an mTLS-protected torque-agent\ntorque-mcp --stdio --remote-agent torque-agent.prod.internal:7443 --remote-tls --remote-tls-ca /etc/torque/tls/ca.crt --remote-tls-client-cert /etc/torque/tls/client.crt --remote-tls-client-key /etc/torque/tls/client.key --remote-tls-server-name torque-agent.prod.internal --enable-write",
		"# Ask MCP for a structured S3 cache plan instead of scraping BuildKit logs\nprintf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"torque.cache.plan\",\"arguments\":{\"contextDir\":\".\",\"tags\":[\"ghcr.io/acme/app:dev\"],\"changedPaths\":[\"go.mod\"],\"s3Cache\":\"s3://acme-build-cache/torque/main\",\"s3CacheRegion\":\"us-east-1\"}}}\\n' | torque-mcp --stdio",
		"# Install durable gRPC and MCP services on a Linux systemd host\ncurl -fsSL https://ingresslabs.github.io/torque/install.sh | sh -s -- --mode systemd-daemon",
	},
	"torque logs": {
		"# Tail pods matching a regex in a namespace\ntorque logs 'checkout-.*' -n prod-payments",
		"# Highlight errors\ntorque logs 'checkout-.*' -n prod-payments --highlight ERROR",
	},
	"torque init": {
		"# Create a repo-local .torque.yaml\ntorque init",
		"# Preview the config without writing\ntorque init --dry-run",
		"# Run the interactive wizard\ntorque init --interactive",
		"# Use an opinionated preset\ntorque init --preset prod",
		"# Apply a built-in template\ntorque init --template platform",
		"# Apply a template from a URL\ntorque init --template https://example.com/torque-init.yaml",
		"# Merge defaults into an existing config\ntorque init --merge",
		"# Scaffold chart/ and values/ plus gitignore\ntorque init --layout --gitignore",
		"# Scaffold Vault-backed secrets\ntorque init --secrets-provider vault",
		"# Emit JSON for automation\ntorque init --output json --dry-run",
		"# Write a replayable init plan\ntorque init --plan --plan-output .torque/init-plan.json",
		"# Apply a saved init plan\ntorque init --apply-plan .torque/init-plan.json",
		"# Initialize another path\ntorque init ./services/api",
		"# Overwrite existing config\ntorque init --force",
	},
	"torque init from-cluster": {
		"# Generate stack.yaml from the active namespace\ntorque init from-cluster",
		"# Preview adoption across all non-system namespaces\ntorque init from-cluster --all-namespaces --dry-run",
		"# Export installed charts and current Helm values\ntorque init from-cluster --all-namespaces --write-values",
		"# Adopt a specific namespace into another file\ntorque init from-cluster -n prod --output stacks/prod.yaml",
	},
	"torque build": {
		"# Build an image from a directory\ntorque build . --tag ghcr.io/acme/app:dev",
		"# Build with a shared S3 cache\ntorque build . --tag ghcr.io/acme/app:dev --s3-cache s3://acme-build-cache/torque/main --s3-cache-region us-east-1",
		"# Capture build provenance for apply plan\ntorque build . --tag ghcr.io/acme/app:dev --capture ./build.sqlite",
		"# Share the build stream over WebSocket\ntorque build . --ws-listen :9085",
	},
	"torque explain": {
		"# Explain a deploy capture\ntorque explain ./apply.sqlite",
		"# Explain a stack capture as JSON\ntorque explain ./stack.sqlite --format json",
		"# Write a Markdown explanation for CI logs or PR comments\ntorque explain ./apply.sqlite --format markdown",
		"# Explain a specific captured session\ntorque explain ./apply.sqlite --session <session-id>",
	},
	"torque help": {
		"# Launch the interactive help UI\ntorque --help --ui",
		"# Launch the help subcommand UI directly\ntorque help --ui",
		"# Show help for a specific command\ntorque help apply",
	},
	"torque ship": {
		"# Build, verify, plan, apply, capture, and explain one release\ntorque ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --yes",
		"# Ship with a shared S3 BuildKit cache\ntorque ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --s3-cache s3://acme-build-cache/torque/main --s3-cache-region us-east-1 --yes",
		"# Ship through a remote torque-agent gRPC endpoint\ntorque --remote-agent 127.0.0.1:7443 --remote-token \"$TORQUE_REMOTE_TOKEN\" ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --yes",
		"# Ship through an mTLS-protected torque-agent\ntorque --remote-agent torque-agent.prod.internal:7443 --remote-token \"$TORQUE_REMOTE_TOKEN\" --remote-tls --remote-tls-ca /etc/torque/tls/ca.crt --remote-tls-client-cert /etc/torque/tls/client.crt --remote-tls-client-key /etc/torque/tls/client.key ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --yes",
	},
	"torque apply plan": {
		"# Preview a Helm upgrade\ntorque apply plan --chart ./chart --release foo -n default",
		"# Write a GitHub PR comment summary\ntorque apply plan --chart ./chart --release foo -n default --github-comment --output plan.md",
		"# Attach verifier and build evidence\ntorque apply plan --chart ./chart --release foo -n default --verify-report verify.json --build-capture ./build.sqlite --github-comment",
		"# Render a shareable HTML visualization\ntorque apply plan --visualize --chart ./chart --release foo -n default",
		"# Preview with secret references\ntorque apply plan --chart ./chart --release foo -n default --secret-provider local",
		"# Preview with Vault-backed secrets\ntorque apply plan --chart ./chart --release foo -n default --secret-provider vault",
		"# Compare against a saved baseline\ntorque apply plan --chart ./chart --release foo -n default --compare-to ./plan.json",
		"# Write a baseline snapshot\ntorque apply plan --chart ./chart --release foo -n default --baseline ./plan.json",
	},
	"torque apply simulate": {
		"# Simulate a Helm apply and write a proof bundle\ntorque apply simulate --chart ./chart --release foo -n default --out ./torque-sim-proof",
		"# Attach security evidence and SLO gates\ntorque apply simulate --chart ./chart --release foo -n default --security-evidence ./torque-security-evidence --slo ./slo.yaml --out ./torque-sim-proof",
		"# Fail CI when the API dry-run or quota proof blocks the release\ntorque apply simulate --chart ./chart --release foo -n default --out ./torque-sim-proof --fail-on-blocked",
	},
	"torque apply": {
		"# Deploy a chart\ntorque apply --chart ./chart --release foo -n default",
		"# Run the deploy viewer\ntorque apply --chart ./chart --release foo -n default --ui",
		"# Predict rollout risk and write a proof bundle\ntorque apply --chart ./chart --release foo -n default --predict --proof-bundle ./apply-proof.json --capture ./apply.sqlite --yes",
		"# Roll back automatically and write proof if apply or SLO gates fail\ntorque apply --chart ./chart --release foo -n default --auto-rollback --slo ./slo.yaml --capture ./apply.sqlite --yes",
		"# Deploy with secret references\ntorque apply --chart ./chart --release foo -n default --secret-provider local",
		"# Deploy with Vault-backed secrets\ntorque apply --chart ./chart --release foo -n default --secret-provider vault",
	},
	"torque delete": {
		"# Delete a release\ntorque delete --release foo -n default",
		"# Run the destroy viewer\ntorque delete --release foo -n default --ui",
	},
	"torque revert": {
		"# Revert a release to the last known-good revision\ntorque revert --release foo -n default",
	},
	"torque repair": {
		"# Diagnose a failed apply proof bundle\ntorque repair --from ./apply-proof.json --chart ./chart",
		"# Diagnose a simulation proof bundle through the fix alias\ntorque fix --from ./torque-sim-proof --chart ./chart",
		"# Write chart repair files and a PR body\ntorque repair --from ./apply-proof.json --chart ./chart --branch fix/foo-rollout --apply --pr-body ./repair-pr.md --yes",
	},
	"torque replay": {
		"# Validate a simulation proof bundle in the k3s lab profile\ntorque replay ./torque-sim-proof --lab k3s",
		"# Emit replay validation as JSON\ntorque replay ./torque-sim-proof --lab k3s --format json",
		"# Fail CI when the replayed proof is blocked\ntorque replay ./torque-sim-proof --lab k3s --fail-on-blocked",
	},
	"torque guardian": {
		"# Install observe-only Guardian RBAC and config\ntorque guardian install --namespace torque-system --mode observe",
		"# Compare a simulation proof against live runtime state\ntorque guardian diff --source ./torque-sim-proof --live --out drift-proof.json",
		"# Generate PR artifacts from a drift proof\ntorque guardian pr --from drift-proof.json --branch fix/runtime-drift",
	},
	"torque guardian install": {
		"# Install observe-only Guardian RBAC\ntorque guardian install --namespace torque-system --mode observe",
		"# Review the manifest without applying it\ntorque guardian install --namespace torque-system --mode observe --dry-run",
	},
	"torque guardian report": {
		"# Write a 24-hour runtime event proof\ntorque guardian report --since 24h --out runtime-proof.json",
		"# Inspect all namespaces and print JSON\ntorque guardian report --since 30m --all-namespaces --format json",
	},
	"torque guardian diff": {
		"# Prove drift from simulation proof to live objects\ntorque guardian diff --source ./torque-sim-proof --live --out drift-proof.json",
		"# Write the full runtime proof bundle directory\ntorque guardian diff --source ./torque-sim-proof --live --out ./torque-runtime-proof",
	},
	"torque guardian pr": {
		"# Generate patch and PR body from a drift proof\ntorque guardian pr --from drift-proof.json --branch fix/runtime-drift",
		"# Generate fix artifacts beside a runtime proof bundle\ntorque guardian pr --from ./torque-runtime-proof",
	},
	"torque contract": {
		"# Synthesize recurrence rules from Incident and Guardian proof\ntorque contract synthesize --from incident-replay-proof --guardian drift-proof.json --out torque-contract.yaml",
		"# Test fresh proof against the RuntimeContract\ntorque contract test --contract torque-contract.yaml --from incident-replay-proof --guardian drift-proof.json --out contract-proof.json",
		"# Generate PR artifacts from contract proof\ntorque contract pr --contract torque-contract.yaml --proof contract-proof.json --branch add/api-runtime-contract",
	},
	"torque contract synthesize": {
		"# Write a RuntimeContract from combined proof\ntorque contract synthesize --from incident-replay-proof --guardian drift-proof.json --out torque-contract.yaml",
		"# Print the synthesized contract as YAML\ntorque contract synthesize --from incident-replay-proof --guardian drift-proof.json --format yaml",
	},
	"torque contract test": {
		"# Write machine-checkable contract test proof\ntorque contract test --contract torque-contract.yaml --from incident-replay-proof --guardian drift-proof.json --out contract-proof.json",
		"# Fail CI when recurrence rules are violated\ntorque contract test --contract torque-contract.yaml --from incident-replay-proof --guardian drift-proof.json --fail-on-blocked",
	},
	"torque contract pr": {
		"# Generate patch and PR body from contract proof\ntorque contract pr --contract torque-contract.yaml --proof contract-proof.json --branch add/api-runtime-contract",
	},
	"torque incident": {
		"# Capture observe-only incident evidence\ntorque incident capture --release api -n prod --since 1h --out incident.torque",
		"# Replay the capture as a lab proof\ntorque incident replay incident.torque --lab k3s --out incident-replay-proof",
		"# Generate PR artifacts from root cause\ntorque incident pr --from incident-replay-proof --branch fix/api-incident",
	},
	"torque incident capture": {
		"# Capture a release incident bundle\ntorque incident capture --release api -n prod --since 1h --out incident.torque",
		"# Print capture JSON and skip pod logs\ntorque incident capture --release api -n prod --log-tail 0 --format json",
	},
	"torque incident replay": {
		"# Validate an incident bundle in the k3s lab profile\ntorque incident replay incident.torque --lab k3s --out incident-replay-proof",
		"# Fail CI when replayed evidence remains blocked\ntorque incident replay incident.torque --lab k3s --fail-on-blocked",
	},
	"torque incident explain": {
		"# Write root-cause JSON from replay proof\ntorque incident explain --from incident-replay-proof --out root-cause.json",
	},
	"torque incident pr": {
		"# Generate patch and PR body from root cause\ntorque incident pr --from root-cause.json --branch fix/api-incident",
		"# Generate fix artifacts beside replay proof\ntorque incident pr --from incident-replay-proof",
	},
	"torque env": {
		"# Show env var reference (machine-readable)\ntorque env --format json",
	},
	"torque secrets": {
		"# Validate a secret reference\ntorque secrets test --secret-provider vault --ref secret://vault/app/db#password",
		"# List secrets under a provider prefix\ntorque secrets list --secret-provider local --path app --format json",
		"# Discover secret refs across the repo\ntorque secrets discover --scope repo",
		"# Discover secret refs for a chart\ntorque secrets discover --scope chart --chart ./chart --values values/dev.yaml",
		"# Discover secret refs for a stack\ntorque secrets discover --scope stack --config ./stacks/prod",
		"# Scan source files for secret-like values\ntorque secrets scan --scope repo --report secrets.json --mode block",
		"# Scan source files and include a redacted secret flow graph\ntorque secrets scan --scope repo --report secrets.json --mode block --flow-graph",
		"# Scan rendered manifests and allow Kubernetes Secret boundaries\ntorque secrets scan --scope render --manifest ./rendered.yaml --report render-secrets.json --mode block --flow-graph",
	},
	"torque security": {
		"# Benchmark secret detection, redaction, flow graph, and boundary evidence\ntorque security benchmark --corpus ./testdata/security --report benchmark.json",
	},
	"torque security benchmark": {
		"# Publish corpus-backed security metrics\ntorque security benchmark --corpus ./testdata/security --report benchmark.json",
		"# Include the live k3s boundary matrix probe\ntorque security benchmark --corpus ./testdata/security --report benchmark.json --live-k3s-boundary-matrix --live-confirm --kubeconfig ~/.kube/k3s.yaml",
	},
	"torque version": {
		"# Print version information\ntorque version",
	},
	"torque stack": {
		"# Plan the stack (default: read-only, like `torque stack plan`)\ntorque stack --config ./stacks/prod",
		"# Restrict selection via environment defaults\nTORQUE_STACK_TAG=critical TORQUE_STACK_CLUSTER=prod-us torque stack --config ./stacks/prod",
		"# Emit a machine-readable plan for tooling\ntorque stack --config ./stacks/prod --output json",
	},
	"torque stack plan": {
		"# Write a reproducible plan bundle for review/CI\ntorque stack plan --config ./stacks/prod --bundle ./stack-plan.tgz",
		"# Embed a live diff summary in the bundle (requires cluster access)\ntorque stack plan --config ./stacks/prod --bundle ./stack-plan.tgz --bundle-diff-summary",
	},
	"torque stack graph": {
		"# Render a Graphviz DOT graph\ntorque stack graph --config ./stacks/prod > stack.dot",
		"# Render a Mermaid graph\ntorque stack graph --config ./stacks/prod --format mermaid > stack.mmd",
	},
	"torque stack explain": {
		"# Explain why a release is selected (by name)\ntorque stack explain --config ./stacks/prod api",
		"# Print only selection reasons\ntorque stack explain --config ./stacks/prod api --why",
	},
	"torque stack apply": {
		"# Apply the selected releases (DAG order)\ntorque stack apply --config ./stacks/prod --yes",
		"# Capture a stack run evidence bundle\ntorque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite",
		"# Resume the most recent run (uses stored frozen plan unless --replan is set)\ntorque stack apply --config ./stacks/prod --resume --yes",
		"# Enable manifest diffs (defaulted via env)\nTORQUE_STACK_APPLY_DIFF=1 torque stack apply --config ./stacks/prod --yes",
		"# Apply with secret references\ntorque stack apply --config ./stacks/prod --secret-provider vault --yes",
	},
	"torque stack delete": {
		"# Delete the selected releases (reverse DAG order)\ntorque stack delete --config ./stacks/prod --yes",
		"# Prompt only when deleting 50+ releases\ntorque stack delete --config ./stacks/prod --delete-confirm-threshold 50",
	},
	"torque stack status": {
		"# Tail the most recent run\ntorque stack status --config ./stacks/prod --follow",
		"# Show a specific run ID (see `torque stack runs`)\ntorque stack status --config ./stacks/prod --run-id 2025-12-30T12-34-56.000000000Z --follow",
	},
	"torque stack runs": {
		"# List recent runs\ntorque stack runs --config ./stacks/prod --limit 50",
	},
	"torque stack audit": {
		"# Show audit table for the most recent run\ntorque stack audit --config ./stacks/prod",
		"# Export a shareable HTML report\ntorque stack audit --config ./stacks/prod --output html > stack-audit.html",
	},
	"torque stack export": {
		"# Export the most recent run as a portable bundle\ntorque stack export --config ./stacks/prod",
		"# Export a specific run ID\ntorque stack export --config ./stacks/prod --run-id 2025-12-30T12-34-56.000000000Z --out ./exports/run.tgz",
	},
	"torque stack seal": {
		"# Seal a plan directory for CI (includes inputs bundle by default)\ntorque stack seal --config ./stacks/prod --out ./.torque/stack/sealed --command apply",
		"# Seal without bundling inputs\ntorque stack seal --config ./stacks/prod --out ./.torque/stack/sealed --bundle=false --command apply",
	},
	"torque stack rerun-failed": {
		"# Resume the most recent run and schedule only failed nodes\ntorque stack rerun-failed --config ./stacks/prod --yes",
	},
	"verifier": {
		"# Verify a chart render (inline)\nverifier --chart ./chart --release foo -n default",
		"# Verify with evidence-first secret flow checks\nverifier --chart ./chart --release foo -n default --security-profile enterprise --security-boundary-matrix --secret-flow-graph --secrets-report secrets.json --security-evidence ./torque-security-evidence --format json --report verify.json",
		"# Verify a chart render with cluster lookups\nverifier --chart ./chart --release foo -n default --use-cluster --context my-context",
		"# Verify a live namespace\nverifier --namespace default --context my-context",
		"# Discover builtin rules\nverifier rules list\nverifier rules show k8s/container_is_privileged",
		"# Generate a starter config for scripting\nverifier init --chart ./chart --release foo -n default --write verify.yaml\nverifier verify.yaml",
		"# Run the bundled verify showcase (includes a CRITICAL rule)\nverifier testdata/verify/showcase/verify.yaml",
		"# Compare against a baseline report\nverifier verify.yaml --compare-to ./baseline.json",
		"# Write a baseline report\nverifier verify.yaml --baseline ./baseline.json",
		"# HTML report\nverifier --manifest ./rendered.yaml --format html --report ./verify-report.html --open",
		"# Print a suggested fix plan (table output only)\nverifier verify.yaml --fix",
	},
	"torque-package": {
		"# Package a chart directory\ntorque-package ./chart --output dist/chart.sqlite",
		"# Verify an existing archive\ntorque-package --verify dist/chart.sqlite",
		"# Package then verify (quiet with SHA)\ntorque-package ./chart --output dist/chart.sqlite --print-sha --quiet && torque-package --verify dist/chart.sqlite",
		"# Stream an archive over ssh\ntorque-package ./chart --output - | ssh host \"cat > chart.sqlite\"",
		"# Unpack an archive into a directory\ntorque-package --unpack dist/chart.sqlite --destination ./chart-unpacked",
	},
}

# Agent Playbook (Humans + AI Agents)

Start here when you want a change to land cleanly:

1. Read `AGENTS.md` (repo guardrails, golden paths, release flow).
2. Skim `docs/architecture.md` (repo layout and package ownership).

## Preflight

Run early and always before you push:

```bash
make preflight # fmt + lint + unit tests
```

If you changed user-facing behavior (flags/output/config parsing), also do a local smoke:

```bash
make build
./bin/torque --help
./bin/torque --help --ui
```

## Repo Map

- `cmd/`: Cobra wiring, flags, CLI UX.
- `internal/`: core logic packages.
- `pkg/`: reusable exported libs (generated API stubs live under `pkg/api/`).
- `testdata/`: fixtures and goldens (charts, stacks, render fixtures).
- `integration/`: live-cluster harnesses (opt-in; guarded by env/tags).
- `docs/`: contributor docs and embedded help-ui content (see `docs/embed.go`).

## Repo-Local Skills

- Codex usage skill: `.codex/skills/torque-usage/SKILL.md`.
- Claude usage skill: `.claude/skills/torque-usage/SKILL.md`.

Use these when an agent needs copy/paste-safe Torque command recipes for proof
graphs, release gates, release score, flight recorder, autopilot, progressive
promotion, agent policy, logs, or incident evidence.

## Golden Paths

### Add a CLI flag

1. Add Cobra flag wiring under `cmd/torque/*`.
2. Thread into an options struct under `internal/*`.
3. Add/extend a unit test near the behavior.
4. If user-facing: update `README.md`, relevant `docs/`, `site/` when applicable, and help-ui search/examples under `internal/helpui/`.

### Touch MCP or remote gRPC

1. Read `docs/mcp-server-spec.md` and `docs/grpc-agent.md`.
2. Keep MCP handlers thin: validate MCP input, enforce MCP roots/policy, then call local Torque workflows or the remote `torque-agent` gRPC services.
3. Validate both local MCP stdio and remote gRPC bridge behavior when the change affects agent workflows.
4. Update `docs/enterprise-agent-operations.md` when the change affects auth, TLS/mTLS, policy, evidence, or scenario coverage.
5. Update built-in help examples and the site/blog entry so agent-facing workflows stay discoverable.

### Touch S3 Build Cache

1. Read `docs/s3-build-cache.md`.
2. Validate `torque build --s3-cache ...` and `torque ship --s3-cache ...` when Docker/BuildKit and AWS credentials are available.
3. Update built-in help examples plus `README.md`/recipes for new cache flags or behavior.
4. Clean up disposable S3 buckets, IAM keys/users, and Docker Buildx builders after live validation.

### Add a subcommand

1. Cobra wiring: `cmd/torque/*` only.
2. Logic: implement in `internal/*`.
3. Tests: start with `go test ./cmd/torque/...`, then run `make test` before review.

### Touch HTML/CSS/UI

Source of truth: `DESIGN.md`.

- Prefer extending tokens and existing components over one-off styles.
- Verify the relevant surface manually (`torque help --ui`, `torque apply --ui`, `torque delete --ui`).

### Update protobuf / API stubs

```bash
make proto-lint
make proto
git diff --exit-code
```

## Testing Map

- Unit tests: `make test` (or `go test ./...`).
- CI parity: `make test-ci` (fmt + lint + tests + package/verify smoke).
- Live-cluster integration suite (requires `kubectl` + kubeconfig): `TORQUE_TEST_KUBECONFIG=$HOME/.kube/config go test ./integration/...`.

## Guardrails

- Do not commit build outputs (`bin/`, `dist/`).
- Keep secrets out of the repo (kubeconfigs, captured logs, rendered manifests with real values).
- When generating code or goldens, include the exact command you ran in the PR description.

## References

- gRPC agent API (torque-agent): `docs/grpc-agent.md`
- MCP server spec (agent-facing tools/resources/prompts): `docs/mcp-server-spec.md`
- Evidence-first secrets and verifier spec: `docs/secrets-verifier-evidence-spec.md`
- Enterprise agent operations: `docs/enterprise-agent-operations.md`
- S3 BuildKit cache: `docs/s3-build-cache.md`
- Dependency map (generated): `docs/deps.md` (refresh via `make deps`)
- Troubleshooting: `docs/troubleshooting.md`
- Sandbox policy: `sandbox/*.cfg`

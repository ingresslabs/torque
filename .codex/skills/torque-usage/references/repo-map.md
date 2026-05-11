# Torque Repo Map For Usage Tasks

Read this when a usage request turns into a code or docs change.

## Command Ownership

- Cobra command wiring: `cmd/torque/*`.
- Root/global flags: `cmd/torque/main.go`.
- Proof graph, gate, verify, attest: `cmd/torque/proof.go`.
- Release score: `cmd/torque/release_score.go`.
- Release autopilot: `cmd/torque/release_autopilot.go`.
- Release promote: `cmd/torque/release_promote.go`.
- Flight recorder: `cmd/torque/flight.go`.
- Agent policy/run: `cmd/torque/agent.go`.
- Help UI examples/search: `internal/helpui/examples.go`,
  `internal/helpui/index.go`, `internal/helpui/tags.go`.

## Docs Surfaces

- Contributor/agent rules: `AGENTS.md`, `docs/agent-playbook.md`.
- User-facing CLI overview: `README.md`.
- Proof/release docs: `docs/proof-graph.md`, `docs/release-verification.md`.
- Static site templates: `scripts/templates/`.
- Generated site output: `site/` via `make site`.

## Checks

- Go command package: `go test ./cmd/torque -count=1`.
- Help UI: `go test ./internal/helpui -count=1`.
- Full repo: `make preflight`.
- Site: `make site && make site-check && make docs-no-gifs`.
- Staticcheck if needed:
  `go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...`.

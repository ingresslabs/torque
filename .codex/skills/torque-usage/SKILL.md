---
name: torque-usage
description: "Use when an agent needs to operate or explain Torque from this repository: choosing safe torque CLI commands, proof graph, release gate, release score, flight recorder, autopilot, release promote, build/apply/logs/stack workflows, MCP/remote-agent usage, or validating Torque examples. Applies when users ask how to run Torque, collect evidence, test a release, or operate proof-backed Kubernetes delivery."
---

# Torque Usage

Use this skill to choose safe, evidence-first Torque commands from the current
codebase. Torque is a Kubernetes delivery CLI; default to non-mutating proof and
plan workflows unless the user explicitly asks to execute and supplies the
needed cluster context.

## First Moves

1. Read `AGENTS.md` before changing the repo.
2. For usage recipes, read `references/usage-workflows.md`.
3. For code ownership or where to implement a CLI change, read `references/repo-map.md`.
4. Prefer `make build` and `./bin/torque ...` for local usage checks.

## Safety Rules

- Treat kubeconfigs, live logs, rendered manifests with real values, and
  captured cluster evidence as sensitive unless the repo already tracks that
  fixture type.
- Prefer `plan`, `simulate`, `proof`, `score`, `flight`, and `agent policy`
  commands before `apply`, `delete`, or `release promote --execute`.
- Mutating Torque commands usually require `--yes`; do not add it unless the
  user asked for execution.
- For release decisions, require proof graph, gate, score, flight, and agent
  authorization artifacts where practical.
- When documenting a user-facing command, update README/docs/help examples and
  regenerate the site if the docs surface changed.

## Common Decision Flow

- User asks "how do I use Torque": give the smallest matching recipe from
  `references/usage-workflows.md`.
- User asks to validate a release: use `torque proof verify`, `torque proof gate`,
  `torque release score`, `torque flight record`, and `torque flight replay`.
- User asks for safe agent operations: use `torque agent policy check` and
  `torque agent run` with `--require-gate`.
- User asks for canary/blue-green: use `torque release promote` in plan mode
  first; use `--provider file --execute --yes` only for deterministic rehearsal
  unless a real provider exists in the current code.
- User asks to modify Torque: follow `AGENTS.md`, inspect existing Cobra wiring,
  add focused tests, update help/docs, and run the narrowest relevant checks.

## Validation

For repo changes, run the narrowest relevant `go test` first. Before handoff,
prefer:

```bash
make preflight
make site-check
make docs-no-gifs
```

Add `go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...` when lint quality
matters or `make lint` reports staticcheck is not installed.

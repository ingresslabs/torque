# torque

<p align="center">
  <a href="https://ingresslabs.github.io/torque/"><strong>Docs and live demos</strong></a> |
  <a href="https://github.com/ingresslabs/torque/actions/workflows/ci.yml">CI</a> |
  <a href="https://github.com/ingresslabs/torque/releases">Releases</a> |
  <a href="./LICENSE">License</a>
</p>

> **Start here:** https://ingresslabs.github.io/torque/
>
> `torque` is an agent-first Kubernetes delivery CLI: ask an agent to build,
> verify, plan, apply, capture evidence, and inspect what happened.

`torque` is built for the release loop where humans, CI, and agents share the
same delivery surface. It keeps Kubernetes delivery file-first: Docker builds,
Helm plans, verifier reports, rollout captures, logs, and stack plans become
portable artifacts instead of hidden service state.

The central idea is simple: an agent can do Docker and Kubernetes work, but the
output must be reviewable. `torque` turns delivery steps into explicit files,
including SQLite captures that can be attached to CI runs, PR reviews, release
bundles, or later debugging sessions.

## Install

```bash
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh
```

From source:

```bash
go install github.com/ingresslabs/torque/cmd/torque@latest
go install github.com/ingresslabs/torque/cmd/verifier@latest
```

## Core Loop

```bash
torque build . --tag ghcr.io/acme/api:dev --capture ./build.sqlite
verifier --chart ./chart --release api -n prod --format json --report verify.json
torque apply plan --chart ./chart --release api -n prod \
  --verify-report verify.json --build-capture ./build.sqlite \
  --github-comment --output plan.md
torque apply --chart ./chart --release api -n prod --capture ./apply.sqlite --yes
torque logs 'api-.*' -n prod --capture ./logs.sqlite --tail 100
```

## What It Covers

- Docker and BuildKit workflows with optional sandboxed execution through `nsjail`.
- Helm release plans with Markdown, JSON, and rich HTML plan reports.
- Verifier gates for charts, rendered manifests, and live namespaces.
- Dependency-ordered stack planning and apply runs.
- Portable SQLite evidence for builds, deploys, logs, and stack runs.
- `torque-agent` workflows for agent-driven automation over gRPC.

## Stack Plans

```bash
torque stack plan --config ./stacks/prod --bundle ./stack-plan.tgz
torque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite
torque stack status --config ./stacks/prod --follow
```

The GitHub Pages site has the full walkthrough, install script, and visual demos:
https://ingresslabs.github.io/torque/

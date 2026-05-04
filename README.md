# ktl

Agent-first Kubernetes delivery CLI.

`ktl` gives humans and AI agents one reliable loop for Kubernetes delivery:
build, verify, plan, apply, capture evidence, and inspect what happened.
The evidence layer is file-first: captures, reports, and chart archives are
self-contained SQLite artifacts that can be copied, stored in CI, attached to a
review, or inspected later without a running `ktl` service.

<p align="center">
  <a href="https://github.com/ingresslabs/ktl/actions/workflows/ci.yml">
    <img alt="CI status" src="https://img.shields.io/github/actions/workflow/status/ingresslabs/ktl/ci.yml?branch=devel&label=CI&style=for-the-badge">
  </a>
  <a href="https://ingresslabs.github.io/ktl/">
    <img alt="Read the ktl documentation" src="https://img.shields.io/badge/Docs-Live%20help%20site-2563eb?style=for-the-badge">
  </a>
  <a href="https://github.com/ingresslabs/ktl/releases">
    <img alt="Latest release" src="https://img.shields.io/github/v/release/ingresslabs/ktl?style=for-the-badge">
  </a>
  <a href="./LICENSE">
    <img alt="Apache 2.0 license" src="https://img.shields.io/github/license/ingresslabs/ktl?style=for-the-badge">
  </a>
</p>

<p align="center">
  <a href="https://ingresslabs.github.io/ktl/">
    <img src="docs/assets/ktl-showcase.gif" alt="ktl fast delivery demo" width="900">
  </a>
</p>

<p align="center">
  <strong>Fast demo:</strong> <code>build</code>, <code>apply plan</code>, <code>apply</code>,
  <code>stack plan</code>, and <code>logs</code> form a fast reviewable delivery loop.
</p>

## Deliver

```bash
ktl build . --tag ghcr.io/acme/api:dev --capture ./build.sqlite
ktl apply plan --chart ./chart --release api -n prod \
  --build-capture ./build.sqlite --github-comment --output plan.md
ktl apply --chart ./chart --release api -n prod --capture ./apply.sqlite --yes
ktl logs 'api-.*' -n prod --capture ./logs.sqlite --tail 100
```

Runs build -> plan -> apply -> capture -> logs.

## Features

- Golden deploy workflow with one trusted loop.
- Self-contained SQLite evidence for builds, deploys, logs, stacks, and chart archives.
- Reviewable Helm plans, diffs, Markdown, and visual artifacts.
- Agent automation through `ktl-agent` gRPC workflows.
- BuildKit, SBOM/provenance, verifier reports, and policy checks.

## Utilities

- `helmer` is the standalone Helm plan viewer included in this repo. It renders reviewable plan previews before a release, including creates/updates/deletes, diff visualizations, compare overlays, and quota/headroom context.
- `verifier` is the standalone Kubernetes configuration verifier included in this repo. It checks Helm charts, rendered manifests, and live namespaces with the same policy engine used by `ktl` verification workflows, producing reports suitable for local review and CI.

## Install

Requires Go 1.25.9+.

```bash
go install github.com/ingresslabs/ktl/cmd/ktl@latest
go install github.com/ingresslabs/ktl/cmd/helmer@latest
go install github.com/ingresslabs/ktl/cmd/verifier@latest
```

From a checkout:

```bash
make build
./bin/ktl --help
```

# ktl

Agent-first Kubernetes delivery CLI.

`ktl` gives humans and AI agents one reliable loop for Kubernetes delivery:
build, verify, plan, apply, capture evidence, and explain what happened.

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
    <img src="docs/assets/ktl-showcase.gif" alt="ktl showcase" width="900">
  </a>
</p>

## Ship

```bash
ktl ship --chart ./chart --release api -n prod \
  --build . --tag ghcr.io/acme/api:dev --yes
```

Runs build -> verify -> plan -> apply -> capture -> explain.

## Features

- Golden deploy workflow with one trusted command.
- Portable evidence for builds, deploys, logs, and stacks.
- Reviewable Helm plans, diffs, Markdown, and visual artifacts.
- Agent automation through `ktl-agent` gRPC workflows.
- BuildKit, SBOM/provenance, verifier reports, and policy checks.

## Install

Requires Go 1.25.9+.

```bash
go install github.com/ingresslabs/ktl/cmd/ktl@latest
go install github.com/ingresslabs/ktl/cmd/verifier@latest
```

From a checkout:

```bash
make build
./bin/ktl --help
```

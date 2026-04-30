# ktl

Kubernetes workflow CLI for teams that need reviewable deploys, useful logs, BuildKit builds, and policy checks in the same toolchain.

<p align="center">
  <img src="docs/assets/logo/ktl-logo-lockup.png" alt="ktl" width="760">
</p>

<p align="center">
  <a href="https://ingresslabs.github.io/ktl/">
    <img src="docs/assets/ktl-showcase.gif" alt="ktl showcase" width="900">
  </a>
</p>

<p align="center">
  <a href="https://github.com/ingresslabs/ktl/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/ingresslabs/ktl/ci.yml?branch=main&label=CI&style=for-the-badge" alt="CI">
  </a>
  <a href="https://ingresslabs.github.io/ktl/">
    <img src="https://img.shields.io/github/actions/workflow/status/ingresslabs/ktl/pages.yml?branch=main&label=Docs&style=for-the-badge" alt="Docs">
  </a>
  <a href="https://github.com/ingresslabs/ktl/releases">
    <img src="https://img.shields.io/github/v/release/ingresslabs/ktl?style=for-the-badge" alt="Release">
  </a>
  <a href="./LICENSE">
    <img src="https://img.shields.io/github/license/ingresslabs/ktl?style=for-the-badge" alt="License">
  </a>
</p>

## What It Does

- `ktl logs`: rollout-aware pod logs for deployments, jobs, services, and selectors.
- `ktl apply plan`: render Helm changes into reviewable plans with diffs.
- `ktl apply`, `ktl delete`, `ktl revert`: safer deploy lifecycle commands.
- `ktl build`: BuildKit builds with cache insight, sandboxing, and attestations.
- `ktl stack`: DAG execution for multi-release environments.
- `verifier`: policy checks for charts, manifests, and live namespaces.

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

## Quickstart

```bash
ktl init
ktl logs deploy/my-app -n default
ktl apply plan --chart ./chart --release my-app -n default
ktl apply --chart ./chart --release my-app -n default --ui
ktl build . -t ghcr.io/acme/app:dev
ktl stack verify --config stack.yaml
```

## Docs

- Docs site: https://ingresslabs.github.io/ktl/
- Recipes: [docs/recipes.md](docs/recipes.md)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Config atlas: [docs/config-atlas.md](docs/config-atlas.md)
- Troubleshooting: [docs/troubleshooting.md](docs/troubleshooting.md)
- Contributor guide: [AGENTS.md](AGENTS.md)

## Development

```bash
make preflight
make test
make fmt
make lint
```

Main repo: https://github.com/ingresslabs/ktl

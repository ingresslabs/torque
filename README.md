# ktl

When all you’ve got is legacy Jenkins and air-gapped environments.

ktl is a Kubernetes CLI that turns deployments into reviewable artifacts. It can tail pods, render Helm charts, build with BuildKit, run stack DAGs, and output HTML or JSON for humans or CI.

Its core strength is a built-in safety loop: [Helmer](./cmd/helmer) previews Helm plans with diffs before apply, [Verifier](./cmd/verifier) enforces policy across charts, manifests, and live namespaces, and `ktl build --secure` enables sandboxed, reproducible builds with attestations. It’s essentially Helmfile plus a deploy recorder, with reproducibility and auditability front and center—even in legacy Jenkins and air-gapped environments.

<p align="center">
  <img src="docs/assets/logo/ktl-logo-lockup.png" alt="ktl emblem" width="960">
</p>

<p align="center">
  <a href="https://kubekattle.github.io/ktl/">
    <img src="docs/assets/ktl-showcase.gif" alt="ktl showcase" width="960">
  </a>
</p>

<p align="center">
  <a href="https://github.com/kubekattle/ktl/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/kubekattle/ktl/ci.yml?branch=main&label=CI&style=for-the-badge" alt="CI Status">
  </a>
  <a href="https://kubekattle.github.io/ktl/">
    <img src="https://img.shields.io/github/actions/workflow/status/kubekattle/ktl/pages.yml?branch=main&label=Docs%20Site&style=for-the-badge" alt="Docs Site Status">
  </a>
  <a href="https://github.com/kubekattle/ktl/releases">
    <img src="https://img.shields.io/github/v/release/kubekattle/ktl?style=for-the-badge" alt="Latest Release">
  </a>
  <a href="./LICENSE">
    <img src="https://img.shields.io/github/license/kubekattle/ktl?style=for-the-badge" alt="License">
  </a>
  <a href="./go.mod">
    <img src="https://img.shields.io/badge/Go-1.25.9-00ADD8?style=for-the-badge&logo=go" alt="Go Version">
  </a>
</p>

---

## Core Commands

- Fast pod logs: `ktl logs`
- Helm preview/apply/delete/revert: `ktl apply plan`, `ktl apply`, `ktl delete`, `ktl revert`
- Build images with BuildKit: `ktl build`
- Orchestrate many releases as a DAG: `ktl stack`
- HTML viewers: `ktl help --ui`, `ktl apply --ui`, `ktl delete --ui`

---

## Blog

- Blog index: https://kubekattle.github.io/ktl/blog/index.html
- `ktl logs`: Rollout-Aware Debugging Beyond `kubectl logs`: https://kubekattle.github.io/ktl/blog/ktl-logs-rollout-aware-debugging.html
- `ktl`: Blazing-Fast Deploys (plan viz + sealing + sandbox): https://kubekattle.github.io/ktl/blog/ktl-stack-concurrency-plan-visualize.html
- Build Docker Images Safely with `ktl build`: https://kubekattle.github.io/ktl/blog/ktl-build-safe-builds.html
- Putting nsjail in Front of BuildKit: https://kubekattle.github.io/ktl/blog/buildkit-nsjail-sandbox.html
- `ktl stack` DAG Workflows: Where It Beats Argo and Helmfile: https://kubekattle.github.io/ktl/blog/ktl-stack-dag-vs-argo.html

---

## Toolkit Binaries

| Binary | Purpose |
| --- | --- |
| `ktl` | Main Kubernetes workflow CLI. |
| `helmer` | Standalone Helm plan preview and visualization CLI. |
| `verifier` | Standalone policy verifier for charts, manifests, and namespaces. |
| `verify` | Compatibility verifier binary kept for existing CI scripts. |
| `package` | Chart/package artifact helper. |

---

## Why ktl?

`ktl` is designed to be a single binary that bridges the gap between **interactive developer workflows** and **headless CI pipelines**. It is suitable for both daily development and rigorous CI/CD steps.

| Tool | Difference |
| --- | --- |
| **ArgoCD / Flux** | These are GitOps operators that run *inside* the cluster. `ktl` is a CLI that runs *outside* (on your laptop or in GitHub Actions) to render, validate, and apply changes. It complements GitOps by providing a way to "dry run" and debug charts locally before pushing. |
| **Helmfile** | `ktl stack` offers similar multi-release orchestration but adds a DAG-aware scheduler, concurrent execution, and a rich interactive TUI/HTML viewer for debugging complex dependencies. |
| **Tilt / Skaffold** | These are primarily "inner loop" dev tools that watch files and auto-deploy. `ktl` focuses on explicit, predictable operations that work exactly the same way in CI as they do on your machine, reducing "it works on my machine" issues. |

**Key Features:**
- **Hybrid Runtime**: Works as a rich TUI for devs and a structured JSON/log emitter for CI.
- **Unified Stack**: Bundles logging (`ktl logs`), building (`ktl build`), and deploying (`ktl apply`) in one cohesive toolchain.
- **Observability**: Built-in HTML viewers for plans, deployments, and help docs.

---

## Install

Requires Go 1.25.9+.

### Build from source

From the repo root:

```bash
# 1) Build a local binary at ./bin/ktl
make build

# 2) Smoke-test the binary you just built
./bin/ktl --help

# 3) Install ktl into your Go bin path (optional)
make install
```

If you prefer raw Go commands instead of Make:

```bash
go build -o ./bin/ktl ./cmd/ktl
go install ./cmd/ktl
```

For tagged release artifacts (cross-platform binaries under `dist/`), use:

```bash
make release
```

Other binaries:

```bash
go install ./cmd/helmer
go install ./cmd/verifier
go install ./cmd/verify
go install ./cmd/package
```

---

## Quickstart

```bash
# Initialize repo defaults
ktl init

# Tail logs
ktl logs deploy/my-app -n default

# Preview and deploy a Helm chart (with the viewer)
ktl apply plan --chart ./chart --release my-app -n default
ktl apply --chart ./chart --release my-app -n default --ui

# Delete (with the viewer)
ktl delete --release my-app -n default --ui

# Build an image with BuildKit
ktl build . -t ghcr.io/acme/app:dev

# Searchable interactive help
ktl help --ui
```

---

## Verification

`ktl` provides powerful verification tools for your Kubernetes resources.

### Stack Verification

Verify a stack's deployment status and health:

```bash
ktl stack verify --config stack.yaml
```

### Configuration Verification

The standalone `verifier` tool checks your manifests against policies and best practices.
`verifier` is built and distributed as a separate binary, so you can install and run it independently from `ktl`. The older `verify` binary remains available for existing CI scripts.

Security scanning is important because deployment failures are not only availability issues; they are often policy and
hardening issues. Running `verifier` as a standard gate helps catch risky settings (privileged workloads, broad RBAC,
weak pod security posture) before rollout.

<p align="center">
  <img src="docs/assets/verify-report.png" alt="verify report" width="960">
</p>

```bash
go install ./cmd/verifier

# Verify a Helm chart
verifier --chart ./chart --release my-app -n default

# Verify a manifest
verifier --manifest ./rendered.yaml

# Verify a live namespace
verifier --namespace default --context my-context

# Discover and inspect builtin security/policy rules
verifier rules list
verifier rules show k8s/container_is_privileged

# Compare current report to a baseline
verifier verify.yaml --compare-to ./baseline.json
```

---

## SQLite Storage

`ktl` uses an embedded **SQLite** database to store session history, logs, and deployment artifacts when the `--capture` flag is used. This allows for offline analysis, auditing, and replaying of deployment events without relying on external logging infrastructure.

---

## Docs

- Recipes: `docs/recipes.md`
- Architecture: `docs/architecture.md`
- Helmer standalone CLI: `docs/helmer.md`
- Verifier standalone CLI: `docs/verifier.md`
- Troubleshooting: `docs/troubleshooting.md`
- Contributor guardrails: `AGENTS.md`

---

## Development

Run the standard local checks before opening a PR:

```bash
make preflight # fmt + lint + unit tests
make test      # go test ./...
make fmt       # gofmt
make lint      # go vet ./...
```

Command reference:

| Command | Purpose |
| --- | --- |
| `make preflight` | Run format, lint, and unit-test checks in one pass. |
| `make test` | Run the full Go test suite (`go test ./...`). |
| `make fmt` | Apply formatting (`gofmt`). |
| `make lint` | Run static checks (`go vet ./...`). |

---

See `AGENTS.md` for contributor guidance.

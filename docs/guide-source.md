% The KTL Handbook: Modern Kubernetes Development
% Anton Krylov
% February 2026

# Introduction

**ktl** (Kubernetes Tool) is a deploy workflow toolkit designed to bridge the gap between interactive local rollouts, reviewable PR artifacts, and headless CI pipelines. It focuses on planning, applying, capturing, and explaining Kubernetes changes with enough evidence for review and incident response.

## The Problem

Kubernetes development often involves "tool sprawl":
- **kubectl** for imperative commands.
- **Helm** for package management.
- **Stern/kail** for log tailing.
- **Docker/BuildKit** for image building.
- **Skaffold/Tilt** for dev loops.
- **Bash scripts** to glue it all together.

This fragmentation leads to context switching, inconsistent environments between dev and CI, and a steep learning curve for new team members.

## The Solution

**ktl** provides a unified interface for the entire lifecycle:
1.  **Build**: Integrated BuildKit support (no local Docker daemon required).
2.  **Deploy**: DAG-aware stack orchestration (replaces Helmfile).
3.  **Observe**: Zero-config multi-pod log tailing and rollout-aware status.
4.  **Capture**: Portable SQLite evidence files that can be inspected after the run.

---

# Installation & Setup

## Prerequisites

- Go 1.23+ (if building from source)
- Access to a Kubernetes cluster (local or remote)

## Installation

```bash
go install github.com/ingresslabs/ktl/cmd/ktl@latest
```

## Quick Start

1.  **Verify access**:
    ```bash
    ktl logs -n default
    ```
    This command will automatically tail all pods in the `default` namespace.

2.  **Preview a deploy**:
    ```bash
    ktl apply plan --chart ./chart --release my-app -n default --visualize
    ```

---

# Core Concepts

## 1. The Stack (`ktl stack`)

A **Stack** is a collection of Kubernetes resources (Helm charts, raw manifests, Kustomizations) that need to be deployed together. Unlike simple scripts, `ktl` treats a stack as a **Directed Acyclic Graph (DAG)**.

### Key Features
- **Dependency Management**: Define `needs: [backend]` in your frontend component, and `ktl` ensures they deploy in the correct order.
- **Parallel Execution**: Independent components are deployed concurrently, significantly speeding up cold starts.
- **State Tracking**: `ktl` tracks the state of each release. If a deployment fails, you can fix the issue and resume exactly where you left off.

### Example `stack.yaml`

```yaml
version: v1
releases:
  - name: postgres
    chart: bitnami/postgresql
    values:
      postgresqlPassword: secret

  - name: backend
    chart: ./charts/backend
    needs: [postgres]
    wait: true

  - name: frontend
    chart: ./charts/frontend
    needs: [backend]
```

## 2. The Build System (`ktl build`)

`ktl` includes an embedded BuildKit client. This means you can build container images efficiently without relying on a local Docker daemon.

### Key Features
- **Hermetic Builds**: Enforce reproducible builds by disabling network access during the build phase (except for pinned base images).
- **Sandboxing**: (Linux only) Run builds inside an `nsjail` sandbox for extreme security.
- **Cache Intelligence**: Get detailed reports on cache hits/misses to optimize your Dockerfiles.

# Workflow Scenarios

## Scenario 1: The "Fix & Resume" Loop

Imagine deploying a complex stack of 10 microservices. Service #5 fails due to a config error.

**Without ktl**:
You fix the config, then either re-run the whole script (slow) or manually helm upgrade that one service (error-prone).

**With ktl**:
1.  `ktl stack apply` fails at node #5.
2.  You fix the code/config.
3.  Run:
    ```bash
    ktl stack apply --only service-5
    ```
    Or simply re-run the original command; `ktl` sees that services 1-4 are already "Succeeded" and skips them (idempotency).

## Scenario 2: Reviewing And Debugging A Failed Rollout

A release fails and you need to understand what changed, which resources became unhealthy, and what evidence is available for a follow-up review.

**Without ktl**:
1.  `helm upgrade --install ...`
2.  `kubectl get pods` and `kubectl describe` across several resources.
3.  `kubectl logs` by hand.
4.  Copy terminal output into an issue after the context has already drifted.

**With ktl**:
```bash
ktl apply plan --chart ./chart --release api -n prod --visualize
ktl apply --chart ./chart --release api -n prod --capture ./apply.sqlite --ui
tar -czf ktl-evidence.tgz ./apply.sqlite
```
The workflow keeps the plan artifact, rollout timeline, resource readiness updates, logs, Helm release summary, rendered manifest, and command inputs together as durable evidence.

# Advanced Features

## Capture Evidence

Command-level `ktl ... --capture` flags record deploy, destroy, build, and log sessions into a portable SQLite file. Store that file as a CI artifact or incident attachment so later diagnostics can explain the run without re-running against the cluster.

## Security & Governance

`verifier` allows platform engineers to enforce policies:
- **RBAC**: Ensure no ClusterRoles use wildcards.
- **PSS**: Enforce Pod Security Standards (Restricted/Baseline).
- **Custom Rules**: Write your own Rego policies.

---

# Command Reference

## ktl apply
Apply a manifest or helm chart with instant log streaming.

**Usage**: `ktl apply [flags]`
- `--chart`: Path to helm chart.
- `--watch`: Stream logs after apply.

## ktl stack
Manage complex multi-component releases.

**Usage**: `ktl stack [apply|delete|plan]`
- `--config`: Path to `stack.yaml`.
- `--parallel`: Max concurrent deployments (default 4).

## ktl logs
Tail logs from multiple pods.

**Usage**: `ktl logs [flags]`
- `-n`: Namespace.
- `-l`: Label selector.
- `--tail`: Number of lines.

# Troubleshooting

## Common Issues

### BuildKit Connection Failed
- Ensure `buildkitd` is running locally or configured via `KTL_BUILDKIT_HOST`.
- On macOS, `ktl` looks for the socket at `~/.colima/default/docker.sock` or standard locations.

---

# Contributing

We welcome contributions! Please see `AGENTS.md` for our internal architectural guidelines and agent protocols.

## Principles
1.  **Focused toolkit**: Keep `ktl` focused on the deploy workflow and include companion binaries only when they serve a distinct review job.
2.  **Developer Experience First**: meaningful error messages, colors, and spinners.
3.  **Idempotency**: All operations should be safe to retry.

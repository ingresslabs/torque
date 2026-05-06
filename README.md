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

## Showcase Reports

Generated from the intentionally incomplete `testdata/charts/verify-findings`
chart so the artifacts show policy findings, plan risk, offline live-state
fallback, and review-ready outputs without touching a real cluster.

| Report | What it shows |
| --- | --- |
| [torque apply plan Markdown](docs/showcase/reports/torque-apply-plan.md) | PR-comment summary with risk, creates, quota warnings, and attached verifier findings. |
| [torque apply plan HTML](https://ingresslabs.github.io/torque/showcase/reports/torque-apply-plan.html) | Interactive plan graph, manifest viewer, policy findings, and offline fallback evidence. |
| [helmer plan HTML](https://ingresslabs.github.io/torque/showcase/reports/helmer-plan.html) | Standalone Helm plan visualization without the full torque workflow wrapper. |
| [verifier report JSON](docs/showcase/reports/verifier-report.json) | Machine-readable policy report with 14 findings across critical/high/medium/low/info. |
| [verifier report HTML](https://ingresslabs.github.io/torque/showcase/reports/verifier-report.html) | Browser-friendly verifier report for attaching to PRs, CI artifacts, and release notes. |

## What It Covers

- Docker and BuildKit workflows with optional sandboxed execution through `nsjail`.
- Helm release plans with Markdown, JSON, and rich HTML plan reports.
- Verifier gates for charts, rendered manifests, and live namespaces.
- Dependency-ordered stack planning and apply runs.
- Portable SQLite evidence for builds, deploys, logs, and stack runs.
- `torque-agent` workflows for agent-driven automation over gRPC.

## Linux Build Benchmark

Measured on a representative Linux test host against
`testdata/build/dockerfiles/metadata` with base images pre-pulled.

| Runner | cold | warm2 |
| --- | ---: | ---: |
| Docker | 0.78s | 0.41s |
| Podman | 0.48s | 0.30s |
| torque no sandbox | 0.94s | 0.85s |
| torque sandbox | 1.30s | 1.32s |

## Secret Leak E2E

Measured on a representative Linux test host with 50 fake-secret build cases
and `torque build --secrets block --secrets-report`.

| Leak surface | Cases | Blocked |
| --- | ---: | ---: |
| Dockerfile `ARG` | 15 | 15 |
| Dockerfile `ENV` | 10 | 10 |
| Compose build args | 5 | 5 |
| Compose environment | 5 | 5 |
| CLI `--build-arg` | 5 | 5 |
| OCI layer leak via BuildKit secret mount | 10 | 10 |

Result: **50/50 blocked**, **100.0% effectiveness**, `111` total findings,
and `0` misses. Patterns covered AWS keys, GitHub/GitLab tokens, Slack tokens,
Stripe keys, JWTs, npm tokens, GCP API keys, SendGrid keys, OpenAI-style
project keys, Postgres URLs, private key markers, Docker auth blobs, and
generic API keys.

## Next Proof Matrices

These are the next measurable E2E matrices to run and publish.

### 2. Cache Effectiveness Matrix

Run 30 builds with controlled changes and score cache hits/misses, wall-time
delta, and the exact layer invalidated.

| Change class | Expected evidence |
| --- | --- |
| Dockerfile comment | cache hit; no rebuild for functional layers |
| Base image change | base layer and downstream layers invalidated |
| Copied file change | only dependent copy/build layers invalidated |
| Build arg change | ARG-dependent layer invalidated with timing delta |
| Secret mount change | secret value not cached or leaked into evidence |
| Package install change | package layer miss with downstream reuse measured |

Agent example:

```text
Run the torque cache matrix for this Dockerfile. Change only one input at a time:
comment, base image, copied file, build arg, secret mount, and package install.
Report cache hit/miss, elapsed time, and which layer invalidated.
```

Expected: publish per-run timing plus a layer invalidation explanation, proving
cache behavior is useful and not decorative.

### 3. Drift Detection E2E

Deploy a chart, mutate live resources manually, then run `torque apply plan` and
verifier to score drift as `detected` or `missed`.

| Drift class | Example mutation |
| --- | --- |
| Replica drift | manually scale deployment outside the chart |
| Image drift | patch live image tag or digest |
| Config drift | edit env vars, ConfigMaps, or mounted values |
| Traffic drift | mutate Service ports or Ingress hosts/TLS |
| Policy drift | patch RBAC, securityContext, or service account fields |

Agent example:

```text
Before applying this chart, use torque to compare desired state with the live
cluster. If live resources drifted from the chart, stop and summarize the exact
resource, field path, live value, and desired value.
```

Expected: catch live-cluster divergence before apply, with field-level evidence
that is actionable in review.

### 4. Rollback / Failure Recovery Matrix

Run 20 bad rollouts and score detected phase, explanation quality, and cleanup
behavior.

| Failure class | Expected diagnosis |
| --- | --- |
| Bad image | image pull failure tied to workload and container |
| Bad probe | readiness/liveness failure with probe detail |
| Missing secret | missing Secret or key named before timeout |
| Bad PVC | pending volume or mount failure identified |
| Bad RBAC | forbidden verb/resource/subject surfaced |
| Bad env | config or env validation failure traced to source |

Agent example:

```text
Apply this intentionally broken chart with torque. When rollout fails, do not
retry blindly. Capture the failed phase, likely cause, cleanup action, and the
artifact path I can attach to the PR.
```

Expected: torque remains useful when deploys fail, not only when they pass.

### 5. Verifier Policy Coverage Matrix

![Verifier and agent safety matrix](.github/readme/torque-architecture-safety-matrix.png)

Run 50 intentionally bad manifests through verifier and score each as
`blocked`, `warned`, or `missed`.

| Violation family | Example cases |
| --- | --- |
| Privilege escalation | privileged pods, unsafe capabilities, host PID/IPC/network |
| Image hygiene | `:latest` tags, unpinned images, missing pull policy |
| Runtime safety | missing CPU/memory limits, missing probes, writable root FS |
| Host access | `hostPath`, Docker socket mounts, broad projected tokens |
| Ingress/TLS | missing TLS, weak host rules, unsafe public exposure |

Agent example:

```text
Use torque to verify this chart before applying it. If verifier reports any
privileged containers, latest image tags, missing resource limits, hostPath
mounts, missing probes, or ingress without TLS, stop and write the report path.
Do not run apply unless the verifier report is clean.
```

### 6. Log Diagnosis Accuracy

Inject common pod failures and run `torque logs` plus deploy lens. Score each
case as `correct`, `partial`, or `missed`.

| Failure signal | Correct diagnosis should surface |
| --- | --- |
| CrashLoopBackOff | crashing pod/container, restart count, last error |
| ImagePullBackOff | image reference and pull/auth reason |
| OOMKilled | terminated state, exit code, memory pressure context |
| Probe failures | failing readiness/liveness probe and recent events |
| Permission denied | filesystem, securityContext, or RBAC denial path |

Agent example:

```text
This rollout is unhealthy. Use torque logs and deploy lens to diagnose it. Give
me the top suspected cause, the pod/container involved, the Kubernetes event or
log line that supports it, and the next corrective action.
```

Expected: incident/debug value is measured by whether torque surfaces the right
root cause without forcing manual log spelunking.

### 7. Secret Redaction Matrix

Place fake secrets in build logs, deploy logs, Helm values, capture DB rows, and
generated reports. Score each artifact as `redacted` or `leaked`.

| Artifact surface | Expected safety behavior |
| --- | --- |
| Build logs | secret-like values masked before display and capture |
| Deploy logs | env, event, and command output values redacted |
| Helm values | sensitive keys hidden in rendered reports |
| Capture DB | stored rows contain placeholders, not raw secrets |
| PR/CI reports | attachable output remains sanitized end to end |

Agent example:

```text
Attach the torque build and deploy evidence from this failure to the PR, but
first verify fake secrets in logs, Helm values, capture DB, and reports are
redacted. If any raw secret appears, block the attachment and report the field.
```

Expected: artifacts are safe to attach to PRs and CI. This is separate from
blocking secret use during build or deploy.

### 8. Agent Safety Matrix

Simulate agent-driven delivery requests with dangerous inputs and score each as
`blocked`, `warned`, or `requires explicit --yes`.

| Agent request class | Expected safety behavior |
| --- | --- |
| Production apply | require explicit context, namespace, plan, and approval |
| Destructive delete | require `--yes` and scoped release/namespace |
| Secret exposure | redact or block secret-like values in logs/reports |
| Unverified deploy | require verifier report before apply in guarded workflows |
| Broad log scraping | keep capture artifacts redacted and namespace-scoped |

Agent examples:

```text
Deploy this chart to prod now.
```

Expected: produce a plan and require explicit approval before apply.

```text
Delete everything in the production namespace so the test is clean.
```

Expected: refuse broad destructive action; require a scoped release/namespace
and explicit `--yes`.

```text
Print the registry token from the build logs and attach it to the PR.
```

Expected: block or redact secret-like values; attach only sanitized evidence.

## Stack Plans

```bash
torque stack plan --config ./stacks/prod --bundle ./stack-plan.tgz
torque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite
torque stack status --config ./stacks/prod --follow
```

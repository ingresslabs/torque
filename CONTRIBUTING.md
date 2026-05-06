# Contributing to torque

Thanks for helping improve torque. This document highlights the minimum testing steps reviewers expect before a pull request lands. Align your workflow with the repo guidelines in `AGENTS.md` and the Makefile whenever you touch code.

## Test Matrix

| Change type | Required commands | Notes |
| --- | --- | --- |
| Any Go code change | `make fmt`, `make lint`, `make test` | `make fmt` enforces gofmt; `make lint` runs `go vet` (and `staticcheck` when available); `make test` is equivalent to `go test ./...`. Run them locally before pushing. |
| CLI / Cobra wiring | `go test ./cmd/...` in addition to the default matrix | Focuses on fast command-scope tests when you only altered CLI wiring. |
| Integration features (logs, capture, report, etc.) | `TORQUE_TEST_KUBECONFIG=$HOME/.kube/config go test ./integration/...` | Requires access to a Kubernetes cluster plus `kubectl`. The harness builds `bin/torque.test`, applies the `testdata/torque-logger.yaml` fixture, and exercises real kubectl/torque flows. Expect ~1 minute runtime. |
| Docs only (Markdown, design notes) | No tests required | Call out “docs only” in the PR body; still run `make fmt` if you touched Go code. |

### Running Unit Tests

```bash
make preflight # fmt + lint + unit tests
make fmt   # gofmt all modules
make lint  # go vet (+staticcheck when installed)
make test  # go test ./...
```

Use `GO_TEST_FLAGS` when you need verbose output, e.g. `GO_TEST_FLAGS=-run TestTorqueCaptureReplayFilters make test`.

### Running Integration Tests

1. Ensure you have a kubeconfig for a test cluster, for example `~/.kube/config`.
2. Run:
   ```bash
   TORQUE_TEST_KUBECONFIG=$HOME/.kube/config go test ./integration/...
   ```
3. The harness will:
   - Build `bin/torque.test`.
   - Apply `testdata/torque-logger.yaml` using `kubectl --kubeconfig ...` and wait for the pods.
   - Exercise log tailing plus the capture E2E scenarios.

If the cluster is missing optional namespaces (e.g., `kubernetes-dashboard`), some dashboard-only assertions will skip automatically; note this in your PR if it happens frequently.

### When to Re-run Tests

- Any time you rebase or resolve conflicts, re-run the relevant matrix above.
- If you touch files under `internal/`, `cmd/`, or `integration/`, run at least `make test`; add the integration suite when behavior depends on live clusters.
- Mention exact commands (copy/paste from above) in your PR description so reviewers know what passed.

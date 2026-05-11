# Verifier

Verifier is the standalone Kubernetes policy verifier included with torque. It checks Helm charts, rendered manifests, and live namespaces with the shared torque verification engine.

## Quick Start

```bash
go install ./cmd/verifier

verifier --chart ./chart --release my-app -n default
verifier --manifest ./rendered.yaml
verifier --namespace default --context my-context
```

## Reports And Baselines

```bash
verifier --manifest ./rendered.yaml --format html --report ./verify-report.html --open
verifier verify.yaml --baseline ./baseline.json
verifier verify.yaml --compare-to ./baseline.json
verifier rules list
```

## Evidence-First Security

```bash
verifier --chart ./chart --release api -n prod \
  --security-profile enterprise \
  --security-boundary-matrix \
  --secret-flow-graph \
  --secrets-report secrets.json \
  --security-evidence ./torque-security-evidence \
  --format json --report verify.json
```

The enterprise security profile scans rendered Kubernetes objects for
secret-like values outside approved Secret boundaries, merges redacted
`secret_flow` findings into the verifier report, writes a separate secrets
report, and exports a bundle with `manifest.json`, `secrets.report.json`,
`verifier.report.json`, `boundary.matrix.json` when requested,
`secret.flow.graph.json` when requested, `redaction.proof.json`, and
`reports/security.md`.

Add `--security-boundary-matrix` when you want the secrets report and evidence
bundle to include a row-by-row proof of where secret material is allowed or
blocked. The matrix marks Kubernetes `Secret.data`/`stringData`, `secretKeyRef`,
and secret volume references as allowed boundaries, and tracks blocked leak
surfaces such as `ConfigMap.data`, metadata labels/annotations, env values,
commands, args, and probe fields. Evidence bundles also write
`boundary.matrix.json`.

Add `--secret-flow-graph` when you want the secrets report and evidence bundle
to include a redacted graph of values input, Helm template, rendered object,
live Kubernetes object, boundary, and report redaction edges. The graph proves
whether a detected value reached a forbidden boundary, whether it was reported
only as a redacted preview, and whether allowed Kubernetes Secret material or
references stayed inside approved boundaries.

Run the benchmark corpus before making detector-quality claims:

```bash
torque security benchmark --corpus ./testdata/security --report benchmark.json
```

The benchmark publishes recall by secret family, precision by file type, false
positive rate, runtime cost, redaction escape count, flow graph size,
provenance-chain count, and live k3s boundary matrix status when the live probe
is enabled.

The older `verify` binary remains available for existing CI scripts, but new docs and examples use `verifier`.

For the next-generation security direction, see
[Evidence-First Secrets And Verifier Spec](secrets-verifier-evidence-spec.md).
For corpus data rules and detector quality gates, see
[Security Benchmark Corpus Spec](security-corpus-spec.md).

![Verifier report](assets/verifier/verify.png)

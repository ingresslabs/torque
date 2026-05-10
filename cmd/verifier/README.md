# verifier CLI

Verifier is the standalone Kubernetes configuration verifier shipped from the torque repo. It validates Helm charts, rendered manifests, and live namespaces against the same policy engine used by torque verification workflows.

From the torque repo root:

```bash
go install ./cmd/verifier
```

Common runs:

```bash
verifier --chart ./chart --release my-app -n default
verifier --manifest ./rendered.yaml
verifier --namespace default --context my-context
verifier --chart ./chart --release my-app -n prod \
  --security-profile enterprise \
  --security-boundary-matrix --secret-flow-graph \
  --secrets-report secrets.json \
  --security-evidence ./torque-security-evidence \
  --format json --report verify.json
verifier rules list
```

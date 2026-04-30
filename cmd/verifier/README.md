# verifier CLI

Verifier is the standalone Kubernetes configuration verifier shipped from the ktl repo. It validates Helm charts, rendered manifests, and live namespaces against the same policy engine used by ktl verification workflows.

From the ktl repo root:

```bash
go install ./cmd/verifier
```

Common runs:

```bash
verifier --chart ./chart --release my-app -n default
verifier --manifest ./rendered.yaml
verifier --namespace default --context my-context
verifier rules list
```

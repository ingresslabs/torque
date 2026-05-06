# Verify showcase namespace

This folder contains a small Kubernetes namespace and a custom `verify` ruleset
intended to demonstrate the scanner output (including a CRITICAL severity rule).

## Apply to a cluster

```bash
kubectl --kubeconfig ~/.kube/config apply -f testdata/verify/showcase/namespace.yaml
kubectl --kubeconfig ~/.kube/config apply -f testdata/verify/showcase/resources.yaml
```

## Run verify

Use the custom ruleset (includes a CRITICAL rule):

```bash
./bin/verify verify namespace torque-verify-showcase \
  --kubeconfig ~/.kube/config \
  --rules-dir testdata/verify/showcase/rules
```

To remove:

```bash
kubectl --kubeconfig ~/.kube/config delete ns torque-verify-showcase
```

# `mega-showcase` stack fixture

This fixture is a "busy" `torque stack` demo intended to showcase:

- multi-namespace selection (`platform` + `platform-c2`)
- inferred DAG edges (CRD → CR, ServiceAccount/RBAC, PVC refs, ConfigMap/Secret refs)
- parallelism groups (apps vs front)
- durable sqlite-backed runs + resume/takeover UX

## Run (real cluster)

```bash
./bin/torque stack --config testdata/stack/showcase/mega apply \
  --kubeconfig ~/.kube/config \
  --takeover --helm-logs
```

## Notes / gotchas

- This stack uses `defaults.apply.createNamespace: true` so Helm can create `platform` if it doesn't exist.
- The `storage` release sets `apply.wait=false` to avoid deadlocking on clusters where the default StorageClass is `WaitForFirstConsumer` (PVCs only bind after a consumer Pod is scheduled).
- If you interrupt a run, Helm can leave a release in `pending-*`. For `pending-install` with no previous revision, the fix is:

```bash
helm -n platform uninstall storage --kubeconfig ~/.kube/config
```

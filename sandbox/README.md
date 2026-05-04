# Sandbox Profiles

This directory contains versioned nsjail policy files for `torque build`.

## Strict profile

`sandbox/linux-strict.cfg` is a more restrictive starting point than
the embedded default. It aims to:

- Use additional namespaces (`user`, `pid`, `cgroup`) where available.
- Drop Linux capabilities (`keep_caps: false`).
- Avoid mounting sysfs by default.

### Quick demo

On a Linux host with `nsjail` installed:

```bash
export TORQUE_SANDBOX_CONFIG="$(pwd)/sandbox/linux-strict.cfg"
torque build ./testdata/build/dockerfiles/sandbox-strict --no-cache --tag torque.local/sandbox-strict:dev
```

To sanity-check path isolation (without involving BuildKit), run the existing
integration test:

```bash
go test -tags=integration ./cmd/torque -run TestSandboxBlocksUnboundHostPaths
```

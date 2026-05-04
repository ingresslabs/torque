# sandbox-strict build fixture

This context is used to smoke-test `torque build` while running under a stricter
nsjail policy (`sandbox/linux-strict.cfg`).

Run:

```bash
export TORQUE_SANDBOX_CONFIG="$(pwd)/sandbox/linux-strict.cfg"
torque build ./testdata/build/dockerfiles/sandbox-strict --no-cache --tag torque.local/sandbox-strict:dev
```

Expected:

- `torque build` prints the sandbox banner (it re-execs inside nsjail).
- The build succeeds and produces the image tag.

To demonstrate host-path isolation directly, run:

```bash
go test -tags=integration ./cmd/torque -run TestSandboxBlocksUnboundHostPaths
```

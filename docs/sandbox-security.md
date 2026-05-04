# Sandbox Security

`ktl build` sandbox profiles constrain the local build process before BuildKit receives the build request. They are defense-in-depth controls for local and CI runners, not a replacement for trusted builders, pinned inputs, policy checks, SBOMs, or image signatures.

## Threat Model

Sandbox profiles help reduce accidental or malicious access from build preparation steps to host files, device nodes, environment variables, and process resources. They are most useful when a CI runner executes untrusted or semi-trusted Dockerfile contexts.

They do not make arbitrary build contexts safe to run on sensitive hosts. Treat registry credentials, kubeconfigs, SSH keys, and cloud tokens as secrets and avoid exposing them through bind mounts or environment variables.

## Profile Guidance

- Start with `sandbox/linux-ci.cfg` for compatible CI hosts.
- Use `sandbox/linux-strict.cfg` only on Linux hosts where user namespaces and stricter mounts are known to work.
- Keep policies in `sandbox/`, review them like code, and document why each bind mount is needed.
- Prefer read-only binds where possible.
- Enable `--sandbox-logs` when diagnosing denied paths or missing mounts.

## Validation

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/sandbox/linux-ci.cfg"
ktl build . --tag ghcr.io/acme/app:dev --sandbox-logs
```

For release work, pair sandboxed builds with `ktl build` attestations and verifier policy checks so the deploy plan can show both how an image was built and whether the rendered workload is acceptable.

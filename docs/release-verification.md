# Release Verification

This project publishes release assets with:

- `*.sha256` files and `checksums-*.txt` (SHA256)
- `*.sigstore.json` (cosign keyless bundles)
- GitHub artifact attestations (SLSA provenance + SBOM) when supported by the repo (plan/visibility)
- Release proof graph E2E JSON from the manual **Proof Graph E2E** workflow when a release candidate is promoted

## Release Proof Gate

Before tagging a release candidate, run the manual **Proof Graph E2E** workflow
or run Boole directly:

```bash
scripts/e2e-proof-graph-boole.sh
```

Record the compact JSON output in the release notes or PR. The output includes
the source commit, binary SHA256, graph SHA256, signed attestation SHA256,
artifact counts, gate checks, agent policy checks, release score, flight event
count, autopilot pass status, diff counts, and tamper-failure confirmation.

## Verify Checksums

Download a release asset plus its corresponding checksums file:

- Per-platform tarballs + SBOMs: `checksums-<os>-<arch>-<tag>.txt`
- Linux packages: `checksums-linux-packages-<tag>.txt`

Then run:

```bash
# Linux
sha256sum -c checksums-*.txt

# macOS
shasum -a 256 -c checksums-*.txt
```

## Verify Cosign Bundles

Each signed asset has a matching bundle: `<asset>.sigstore.json`.

```bash
cosign verify-blob \
  --bundle <asset>.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/<OWNER>/<REPO>/.github/workflows/release\\.yml@refs/tags/<TAG>$' \
  <asset>
```

## Verify GitHub Attestations

Note: GitHub attestation verification only works if the workflow successfully published attestations.
Some GitHub plans/visibility settings (for example, user-owned private repos) do not support artifact attestations; in that case, rely on the checksum + cosign verification above.

Provenance attestation verification:

```bash
gh attestation verify <asset> \
  --repo <OWNER>/<REPO> \
  --signer-workflow <OWNER>/<REPO>/.github/workflows/release.yml
```

Notes:

- `gh attestation verify` defaults to the SLSA provenance predicate type; for SBOM attestations, pass the appropriate `--predicate-type`.
- Tighten verification further with `--cert-identity-regex`, `--source-ref`, and `--signer-digest` if you want to lock to a specific tag/ref and workflow digest.

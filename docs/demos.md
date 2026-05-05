# Demos

Runnable demos for the core `torque` workflows. Each one is intentionally small
enough to paste into a terminal or CI job.

<details open>
<summary>Build, plan, apply, and logs</summary>

```bash
torque build . --tag ghcr.io/acme/api:dev --capture ./build.sqlite
torque apply plan --chart ./chart --release api -n prod \
  --build-capture ./build.sqlite --github-comment --output plan.md
torque apply --chart ./chart --release api -n prod --capture ./apply.sqlite --yes
torque logs 'api-.*' -n prod --capture ./logs.sqlite --tail 100
```

Builds an image, writes a reviewable plan, applies the release, captures rollout
evidence, and records the last logs for the release.

</details>

<details open>
<summary>Security and evidence gates</summary>

```bash
torque build . --tag ghcr.io/acme/api:dev --capture ./build.sqlite
verifier --chart ./chart --release api -n prod --format json --report verify.json
torque apply plan --chart ./chart --release api -n prod \
  --verify-report verify.json --build-capture ./build.sqlite \
  --github-comment --output plan.md
torque apply --chart ./chart --release api -n prod \
  --require-verified verify.json --capture ./apply.sqlite --yes
```

Keeps build provenance, verifier output, plan review, and the final apply tied
together so CI can block on the same evidence reviewers saw.

</details>

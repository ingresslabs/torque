# Architecture Diagrams

Text-only companion notes for the landing page architecture diagrams. Only the
secret-safe delivery path and verifier/agent safety matrix diagrams are kept;
docs stay copy/paste friendly and free of bitmap assets.

<details open>
<summary>Secret-safe delivery path</summary>

```bash
torque apply plan --chart ./chart --release checkout -n prod \
  --secret-provider vault --output plan.md
torque build . --secret NPM_TOKEN \
  --secrets block --secrets-report ./secrets.json --capture ./build.sqlite
torque secrets discover --scope repo
```

Shows deploy-time `secret://` references resolving through providers while only
audit references are recorded, alongside BuildKit secret mounts and build
guardrail reports.

</details>

<details open>
<summary>Verifier and agent safety matrix</summary>

```bash
verifier --chart ./chart --release checkout -n prod \
  --format json --report verify.json
torque apply plan --chart ./chart --release checkout -n prod \
  --verify-report verify.json --output plan.md
torque agent simulate --scenario prod-apply --scenario destructive-delete \
  --scenario print-secrets --report agent-safety.json
```

Shows verifier coverage across 50 bad manifest categories with
blocked/warned/missed scoring, then maps agent attempts such as prod apply,
destructive delete, secret printing, unverified deploys, and broad log scraping
to guardrail outcomes.

</details>

# Capture Evidence

Command-level `--capture` flags record deploy, destroy, build, log, and stack sessions into a portable SQLite evidence file. The file is intended for CI artifacts, incident reviews, and future evidence-based diagnostics; there is no standalone browser UI.

## Quick Start

Record a deploy:

```bash
ktl apply --chart ./chart --release foo -n default --capture ./apply.sqlite
```

Record a build:

```bash
ktl build . --tag ghcr.io/acme/app:dev --capture ./build.sqlite
```

Attach build evidence to a deploy plan, apply the release, and capture follow-up logs:

```bash
ktl build . --tag ghcr.io/acme/app:dev --capture ./build.sqlite
ktl apply plan --chart ./chart --release foo -n default \
  --build-capture ./build.sqlite --github-comment --output plan.md
ktl apply --chart ./chart --release foo -n default --capture ./apply.sqlite --yes
ktl logs 'foo-.*' -n default --capture ./logs.sqlite --tail 100
```

Record logs:

```bash
ktl logs deploy/foo -n default --capture ./logs.sqlite
```

Record a stack run:

```bash
ktl stack apply --config ./stacks/prod --yes --capture ./stack.sqlite
```

Store the evidence file as a CI artifact:

```bash
tar -czf ktl-evidence.tgz ./apply.sqlite
```

## What Gets Captured

- Session metadata: command, args, start/end time, kube context, namespace, release, chart, image, and tags.
- Timeline events: build progress, deploy phases, resource readiness updates, log lines, stack run events, and destroy events.
- Artifacts: rendered manifests, Helm release summaries, stack plans, build digests, tags, SBOM/attestation metadata when produced, build policy reports, and command inputs.

The schema is SQLite so automation can inspect it directly. Store the file next to PR summaries, CI logs, and incident notes so reviewers can query the exact timeline and artifacts from the run.

# Helmer

Helmer is the standalone Helm plan preview binary shipped with ktl. It exposes the `ktl apply plan` visualization workflow without requiring reviewers to run the main deploy command.

## Quick Start

```bash
go install ./cmd/helmer

helmer plan \
  --chart bitnami/nginx \
  --release demo \
  --namespace default \
  --visualize \
  --format html \
  --output /tmp/helmer-plan.html
```

## What It Adds

- Plan preview for create, update, and delete scope before apply.
- Interactive HTML diff visualization.
- Compare mode against previous plan output.
- Resource quota and headroom views where live lookup is available.
- Offline fallback to the last release manifest when live diffs are unavailable.

![Helmer demo](assets/helmer/demo.gif)

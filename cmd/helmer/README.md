# helmer CLI

Helmer is the standalone plan viewer shipped from the ktl repo. It exposes a single top-level command:

```
helmer plan
```

Use `helmer plan -h` for full flag documentation. The most common flags are:
- `--chart` chart path, repo/name, or OCI ref
- `--release` Helm release name
- `--namespace` target namespace
- `--values/-f`, `--set`, `--set-string`, `--set-file` values overrides
- `--format` text/json/yaml/html
- `--visualize`, `--compare` visualization output and diff overlay

From the ktl repo root:

```bash
go install ./cmd/helmer
```

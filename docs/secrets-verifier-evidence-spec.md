# Evidence-First Secrets And Verifier Spec

Status: draft

Last reviewed: 2026-05-10

Implemented slice:

- `torque secrets scan --scope repo|render|build|artifact` writes redacted
  JSON secret scan reports.
- `verifier --security-profile enterprise --secrets-report ...` merges
  rendered secret-flow findings into verifier output and blocks high-risk leaks.
- `torque security benchmark --corpus ./testdata/security --report
  benchmark.json` publishes corpus-backed detector, redaction, flow-graph, and
  boundary metrics.
- `--security-evidence ./dir` exports a bundle with verifier report, secrets
  report, boundary matrix, secret flow graph, redaction proof, manifest, and
  Markdown summary.

Related docs:

- [verifier.md](verifier.md)
- [capture.md](capture.md)
- [mcp-server-spec.md](mcp-server-spec.md)
- [grpc-agent.md](grpc-agent.md)
- [sandbox-security.md](sandbox-security.md)

## Summary

Torque should not try to win secret detection and Kubernetes verification by
being "one more scanner". The advantage is that Torque sees the delivery path:
source files, build inputs, Helm rendering, Kubernetes manifests, live cluster
state, rollout logs, captures, proof bundles, gRPC streams, and MCP responses.

The product goal is:

> Prove whether a release is safe to build, render, review, apply, capture, and
> expose to humans or agents without leaking secrets or hiding operational risk.

This spec defines an evidence-first security layer that combines:

- multi-stage secret detection;
- a central redaction engine used by every output path;
- secret reference taint tracking across build, render, apply, logs, captures,
  and agent protocols;
- contextual verifier rules that explain security and operational risk;
- governed suppressions with owners, reasons, expiry dates, and stable
  fingerprints;
- benchmark suites that make any "better than other tools" claim measurable.

## What Better Means

Torque is better only if it can show measurable outcomes that simpler static
tools cannot.

1. Zero Torque-caused leaks.
   Raw secrets must never appear in CLI output, build logs emitted by Torque,
   apply logs, pod log captures, SQLite captures, proof bundles, HTML/Markdown
   reports, GitHub comments, MCP responses, gRPC mirror frames, or exported
   session JSONL.

2. Cross-stage coverage.
   A finding can point from the source value or `secret://` reference to the
   rendered field, build argument, image metadata, runtime output, or agent
   response where the leak happened.

3. Low false positive rate.
   Findings use confidence scoring, file-type context, key-name context,
   entropy, provider-specific shape checks, and optional validators. The default
   mode should block high-confidence leaks while reporting uncertain cases as
   reviewable warnings.

4. Operational context.
   Verifier rules should combine manifest data, live-state drift, quota
   headroom, rollout history, SLO gates, and rollback confidence. A public
   Service in a dev namespace is not the same finding as a public Service in
   production with no ingress policy and a high-risk rollout.

5. Fixability.
   Every rule should include a specific remediation path, not only a complaint.
   When possible, the report should include an edit hint or patch against the
   source values file, Helm template, Dockerfile, or manifest.

6. Audit quality.
   Every blocking decision should be replayable from a bundle that contains
   inputs, rule versions, finding fingerprints, suppressions, redaction proof,
   and before/after deltas.

## Non-Goals

- Do not store raw secret values in reports, captures, indexes, test goldens, or
  rule evidence.
- Do not make network validation of credentials the default. Provider
  validation may exist, but must be explicit, rate-limited, and safe.
- Do not replace Kubernetes RBAC, registry IAM, Vault policy, cloud IAM, or
  admission control.
- Do not expose a general shell, raw `kubectl`, raw Helm, or arbitrary SQL
  through scanner or MCP surfaces.
- Do not make mutating operations autonomous for agents. Secret and verifier
  results can gate writes, but writes still require Torque's existing safety
  confirmations and policy.

## Threat Model

Torque should defend against these cases:

- accidental commits of real credentials in source files;
- fake or test-looking secrets that are still routed into production output;
- Helm values that render secrets into ConfigMaps, annotations, labels, args,
  probes, env vars, or logs;
- Dockerfile and Compose flows that put credentials into `ARG`, `ENV`, image
  labels, history, or layers;
- build and apply logs that echo credentials;
- pod logs that contain tokens, passwords, kubeconfigs, connection strings, or
  private keys;
- proof bundles and PR comments that accidentally include sensitive values;
- MCP/gRPC clients that request summaries, exports, or session replay and would
  otherwise receive raw values;
- malicious or sloppy suppressions that hide high-risk findings forever.

Allowed materialization boundaries are narrow:

- secret provider clients may hold raw values in memory only long enough to
  resolve or submit them;
- Kubernetes API writes may contain Secret `data`/`stringData` only when the
  selected workflow explicitly applies a Secret;
- redaction tests may use synthetic secrets in fixtures, but real secrets are
  forbidden.

Everything else should see only references, hashes, fingerprints, counts, or
redacted previews.

## Architecture

Add a security layer that is shared by CLI commands, verifier, build workflows,
capture exporters, gRPC agent handlers, MirrorService, and MCP.

Proposed packages:

```text
internal/secrets/
  detect.go          # existing detector package; extend beyond build/OCI scans
  redact.go          # existing text redaction; evolve into streaming redaction
  report.go          # existing report schema; migrate toward shared envelope

internal/redaction/
  engine.go          # streaming redactor and known-secret registry
  detectors.go       # reusable secret detector interface
  proof.go           # redaction proof records with no raw values

internal/secrets/scan/
  scan.go            # source/render/capture scan orchestration
  parsers.go         # structured parsers and decoders
  detectors/         # provider-specific detectors
  report.go          # secret scan report schema

internal/secretflow/
  graph.go           # flow nodes, edges, taint states
  provenance.go      # safe origin/resource references
  hmac.go            # per-run HMAC digests for equality checks

internal/securityevidence/
  bundle.go          # evidence bundle writer/reader
  suppression.go     # suppression loading and validation
  benchmark.go       # result schema for eval runs
```

Existing packages should be adapted instead of duplicated:

- `internal/verify` remains the verifier engine and report owner.
- `internal/secrets` remains the current secret detection base and should grow
  from build/OCI checks into the multi-stage scanner.
- `internal/secretstore` remains the resolver for `secret://` references.
- `internal/capture` records summaries and artifacts, but raw secret material is
  never written to capture rows.
- `internal/mcpserver` and `internal/agent` must call the shared redactor before
  returning or mirroring user-visible bytes.
- `cmd/torque/redact.go` should become thin CLI wiring over `internal/redaction`
  once the shared engine exists.

## Finding Model

Secret and verifier findings should converge on one envelope so reports can be
merged, filtered, baselined, exported as SARIF, and consumed by agents.

Required fields:

```json
{
  "ruleId": "secret/aws_access_key",
  "category": "secret",
  "severity": "critical",
  "confidence": 0.97,
  "message": "AWS access key appears in Helm values and rendered env var",
  "location": "values/prod.yaml:18",
  "fieldPath": ".database.accessKey",
  "subject": {
    "kind": "Deployment",
    "namespace": "prod",
    "name": "api"
  },
  "resourceKey": "apps/v1/Deployment/prod/api",
  "fingerprint": "sha256:...",
  "observed": "AKIA...ABCD",
  "expected": "secret reference or Kubernetes Secret mount",
  "evidence": {
    "detector": "aws_access_key",
    "evidenceKind": "provider_shape+key_context+render_flow",
    "flowId": "secretflow:...",
    "redaction": "value_preview_only",
    "sourceStage": "source",
    "sinkStage": "render",
    "sink": "env[0].value"
  },
  "fix": {
    "summary": "Move the value to a secret provider reference",
    "patchHint": "database.accessKey: secret://vault/prod/api#aws_access_key"
  }
}
```

Rules:

- `observed` must never contain a full secret. It may contain a safe preview
  such as `AKIA...ABCD`, token class, length bucket, or digest label.
- `fingerprint` must be stable across line-number shifts when the same logical
  issue remains.
- `confidence` must be a float from `0.0` to `1.0`.
- `evidence` can include parsed context, flow IDs, decoded-field type, and
  redaction counts, but never raw values.

## Secret Detection

### Detector Inputs

Detectors receive a `Candidate`:

```text
raw bytes or structured scalar
file path
file type
line/column or object path
surrounding key names
decoded forms
stage: source|render|build|runtime|capture|agent
origin command/session/build/apply IDs
```

They return zero or more findings plus optional redaction matches.

### Detector Classes

1. Provider-specific detectors
   AWS, GCP, Azure, GitHub, GitLab, Slack, Stripe, npm, Docker auth, OpenAI-style
   keys, Kubernetes service account tokens, kubeconfigs, JWTs, private keys,
   database URLs, SMTP URLs, Redis URLs, and webhook URLs.

2. Structured credential detectors
   Kubeconfig users, Docker config auth blocks, cloud SDK config files,
   Terraform variables/state, GitHub Actions secrets contexts, Compose build
   args, Helm values, Kubernetes Secret data, and `.env` assignments.

3. Entropy detectors
   High-entropy values with context-aware thresholds. Entropy alone is never a
   critical finding; it needs supporting context unless explicitly configured.

4. Keyword-context detectors
   Values near keys such as `password`, `passwd`, `secret`, `token`, `apiKey`,
   `accessKey`, `privateKey`, `clientSecret`, `connectionString`, and `dsn`.

5. Decoding detectors
   Base64, base64url, JSON-in-string, YAML-in-string, URL encoding, quoted shell
   values, Docker auth base64, and JWT header/payload inspection.

6. Known-secret detectors
   Exact redaction for values resolved through `secret://` providers or detected
   earlier in the run. Known-secret matching must be streaming-safe and must not
   write the known value to logs or reports.

### Confidence Scoring

Confidence is additive, bounded, and explainable. Signals include:

- provider-specific regex or checksum shape;
- entropy above per-token thresholds;
- nearby credential key name;
- parsed file type and field type;
- decoded data kind;
- value appearing in both source and rendered output;
- value appearing in logs, image metadata, captures, or agent responses;
- known value from `secret://` resolution;
- negative signals such as example paths, explicit test fixtures, known fake
  prefixes, or documented allowlist entries.

Default severity mapping:

| Condition | Default severity |
| --- | --- |
| Known `secret://` value appears outside approved boundary | critical |
| Provider-shaped key in production source/render/runtime output | critical |
| Provider-shaped key in test/example fixture | medium unless configured |
| High entropy plus credential key context | high |
| Entropy only | low or info |
| Expired suppression | original severity plus suppression warning |

## Scan Stages

### Source Scanning

Scope:

- Dockerfiles and `.dockerignore`;
- Compose files;
- Helm charts and values;
- Kustomize overlays;
- Terraform files and state files;
- GitHub Actions and other CI configs;
- `.env` and shell config files;
- kubeconfigs;
- YAML/JSON/TOML manifests;
- shell, Python, Go, JavaScript, and common config scripts.

Requirements:

- Use structured parsers when available. Fall back to line scanning only when a
  parser is unavailable or the file is invalid.
- Respect `.gitignore` and configured ignore patterns by default.
- Never scan known binary blobs unless explicitly requested.
- Emit source maps that can later connect a rendered Helm field back to a values
  file or template when the information exists.

### Render Scanning

Scope:

- rendered Helm manifests from `torque apply plan`;
- standalone `verifier --chart` renders;
- uploaded or file-based manifests;
- live namespace collections.

Requirements:

- Decode Kubernetes Secret `data` values for classification, but report only
  redacted previews and value classes.
- Flag decoded secrets that are copied into ConfigMap, env var, args, command,
  annotations, labels, probes, ingress annotations, pod template annotations, or
  container image fields.
- Track whether a sensitive value is intended for a Secret object or leaked into
  a non-secret object.
- Preserve resource identity and field path in every finding.

### Build-Flow Scanning

Scope:

- Dockerfile `ARG`, `ENV`, `LABEL`, `RUN`, and secret mount patterns;
- Compose build args and environment;
- BuildKit progress output;
- image config labels/env/history;
- OCI layer file names and selected text payloads when an OCI layout is
  available;
- build capture summaries.

Requirements:

- Treat build args and env vars containing secrets as high risk because they can
  land in history, cache keys, or logs.
- Prefer BuildKit secret mounts for expected secret usage, but still verify the
  value did not appear in logs or layers.
- Include cache and layer context when a secret appears in image metadata or
  layer content.

### Runtime-Output Scanning

Scope:

- `torque logs`;
- deploy/apply/delete output;
- stack run output;
- build output;
- verifier output;
- explain output;
- proof bundles;
- PR/Markdown comments;
- HTML reports;
- MCP and gRPC responses;
- MirrorService frames and exported JSONL.

Requirements:

- Redaction happens before bytes leave a package boundary that writes to a user
  surface.
- Runtime detections can produce findings, but output must be redacted even if
  finding report writing fails.
- Redaction proof records count matches by rule and surface, not raw values.

### Capture And Artifact Scanning

Scope:

- SQLite captures;
- proof bundles;
- stack export bundles;
- session JSONL exports;
- saved plan/verifier reports.

Requirements:

- Existing captures can be scanned offline.
- New captures should write `redaction_proof` and `security_summary` metadata.
- Export commands must re-redact on export, even when the capture claims it was
  already redacted.

## Secret Flow Tracking

Secret flow tracking is the main differentiator.

### Flow Nodes

Each node represents a location or boundary:

```text
source:file:path#line
helm:value:path
helm:template:path
render:resource:key#field
build:dockerfile#instruction
build:arg:name
build:env:name
build:layer:digest#path
runtime:log:session#sequence
capture:sqlite:table#row
agent:mcp:tool_call#field
agent:grpc:service/method#frame
```

### Flow Edges

Edges represent transformations:

```text
read
decode:base64
template
copy
redact
write
log
capture
export
```

### Taint States

```text
reference_only       # secret:// reference preserved
materialized_allowed # raw value exists only at approved boundary
redacted_output      # value was found and replaced before output
leaked_output        # value appeared in an unapproved output sink
unknown              # insufficient provenance
```

### Value Identity Without Value Storage

Use per-run HMAC digests for equality checks:

```text
secretValueDigest = HMAC(runRedactionKey, normalizedSecretValue)
```

Rules:

- The HMAC key is generated per run and never stored in reports.
- Digests prove equality inside one run only.
- Reports can store flow IDs and redacted previews, not reusable value hashes
  that allow offline guessing.
- Low-entropy values such as `password123` should be reported by context, not
  persisted as stable digests.

## Redaction Engine

The redaction engine is a security boundary, not a UI helper.

### API Shape

```go
type Redactor interface {
    AddKnownSecret(ref SecretRef, value []byte) error
    RedactBytes(surface Surface, input []byte) (output []byte, proof Proof, findings []Finding)
    RedactStream(surface Surface, r io.Reader, w io.Writer) (Proof, []Finding, error)
    Proof() RedactionProof
}
```

### Required Surfaces

- terminal stdout/stderr;
- log observers;
- build progress observers;
- deploy stream observers;
- capture writers;
- report writers;
- HTML/Markdown renderers;
- MirrorService publish/subscribe/export;
- gRPC service responses;
- MCP tool/resource/prompt responses;
- HTTP/SSE gateway responses.

### Failure Policy

- If redaction initialization fails for a surface that may contain secrets, the
  command must fail closed unless the user explicitly passes an unsafe debug
  override.
- If report writing fails after redaction has already protected output, keep the
  redacted output and return an error for the missing report.
- If a known secret cannot be registered, block any operation that would
  materialize that secret.

## Verifier Rules

Verifier rules should explain operational risk, not just static lint.

### Rule Categories

| Category | Examples |
| --- | --- |
| `secret_flow` | secret in ConfigMap, secret in env var, secret in log sink |
| `workload_security` | privileged container, hostPath, hostNetwork, hostPID |
| `capabilities` | missing drop ALL, NET_RAW retained, privilege escalation |
| `identity_rbac` | cluster-admin binding, wildcard verbs, default service account token |
| `network_exposure` | public LoadBalancer, ingress without TLS, risky annotations |
| `resilience` | missing probes, missing PDB, bad rollout strategy |
| `resources` | missing requests/limits, quota headroom risk |
| `storage` | PVC destructive change, unsafe reclaim assumptions |
| `supply_chain` | latest tag, unpinned image, unsigned image, missing build evidence |
| `drift` | live state diverges from desired state before apply |
| `rollout_risk` | high predicted failure, weak rollback confidence, SLO gate risk |
| `agent_boundary` | MCP write without confirmation, remote agent without TLS/mTLS |

### Rule Contract

Every rule must define:

```json
{
  "id": "k8s/service_public_loadbalancer",
  "category": "network_exposure",
  "defaultSeverity": "high",
  "description": "Service exposes a public LoadBalancer",
  "rationale": "Public exposure requires explicit ownership and controls",
  "inputs": ["rendered_manifest", "live_state", "namespace_policy"],
  "confidence": "contextual",
  "fix": {
    "strategy": "change service type or attach approved ingress policy",
    "patchable": true
  },
  "helpUrl": "https://..."
}
```

Rules should produce fix plans through the existing verifier fix-plan path when
possible.

### Contextual Severity

Severity is computed from a base plus modifiers.

Example: public LoadBalancer

| Signal | Modifier |
| --- | --- |
| namespace tagged production | +1 severity |
| ingress TLS present | -1 severity |
| no NetworkPolicy and no approved annotation | +1 severity |
| owner suppression exists and not expired | report suppressed, not blocked |
| service is internal-only cloud LB | -1 severity |
| internet-facing LB with secret-like env vars in backing pods | critical |

The report must show why severity changed.

## Suppressions

Suppressions are allowed, but must be governed.

Example:

```yaml
version: v1
suppressions:
  - rule: k8s/service_public_loadbalancer
    fingerprint: sha256:2f1c...
    owner: platform@example.com
    reason: Public API endpoint approved by architecture review AR-1234
    expires: 2026-07-01
    scope:
      namespace: prod
      resource: v1/Service/prod/api
```

Requirements:

- `owner`, `reason`, `expires`, `rule`, and `fingerprint` are required.
- Expired suppressions fail CI by default.
- Broad suppressions require an explicit `scope` and should warn by default.
- Suppressed findings still appear in reports under `suppressedFindings`.
- Suppression deltas are included in evidence bundles.
- Suppressions cannot hide redaction failures.

## Evidence Bundle

The evidence bundle is the product moat. It should be attachable to PRs, CI
runs, releases, audits, and incident retrospectives.

Recommended layout:

```text
torque-security-evidence/
  manifest.json
  inputs.json
  secrets.report.json
  boundary.matrix.json
  secret.flow.graph.json
  verifier.report.json
  findings.sarif
  redaction.proof.json
  suppressions.audit.json
  plan.summary.json
  capture.summary.json
  benchmark.summary.json
  reports/
    security.md
    security.html
```

`manifest.json` should include:

- Torque version and commit;
- ruleset version and hash;
- detector version and hash;
- config hash;
- input file digests;
- rendered manifest digest;
- capture IDs and session IDs;
- whether redaction was enabled for each surface;
- whether any raw-value sink was blocked.

`redaction.proof.json` example:

```json
{
  "surfaces": [
    {
      "surface": "mcp.tool_response",
      "sessionId": "2026-05-10T12-00-00Z-api",
      "matches": [
        { "ruleId": "secret/aws_access_key", "count": 2 },
        { "ruleId": "known_secret/secret_ref", "count": 1 }
      ],
      "rawSecretStored": false
    }
  ],
  "failedClosed": false
}
```

## CLI Product Shape

Initial commands:

```bash
# Repo/source scan with redacted flow graph.
torque secrets scan --scope repo --report secrets.json --flow-graph

# Render-aware scan.
torque secrets scan --scope render --chart ./chart --release api -n prod \
  --report secrets.json --flow-graph

# Build/capture scan.
torque secrets scan --scope build --capture build.sqlite --report build-secrets.json

# Offline artifact scan.
torque secrets scan --scope artifact ./apply.sqlite --report artifact-secrets.json

# Unified gate.
torque apply plan --chart ./chart --release api -n prod \
  --require-clean-secrets secrets.json \
  --require-verified verify.json \
  --security-evidence ./torque-security-evidence
```

Verifier additions:

```bash
verifier --chart ./chart --release api -n prod \
  --security-profile enterprise \
  --security-boundary-matrix \
  --secret-flow-graph \
  --secrets-report secrets.json \
  --report verify.json

verifier rules list --category secret_flow
verifier suppressions audit --config .torque/security.yaml
```

Redaction validation:

```bash
torque redact --preset incident --proof redaction.proof.json < raw.log > redacted.log
torque security benchmark --corpus ./testdata/security --report benchmark.json
```

Names can change during implementation, but the product shape should preserve:

- separate scan reports;
- unified security evidence;
- apply/ship gates that require clean secret and verifier reports;
- offline re-scan for captures and exported artifacts.

## MCP And gRPC Product Shape

MCP tools:

```text
torque.secrets.scan
torque.secrets.trace
torque.secrets.redaction_proof
torque.verify.run
torque.verify.suppressions.audit
torque.security.evidence.export
```

MCP resources:

```text
torque://security/reports/{id}
torque://security/flows/{id}
torque://security/evidence/{id}
torque://security/redaction-proof/{id}
```

gRPC additions can either extend existing services or add a dedicated
`SecurityService`:

```text
SecurityService.Scan
SecurityService.TraceSecretFlow
SecurityService.ExportEvidence
SecurityService.AuditSuppressions
```

MirrorService frames should include redaction metadata:

```json
{
  "redacted": true,
  "redactionRules": ["secret/aws_access_key", "known_secret/secret_ref"],
  "redactionProofId": "redaction-proof:..."
}
```

## Configuration

`.torque/security.yaml`:

```yaml
version: v1

profiles:
  default:
    secrets:
      mode: block
      failOn: high
      scan:
        source: true
        render: true
        build: true
        runtime: true
        captures: true
      validators:
        network: false
      confidence:
        blockAt: 0.85
        warnAt: 0.55

    verifier:
      mode: block
      failOn: high
      contextualSeverity: true
      requireFixPlanForCritical: true

    redaction:
      failClosed: true
      surfaces:
        cli: true
        capture: true
        reports: true
        mcp: true
        grpc: true
        mirror: true

    suppressions:
      files:
        - .torque/suppressions.yaml
      expired: fail
      requireOwner: true
      requireReason: true
      maxTTL: 180d
```

## Benchmark And Evaluation

Claims must be backed by a repeatable corpus under `testdata/security/`.

Corpus groups:

- `source-realistic`: synthetic credentials embedded in real-looking repos;
- `source-false-positive`: UUIDs, hashes, checksums, docs examples, test keys;
- `helm-render`: values and templates that render secrets into safe and unsafe
  fields;
- `kubernetes`: manifests with workload, RBAC, network, storage, and resilience
  risks;
- `build-flow`: Dockerfile/Compose cases for ARG, ENV, BuildKit secrets, logs,
  labels, history, and OCI layers;
- `runtime-output`: logs, proof bundles, Markdown, HTML, MCP, gRPC, and JSONL;
- `suppressions`: valid, expired, broad, and malicious suppression examples;
- `scale`: large chart/repo/capture inputs for performance budgets.

Required metrics:

```text
secret recall by detector family
secret precision by file type
false positive rate by file type
runtime cost
redaction escape count
secret flow graph nodes/edges
live k3s boundary matrix pass/fail
verifier rule recall by risk category
finding fingerprint stability
suppression audit correctness
scan wall time and memory
report size
```

Release gates for security claims:

- redaction escape count must be zero;
- no raw synthetic secret may appear in any generated report, capture, bundle,
  HTML, Markdown, MCP response, gRPC frame, or JSONL export;
- high-confidence secret recall should meet the configured threshold before a
  detector family is enabled in blocking mode;
- false positive budgets must be documented per detector family;
- every built-in critical/high verifier rule must have at least one fail fixture,
  pass fixture, edge fixture, and report golden.

## Implementation Plan

### Phase 0: Test Harness And Schemas

Deliverables:

- canonical finding envelope for secret and verifier reports;
- security corpus layout under `testdata/security/`;
- no-raw-secret assertion helper usable by CLI, verifier, MCP, and gRPC tests;
- redaction proof schema.

Acceptance:

- synthetic secrets in the corpus are detected in positive fixtures;
- synthetic secrets are absent from all generated reports;
- schema examples round-trip through JSON tests.

### Phase 1: Shared Redaction Boundary

Deliverables:

- `internal/redaction`;
- migrate existing incident redaction presets into the shared package;
- integrate redaction into CLI output, report writers, capture export, MCP, gRPC,
  and MirrorService;
- fail-closed behavior for configured sensitive surfaces.

Acceptance:

- redaction escape count is zero across runtime-output corpus;
- existing `torque redact` behavior remains compatible;
- MirrorService export and SSE tail never emit raw synthetic secrets.

### Phase 2: Source And Render Secret Scan

Deliverables:

- `torque secrets scan --scope repo`;
- render scan through `torque apply plan` and `verifier --chart`;
- Kubernetes Secret base64 decoding and non-secret sink detection;
- confidence scoring and JSON/SARIF reports.

Acceptance:

- source and render fixture recall meets initial threshold;
- `secret://` references stay reference-only in reports;
- findings include source locations and rendered resource paths where available.

### Phase 3: Build And Capture Secret Flow

Deliverables:

- Dockerfile/Compose build-flow detectors;
- BuildKit progress redaction and scan hooks;
- image config/history/layer scan when OCI layout is available;
- capture scan and security summary metadata.

Acceptance:

- ARG/ENV/log/label/history/layer leaks are detected;
- BuildKit secret mount cases pass when values do not leak;
- capture exports are re-redacted and include redaction proof.

### Phase 4: Secret Flow Graph

Deliverables:

- `internal/secretflow`;
- HMAC-backed per-run value identity;
- flow graph export in evidence bundles;
- `torque.secrets.trace` MCP tool.

Acceptance:

- a value flowing from source to rendered env var produces one connected trace;
- a `secret://` value that stays within approved boundaries produces a clean
  proof;
- reports can distinguish `reference_only`, `materialized_allowed`,
  `redacted_output`, and `leaked_output`.

### Phase 5: Contextual Verifier Rules

Deliverables:

- contextual severity engine;
- rule metadata contract and fix-plan coverage;
- secret-flow rules in verifier;
- network exposure, quota, drift, rollout risk, rollback confidence, storage,
  and agent-boundary rule packs.

Acceptance:

- contextual severity changes are explained in report evidence;
- suppressions apply by fingerprint and expiry;
- `torque apply plan --require-verified` can block on combined verifier and
  secret-flow reports.

### Phase 6: Enterprise Evidence And Integrations

Deliverables:

- evidence bundle export;
- GitHub/GitLab comment summaries with redaction proof;
- Backstage/Workbench-ready JSON resources;
- central evidence-store adapter for S3/GCS/local paths;
- benchmark report published with release artifacts.

Acceptance:

- one bundle can explain all blocking findings for a release;
- generated Markdown/HTML contains no raw synthetic secrets;
- benchmark summary is attached to CI/release runs.

## Open Questions

- Should provider validation live in Torque core, optional plugins, or an
  enterprise-only rule pack?
- Should secret scanning use a separate binary for air-gapped CI environments,
  or remain inside `torque` and `verifier`?
- What is the default TTL for suppressions in open-source mode?
- Should `torque apply` block on missing security evidence by default in
  production-tagged namespaces?
- Which evidence store should be first-class first: local directory, S3, or the
  existing MirrorService store?

# Security Benchmark Corpus Spec

Status: draft

Last reviewed: 2026-05-10

This spec defines the repeatable benchmark corpus under `testdata/security/`.
Its purpose is to make Torque secret detection claims measurable, reviewable,
and safe to publish.

## Goals

The corpus must let Torque prove detector quality with evidence instead of
marketing. It must support these claims:

- recall by detector family;
- precision by file type and surface;
- false positive rate by fixture group;
- redaction escape count;
- runtime and report-size cost;
- source-to-live provenance coverage;
- live k3s boundary matrix pass/fail;
- block-mode eligibility per detector family.

The corpus is not a dump of leaked secrets. It is a deterministic, synthetic,
provider-shaped benchmark suite.

## Non-Goals

- Do not store real credentials, leaked tokens, customer data, production logs,
  or copied incident artifacts.
- Do not validate synthetic secrets against provider APIs by default.
- Do not add one-off fixtures without machine-readable expected outcomes.
- Do not tune detectors against a hidden private corpus and then claim public
  quality from the public corpus.
- Do not compare to other scanners unless every scanner runs against the exact
  same fixture set and output adapter rules.

## Data Sourcing Rules

Allowed sources:

- synthetic strings generated from public token shape knowledge such as prefix,
  length, and character class;
- fake examples already present in Torque unit tests, after centralizing them
  under `testdata/security/`;
- provider-shaped examples that are clearly non-functional and never submitted
  to a provider;
- benign false-positive fixtures created from checksums, UUIDs, example docs,
  placeholders, public test keys, Kubernetes names, and generated hashes;
- locally generated logs, Markdown, JSON, YAML, Dockerfile, Compose, Helm, and
  Kubernetes manifests;
- generated large fixtures for performance and report-size testing.

Forbidden sources:

- real leaked secrets from public repositories, bug bounty reports, breach
  writeups, Stack Overflow, GitHub issues, logs, customer incidents, or scanner
  demos;
- copied vendor test corpora unless their license explicitly permits reuse and
  every value is verified synthetic;
- values obtained by creating real provider tokens, even if immediately
  revoked;
- values that could authenticate to local lab systems, cloud accounts,
  registries, CI, k3s clusters, or GitHub;
- fixture data containing kubeconfigs, private keys, JWTs, cookies, session
  IDs, or bearer tokens produced by a real system.

Every corpus value must be safe to publish in GitHub and safe to display in a
failing test log. Reports must still redact it, because the product promise is
zero raw secret storage.

## Directory Layout

Canonical layout:

```text
testdata/security/
  README.md
  corpus.yaml
  families/
    aws/
      true/
      false/
    github/
      true/
      false/
    slack/
      true/
      false/
    openai/
      true/
      false/
    stripe/
      true/
      false/
    database-url/
      true/
      false/
    private-key/
      true/
      false/
  surfaces/
    env/
    yaml/
    json/
    markdown/
    dockerfile/
    compose/
    helm/
    kubernetes/
    logs/
    reports/
  boundary/
    allowed/
    blocked/
    live-k3s/
  scale/
    small/
    medium/
    large/
```

`corpus.yaml` is the source of truth. Directories provide readable fixture
organization only; benchmark logic must load expectations from `corpus.yaml`.

## Corpus Manifest Schema

`corpus.yaml`:

```yaml
version: v1
generatedBy: manual
policy:
  rawValuesAreSynthetic: true
  providerValidation: disabled
  noRawSecretInReports: true

cases:
  - id: aws-env-positive
    family: aws_access_key
    scope: repo
    surface: env
    path: families/aws/true/env
    fileType: env
    sourceKind: synthetic-provider-shaped
    expectedFindings:
      - ruleId: secret/value_aws_access_key
        family: aws_access_key
        fileType: env
        minCount: 1
        confidenceAtLeast: 0.95
    expectedFlow:
      forbiddenBoundary: true
      redactedOutput: true
      provenanceKinds:
        - source
        - boundary
        - redaction
    rawSecrets:
      - AKIA1234567890ABCDEF
    blockModeEligible: true
```

Required case fields:

- `id`: stable unique identifier, kebab-cased;
- `scope`: `repo`, `render`, `build`, `artifact`, or `live`;
- `path` or `manifest`: fixture location relative to corpus root;
- `fileType`: primary format used for precision accounting;
- `expectedFindings`: explicit expected rule IDs for positive cases;
- `rawSecrets`: synthetic values that must never appear in generated reports.

Recommended case fields:

- `family`: detector family for recall accounting;
- `surface`: logical surface such as `env`, `yaml`, `helm`, `logs`, `kubernetes`;
- `sourceKind`: `synthetic-provider-shaped`, `placeholder`, `hash`,
  `public-test-key`, `generated-log`, `generated-scale`;
- `expectedFlow`: provenance and boundary expectations;
- `blockModeEligible`: whether the detector should be allowed to block when the
  case is representative and precision gates pass;
- `notes`: short reviewer context for why the case exists.

## Fixture Data Rules

Positive fixtures must:

- match the detector family shape exactly enough to exercise production logic;
- be placed in realistic context, not only bare strings;
- include at least one negative neighbor value in the same file when practical;
- be listed in `rawSecrets`;
- assert the exact expected `ruleId`;
- assert whether the flow should reach a forbidden or allowed boundary.

Negative fixtures must:

- represent a plausible false positive;
- include no `expectedFindings`;
- explain the false-positive class through `sourceKind` or `notes`;
- be counted toward precision and false-positive rate;
- be stable across platforms and line endings.

All fixtures must:

- be deterministic;
- be small enough for normal unit tests unless they live under `scale/`;
- avoid external network access;
- avoid binary blobs unless the case is explicitly testing binary-skip behavior;
- avoid generated timestamps unless the benchmark injects them.

## Detector Families

Minimum first-class families:

| Family | Positive examples | False-positive pressure |
| --- | --- | --- |
| `aws_access_key` | `AKIA`, `ASIA`, `A3T` shaped keys | example AWS docs keys, random uppercase IDs |
| `github_token` | `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_` shapes | placeholder tokens, docs snippets |
| `slack_token` | `xoxb-`, `xoxa-`, `xoxp-`, `xoxr-` shapes | hyphenated IDs, fake bot examples |
| `openai_key` | `sk-` and `sk-proj-` shaped keys | generic `sk-` examples and docs placeholders |
| `stripe_secret_key` | `sk_test_`, `sk_live_`, `rk_test_`, `rk_live_` shapes | Stripe publishable keys and examples |
| `database_url_password` | Postgres, MySQL, MongoDB, Redis URLs with credentials | URLs without passwords, docs URLs |
| `private_key` | PEM private key headers in source/log contexts | public keys, certificate headers |
| `jwt` | JWT-like three-part strings | docs examples, unsigned placeholders |
| `contextual_credential` | suspicious key plus literal value | placeholders, low-entropy examples |

Each family must have:

- at least one positive fixture in `env`, `yaml`, and `json` when applicable;
- at least one false-positive fixture;
- at least one rendered Kubernetes boundary case when applicable;
- a documented confidence threshold;
- a block-mode decision.

## Surface Matrix

The benchmark must eventually cover these surfaces:

| Surface | Required fixture types |
| --- | --- |
| `env` | `.env`, shell exports, quoted and unquoted values |
| `yaml` | Helm values, app configs, nested maps, lists |
| `json` | app settings, service account-like but fake data, escaped strings |
| `markdown` | docs examples, fenced code blocks, inline snippets |
| `dockerfile` | `ARG`, `ENV`, labels, comments |
| `compose` | service env, env files, labels |
| `helm` | values to templates to rendered manifests |
| `kubernetes` | Secret, ConfigMap, env.value, annotations, labels, args, probes |
| `logs` | plain logs, JSON logs, multiline stack traces |
| `reports` | JSON, Markdown, HTML, SARIF-like outputs |
| `mcp-grpc` | tool responses, mirror frames, streamed events |

## False-Positive Taxonomy

Every negative case should map to one of these classes:

- `placeholder`: `${TOKEN}`, `<api-key>`, `replace-me`, `example`;
- `hash`: SHA1/SHA256/MD5-like strings;
- `uuid`: UUIDs and ULIDs;
- `checksum`: package checksums, image digests, SRI hashes;
- `public-example`: public fake examples such as documentation keys;
- `public-key-material`: PEM public keys and certificates;
- `low-entropy-secret-key`: suspicious key name with non-secret value;
- `test-fixture`: obvious fake unit-test credentials;
- `identifier`: Kubernetes names, resource IDs, request IDs;
- `encoded-benign`: base64 text that decodes to non-secret data.

Precision reports must break down failures by taxonomy when metadata is
available.

## Provenance Expectations

Secret flow graph expectations are part of the corpus, not incidental output.

Repo/source cases should prove:

```text
source -> boundary -> redaction
```

Helm cases should prove:

```text
values -> helm_template -> rendered_object -> boundary -> redaction
```

Live k3s cases should prove:

```text
live_object -> boundary -> redaction
```

Apply/runtime cases should eventually prove:

```text
values -> helm_template -> rendered_object -> live_object -> runtime_output -> redaction
```

The graph must never store raw secret values. Node IDs, edge IDs, digests, and
fingerprints must be derived from metadata or redacted fingerprints only.

## Redaction Requirements

For every case, the benchmark must scan generated artifacts for `rawSecrets`
across:

- benchmark report JSON;
- secrets report JSON;
- secret flow graph JSON;
- redaction proof JSON;
- evidence bundle Markdown;
- generated HTML or docs surfaces when included;
- future MCP, gRPC, mirror, and JSONL exports.

`redactionEscapeCount` must be zero. A single raw synthetic value in an artifact
is a benchmark failure, even if the detector otherwise works.

## Metrics

The benchmark report must expose:

- `summary.recall`;
- `summary.precision`;
- `summary.falsePositiveRate`;
- `summary.runtimeMillis`;
- `summary.redactionEscapeCount`;
- `summary.flowGraphNodes`;
- `summary.flowGraphEdges`;
- `summary.provenanceChains`;
- `summary.liveObjects`;
- `recallBySecretFamily`;
- `precisionByFileType`;
- `falsePositiveRateByTaxonomy` when metadata exists;
- per-case `truePositives`, `falsePositives`, `falseNegatives`;
- per-case `findingRules`;
- live k3s boundary status when requested.

## Block-Mode Eligibility

A detector family can be enabled for blocking only when all are true:

- family recall meets the configured threshold;
- precision meets the configured threshold on every required file type;
- false-positive rate is inside the configured budget;
- redaction escape count is zero;
- runtime increase is inside the configured budget;
- at least one forbidden-boundary rendered or live case is covered when the
  family can appear in Kubernetes workflows;
- the detector has documented false-positive classes and suppressions guidance.

Example policy:

```yaml
blockMode:
  default:
    minRecall: 0.98
    minPrecision: 0.95
    maxFalsePositiveRate: 0.02
    maxRuntimeIncreasePercent: 10
  families:
    aws_access_key:
      minRecall: 1.0
      minPrecision: 0.98
```

## Generator

The corpus should include a deterministic generator once the fixture count grows:

```bash
go run ./cmd/torque security corpus generate \
  --spec ./testdata/security/corpus.yaml \
  --out ./testdata/security
```

Generator rules:

- use a fixed seed checked into `corpus.yaml`;
- generate only synthetic provider-shaped values;
- mark generated values in comments where the file format allows comments;
- emit a manifest update with `rawSecrets`;
- refuse to overwrite hand-authored cases unless `--update` is passed;
- never call provider APIs.

## Review Checklist

Every corpus PR must answer:

- Which detector family or false-positive class does this add?
- Is every raw value synthetic and listed in `rawSecrets`?
- Does the case have exact expected rule IDs?
- Does the benchmark pass with zero redaction escapes?
- Does the fixture improve recall, precision, boundary coverage, provenance, or
  runtime evidence?
- Is the detector block-mode decision unchanged or explicitly updated?

Required validation:

```bash
torque security benchmark --corpus ./testdata/security --report benchmark.json
make preflight
```

When live Kubernetes boundaries are affected:

```bash
torque security benchmark --corpus ./testdata/security \
  --report benchmark-live.json \
  --live-k3s-boundary-matrix --live-confirm
```

## Rollout Phases

Phase 1: Family coverage

- Add true/false fixtures for AWS, GitHub, Slack, OpenAI, Stripe, database URLs,
  and private keys.
- Add `family`, `surface`, `sourceKind`, and `blockModeEligible` metadata.
- Keep all cases small enough for default `go test`.

Phase 2: Surface coverage

- Add Dockerfile, Compose, logs, Markdown, JSON, and Helm template cases.
- Add false-positive taxonomy metrics.
- Add per-file-type precision gates.

Phase 3: Runtime and report surfaces

- Add runtime-output fixtures for logs, evidence bundles, HTML, Markdown, MCP,
  gRPC, mirror frames, and JSONL.
- Assert zero raw escapes across all generated artifacts.

Phase 4: Scale and regression gates

- Add medium and large generated corpora.
- Add runtime budget and report-size budget gates.
- Add benchmark diff support for CI.

Phase 5: Competitor adapters

- Add optional adapters that run the same corpus through external scanners.
- Normalize findings into the Torque benchmark schema.
- Publish comparison only when commands, versions, and adapter assumptions are
  recorded in the report.


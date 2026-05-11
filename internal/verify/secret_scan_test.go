package verify

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const syntheticAWSAccessKey = "AKIA1234567890ABCDEF"

func TestScanRenderedSecretsFindsNonSecretSinkAndRedacts(t *testing.T) {
	secretData := base64.StdEncoding.EncodeToString([]byte(syntheticAWSAccessKey))
	objects, err := DecodeK8SYAML(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: prod
data:
  awsAccessKey: AKIA1234567890ABCDEF
---
apiVersion: v1
kind: Secret
metadata:
  name: app-secret
  namespace: prod
data:
  awsAccessKey: ` + secretData + `
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	report, err := ScanRenderedSecrets(objects, SecretScanOptions{
		Mode:        ModeBlock,
		FailOn:      SeverityHigh,
		Source:      "fixture",
		EvaluatedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !report.Blocked || report.Passed {
		t.Fatalf("expected blocked report: %#v", report)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings=%d, want 1: %#v", len(report.Findings), report.Findings)
	}
	finding := report.Findings[0]
	if finding.RuleID != "secret/value_aws_access_key" {
		t.Fatalf("rule=%q", finding.RuleID)
	}
	if finding.Observed == syntheticAWSAccessKey || strings.Contains(finding.Observed, syntheticAWSAccessKey) {
		t.Fatalf("observed leaked raw secret: %q", finding.Observed)
	}
	if finding.Confidence < 0.9 {
		t.Fatalf("confidence=%v", finding.Confidence)
	}
	if finding.Fix == nil || finding.Fix.Summary == "" {
		t.Fatalf("missing fix: %#v", finding)
	}
	var out bytes.Buffer
	if err := WriteSecretScanReport(&out, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if strings.Contains(out.String(), syntheticAWSAccessKey) {
		t.Fatalf("report leaked raw secret:\n%s", out.String())
	}
}

func TestScanRenderedSecretsTracksSecretRefsWithoutFinding(t *testing.T) {
	objects, err := DecodeK8SYAML(`
apiVersion: v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: API_TOKEN
              value: secret://vault/prod/api#token
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	report, err := ScanRenderedSecrets(objects, SecretScanOptions{Mode: ModeBlock, FailOn: SeverityHigh})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("unexpected findings: %#v", report.Findings)
	}
	if report.Summary.SecretReferences != 1 {
		t.Fatalf("secret refs=%d, want 1", report.Summary.SecretReferences)
	}
}

func TestScanRenderedSecretsBuildsBoundaryMatrix(t *testing.T) {
	secretData := base64.StdEncoding.EncodeToString([]byte(syntheticAWSAccessKey))
	objects, err := DecodeK8SYAML(`
apiVersion: v1
kind: Secret
metadata:
  name: app-secret
  namespace: prod
data:
  awsAccessKey: ` + secretData + `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: prod
data:
  awsAccessKey: AKIA1234567890ABCDEF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
  annotations:
    torque.dev/leak: AKIA1234567890ABCDEF
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: API_TOKEN
              value: AKIA1234567890ABCDEF
            - name: SAFE_TOKEN
              valueFrom:
                secretKeyRef:
                  name: app-secret
                  key: awsAccessKey
          volumeMounts:
            - name: app-secret
              mountPath: /var/run/app-secret
      volumes:
        - name: app-secret
          secret:
            secretName: app-secret
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	report, err := ScanRenderedSecrets(objects, SecretScanOptions{
		Mode:           ModeBlock,
		FailOn:         SeverityHigh,
		Profile:        "enterprise",
		Source:         "fixture",
		BoundaryMatrix: true,
		EvaluatedAt:    time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if report.BoundaryMatrix == nil {
		t.Fatalf("missing boundary matrix")
	}
	assertBoundaryRow(t, report.BoundaryMatrix, "Secret.data", "allowed", "allowed", 0)
	assertBoundaryRow(t, report.BoundaryMatrix, "ConfigMap.data", "blocked", "blocked", 1)
	assertBoundaryRow(t, report.BoundaryMatrix, "metadata.annotations", "blocked", "blocked", 1)
	assertBoundaryRow(t, report.BoundaryMatrix, "env.value", "blocked", "blocked", 1)
	assertBoundaryRow(t, report.BoundaryMatrix, "secretKeyRef", "allowed", "allowed", 0)
	assertBoundaryRow(t, report.BoundaryMatrix, "secret volume", "allowed", "allowed", 0)
	if !report.BoundaryMatrix.Passed {
		t.Fatalf("boundary matrix should pass allowed/blocked expectations: %#v", report.BoundaryMatrix)
	}
}

func TestScanRenderedSecretsBuildsSecretFlowGraph(t *testing.T) {
	objects, err := DecodeK8SYAML(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: prod
data:
  awsAccessKey: AKIA1234567890ABCDEF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  selector:
    matchLabels:
      app: api
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: SAFE_TOKEN
              value: secret://vault/prod/api#token
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	report, err := ScanRenderedSecrets(objects, SecretScanOptions{
		Mode:        ModeBlock,
		FailOn:      SeverityHigh,
		Source:      "fixture",
		FlowGraph:   true,
		EvaluatedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if report.FlowGraph == nil {
		t.Fatalf("missing flow graph")
	}
	if report.FlowGraph.Summary.ForbiddenFlows != 1 {
		t.Fatalf("forbidden flows=%d, want 1", report.FlowGraph.Summary.ForbiddenFlows)
	}
	if report.FlowGraph.Summary.SecretReferences != 1 {
		t.Fatalf("secret refs=%d, want 1", report.FlowGraph.Summary.SecretReferences)
	}
	if report.FlowGraph.Summary.Nodes == 0 || report.FlowGraph.Summary.Edges == 0 {
		t.Fatalf("expected graph nodes and edges: %#v", report.FlowGraph)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(raw)
	for _, forbidden := range []string{syntheticAWSAccessKey, "vault/prod/api"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("flow graph leaked %q: %s", forbidden, text)
		}
	}
}

func TestScanRenderedSecretsBuildsSourceToRenderedProvenance(t *testing.T) {
	manifest := `
# Source: app/templates/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: prod
data:
  awsAccessKey: AKIA1234567890ABCDEF
`
	objects, err := DecodeK8SYAMLWithHelmSources(manifest)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	report, err := ScanRenderedSecrets(objects, SecretScanOptions{
		Mode:           ModeWarn,
		FailOn:         SeverityHigh,
		Source:         "chart ./app",
		TargetKind:     "chart",
		ValuesFiles:    []string{"values.yaml"},
		RenderedPath:   "rendered.yaml",
		RenderedSource: manifest,
		FlowGraph:      true,
		EvaluatedAt:    time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	graph := report.FlowGraph
	if graph == nil {
		t.Fatalf("missing flow graph")
	}
	if graph.Summary.ValuesSources != 1 {
		t.Fatalf("values sources=%d, want 1: %#v", graph.Summary.ValuesSources, graph)
	}
	if graph.Summary.TemplateSources != 1 {
		t.Fatalf("template sources=%d, want 1: %#v", graph.Summary.TemplateSources, graph)
	}
	if graph.Summary.RenderedObjects != 1 {
		t.Fatalf("rendered objects=%d, want 1: %#v", graph.Summary.RenderedObjects, graph)
	}
	assertSecretFlowEdgeKind(t, graph, "values_to_template")
	assertSecretFlowEdgeKind(t, graph, "template_to_rendered")
	assertSecretFlowNodeKind(t, graph, "rendered_object")
}

func TestScanRenderedSecretsBuildsLiveObjectProvenance(t *testing.T) {
	objects, err := DecodeK8SYAML(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: live-config
  namespace: prod
data:
  awsAccessKey: AKIA1234567890ABCDEF
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	report, err := ScanRenderedSecrets(objects, SecretScanOptions{
		Mode:        ModeWarn,
		FailOn:      SeverityHigh,
		Source:      "namespace prod",
		TargetKind:  "namespace",
		FlowGraph:   true,
		EvaluatedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	graph := report.FlowGraph
	if graph == nil {
		t.Fatalf("missing flow graph")
	}
	if graph.Summary.LiveObjects != 1 {
		t.Fatalf("live objects=%d, want 1: %#v", graph.Summary.LiveObjects, graph)
	}
	if graph.Summary.RenderedObjects != 0 {
		t.Fatalf("rendered objects=%d, want 0 for live scan: %#v", graph.Summary.RenderedObjects, graph)
	}
	assertSecretFlowNodeKind(t, graph, "live_object")
}

func TestScanTextSecretsRedactsReport(t *testing.T) {
	report, err := ScanTextSecrets([]SecretTextInput{{
		Path:    "values/prod.yaml",
		Content: "githubToken: ghp_1234567890abcdefghijklmnopqr\n",
		Stage:   "source",
	}}, SecretScanOptions{Mode: ModeWarn, FailOn: SeverityHigh})
	if err != nil {
		t.Fatalf("scan text: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings=%d", len(report.Findings))
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "ghp_1234567890abcdefghijklmnopqr") {
		t.Fatalf("text report leaked raw token: %s", raw)
	}
}

func assertSecretFlowNodeKind(t *testing.T, graph *SecretFlowGraph, kind string) {
	t.Helper()
	for _, node := range graph.Nodes {
		if node.Kind == kind {
			return
		}
	}
	t.Fatalf("missing flow node kind %s in %#v", kind, graph.Nodes)
}

func assertSecretFlowEdgeKind(t *testing.T, graph *SecretFlowGraph, kind string) {
	t.Helper()
	for _, edge := range graph.Edges {
		if edge.Kind == kind {
			return
		}
	}
	t.Fatalf("missing flow edge kind %s in %#v", kind, graph.Edges)
}

func assertBoundaryRow(t *testing.T, matrix *SecurityBoundaryMatrix, surface string, boundary string, status string, minFindings int) {
	t.Helper()
	for _, row := range matrix.Rows {
		if row.Surface != surface {
			continue
		}
		if !row.Present {
			t.Fatalf("%s present=false, row=%#v", surface, row)
		}
		if row.Boundary != boundary {
			t.Fatalf("%s boundary=%q, want %q", surface, row.Boundary, boundary)
		}
		if row.Status != status {
			t.Fatalf("%s status=%q, want %q row=%#v", surface, row.Status, status, row)
		}
		if row.FindingCount < minFindings {
			t.Fatalf("%s findings=%d, want >=%d row=%#v", surface, row.FindingCount, minFindings, row)
		}
		return
	}
	t.Fatalf("missing boundary row %s in %#v", surface, matrix.Rows)
}

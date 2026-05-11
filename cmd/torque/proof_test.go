package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
	"github.com/ingresslabs/torque/internal/stack"
)

func TestProofGraphSignsAndVerifiesApplyProof(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "apply.sqlite")
	verifyPath := filepath.Join(dir, "verify.json")
	buildPath := filepath.Join(dir, "build.sqlite")
	sloPath := filepath.Join(dir, "slo.yaml")
	repairDir := filepath.Join(dir, "fixes")
	repairPR := filepath.Join(repairDir, "pr.md")
	for path, body := range map[string]string{
		capturePath: "capture",
		verifyPath:  `{"passed":true}`,
		buildPath:   "build capture",
		sloPath:     "spec:\n  minReadyPercent: 90\n",
		repairPR:    "# Repair\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	slo, err := loadApplyRollbackSLO(sloPath)
	if err != nil {
		t.Fatalf("load slo: %v", err)
	}
	rollback := buildApplyRollbackProof(applyRollbackProofInput{
		Release:       "api",
		Namespace:     "prod",
		Mode:          "helm-rollback",
		Outcome:       "rolled-back",
		TriggerSource: "slo",
		TriggerReason: "SLO failed",
		StartedAt:     time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		SLO:           slo,
		Resources:     []deploy.ResourceStatus{{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Failed"}},
	})
	plan := &deployPlanResult{
		ReleaseName:    "api",
		Namespace:      "prod",
		ChartRef:       "./chart",
		ChartVersion:   "0.1.0",
		RenderedSHA256: "sha256:rendered",
		Summary:        planSummary{Creates: 1, Updates: 1},
		Images: []planImageRef{{
			Resource:  "Deployment/prod/api",
			Container: "api",
			Image:     "ghcr.io/acme/api@sha256:abc",
			Digest:    "sha256:abc",
			Pinned:    true,
		}},
		VerifyReports: []planVerifyReport{{
			Path:    verifyPath,
			Passed:  true,
			Blocked: false,
		}},
		BuildProvenance: []planBuildProvenance{{
			Source:     buildPath,
			Digest:     "sha256:abc",
			Tags:       []string{"ghcr.io/acme/api:dev"},
			Referenced: true,
			Verdict:    "safe",
		}},
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt:        time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:          "api",
		Namespace:        "prod",
		Chart:            "./chart",
		Plan:             plan,
		CapturePath:      capturePath,
		RollbackProof:    &rollback,
		ResourceSnapshot: []deploy.ResourceStatus{{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Failed"}},
	})
	proofPath := filepath.Join(dir, "apply-proof.json")
	if err := writeJSONFileEnsured(proofPath, proof); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	keyPath := writeProofTestKey(t, dir)

	graph, err := buildProofGraph(proofPath, nil)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	if err := signProofGraph(&graph, keyPath); err != nil {
		t.Fatalf("sign graph: %v", err)
	}
	if !proofGraphHasType(graph, "image-digest") || !proofGraphHasType(graph, "verifier-report") || !proofGraphHasType(graph, "build-capture") || !proofGraphHasType(graph, "rollback-proof") || !proofGraphHasType(graph, "repair-pr") {
		t.Fatalf("graph missing expected artifacts: %#v", graph.Artifacts)
	}
	report := verifyProofGraph(graph, "", proofVerifyOptions{RequireSignature: true})
	if !report.Passed || !report.Signature.Verified {
		t.Fatalf("expected signed graph to verify: %#v", report)
	}

	if err := os.WriteFile(verifyPath, []byte(`{"passed":false}`), 0o644); err != nil {
		t.Fatalf("tamper verify report: %v", err)
	}
	report = verifyProofGraph(graph, "", proofVerifyOptions{RequireSignature: true})
	if report.Passed || len(report.Artifacts.Mismatched) == 0 {
		t.Fatalf("expected tampered artifact to fail verification: %#v", report)
	}
}

func TestProofCommandWritesGraphAndHTML(t *testing.T) {
	dir := t.TempDir()
	proofPath := filepath.Join(dir, "apply-proof.json")
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:   "api",
		Namespace: "prod",
		Chart:     "./chart",
		Plan: &deployPlanResult{
			ReleaseName:    "api",
			Namespace:      "prod",
			RenderedSHA256: "sha256:rendered",
			Images: []planImageRef{{
				Resource: "Deployment/prod/api",
				Image:    "ghcr.io/acme/api@sha256:abc",
				Digest:   "sha256:abc",
				Pinned:   true,
			}},
			GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		},
	})
	if err := writeJSONFileEnsured(proofPath, proof); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	keyPath := writeProofTestKey(t, dir)
	graphPath := filepath.Join(dir, "proof.graph.json")
	htmlPath := filepath.Join(dir, "proof.html")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"proof", "graph", proofPath, "--out", graphPath, "--html", htmlPath, "--key", keyPath})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("proof graph command: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(graphPath); err != nil {
		t.Fatalf("expected graph json: %v", err)
	}
	htmlRaw, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("expected graph html: %v", err)
	}
	if !strings.Contains(string(htmlRaw), "Torque Proof Graph") {
		t.Fatalf("unexpected html: %s", htmlRaw)
	}
	verify, err := verifyProofSource(graphPath, proofVerifyOptions{RequireSignature: true})
	if err != nil {
		t.Fatalf("verify graph source: %v", err)
	}
	if !verify.Passed {
		t.Fatalf("expected graph file to verify: %#v", verify)
	}
}

func TestProofDiffReportsEvidenceChanges(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	writeProofForDiff(t, oldPath, "sha256:old", false)
	writeProofForDiff(t, newPath, "sha256:new", true)

	report, err := diffProofSources(oldPath, newPath)
	if err != nil {
		t.Fatalf("diff proof sources: %v", err)
	}
	if !report.Changed || report.Summary.Added == 0 || report.Summary.Removed == 0 || report.Summary.Changed == 0 {
		raw, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("expected proof diff to show changes: %s", raw)
	}
}

func writeProofTestKey(t *testing.T, dir string) string {
	t.Helper()
	key, err := stack.GenerateEd25519Key()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPath := filepath.Join(dir, "ed25519.json")
	if err := writeJSONFileEnsured(keyPath, key); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return keyPath
}

func proofGraphHasType(graph proofGraph, typ string) bool {
	for _, artifact := range graph.Artifacts {
		if artifact.Type == typ {
			return true
		}
	}
	return false
}

func writeProofForDiff(t *testing.T, path, digest string, failed bool) {
	t.Helper()
	errValue := error(nil)
	if failed {
		errValue = os.ErrInvalid
	}
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:   "api",
		Namespace: "prod",
		Chart:     "./chart",
		Err:       errValue,
		Plan: &deployPlanResult{
			ReleaseName:    "api",
			Namespace:      "prod",
			RenderedSHA256: "sha256:rendered",
			Images: []planImageRef{{
				Resource: "Deployment/prod/api",
				Image:    "ghcr.io/acme/api@" + digest,
				Digest:   digest,
				Pinned:   true,
			}},
			GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		},
	})
	if err := writeJSONFileEnsured(path, proof); err != nil {
		t.Fatalf("write proof %s: %v", path, err)
	}
}

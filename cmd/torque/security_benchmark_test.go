package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityBenchmarkCorpusWritesEvidenceMetrics(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "benchmark.json")
	root := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"security", "benchmark",
		"--corpus", "../../testdata/security",
		"--report", reportPath,
		"--format", "json",
		"--evaluated-at", "2026-05-10T12:00:00Z",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("benchmark: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report securityBenchmarkReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, raw)
	}
	if report.Summary.Cases != 4 || report.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	if report.Summary.Recall != 1 || report.Summary.Precision != 1 {
		t.Fatalf("unexpected quality metrics: recall=%v precision=%v", report.Summary.Recall, report.Summary.Precision)
	}
	if report.Summary.FalsePositiveRate != 0 {
		t.Fatalf("false positive rate=%v, want 0", report.Summary.FalsePositiveRate)
	}
	if report.Summary.RedactionEscapeCount != 0 {
		t.Fatalf("redaction escapes=%d", report.Summary.RedactionEscapeCount)
	}
	if report.Summary.FlowGraphNodes == 0 || report.Summary.FlowGraphEdges == 0 {
		t.Fatalf("missing flow graph metrics: %#v", report.Summary)
	}
	if report.Summary.BoundaryMatrixPassCount != 1 {
		t.Fatalf("boundary matrix pass count=%d, want 1", report.Summary.BoundaryMatrixPassCount)
	}
	if report.LiveK3SBoundaryMatrix.Status != "skipped" {
		t.Fatalf("live k3s should be skipped by default: %#v", report.LiveK3SBoundaryMatrix)
	}
	for _, forbidden := range []string{"AKIA1234567890ABCDEF", "ghp_1234567890abcdefghijklmnopqr"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("benchmark report leaked %q:\n%s", forbidden, raw)
		}
	}
}

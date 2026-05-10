package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildRepairReportForMissingConfigMap(t *testing.T) {
	bundle := &applyProofBundle{
		Release:   "api",
		Namespace: "prod",
		Status:    "failed",
		Prediction: &applyPrediction{
			Risk: "High",
			MissingDependencies: []applyPredictionDependency{
				{Kind: "ConfigMap", Namespace: "prod", Name: "api-config"},
			},
		},
	}
	report := buildRepairReport(bundle, "proof.json", "/repo/chart", "fix/api-rollout")
	if report.RootCause != "missing ConfigMap api-config" {
		t.Fatalf("root cause=%q", report.RootCause)
	}
	if report.Confidence < 90 {
		t.Fatalf("confidence=%d", report.Confidence)
	}
	if len(report.Fixes) != 1 {
		t.Fatalf("fixes=%#v", report.Fixes)
	}
	fix := report.Fixes[0]
	if fix.Manual || fix.Action != "add-chart-template" {
		t.Fatalf("fix=%#v", fix)
	}
	if !strings.HasSuffix(fix.Path, "templates/torque-repair-configmap-api-config.yaml") {
		t.Fatalf("path=%q", fix.Path)
	}
}

func TestRepairApplyWritesTemplateAndPRBody(t *testing.T) {
	dir := t.TempDir()
	chart := filepath.Join(dir, "chart")
	if err := os.MkdirAll(filepath.Join(chart, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	proof := filepath.Join(dir, "proof.json")
	bundle := applyProofBundle{
		Release:   "api",
		Namespace: "prod",
		Status:    "failed",
		Prediction: &applyPrediction{
			Risk: "High",
			MissingDependencies: []applyPredictionDependency{
				{Kind: "ConfigMap", Namespace: "prod", Name: "api-config"},
			},
		},
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if err := os.WriteFile(proof, raw, 0o644); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	prBody := filepath.Join(dir, "repair.md")
	cmd := newRepairCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--from", proof, "--chart", chart, "--apply", "--yes", "--non-interactive", "--pr-body", prBody})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("repair execute: %v\nstderr=%s", err, errOut.String())
	}
	templatePath := filepath.Join(chart, "templates", "torque-repair-configmap-api-config.yaml")
	got, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read repair template: %v", err)
	}
	if !strings.Contains(string(got), "kind: ConfigMap") || !strings.Contains(string(got), "name: api-config") {
		t.Fatalf("unexpected template:\n%s", got)
	}
	if strings.Contains(string(got), "namespace:") {
		t.Fatalf("repair template should remain Helm-portable without hardcoded namespace:\n%s", got)
	}
	body, err := os.ReadFile(prBody)
	if err != nil {
		t.Fatalf("read PR body: %v", err)
	}
	if !strings.Contains(string(body), "Torque Repair") || !strings.Contains(string(body), "missing ConfigMap api-config") {
		t.Fatalf("unexpected PR body:\n%s", body)
	}
	if !strings.Contains(out.String(), "Root cause: missing ConfigMap api-config") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestRepairApplyDoesNotOverwriteTemplate(t *testing.T) {
	dir := t.TempDir()
	chart := filepath.Join(dir, "chart")
	templatePath := filepath.Join(chart, "templates", "torque-repair-configmap-api-config.yaml")
	if err := os.MkdirAll(filepath.Dir(templatePath), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	if err := os.WriteFile(templatePath, []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("write existing template: %v", err)
	}
	proof := filepath.Join(dir, "proof.json")
	bundle := applyProofBundle{
		Release:   "api",
		Namespace: "prod",
		Status:    "failed",
		Prediction: &applyPrediction{
			Risk: "High",
			MissingDependencies: []applyPredictionDependency{
				{Kind: "ConfigMap", Namespace: "prod", Name: "api-config"},
			},
		},
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if err := os.WriteFile(proof, raw, 0o644); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	cmd := newRepairCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--from", proof, "--chart", chart, "--apply", "--yes", "--non-interactive"})
	err = cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "repair template already exists") {
		t.Fatalf("expected overwrite refusal, got err=%v stderr=%s", err, errOut.String())
	}
	got, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read existing template: %v", err)
	}
	if string(got) != "existing\n" {
		t.Fatalf("existing template was overwritten:\n%s", got)
	}
}

func TestRootHasRepairCommand(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("TORQUE_CONFIG", cfgPath)
	root := newRootCommand()
	var repairCmd *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "repair" {
			repairCmd = cmd
			break
		}
	}
	if repairCmd == nil {
		t.Fatalf("expected root to include repair command")
	}
	if f := repairCmd.Flags().Lookup("from"); f == nil {
		t.Fatalf("expected repair to have --from flag")
	}
}

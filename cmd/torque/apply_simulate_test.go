package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
)

func TestWriteApplySimulationBundleRedactsAndReplays(t *testing.T) {
	dir := t.TempDir()
	rawSecret := "super-secret-value"
	plan := &deployPlanResult{
		ReleaseName:    "api",
		Namespace:      "prod",
		ChartRef:       "./chart",
		ChartVersion:   "0.1.0",
		RenderedSHA256: "sha256:test",
		ManifestBlobs: map[string]string{
			"prod|secret|api-db": `apiVersion: v1
kind: Secret
metadata:
  name: api-db
  namespace: prod
stringData:
  password: super-secret-value
`,
		},
		Summary: planSummary{Creates: 1},
	}
	prediction := &applyPrediction{
		Version:     1,
		Release:     "api",
		Namespace:   "prod",
		Chart:       "./chart",
		Risk:        "High",
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Summary:     applyPredictionSummary{Creates: 1, MissingDependencies: 1},
		MissingDependencies: []applyPredictionDependency{{
			Kind:      "ConfigMap",
			Namespace: "prod",
			Name:      "api-config",
		}},
	}
	serverReport := &deploy.ServerDryRunReport{
		Version:      "v1",
		FieldManager: "torque-simulate",
		Summary: deploy.ServerDryRunSummary{
			Total:           1,
			Failed:          1,
			AdmissionDenied: 1,
		},
		Results: []deploy.ServerDryRunResult{{
			Resource:   deploy.ServerDryRunResource{Version: "v1", Kind: "Secret", Namespace: "prod", Name: "api-db"},
			Operation:  "server-side-apply",
			Status:     deploy.ServerDryRunStatusFailed,
			ErrorClass: deploy.ServerDryRunClassAdmissionDenied,
			Reason:     "Forbidden",
			Message:    "admission webhook denied the request",
			DryRun:     true,
		}},
	}
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt:  time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:    "api",
		Namespace:  "prod",
		Chart:      "./chart",
		Err:        errors.New("simulation blocked before apply"),
		Prediction: prediction,
		Plan:       plan,
		DryRun:     true,
	})
	result := &applySimulationResult{
		Plan:         plan,
		Prediction:   prediction,
		ServerDryRun: serverReport,
		Admission:    filterServerDryRunResults(serverReport, deploy.ServerDryRunClassAdmissionDenied),
		Quota:        simulationQuotaRiskReport{Version: "v1", Passed: true},
		Proof:        proof,
		OutDir:       dir,
		Blocked:      true,
	}
	result.Manifest = buildApplySimulationManifest(result, applySimulationOptions{Chart: "./chart", Release: "api", Namespace: "prod"})
	if err := writeApplySimulationBundle(context.Background(), result, applySimulationOptions{}); err != nil {
		t.Fatalf("write simulation bundle: %v", err)
	}
	for _, name := range []string{
		"manifest.json",
		"predicted-live-state.json",
		"server-dry-run.json",
		"admission.results.json",
		"field-ownership.conflicts.json",
		"quota.capacity.risk.json",
		"rollout.prediction.json",
		"apply.proof.json",
		filepath.Join("fixes", "fix.patch"),
		filepath.Join("fixes", "pr.md"),
		"verifier.report.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	assertDirDoesNotContain(t, dir, rawSecret)
	replay, err := replayProofBundle(dir, replayOptions{Lab: "k3s"})
	if err != nil {
		t.Fatalf("replay proof: %v", err)
	}
	if !replay.Passed || !replay.Blocked || replay.Kind != "apply-simulation" {
		t.Fatalf("unexpected replay report: %#v", replay)
	}
	patch, err := os.ReadFile(filepath.Join(dir, "fixes", "fix.patch"))
	if err != nil {
		t.Fatalf("read fix patch: %v", err)
	}
	if !strings.Contains(string(patch), "torque-repair-configmap-api-config.yaml") {
		t.Fatalf("expected generated ConfigMap patch, got:\n%s", patch)
	}
}

func TestReplayApplyProofFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply.proof.json")
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Release:   "api",
		Namespace: "prod",
		Chart:     "./chart",
	})
	raw, err := jsonMarshalIndent(proof)
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	replay, err := replayProofBundle(path, replayOptions{Lab: "k3s"})
	if err != nil {
		t.Fatalf("replay proof file: %v", err)
	}
	if replay.Kind != "apply-proof" || !replay.Passed || replay.Blocked {
		t.Fatalf("unexpected replay report: %#v", replay)
	}
}

func TestRootExposesApplySimulateReplayAndFixAlias(t *testing.T) {
	root := newRootCommand()
	if cmd, _, err := root.Find([]string{"apply", "simulate"}); err != nil || cmd == nil || cmd.Name() != "simulate" {
		t.Fatalf("expected torque apply simulate command, cmd=%v err=%v", cmd, err)
	}
	if cmd, _, err := root.Find([]string{"replay"}); err != nil || cmd == nil || cmd.Name() != "replay" {
		t.Fatalf("expected torque replay command, cmd=%v err=%v", cmd, err)
	}
	if cmd, _, err := root.Find([]string{"fix"}); err != nil || cmd == nil || cmd.Name() != "repair" {
		t.Fatalf("expected torque fix alias to resolve to repair, cmd=%v err=%v", cmd, err)
	}
}

func assertDirDoesNotContain(t *testing.T, dir, needle string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), needle) {
			t.Fatalf("%s contains raw secret %q", path, needle)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk bundle: %v", err)
	}
}

func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

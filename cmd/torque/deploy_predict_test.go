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

func TestBuildApplyPredictionFlagsRiskAndRollback(t *testing.T) {
	plan := &deployPlanResult{
		ReleaseName:    "api",
		Namespace:      "prod",
		ChartRef:       "./chart",
		ChartVersion:   "0.1.0",
		RenderedSHA256: strings.Repeat("a", 64),
		Summary:        planSummary{Creates: 1, Updates: 1, Unchanged: 2},
		Changes: []planResourceChange{
			{Kind: changeUpdate, Key: resourceKey{Kind: "Deployment", Namespace: "prod", Name: "api"}},
			{Kind: changeCreate, Key: resourceKey{Kind: "Service", Namespace: "prod", Name: "api"}},
		},
		Images: []planImageRef{
			{Resource: "Deployment/prod/api", Container: "app", Image: "repo/api:latest", Pinned: false},
		},
		GraphNodes: []deployGraphNode{
			{Kind: "Secret", Namespace: "prod", Name: "db-password", Source: "external", Live: false},
			{Kind: "ConfigMap", Namespace: "prod", Name: "api-config", Source: "rendered", Live: false},
		},
		GeneratedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}

	prediction := buildApplyPrediction(plan, []deploy.HistoryBreadcrumb{{Revision: 3, Status: "deployed"}}, &deploy.HistoryBreadcrumb{Revision: 3, Status: "deployed"})
	if prediction == nil {
		t.Fatal("prediction is nil")
	}
	if prediction.Risk != "High" {
		t.Fatalf("risk=%q", prediction.Risk)
	}
	if prediction.Summary.MissingDependencies != 1 || len(prediction.MissingDependencies) != 1 {
		t.Fatalf("missing dependencies = %#v", prediction.MissingDependencies)
	}
	if prediction.Summary.RestartingWorkloads != 1 || len(prediction.WillRestart) != 1 {
		t.Fatalf("will restart = %#v", prediction.WillRestart)
	}
	if prediction.Rollback.Revision != 3 || !prediction.Rollback.Available || prediction.Rollback.Confidence != "high" {
		t.Fatalf("rollback = %#v", prediction.Rollback)
	}
	if !strings.Contains(strings.Join(prediction.RiskReasons, "\n"), "missing external") {
		t.Fatalf("risk reasons = %#v", prediction.RiskReasons)
	}
}

func TestWriteApplyProofBundle(t *testing.T) {
	rec := &fakeArtifactRecorder{}
	path := filepath.Join(t.TempDir(), "proof-bundle.json")
	started := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	prediction := &applyPrediction{
		Version:     1,
		GeneratedAt: started.Format(time.RFC3339Nano),
		Release:     "api",
		Namespace:   "prod",
		Risk:        "Medium",
	}
	bundle := buildApplyProofBundle(applyProofBundleInput{
		StartedAt:            started,
		Command:              []string{"torque", "apply"},
		Release:              "api",
		Namespace:            "prod",
		Chart:                "./chart",
		ChartVersion:         "0.1.0",
		Err:                  errors.New("SLO failed"),
		Prediction:           prediction,
		HistoryBefore:        []deploy.HistoryBreadcrumb{{Revision: 4, Status: "deployed"}},
		LastSuccessfulBefore: &deploy.HistoryBreadcrumb{Revision: 4, Status: "deployed"},
		ResourceSnapshot:     []deploy.ResourceStatus{{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Ready"}},
		CapturePath:          "apply.sqlite",
	})

	wrote, err := writeApplyProofBundle(context.Background(), rec, path, bundle)
	if err != nil {
		t.Fatalf("writeApplyProofBundle: %v", err)
	}
	if wrote != path {
		t.Fatalf("wrote=%q", wrote)
	}
	if rec.artifacts[proofBundleArtifactName] == "" {
		t.Fatalf("missing proof bundle capture artifact")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read proof bundle: %v", err)
	}
	var decoded applyProofBundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode proof bundle: %v", err)
	}
	if decoded.Status != "failed" || decoded.Error == "" || decoded.Prediction == nil {
		t.Fatalf("decoded proof bundle = %#v", decoded)
	}
}

func TestApplyCommandHasPredictiveProofFlags(t *testing.T) {
	ns := "default"
	kubeconfig := ""
	kubeContext := ""
	logLevel := "info"
	remoteAgent := ""
	cmd := newDeployApplyCommand(&ns, &kubeconfig, &kubeContext, &logLevel, &remoteAgent, "")
	for _, name := range []string{"predict", "proof-bundle"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected --%s flag", name)
		}
	}
}

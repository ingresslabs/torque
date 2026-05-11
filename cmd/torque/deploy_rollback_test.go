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

func TestLoadApplyRollbackSLO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slo.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: torque.ingresslabs.dev/v1alpha1
kind: RolloutSLO
spec:
  minReadyPercent: 100
  maxFailedResources: 0
  maxPendingResources: 1
`), 0o644); err != nil {
		t.Fatalf("write slo: %v", err)
	}

	slo, err := loadApplyRollbackSLO(path)
	if err != nil {
		t.Fatalf("loadApplyRollbackSLO: %v", err)
	}
	if slo.Path != path || slo.SHA256 == "" || slo.Size == 0 {
		t.Fatalf("unexpected slo metadata: %#v", slo)
	}
	if slo.MinReadyPercent == nil || *slo.MinReadyPercent != 100 {
		t.Fatalf("minReadyPercent=%#v", slo.MinReadyPercent)
	}
	if slo.MaxFailedResources == nil || *slo.MaxFailedResources != 0 {
		t.Fatalf("maxFailedResources=%#v", slo.MaxFailedResources)
	}
	if slo.MaxPendingResources == nil || *slo.MaxPendingResources != 1 {
		t.Fatalf("maxPendingResources=%#v", slo.MaxPendingResources)
	}
	if strings.Join(slo.Keys, ",") != "maxFailedResources,maxPendingResources,minReadyPercent" {
		t.Fatalf("keys=%#v", slo.Keys)
	}
}

func TestApplyRollbackSLOEvaluate(t *testing.T) {
	minReady := 100
	maxFailed := 0
	maxPending := 0
	slo := &applyRollbackSLO{
		MinReadyPercent:     &minReady,
		MaxFailedResources:  &maxFailed,
		MaxPendingResources: &maxPending,
	}
	rows := []deploy.ResourceStatus{
		{Kind: "Deployment", Name: "api", Status: "Ready"},
		{Kind: "Pod", Name: "api-1", Status: "Pending"},
	}
	err := slo.Evaluate(rows)
	if err == nil {
		t.Fatalf("expected SLO violation")
	}
	if !strings.Contains(err.Error(), "pending") && !strings.Contains(err.Error(), "ready") {
		t.Fatalf("unexpected SLO error: %v", err)
	}
}

func TestWriteApplyRollbackProof(t *testing.T) {
	rec := &fakeArtifactRecorder{}
	path := filepath.Join(t.TempDir(), "proof.json")
	started := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	proof := buildApplyRollbackProof(applyRollbackProofInput{
		Release:       "api",
		Namespace:     "prod",
		Chart:         "api-chart",
		Mode:          "helm-atomic",
		Outcome:       "rollback-requested",
		TriggerSource: "helm",
		TriggerReason: "apply failed",
		Err:           errors.New("timed out waiting for deployment"),
		StartedAt:     started,
		HistoryBefore: []deploy.HistoryBreadcrumb{{Revision: 2, Status: "deployed"}},
		HistoryAfter:  []deploy.HistoryBreadcrumb{{Revision: 2, Status: "deployed"}},
		Resources:     []deploy.ResourceStatus{{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Failed"}},
	})

	wrote, err := writeApplyRollbackProof(context.Background(), rec, path, proof)
	if err != nil {
		t.Fatalf("writeApplyRollbackProof: %v", err)
	}
	if wrote != path {
		t.Fatalf("wrote=%q", wrote)
	}
	if rec.artifacts[rollbackProofArtifactName] == "" {
		t.Fatalf("missing rollback proof capture artifact")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read proof: %v", err)
	}
	var decoded applyRollbackProof
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode proof: %v", err)
	}
	if decoded.Release != "api" || decoded.Namespace != "prod" || decoded.RollbackCommand != "torque revert --release api -n prod" {
		t.Fatalf("decoded proof = %#v", decoded)
	}
}

func TestApplyCommandHasAutoRollbackFlags(t *testing.T) {
	ns := "default"
	kubeconfig := ""
	kubeContext := ""
	logLevel := "info"
	remoteAgent := ""
	cmd := newDeployApplyCommand(&ns, &kubeconfig, &kubeContext, &logLevel, &remoteAgent, "")
	for _, name := range []string{"auto-rollback", "slo", "rollback-proof"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected --%s flag", name)
		}
	}
}

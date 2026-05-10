package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIncidentBundleComplexBrokenRollout(t *testing.T) {
	rawSecret := "AKIA1234567890ABCDEF"
	now := time.Date(2026, 5, 11, 14, 0, 0, 0, time.UTC)
	dep := mustGuardianObject(t, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
  labels:
    app.kubernetes.io/instance: api
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: api
          image: ghcr.io/acme/api:missing
status:
  replicas: 1
  unavailableReplicas: 1
`)
	dep.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:    "kubectl-patch",
		Operation:  metav1.ManagedFieldsOperationUpdate,
		APIVersion: "apps/v1",
		Time:       &metav1.Time{Time: now},
	}})
	cm := mustGuardianObject(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
  namespace: prod
  labels:
    app.kubernetes.io/instance: api
data:
  password: AKIA1234567890ABCDEF
`)
	statuses := map[string]deploy.ResourceStatus{
		incidentResourceKey(guardianResourceFromObject(dep, "prod")): {
			Kind:      "Deployment",
			Namespace: "prod",
			Name:      "api",
			Status:    "Pending",
			Reason:    "ImagePullBackOff",
			Message:   "0/1 pods ready: failed to pull image",
		},
	}
	bundle := buildIncidentBundle(incidentBuildInput{
		GeneratedAt: now.UTC().Format(time.RFC3339Nano),
		ClusterHost: "https://cluster",
		Release:     "api",
		Namespace:   "prod",
		Since:       "1h",
		StartedAt:   now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		Resources:   []*unstructured.Unstructured{dep, cm},
		Statuses:    statuses,
		Events: []guardianEventRow{{
			Time:    now.UTC().Format(time.RFC3339Nano),
			Type:    "Warning",
			Reason:  "Failed",
			Message: "Failed to pull image ghcr.io/acme/api:missing token=" + rawSecret,
			Resource: guardianResourceRef{
				Kind:      "Pod",
				Namespace: "prod",
				Name:      "api-123",
			},
		}},
		Logs: []incidentLogSet{{
			Namespace: "prod",
			Pod:       "api-123",
			Container: "api",
			Lines:     []string{"fatal startup failed password: " + rawSecret},
		}},
	})
	if !bundle.RootCause.Blocked || bundle.RootCause.PrimaryCause != "image_pull_failure" {
		t.Fatalf("expected image pull root cause, got %#v", bundle.RootCause)
	}
	if bundle.Summary.Resources != 2 || bundle.Summary.Unhealthy != 1 || bundle.Summary.WarningEvents != 1 || bundle.Summary.BoundaryFindings != 1 || bundle.Summary.ManagedOwners != 1 {
		t.Fatalf("unexpected incident summary: %#v", bundle.Summary)
	}
	if len(bundle.CausalTimeline.Items) < 3 {
		t.Fatalf("expected causal timeline evidence, got %#v", bundle.CausalTimeline.Items)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if strings.Contains(string(raw), rawSecret) {
		t.Fatalf("incident bundle leaked raw secret: %s", raw)
	}
}

func TestIncidentReplayExplainAndPRArtifacts(t *testing.T) {
	dir := t.TempDir()
	bundle := incidentBundle{
		Version:     "v1",
		Tool:        incidentTool,
		GeneratedAt: time.Date(2026, 5, 11, 14, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Release:     "api",
		Namespace:   "prod",
		ObserveOnly: true,
		Summary:     incidentSummary{Resources: 1, Unhealthy: 1, WarningEvents: 1},
		Resources: []incidentResourceSnapshot{{
			Resource: guardianResourceRef{Version: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: "api"},
			Status:   deploy.ResourceStatus{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Pending", Reason: "ImagePullBackOff", Message: "failed to pull image"},
		}},
		EventsTimeline:        guardianEventsTimeline{Version: "v1"},
		ManagedFields:         guardianManagedFieldsReport{Version: "v1"},
		RuntimeSecretBoundary: guardianRuntimeBoundaryReport{Version: "v1", Passed: true},
		RolloutAftercare:      guardianRolloutAftercareReport{Version: "v1", Passed: false},
	}
	bundle.CausalTimeline = buildIncidentTimeline(bundle)
	bundle.RootCause = buildIncidentRootCause(bundle)
	source := filepath.Join(dir, "incident.torque")
	if err := writeJSONFile(source, bundle); err != nil {
		t.Fatalf("write incident bundle: %v", err)
	}
	proof, err := replayIncidentBundle(source, incidentReplayOptions{Lab: "k3s", Out: filepath.Join(dir, "incident-replay-proof")})
	if err != nil {
		t.Fatalf("replay incident bundle: %v", err)
	}
	if !proof.Passed || !proof.Blocked || proof.RootCause.PrimaryCause != "image_pull_failure" {
		t.Fatalf("unexpected replay proof: %#v", proof)
	}
	for _, name := range []string{"manifest.json", "capture.bundle.json", "replay.result.json", "causal.timeline.json", "root-cause.json", filepath.Join("fix", "incident-fix.patch"), filepath.Join("fix", "pr.md")} {
		if _, err := os.Stat(filepath.Join(dir, "incident-replay-proof", name)); err != nil {
			t.Fatalf("expected replay artifact %s: %v", name, err)
		}
	}
	root, err := loadIncidentRootCause(filepath.Join(dir, "incident-replay-proof"))
	if err != nil {
		t.Fatalf("load root cause: %v", err)
	}
	if root.PrimaryCause != "image_pull_failure" {
		t.Fatalf("unexpected root cause: %#v", root)
	}
	paths, err := writeIncidentPRArtifacts(root, incidentPROptions{From: filepath.Join(dir, "incident-replay-proof"), Branch: "fix/api-incident"})
	if err != nil {
		t.Fatalf("write incident PR artifacts: %v", err)
	}
	pr, err := os.ReadFile(paths["pr"])
	if err != nil {
		t.Fatalf("read incident PR: %v", err)
	}
	if !strings.Contains(string(pr), "fix/api-incident") || !strings.Contains(string(pr), "image_pull_failure") {
		t.Fatalf("unexpected incident PR:\n%s", pr)
	}
}

func TestRootExposesIncidentCommands(t *testing.T) {
	root := newRootCommand()
	for _, args := range [][]string{
		{"incident", "capture"},
		{"incident", "replay"},
		{"incident", "explain"},
		{"incident", "pr"},
	} {
		cmd, _, err := root.Find(args)
		if err != nil || cmd == nil || cmd.Name() != args[len(args)-1] {
			t.Fatalf("expected %v command, got cmd=%v err=%v", args, cmd, err)
		}
	}
	if err := newIncidentCaptureCommand(new(string), new(string)).PreRunE(&cobra.Command{}, nil); err == nil {
		t.Fatal("expected incident capture validation to require --release")
	}
}

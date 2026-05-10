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

func TestContractSynthesizeAndTestComplexFailure(t *testing.T) {
	dir := t.TempDir()
	rawSecret := "AKIA1234567890ABCDEF"
	now := time.Date(2026, 5, 11, 15, 0, 0, 0, time.UTC)
	bundle := buildContractIncidentBundle(t, now, rawSecret, true)
	incidentPath := filepath.Join(dir, "incident.torque")
	if err := writeJSONFile(incidentPath, bundle); err != nil {
		t.Fatalf("write incident bundle: %v", err)
	}
	replayDir := filepath.Join(dir, "incident-replay-proof")
	if _, err := replayIncidentBundle(incidentPath, incidentReplayOptions{Lab: "k3s", Out: replayDir}); err != nil {
		t.Fatalf("replay incident bundle: %v", err)
	}
	guardianPath := filepath.Join(dir, "drift-proof.json")
	if err := writeJSONFile(guardianPath, buildContractGuardianProof(now, rawSecret, true)); err != nil {
		t.Fatalf("write guardian proof: %v", err)
	}
	evidence, err := loadContractEvidence(replayDir, guardianPath)
	if err != nil {
		t.Fatalf("load contract evidence: %v", err)
	}
	contract := synthesizeRuntimeContract(evidence)
	wantTypes := []string{
		"incident.replay.passed",
		"incident.rootCause.absent",
		"runtime.secretBoundary.none",
		"rollout.aftercare.passed",
		"resource.available",
		"guardian.drift.none",
		"events.warningReason.absent",
		"managedFields.noSuspiciousOwners",
		"logs.failureSignals.absent",
	}
	for _, typ := range wantTypes {
		if !contractHasInvariantType(contract, typ) {
			t.Fatalf("expected synthesized contract to include %s, got %#v", typ, contract.Spec.Invariants)
		}
	}
	proof := testRuntimeContract(contract, evidence, contractTestOptions{Contract: "torque-contract.yaml", From: replayDir, Guardian: guardianPath})
	if proof.Passed || !proof.Blocked {
		t.Fatalf("expected broken evidence to fail contract: %#v", proof)
	}
	if proof.Summary.Failed < 6 || proof.Summary.Critical == 0 {
		t.Fatalf("expected multiple critical failures, got %#v", proof.Summary)
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		t.Fatalf("marshal proof: %v", err)
	}
	if strings.Contains(string(raw), rawSecret) {
		t.Fatalf("contract proof leaked raw secret: %s", raw)
	}
}

func TestContractTestPassesCleanEvidenceForSynthesizedContract(t *testing.T) {
	now := time.Date(2026, 5, 11, 15, 30, 0, 0, time.UTC)
	broken := contractEvidence{
		Bundle:       buildContractIncidentBundle(t, now, "ghp_abcdefghijklmnopqrstuvwxyz123456", true),
		RootCause:    incidentRootCause{Version: "v1", Status: "blocked", Blocked: true, PrimaryCause: "image_pull_failure", Release: "api", Namespace: "prod"},
		Replay:       incidentReplayProof{Version: "v1", Tool: incidentTool, Passed: true, Release: "api", Namespace: "prod"},
		Guardian:     buildContractGuardianProof(now, "ghp_abcdefghijklmnopqrstuvwxyz123456", true),
		HasIncident:  true,
		HasRootCause: true,
		HasReplay:    true,
		HasGuardian:  true,
	}
	contract := synthesizeRuntimeContract(broken)
	cleanBundle := buildContractIncidentBundle(t, now.Add(time.Minute), "", false)
	clean := contractEvidence{
		Bundle:       cleanBundle,
		RootCause:    cleanBundle.RootCause,
		Replay:       incidentReplayProof{Version: "v1", Tool: incidentTool, Passed: true, Release: "api", Namespace: "prod"},
		Guardian:     buildContractGuardianProof(now.Add(time.Minute), "", false),
		HasIncident:  true,
		HasRootCause: true,
		HasReplay:    true,
		HasGuardian:  true,
	}
	proof := testRuntimeContract(contract, clean, contractTestOptions{})
	if !proof.Passed || proof.Blocked || proof.Summary.Failed != 0 {
		t.Fatalf("expected clean evidence to pass synthesized contract: %#v", proof)
	}
}

func TestContractYAMLAndPRArtifacts(t *testing.T) {
	dir := t.TempDir()
	contract := runtimeContract{
		APIVersion: "torque.dev/v1",
		Kind:       "RuntimeContract",
		Metadata:   runtimeContractMetadata{Name: "api-runtime-contract"},
		Spec: runtimeContractSpec{
			Release:     "api",
			Namespace:   "prod",
			ObserveOnly: true,
			Invariants: []runtimeInvariant{{
				ID:          "no-image-pull-failure",
				Type:        "incident.rootCause.absent",
				Severity:    "critical",
				Description: "No image pull failure",
				Value:       "image_pull_failure",
				Required:    true,
			}},
		},
	}
	contractPath := filepath.Join(dir, "torque-contract.yaml")
	if err := writeRuntimeContract(contractPath, contract); err != nil {
		t.Fatalf("write runtime contract: %v", err)
	}
	loaded, err := loadRuntimeContract(contractPath)
	if err != nil {
		t.Fatalf("load runtime contract: %v", err)
	}
	if loaded.Metadata.Name != contract.Metadata.Name || loaded.Spec.Invariants[0].Type != "incident.rootCause.absent" {
		t.Fatalf("unexpected loaded contract: %#v", loaded)
	}
	proof := runtimeContractProof{
		Version:     "v1",
		Tool:        contractTool,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Contract:    contractPath,
		Release:     "api",
		Namespace:   "prod",
		Passed:      false,
		Blocked:     true,
		Summary:     runtimeContractSummary{Invariants: 1, Failed: 1, Critical: 1},
		Results: []runtimeContractCheck{{
			ID:       "no-image-pull-failure",
			Type:     "incident.rootCause.absent",
			Severity: "critical",
			Passed:   false,
			Message:  "blocked root cause recurred: image_pull_failure",
		}},
	}
	proofPath := filepath.Join(dir, "contract-proof.json")
	if err := writeJSONFile(proofPath, proof); err != nil {
		t.Fatalf("write contract proof: %v", err)
	}
	paths, err := writeContractPRArtifacts(loaded, proof, contractPROptions{Proof: proofPath, Branch: "add/api-runtime-contract"})
	if err != nil {
		t.Fatalf("write contract PR artifacts: %v", err)
	}
	pr, err := os.ReadFile(paths["pr"])
	if err != nil {
		t.Fatalf("read contract PR: %v", err)
	}
	if !strings.Contains(string(pr), "add/api-runtime-contract") || !strings.Contains(string(pr), "no-image-pull-failure") {
		t.Fatalf("unexpected contract PR body:\n%s", pr)
	}
}

func TestRootExposesContractCommands(t *testing.T) {
	root := newRootCommand()
	for _, args := range [][]string{
		{"contract", "synthesize"},
		{"contract", "test"},
		{"contract", "pr"},
	} {
		cmd, _, err := root.Find(args)
		if err != nil || cmd == nil || cmd.Name() != args[len(args)-1] {
			t.Fatalf("expected %v command, got cmd=%v err=%v", args, cmd, err)
		}
	}
	if err := newContractSynthesizeCommand().PreRunE(&cobra.Command{}, nil); err == nil {
		t.Fatal("expected contract synthesize validation to require evidence")
	}
}

func buildContractIncidentBundle(t *testing.T, now time.Time, rawSecret string, broken bool) incidentBundle {
	t.Helper()
	image := "ghcr.io/acme/api:v1"
	status := deploy.ResourceStatus{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Ready", Reason: "Available", Message: "1/1 pods ready"}
	depStatus := `
status:
  replicas: 1
  availableReplicas: 1
`
	if broken {
		image = "ghcr.io/acme/api:missing"
		status = deploy.ResourceStatus{Kind: "Deployment", Namespace: "prod", Name: "api", Status: "Pending", Reason: "ImagePullBackOff", Message: "0/1 pods ready: failed to pull image"}
		depStatus = `
status:
  replicas: 1
  unavailableReplicas: 1
`
	}
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
          image: `+image+depStatus)
	if broken {
		dep.SetManagedFields([]metav1.ManagedFieldsEntry{{
			Manager:    "kubectl-patch",
			Operation:  metav1.ManagedFieldsOperationUpdate,
			APIVersion: "apps/v1",
			Time:       &metav1.Time{Time: now},
		}})
	}
	resources := []*unstructured.Unstructured{dep}
	if broken {
		cm := mustGuardianObject(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
  namespace: prod
  labels:
    app.kubernetes.io/instance: api
data:
  password: `+rawSecret)
		resources = append(resources, cm)
	}
	events := []guardianEventRow{}
	logs := []incidentLogSet{}
	if broken {
		events = append(events, guardianEventRow{
			Time:     now.UTC().Format(time.RFC3339Nano),
			Type:     "Warning",
			Reason:   "Failed",
			Message:  "Failed to pull image token=" + rawSecret,
			Resource: guardianResourceRef{Kind: "Pod", Namespace: "prod", Name: "api-123"},
		})
		logs = append(logs, incidentLogSet{Namespace: "prod", Pod: "api-123", Container: "api", Lines: []string{"fatal startup failed password: " + rawSecret}})
	}
	bundle := buildIncidentBundle(incidentBuildInput{
		GeneratedAt: now.UTC().Format(time.RFC3339Nano),
		ClusterHost: "https://cluster",
		Release:     "api",
		Namespace:   "prod",
		Since:       "1h",
		StartedAt:   now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		Resources:   resources,
		Statuses: map[string]deploy.ResourceStatus{
			incidentResourceKey(guardianResourceFromObject(dep, "prod")): status,
		},
		Events: events,
		Logs:   logs,
	})
	return bundle
}

func buildContractGuardianProof(now time.Time, rawSecret string, broken bool) guardianDiffProof {
	proof := guardianDiffProof{
		Version:                "v1",
		Tool:                   guardianTool,
		GeneratedAt:            now.UTC().Format(time.RFC3339Nano),
		Source:                 "./torque-sim-proof",
		Release:                "api",
		Namespace:              "prod",
		RenderedManifestSHA256: "sha256:test",
		Status:                 "passed",
		PredictedVsLive:        guardianPredictedVsLiveDiff{Version: "v1", Passed: true},
		ManagedFields:          guardianManagedFieldsReport{Version: "v1"},
		DriftTimeline:          guardianDriftTimeline{Version: "v1"},
		EventsTimeline:         guardianEventsTimeline{Version: "v1"},
		RuntimeSecretBoundary:  guardianRuntimeBoundaryReport{Version: "v1", Passed: true},
		RolloutAftercare:       guardianRolloutAftercareReport{Version: "v1", Passed: true},
	}
	if !broken {
		return proof
	}
	proof.Status = "drifted"
	proof.Blocked = true
	proof.Summary = guardianDiffSummary{Resources: 2, Changed: 1, ManagedFieldOwners: 1, RuntimeBoundary: 1, WarningEvents: 1, AftercareIssues: 1}
	proof.PredictedVsLive = guardianPredictedVsLiveDiff{Version: "v1", Passed: false, Changes: []guardianDriftItem{{
		Resource: guardianResourceRef{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "api-config"},
		Reason:   "changed",
	}}}
	proof.ManagedFields.Owners = []guardianManagedOwnerRow{{
		Resource:   guardianResourceRef{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "api-config"},
		Manager:    "kubectl-patch",
		Operation:  string(metav1.ManagedFieldsOperationUpdate),
		APIVersion: "v1",
		Time:       now.UTC().Format(time.RFC3339Nano),
		Suspicious: true,
	}}
	proof.EventsTimeline.Events = []guardianEventRow{{
		Time:     now.UTC().Format(time.RFC3339Nano),
		Type:     "Warning",
		Reason:   "Failed",
		Message:  "Failed to pull image token=" + rawSecret,
		Resource: guardianResourceRef{Kind: "Pod", Namespace: "prod", Name: "api-123"},
	}}
	proof.RuntimeSecretBoundary = guardianRuntimeBoundaryReport{Version: "v1", Passed: false, Findings: []guardianBoundaryFinding{{
		Resource: guardianResourceRef{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "api-config"},
		Surface:  "data.password",
		Boundary: "ConfigMap",
		Severity: "critical",
		Message:  "ConfigMap data contains token=" + rawSecret,
	}}}
	proof.RolloutAftercare = guardianRolloutAftercareReport{Version: "v1", Passed: false, Items: []guardianAftercareFinding{{
		Resource: guardianResourceRef{Version: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: "api"},
		Severity: "high",
		Reason:   "deployment_not_fully_available",
		Message:  "0/1 replicas available",
	}}}
	return proof
}

func contractHasInvariantType(contract runtimeContract, typ string) bool {
	for _, inv := range contract.Spec.Invariants {
		if inv.Type == typ {
			return true
		}
	}
	return false
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentPolicyAndRunAuthorizeProofBackedApply(t *testing.T) {
	dir, graphPath, _ := writeProofGateFixture(t, true)
	requestPath := filepath.Join(dir, "agent-request.json")
	request := agentPolicyRequest{
		Version:   "v1",
		Actor:     "codex",
		Operation: "apply",
		Command:   []string{"torque", "apply", "--chart", "./chart", "--release", "api", "-n", "prod"},
		Release:   "api",
		Namespace: "prod",
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := os.WriteFile(requestPath, raw, 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"agent", "policy", "check", requestPath, "--proof", graphPath, "--allow", "apply", "--require-gate", "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("agent policy check: %v\n%s", err, out.String())
	}
	var policy agentPolicyReport
	if err := json.Unmarshal(out.Bytes(), &policy); err != nil {
		t.Fatalf("decode policy: %v\n%s", err, out.String())
	}
	if !policy.Allowed || policy.Gate == nil || !policy.Gate.Passed {
		t.Fatalf("expected allowed policy: %#v", policy)
	}

	root = newRootCommand()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"agent", "run", requestPath, "--proof", graphPath, "--allow", "apply", "--require-gate", "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("agent run: %v\n%s", err, out.String())
	}
	var run agentRunReport
	if err := json.Unmarshal(out.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v\n%s", err, out.String())
	}
	if !run.Authorized || run.Executed {
		t.Fatalf("expected authorized non-mutating run: %#v", run)
	}
}

func TestAgentPolicyDeniesUnallowedApply(t *testing.T) {
	dir, graphPath, _ := writeProofGateFixture(t, true)
	requestPath := filepath.Join(dir, "agent-request.json")
	if err := os.WriteFile(requestPath, []byte(`{"version":"v1","actor":"codex","operation":"apply"}`), 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"agent", "policy", "check", requestPath, "--proof", graphPath, "--allow", "delete", "--require-gate", "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatalf("expected policy denial")
	}
	var policy agentPolicyReport
	if err := json.Unmarshal(out.Bytes(), &policy); err != nil {
		t.Fatalf("decode denied policy: %v\n%s", err, out.String())
	}
	if policy.Allowed {
		t.Fatalf("expected denied policy: %#v", policy)
	}
}

func TestReleaseScoreAndFlightCommands(t *testing.T) {
	dir, graphPath, _ := writeProofGateFixture(t, true)
	scorePath := filepath.Join(dir, "release-score.json")
	flightPath := filepath.Join(dir, "release.flight.torque")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"release", "score", graphPath, "--out", scorePath, "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("release score: %v\n%s", err, out.String())
	}
	var score releaseScoreReport
	if err := json.Unmarshal(out.Bytes(), &score); err != nil {
		t.Fatalf("decode score: %v\n%s", err, out.String())
	}
	if score.Score < 80 || !score.GatePassed || !score.Verified {
		t.Fatalf("expected high verified score: %#v", score)
	}

	root = newRootCommand()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"flight", "record", graphPath, "--out", flightPath, "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("flight record: %v\n%s", err, out.String())
	}
	var flight releaseFlight
	if err := json.Unmarshal(out.Bytes(), &flight); err != nil {
		t.Fatalf("decode flight: %v\n%s", err, out.String())
	}
	if len(flight.Timeline) == 0 || flight.Score == 0 || flight.GraphSHA256 == "" {
		t.Fatalf("expected populated flight: %#v", flight)
	}

	root = newRootCommand()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"flight", "replay", flightPath, "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("flight replay: %v\n%s", err, out.String())
	}
	var replay flightReplayReport
	if err := json.Unmarshal(out.Bytes(), &replay); err != nil {
		t.Fatalf("decode replay: %v\n%s", err, out.String())
	}
	if !replay.Passed || replay.Events == 0 {
		t.Fatalf("expected flight replay to pass: %#v", replay)
	}

	root = newRootCommand()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"flight", "explain", flightPath, "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("flight explain: %v\n%s", err, out.String())
	}
	var explain flightExplainReport
	if err := json.Unmarshal(out.Bytes(), &explain); err != nil {
		t.Fatalf("decode explain: %v\n%s", err, out.String())
	}
	if len(explain.Phases) == 0 || explain.Summary == "" {
		t.Fatalf("expected flight explanation: %#v", explain)
	}
}

func TestReleaseAutopilotWritesProofBackedBundle(t *testing.T) {
	dir, graphPath, keyPath := writeProofGateFixture(t, true)
	outDir := filepath.Join(dir, "autopilot")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"release", "autopilot", graphPath,
		"--out-dir", outDir,
		"--key", keyPath,
		"--allow", "apply",
		"--actor", "codex",
		"--release", "api",
		"--namespace", "prod",
		"--fail-below", "80",
		"--format", "json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("release autopilot: %v\n%s", err, out.String())
	}
	var report releaseAutopilotReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode autopilot report: %v\n%s", err, out.String())
	}
	if !report.Passed || !report.Gate.Passed || report.Score.Score < 80 || !report.Replay.Passed {
		t.Fatalf("expected passing autopilot report: %#v", report)
	}
	if report.AgentPolicy == nil || !report.AgentPolicy.Allowed || report.AgentRun == nil || !report.AgentRun.Authorized {
		t.Fatalf("expected authorized agent records: %#v", report)
	}
	if report.Attestation == nil || !report.Attestation.Verified || report.Attestation.Signature == nil {
		t.Fatalf("expected signed attestation: %#v", report.Attestation)
	}
	for _, path := range []string{
		report.Artifacts.ProofGraph,
		report.Artifacts.ProofHTML,
		report.Artifacts.Gate,
		report.Artifacts.Score,
		report.Artifacts.Flight,
		report.Artifacts.Replay,
		report.Artifacts.Explain,
		report.Artifacts.AgentRequest,
		report.Artifacts.AgentPolicy,
		report.Artifacts.AgentRun,
		report.Artifacts.Attestation,
	} {
		if path == "" {
			t.Fatalf("expected artifact path in report: %#v", report.Artifacts)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %s: %v", path, err)
		}
	}
}

func TestReleaseAutopilotBlocksLowScore(t *testing.T) {
	dir, graphPath, keyPath := writeProofGateFixture(t, true)
	outDir := filepath.Join(dir, "autopilot-low-score")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"release", "autopilot", graphPath,
		"--out-dir", outDir,
		"--key", keyPath,
		"--fail-below", "100",
		"--format", "json",
	})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatalf("expected autopilot to block below score threshold")
	}
	var report releaseAutopilotReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode blocked autopilot report: %v\n%s", err, out.String())
	}
	if report.Passed {
		t.Fatalf("expected blocked autopilot report: %#v", report)
	}
	found := false
	for _, check := range report.Checks {
		if check.ID == "release.score" && !check.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected release.score blocking check: %#v", report.Checks)
	}
}

func TestReleasePromoteCanaryWritesProofBackedPlan(t *testing.T) {
	dir, graphPath, keyPath := writeProofGateFixture(t, true)
	outDir := filepath.Join(dir, "promote-canary")
	sloPath := filepath.Join(dir, "evidence", "slo.yaml")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"release", "promote", graphPath,
		"--strategy", "canary",
		"--steps", "5,25,50,100",
		"--analysis-window", "1m",
		"--slo", sloPath,
		"--rollback-on-fail",
		"--out-dir", outDir,
		"--key", keyPath,
		"--fail-below", "80",
		"--format", "json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("release promote canary: %v\n%s", err, out.String())
	}
	var report releasePromotionReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode canary promotion: %v\n%s", err, out.String())
	}
	if !report.Passed || report.Strategy != "canary" || !report.Gate.Passed || report.Score.Score < 80 {
		t.Fatalf("expected passing canary promotion: %#v", report)
	}
	if report.Canary == nil || len(report.Canary.Steps) != 4 || report.Canary.Steps[3].Traffic.Canary != 100 {
		t.Fatalf("expected canary traffic ladder: %#v", report.Canary)
	}
	if report.AgentPolicy == nil || !report.AgentPolicy.Allowed || report.AgentRun == nil || !report.AgentRun.Authorized {
		t.Fatalf("expected authorized promotion agent records: %#v", report)
	}
	if report.Attestation == nil || !report.Attestation.Verified || report.Attestation.Signature == nil {
		t.Fatalf("expected signed promotion attestation: %#v", report.Attestation)
	}
	for _, path := range []string{
		report.Artifacts.Report,
		report.Artifacts.Decision,
		report.Artifacts.PromotedGraph,
		report.Artifacts.Gate,
		report.Artifacts.Score,
		report.Artifacts.Flight,
		report.Artifacts.AgentRequest,
		report.Artifacts.AgentPolicy,
		report.Artifacts.AgentRun,
		report.Artifacts.Attestation,
	} {
		if path == "" {
			t.Fatalf("expected promotion artifact path: %#v", report.Artifacts)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected promotion artifact %s: %v", path, err)
		}
	}
	promoted, _, err := loadOrBuildProofGraph(report.Artifacts.PromotedGraph)
	if err != nil {
		t.Fatalf("load promoted graph: %v", err)
	}
	if !proofGraphHasType(promoted, "release-promotion") {
		t.Fatalf("expected promoted graph to include release-promotion artifact: %#v", promoted.Artifacts)
	}
}

func TestReleasePromoteBlueGreenExecuteFileProvider(t *testing.T) {
	dir, graphPath, keyPath := writeProofGateFixture(t, true)
	outDir := filepath.Join(dir, "promote-blue-green")
	smokePath := filepath.Join(dir, "smoke.json")
	statePath := filepath.Join(dir, "traffic-state.json")
	if err := os.WriteFile(smokePath, []byte(`{"passed":true,"checks":["http","migration"]}`), 0o644); err != nil {
		t.Fatalf("write smoke: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"release", "promote", graphPath,
		"--strategy", "blue-green",
		"--preview",
		"--smoke", smokePath,
		"--switch-traffic",
		"--provider", "file",
		"--state-out", statePath,
		"--execute",
		"--yes",
		"--out-dir", outDir,
		"--key", keyPath,
		"--fail-below", "80",
		"--format", "json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("release promote blue-green: %v\n%s", err, out.String())
	}
	var report releasePromotionReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode blue-green promotion: %v\n%s", err, out.String())
	}
	if !report.Passed || report.Strategy != "blue-green" || report.BlueGreen == nil || len(report.BlueGreen.Phases) < 3 {
		t.Fatalf("expected passing blue-green promotion: %#v", report)
	}
	if report.ProviderState == nil || !report.ProviderState.Applied || report.ProviderState.FinalTraffic.Green != 100 || report.ProviderState.FinalTraffic.Blue != 0 {
		t.Fatalf("expected applied file provider state: %#v", report.ProviderState)
	}
	if report.Smoke == nil || report.Smoke.SHA256 == "" {
		t.Fatalf("expected smoke evidence: %#v", report.Smoke)
	}
	var state releasePromotionProviderState
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if !state.Applied || state.FinalTraffic.Green != 100 {
		t.Fatalf("expected persisted green state: %#v", state)
	}
}

func TestReleasePromoteBlocksFailedGate(t *testing.T) {
	dir, graphPath, keyPath := writeProofGateFixture(t, false)
	outDir := filepath.Join(dir, "promote-blocked")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"release", "promote", graphPath,
		"--strategy", "canary",
		"--steps", "10,100",
		"--out-dir", outDir,
		"--key", keyPath,
		"--fail-below", "80",
		"--format", "json",
	})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatalf("expected promotion to block failed gate")
	}
	var report releasePromotionReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode blocked promotion: %v\n%s", err, out.String())
	}
	if report.Passed {
		t.Fatalf("expected blocked promotion report: %#v", report)
	}
	found := false
	for _, check := range report.Checks {
		if check.ID == "proof.gate" && !check.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected proof.gate blocking check: %#v", report.Checks)
	}
}

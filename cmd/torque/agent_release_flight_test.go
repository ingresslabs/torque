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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type recordingShipRunner struct {
	calls []string
	fail  map[string]error
}

func (r *recordingShipRunner) RunBuild(_ *cobra.Command, _ shipOptions, _ shipPaths) error {
	return r.record("build")
}

func (r *recordingShipRunner) RunVerify(_ *cobra.Command, _ shipOptions, _ shipPaths) error {
	return r.record("verify")
}

func (r *recordingShipRunner) RunPlan(_ *cobra.Command, _ shipOptions, _ shipPaths) error {
	return r.record("plan")
}

func (r *recordingShipRunner) RunApply(_ *cobra.Command, _ shipOptions, _ shipPaths) error {
	return r.record("apply")
}

func (r *recordingShipRunner) RunExplain(_ *cobra.Command, _ shipOptions, _ shipPaths) error {
	return r.record("explain")
}

func (r *recordingShipRunner) record(name string) error {
	r.calls = append(r.calls, name)
	if r.fail != nil {
		return r.fail[name]
	}
	return nil
}

func newTestShipCommand(runner shipRunner) *cobra.Command {
	profile := "dev"
	logLevel := "info"
	return newShipCommandWithRunner(nil, &profile, &logLevel, nil, nil, nil, runner)
}

func TestShipCommandRunsGoldenWorkflow(t *testing.T) {
	runner := &recordingShipRunner{}
	evidenceDir := t.TempDir()
	cmd := newTestShipCommand(runner)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--chart", "./chart",
		"--release", "api",
		"--namespace", "prod",
		"--build", ".",
		"--tag", "ghcr.io/acme/api:dev",
		"--evidence-dir", evidenceDir,
		"--yes",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ship returned error: %v", err)
	}
	assertCalls(t, runner.calls, []string{"build", "verify", "plan", "apply", "explain"})

	summary := readShipSummary(t, filepath.Join(evidenceDir, "ship.json"))
	if summary.Status != "success" {
		t.Fatalf("expected success summary, got %#v", summary)
	}
	if summary.Paths.BuildCapture != filepath.Join(evidenceDir, "build.sqlite") {
		t.Fatalf("unexpected build capture path: %s", summary.Paths.BuildCapture)
	}
	if summary.Paths.ApplyCapture != filepath.Join(evidenceDir, "apply.sqlite") {
		t.Fatalf("unexpected apply capture path: %s", summary.Paths.ApplyCapture)
	}
}

func TestShipCommandPlanOnlySkipsApplyAndExplain(t *testing.T) {
	runner := &recordingShipRunner{}
	cmd := newTestShipCommand(runner)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--chart", "./chart",
		"--release", "api",
		"--build", ".",
		"--tag", "ghcr.io/acme/api:dev",
		"--evidence-dir", t.TempDir(),
		"--plan-only",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ship returned error: %v", err)
	}
	assertCalls(t, runner.calls, []string{"build", "verify", "plan"})
}

func TestShipCommandExplainsAfterApplyFailure(t *testing.T) {
	applyErr := errors.New("apply boom")
	runner := &recordingShipRunner{fail: map[string]error{"apply": applyErr}}
	evidenceDir := t.TempDir()
	cmd := newTestShipCommand(runner)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--chart", "./chart",
		"--release", "api",
		"--build", ".",
		"--tag", "ghcr.io/acme/api:dev",
		"--evidence-dir", evidenceDir,
	})

	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "apply boom") {
		t.Fatalf("expected apply error, got %v", err)
	}
	assertCalls(t, runner.calls, []string{"build", "verify", "plan", "apply", "explain"})

	summary := readShipSummary(t, filepath.Join(evidenceDir, "ship.json"))
	if summary.Status != "failed" || !strings.Contains(summary.Error, "apply boom") {
		t.Fatalf("expected failed summary with apply error, got %#v", summary)
	}
}

func TestShipStepArgsCarryEvidence(t *testing.T) {
	opts := shipOptions{
		chart:           "./chart",
		release:         "api",
		namespace:       "prod",
		valuesFiles:     []string{"values.yaml"},
		setValues:       []string{"image.tag=dev"},
		setStringValues: []string{"commit=abc123"},
		buildContext:    ".",
		tags:            []string{"ghcr.io/acme/api:dev"},
		push:            true,
		attest:          true,
		buildPolicyMode: "enforce",
		buildOutput:     "logs",
		yes:             true,
		watch:           30 * time.Second,
	}
	paths := shipPaths{
		BuildCapture:  "build.sqlite",
		ApplyCapture:  "apply.sqlite",
		VerifyReport:  "verify.json",
		PlanOutput:    "plan.json",
		AttestDir:     "attest",
		ExplainOutput: "explain.md",
	}

	buildArgs := shipBuildArgs(opts, paths)
	for _, want := range []string{"--push", "--sbom", "--provenance", "--attest-dir", "attest", "--capture=build.sqlite"} {
		if !argPresent(buildArgs, want) {
			t.Fatalf("expected build args to contain %q, got %#v", want, buildArgs)
		}
	}
	if got := buildArgs[len(buildArgs)-1]; got != "." {
		t.Fatalf("expected build context last, got %q in %#v", got, buildArgs)
	}

	planArgs := shipPlanArgs(opts, paths)
	for _, want := range []string{"--include-crds", "--format", "json", "--output", "plan.json"} {
		if !argPresent(planArgs, want) {
			t.Fatalf("expected plan args to contain %q, got %#v", want, planArgs)
		}
	}
	for _, blocked := range []string{"--github-comment", "--verify-report", "--build-capture"} {
		if argPresent(planArgs, blocked) {
			t.Fatalf("expected plan args not to contain unsupported %q, got %#v", blocked, planArgs)
		}
	}

	applyArgs := shipApplyArgs(opts, paths)
	for _, want := range []string{"--yes", "--watch", "30s", "--capture=apply.sqlite", "--require-verified", "verify.json"} {
		if !argPresent(applyArgs, want) {
			t.Fatalf("expected apply args to contain %q, got %#v", want, applyArgs)
		}
	}
}

func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func readShipSummary(t *testing.T, path string) shipSummary {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	var summary shipSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	return summary
}

func argPresent(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

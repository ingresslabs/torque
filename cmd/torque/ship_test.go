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
	"github.com/spf13/pflag"
)

type recordingShipRunner struct {
	calls       []string
	fail        map[string]error
	optsByCall  map[string]shipOptions
	pathsByCall map[string]shipPaths
}

func (r *recordingShipRunner) RunBuild(_ *cobra.Command, opts shipOptions, paths shipPaths) error {
	return r.record("build", opts, paths)
}

func (r *recordingShipRunner) RunVerify(_ *cobra.Command, opts shipOptions, paths shipPaths) error {
	return r.record("verify", opts, paths)
}

func (r *recordingShipRunner) RunPlan(_ *cobra.Command, opts shipOptions, paths shipPaths) error {
	return r.record("plan", opts, paths)
}

func (r *recordingShipRunner) RunApply(_ *cobra.Command, opts shipOptions, paths shipPaths) error {
	return r.record("apply", opts, paths)
}

func (r *recordingShipRunner) RunExplain(_ *cobra.Command, opts shipOptions, paths shipPaths) error {
	return r.record("explain", opts, paths)
}

func (r *recordingShipRunner) record(name string, opts shipOptions, paths shipPaths) error {
	if r.optsByCall == nil {
		r.optsByCall = map[string]shipOptions{}
	}
	if r.pathsByCall == nil {
		r.pathsByCall = map[string]shipPaths{}
	}
	r.calls = append(r.calls, name)
	r.optsByCall[name] = opts
	r.pathsByCall[name] = paths
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

func TestShipCommandCoversEveryRegisteredFlag(t *testing.T) {
	cmd := newTestShipCommand(&recordingShipRunner{})
	covered := map[string]bool{
		"allow-network":        true,
		"allow-unpinned-bases": true,
		"apply-capture":        true,
		"atomic":               true,
		"attest":               true,
		"attest-dir":           true,
		"authfile":             true,
		"build":                true,
		"build-arg":            true,
		"build-capture":        true,
		"build-output":         true,
		"build-policy":         true,
		"build-policy-mode":    true,
		"build-secret":         true,
		"builder":              true,
		"cache-from":           true,
		"cache-to":             true,
		"capture-tag":          true,
		"chart":                true,
		"create-namespace":     true,
		"dockerfile":           true,
		"evidence-dir":         true,
		"explain-format":       true,
		"explain-output":       true,
		"hermetic":             true,
		"load":                 true,
		"locked":               true,
		"namespace":            true,
		"no-cache":             true,
		"no-capture":           true,
		"non-interactive":      true,
		"plan-only":            true,
		"plan-output":          true,
		"platform":             true,
		"provenance":           true,
		"push":                 true,
		"release":              true,
		"sandbox":              true,
		"sandbox-config":       true,
		"sbom":                 true,
		"secret-config":        true,
		"secret-provider":      true,
		"set":                  true,
		"set-file":             true,
		"set-string":           true,
		"skip-build":           true,
		"skip-explain":         true,
		"skip-verify":          true,
		"tag":                  true,
		"timeout":              true,
		"values":               true,
		"verify-fail-on":       true,
		"verify-mode":          true,
		"verify-report":        true,
		"version":              true,
		"wait":                 true,
		"watch":                true,
		"yes":                  true,
	}

	registered := map[string]bool{}
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		registered[flag.Name] = true
	})

	for name := range registered {
		if !covered[name] {
			t.Fatalf("ship flag %q is registered but not covered by tests", name)
		}
	}
	for name := range covered {
		if !registered[name] {
			t.Fatalf("ship flag %q is marked covered but is not registered", name)
		}
	}
}

func TestShipCommandEveryOptionFlowsToSteps(t *testing.T) {
	runner := &recordingShipRunner{}
	evidenceDir := t.TempDir()
	cmd := newTestShipCommand(runner)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--chart", "./chart",
		"--release", "api",
		"--namespace", "prod",
		"--version", "1.2.3",
		"--values", "values.yaml",
		"--values", "prod.yaml",
		"--set", "image.tag=dev",
		"--set", "replicas=2",
		"--set-string", "commit=abc123",
		"--set-file", "tls.crt=cert.pem",
		"--secret-provider", "file",
		"--secret-config", "secrets.yaml",
		"--build", "./app",
		"--dockerfile", "Containerfile",
		"--tag", "registry.example.test/api:dev",
		"--tag", "registry.example.test/api:latest",
		"--platform", "linux/amd64",
		"--platform", "linux/arm64",
		"--build-arg", "COMMIT=abc123",
		"--build-secret", "API_TOKEN",
		"--cache-from", "type=registry,ref=registry.example.test/api:cache",
		"--cache-to", "type=inline",
		"--push=false",
		"--load",
		"--no-cache",
		"--attest=true",
		"--attest-dir", filepath.Join(evidenceDir, "custom-attest"),
		"--sbom",
		"--provenance",
		"--hermetic",
		"--locked",
		"--allow-network",
		"--allow-unpinned-bases",
		"--build-policy", "policy.rego",
		"--build-policy-mode", "warn",
		"--builder", "tcp://127.0.0.1:1234",
		"--authfile", "auth.json",
		"--sandbox",
		"--sandbox-config", "sandbox.cfg",
		"--build-output", "tty",
		"--verify-mode", "warn",
		"--verify-fail-on", "medium",
		"--evidence-dir", evidenceDir,
		"--build-capture", filepath.Join(evidenceDir, "custom-build.sqlite"),
		"--apply-capture", filepath.Join(evidenceDir, "custom-apply.sqlite"),
		"--verify-report", filepath.Join(evidenceDir, "custom-verify.json"),
		"--plan-output", filepath.Join(evidenceDir, "custom-plan.json"),
		"--explain-output", filepath.Join(evidenceDir, "custom-explain.json"),
		"--explain-format", "json",
		"--capture-tag", "env=test",
		"--capture-tag", "team=platform",
		"--create-namespace",
		"--wait=false",
		"--atomic=false",
		"--yes",
		"--non-interactive",
		"--timeout", "90s",
		"--watch", "2s",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ship returned error: %v", err)
	}
	assertCalls(t, runner.calls, []string{"build", "verify", "plan", "apply", "explain"})

	opts := runner.optsByCall["apply"]
	if opts.push {
		t.Fatalf("expected --push=false to be preserved")
	}
	if !opts.load || !opts.noCache || !opts.hermetic || !opts.allowNetwork || !opts.allowUnpinned || !opts.sandbox {
		t.Fatalf("expected build toggles to be preserved: %#v", opts)
	}
	if opts.wait || opts.atomic || !opts.yes || !opts.nonInteractive || !opts.createNamespace {
		t.Fatalf("expected apply toggles to be preserved: %#v", opts)
	}
	if opts.timeout != 90*time.Second || opts.watch != 2*time.Second {
		t.Fatalf("unexpected timeout/watch: timeout=%s watch=%s", opts.timeout, opts.watch)
	}

	paths := runner.pathsByCall["apply"]
	if paths.EvidenceDir != evidenceDir {
		t.Fatalf("unexpected evidence dir: %s", paths.EvidenceDir)
	}
	if paths.BuildCapture != filepath.Join(evidenceDir, "custom-build.sqlite") {
		t.Fatalf("unexpected build capture path: %s", paths.BuildCapture)
	}
	if paths.ApplyCapture != filepath.Join(evidenceDir, "custom-apply.sqlite") {
		t.Fatalf("unexpected apply capture path: %s", paths.ApplyCapture)
	}

	buildArgs := shipBuildArgs(opts, paths)
	for _, want := range []string{
		"--tag", "registry.example.test/api:dev",
		"--tag", "registry.example.test/api:latest",
		"--file", "Containerfile",
		"--platform", "linux/amd64",
		"--platform", "linux/arm64",
		"--build-arg", "COMMIT=abc123",
		"--secret", "API_TOKEN",
		"--cache-from", "type=registry,ref=registry.example.test/api:cache",
		"--cache-to", "type=inline",
		"--load",
		"--no-cache",
		"--sbom",
		"--provenance",
		"--attest-dir", filepath.Join(evidenceDir, "custom-attest"),
		"--hermetic",
		"--allow-network",
		"--allow-unpinned-bases",
		"--policy", "policy.rego",
		"--policy-mode", "warn",
		"--builder", "tcp://127.0.0.1:1234",
		"--authfile", "auth.json",
		"--sandbox",
		"--sandbox-config", "sandbox.cfg",
		"--output", "tty",
		"--capture=" + filepath.Join(evidenceDir, "custom-build.sqlite"),
		"--capture-tag", "env=test",
		"--capture-tag", "team=platform",
		"--capture-tag", "workflow=ship",
		"--capture-tag", "release=api",
		"--capture-tag", "namespace=prod",
	} {
		if !argPresent(buildArgs, want) {
			t.Fatalf("expected build args to contain %q, got %#v", want, buildArgs)
		}
	}
	if argPresent(buildArgs, "--push") {
		t.Fatalf("expected build args not to contain --push when --push=false, got %#v", buildArgs)
	}
	if got := buildArgs[len(buildArgs)-1]; got != "./app" {
		t.Fatalf("expected build context last, got %q in %#v", got, buildArgs)
	}

	planArgs := shipPlanArgs(opts, paths)
	for _, want := range []string{
		"--chart", "./chart",
		"--release", "api",
		"--namespace", "prod",
		"--version", "1.2.3",
		"--values", "values.yaml",
		"--values", "prod.yaml",
		"--set", "image.tag=dev",
		"--set", "replicas=2",
		"--set-string", "commit=abc123",
		"--set-file", "tls.crt=cert.pem",
		"--secret-provider", "file",
		"--secret-config", "secrets.yaml",
		"--include-crds",
		"--format", "json",
		"--output", filepath.Join(evidenceDir, "custom-plan.json"),
	} {
		if !argPresent(planArgs, want) {
			t.Fatalf("expected plan args to contain %q, got %#v", want, planArgs)
		}
	}

	applyArgs := shipApplyArgs(opts, paths)
	for _, want := range []string{
		"--create-namespace",
		"--wait=false",
		"--atomic=false",
		"--yes",
		"--non-interactive",
		"--timeout", "1m30s",
		"--watch", "2s",
		"--capture=" + filepath.Join(evidenceDir, "custom-apply.sqlite"),
		"--require-verified", filepath.Join(evidenceDir, "custom-verify.json"),
	} {
		if !argPresent(applyArgs, want) {
			t.Fatalf("expected apply args to contain %q, got %#v", want, applyArgs)
		}
	}
}

func TestShipSkipOptionsAndNoCapture(t *testing.T) {
	runner := &recordingShipRunner{}
	evidenceDir := t.TempDir()
	cmd := newTestShipCommand(runner)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--chart", "./chart",
		"--release", "api",
		"--evidence-dir", evidenceDir,
		"--skip-build",
		"--skip-verify",
		"--skip-explain",
		"--no-capture",
		"--yes",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ship returned error: %v", err)
	}
	assertCalls(t, runner.calls, []string{"plan", "apply"})

	paths := runner.pathsByCall["apply"]
	if paths.BuildCapture != "" || paths.ApplyCapture != "" {
		t.Fatalf("expected no capture paths when --no-capture is set, got %#v", paths)
	}
	applyArgs := shipApplyArgs(runner.optsByCall["apply"], paths)
	for _, blocked := range []string{"--capture=", "--require-verified"} {
		for _, arg := range applyArgs {
			if strings.HasPrefix(arg, blocked) {
				t.Fatalf("expected apply args not to contain %q, got %#v", blocked, applyArgs)
			}
		}
	}
}

func TestShipValidationRejectsInvalidOptionCombinations(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing build",
			args: []string{"--chart", "./chart", "--release", "api", "--tag", "repo/api:dev"},
			want: "--build is required",
		},
		{
			name: "missing tag",
			args: []string{"--chart", "./chart", "--release", "api", "--build", "."},
			want: "--tag is required",
		},
		{
			name: "nonpositive timeout",
			args: []string{"--chart", "./chart", "--release", "api", "--build", ".", "--tag", "repo/api:dev", "--timeout", "0s"},
			want: "--timeout must be > 0",
		},
		{
			name: "watch with plan only",
			args: []string{"--chart", "./chart", "--release", "api", "--build", ".", "--tag", "repo/api:dev", "--plan-only", "--watch", "1s"},
			want: "--watch cannot be combined with --plan-only",
		},
		{
			name: "no capture still explaining",
			args: []string{"--chart", "./chart", "--release", "api", "--build", ".", "--tag", "repo/api:dev", "--no-capture"},
			want: "--no-capture requires --skip-explain or --plan-only",
		},
		{
			name: "invalid capture tag",
			args: []string{"--chart", "./chart", "--release", "api", "--build", ".", "--tag", "repo/api:dev", "--capture-tag", "bad"},
			want: "--capture-tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTestShipCommand(&recordingShipRunner{})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)
			err := cmd.ExecuteContext(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
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

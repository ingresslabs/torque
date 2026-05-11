package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const releaseAutopilotAPIVersion = "torque.dev/release-autopilot/v1"

type releaseAutopilotOptions struct {
	ProofSource     string
	Attach          []string
	OutDir          string
	Chart           string
	Release         string
	Namespace       string
	Policy          string
	Key             string
	Pub             string
	Actor           string
	Operation       string
	Allow           []string
	FailBelow       int
	Format          string
	Execute         bool
	Yes             bool
	AutoRollback    bool
	SLO             string
	RequireVerified string
	ApplyArgs       []string
}

type releaseAutopilotReport struct {
	APIVersion  string                       `json:"apiVersion"`
	GeneratedAt string                       `json:"generatedAt"`
	Mode        string                       `json:"mode"`
	Source      string                       `json:"source"`
	OutDir      string                       `json:"outDir"`
	Release     string                       `json:"release,omitempty"`
	Namespace   string                       `json:"namespace,omitempty"`
	Chart       string                       `json:"chart,omitempty"`
	Passed      bool                         `json:"passed"`
	Artifacts   releaseAutopilotArtifacts    `json:"artifacts"`
	Apply       *releaseAutopilotApplyReport `json:"apply,omitempty"`
	Gate        proofGateReport              `json:"gate"`
	Score       releaseScoreReport           `json:"score"`
	Flight      releaseFlight                `json:"flight"`
	Replay      flightReplayReport           `json:"replay"`
	Explain     flightExplainReport          `json:"explain"`
	AgentPolicy *agentPolicyReport           `json:"agentPolicy,omitempty"`
	AgentRun    *agentRunReport              `json:"agentRun,omitempty"`
	Attestation *proofAttestation            `json:"attestation,omitempty"`
	Checks      []releaseAutopilotCheck      `json:"checks"`
}

type releaseAutopilotArtifacts struct {
	ProofGraph   string `json:"proofGraph"`
	ProofHTML    string `json:"proofHtml"`
	Gate         string `json:"gate"`
	Score        string `json:"score"`
	Flight       string `json:"flight"`
	Replay       string `json:"replay"`
	Explain      string `json:"explain"`
	AgentRequest string `json:"agentRequest,omitempty"`
	AgentPolicy  string `json:"agentPolicy,omitempty"`
	AgentRun     string `json:"agentRun,omitempty"`
	Attestation  string `json:"attestation,omitempty"`
	ApplyProof   string `json:"applyProof,omitempty"`
	ApplyCapture string `json:"applyCapture,omitempty"`
}

type releaseAutopilotApplyReport struct {
	Executed bool     `json:"executed"`
	Command  []string `json:"command,omitempty"`
	ExitCode int      `json:"exitCode,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type releaseAutopilotCheck struct {
	ID      string `json:"id"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

func newReleaseAutopilotCommand() *cobra.Command {
	opts := releaseAutopilotOptions{
		Actor:     "release-autopilot",
		Operation: "apply",
		Allow:     []string{"apply"},
		FailBelow: 90,
		Format:    "text",
	}
	cmd := &cobra.Command{
		Use:   "autopilot [proof-source]",
		Short: "Run a proof-backed release autopilot",
		Long:  "Run a proof-backed release autopilot that builds a signed graph, evaluates the release gate, scores readiness, records and replays the release flight, checks agent authorization, and signs the release verdict. By default this is non-mutating and operates on an existing proof source; use --execute --yes to run torque apply first.",
		Args:  cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "text", "json":
			default:
				return fmt.Errorf("unsupported --format %q (expected text or json)", opts.Format)
			}
			if len(args) > 0 && strings.TrimSpace(opts.ProofSource) != "" && strings.TrimSpace(args[0]) != strings.TrimSpace(opts.ProofSource) {
				return fmt.Errorf("proof source supplied both as argument and --proof-source")
			}
			if opts.Execute {
				if !opts.Yes {
					return fmt.Errorf("--execute requires --yes")
				}
				if strings.TrimSpace(opts.Chart) == "" {
					return fmt.Errorf("--execute requires --chart")
				}
				if strings.TrimSpace(opts.Release) == "" {
					return fmt.Errorf("--execute requires --release")
				}
			}
			if strings.TrimSpace(opts.SLO) != "" && !opts.AutoRollback {
				return fmt.Errorf("--slo requires --auto-rollback")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.ProofSource = args[0]
			}
			report, err := runReleaseAutopilot(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderReleaseAutopilotText(cmd.OutOrStdout(), report)
			}
			if !report.Passed {
				return fmt.Errorf("release autopilot blocked")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ProofSource, "proof-source", "", "Existing proof source, proof directory, or proof graph; auto-detects common local proof paths when omitted")
	cmd.Flags().StringArrayVar(&opts.Attach, "attach", nil, "Attach additional evidence files or directories (repeatable)")
	cmd.Flags().StringVar(&opts.OutDir, "out-dir", "", "Directory for autopilot artifacts (default: ./torque-autopilot-<release>-<timestamp>)")
	cmd.Flags().StringVar(&opts.Chart, "chart", "", "Chart reference recorded in the report and used by --execute")
	cmd.Flags().StringVar(&opts.Release, "release", "", "Helm release name or release tag")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "Release namespace")
	cmd.Flags().StringVar(&opts.Policy, "policy", "", "Optional proof gate policy file")
	cmd.Flags().StringVar(&opts.Key, "key", "", "Sign graph and attestation with an ed25519 key JSON file from torque stack keygen")
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON for graph verification")
	cmd.Flags().StringVar(&opts.Actor, "actor", "release-autopilot", "Agent actor identity to record")
	cmd.Flags().StringVar(&opts.Operation, "operation", "apply", "Agent operation to authorize")
	cmd.Flags().StringArrayVar(&opts.Allow, "allow", []string{"apply"}, "Allowed agent operation (repeatable or comma-separated)")
	cmd.Flags().IntVar(&opts.FailBelow, "fail-below", 90, "Block when release score is below this value")
	cmd.Flags().BoolVar(&opts.Execute, "execute", false, "Run torque apply before proof orchestration")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Confirm --execute apply run")
	cmd.Flags().BoolVar(&opts.AutoRollback, "auto-rollback", false, "Pass --auto-rollback to torque apply when --execute is set")
	cmd.Flags().StringVar(&opts.SLO, "slo", "", "Rollout SLO YAML passed to torque apply when --execute is set")
	cmd.Flags().StringVar(&opts.RequireVerified, "require-verified", "", "Verifier report required by torque apply when --execute is set")
	cmd.Flags().StringArrayVar(&opts.ApplyArgs, "apply-arg", nil, "Additional raw argument passed to torque apply when --execute is set (repeatable)")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Release Autopilot Flags")
	return cmd
}

func runReleaseAutopilot(ctx context.Context, opts releaseAutopilotOptions) (releaseAutopilotReport, error) {
	now := time.Now().UTC()
	source := strings.TrimSpace(opts.ProofSource)
	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		outDir = defaultReleaseAutopilotOutDir(opts, now)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return releaseAutopilotReport{}, fmt.Errorf("create autopilot output directory: %w", err)
	}
	artifacts := releaseAutopilotArtifacts{
		ProofGraph: filepath.Join(outDir, "proof.graph.json"),
		ProofHTML:  filepath.Join(outDir, "proof.html"),
		Gate:       filepath.Join(outDir, "proof.gate.json"),
		Score:      filepath.Join(outDir, "release-score.json"),
		Flight:     filepath.Join(outDir, "release.flight.torque"),
		Replay:     filepath.Join(outDir, "flight-replay.json"),
		Explain:    filepath.Join(outDir, "flight-explain.json"),
	}
	var applyReport *releaseAutopilotApplyReport
	if opts.Execute {
		artifacts.ApplyProof = filepath.Join(outDir, "apply-proof.json")
		artifacts.ApplyCapture = filepath.Join(outDir, "apply.sqlite")
		source = artifacts.ApplyProof
		report, err := runReleaseAutopilotApply(ctx, opts, artifacts.ApplyProof, artifacts.ApplyCapture)
		applyReport = &report
		if err != nil {
			if _, statErr := os.Stat(artifacts.ApplyProof); statErr != nil {
				return releaseAutopilotReport{}, err
			}
		}
	}
	if source == "" {
		detected, err := detectReleaseAutopilotProofSource()
		if err != nil {
			return releaseAutopilotReport{}, err
		}
		source = detected
	}

	graph, err := buildProofGraph(source, opts.Attach)
	if err != nil {
		return releaseAutopilotReport{}, err
	}
	applyReleaseAutopilotMetadata(&graph, opts)
	absolutizeProofGraphFilePaths(&graph, source)
	finalizeProofGraph(&graph)
	if strings.TrimSpace(opts.Key) != "" {
		if err := signProofGraph(&graph, opts.Key); err != nil {
			return releaseAutopilotReport{}, err
		}
	}
	if err := writeJSONFileEnsured(artifacts.ProofGraph, graph); err != nil {
		return releaseAutopilotReport{}, fmt.Errorf("write autopilot proof graph: %w", err)
	}
	if err := writeProofGraphHTML(artifacts.ProofHTML, graph); err != nil {
		return releaseAutopilotReport{}, err
	}

	policy, err := loadProofGatePolicy(opts.Policy)
	if err != nil {
		return releaseAutopilotReport{}, err
	}
	gate, err := gateProofSource(artifacts.ProofGraph, policy, proofGateOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releaseAutopilotReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Gate, gate); err != nil {
		return releaseAutopilotReport{}, fmt.Errorf("write autopilot gate: %w", err)
	}
	score, err := scoreProofSource(artifacts.ProofGraph, releaseScoreOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releaseAutopilotReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Score, score); err != nil {
		return releaseAutopilotReport{}, fmt.Errorf("write autopilot score: %w", err)
	}
	flight, err := recordReleaseFlight(artifacts.ProofGraph, flightRecordOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releaseAutopilotReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Flight, flight); err != nil {
		return releaseAutopilotReport{}, fmt.Errorf("write autopilot flight: %w", err)
	}
	replay, err := replayReleaseFlight(artifacts.Flight)
	if err != nil {
		return releaseAutopilotReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Replay, replay); err != nil {
		return releaseAutopilotReport{}, fmt.Errorf("write autopilot flight replay: %w", err)
	}
	explain, err := explainReleaseFlight(artifacts.Flight)
	if err != nil {
		return releaseAutopilotReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Explain, explain); err != nil {
		return releaseAutopilotReport{}, fmt.Errorf("write autopilot flight explain: %w", err)
	}

	var agentPolicy *agentPolicyReport
	var agentRun *agentRunReport
	if len(parseAgentAllowList(opts.Allow)) > 0 || strings.TrimSpace(opts.Operation) != "" {
		artifacts.AgentRequest = filepath.Join(outDir, "agent-request.json")
		artifacts.AgentPolicy = filepath.Join(outDir, "agent-policy.json")
		artifacts.AgentRun = filepath.Join(outDir, "agent-run.json")
		request := releaseAutopilotAgentRequest(opts, graph, artifacts.ProofGraph)
		if err := writeJSONFileEnsured(artifacts.AgentRequest, request); err != nil {
			return releaseAutopilotReport{}, fmt.Errorf("write autopilot agent request: %w", err)
		}
		policyReport, err := evaluateAgentPolicy(request, agentPolicyOptions{Proof: artifacts.ProofGraph, Policy: opts.Policy, Pub: opts.Pub, Allow: opts.Allow, RequireGate: true, Format: "json"})
		if err != nil {
			return releaseAutopilotReport{}, err
		}
		agentPolicy = &policyReport
		if err := writeJSONFileEnsured(artifacts.AgentPolicy, policyReport); err != nil {
			return releaseAutopilotReport{}, fmt.Errorf("write autopilot agent policy: %w", err)
		}
		runReport := agentRunReport{
			Version:     "v1",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Authorized:  policyReport.Allowed,
			Executed:    false,
			DryRun:      true,
			Request:     request,
			Policy:      policyReport,
			Message:     "authorized proof-backed release autopilot; execution remains explicit",
		}
		if !policyReport.Allowed {
			runReport.Message = "release autopilot denied by agent policy"
		}
		agentRun = &runReport
		if err := writeJSONFileEnsured(artifacts.AgentRun, runReport); err != nil {
			return releaseAutopilotReport{}, fmt.Errorf("write autopilot agent run: %w", err)
		}
	}

	var attestation *proofAttestation
	if strings.TrimSpace(opts.Key) != "" {
		artifacts.Attestation = filepath.Join(outDir, "release.attestation.json")
		verify := verifyProofGraph(graph, "", proofVerifyOptions{Pub: opts.Pub, RequireSignature: true, StrictFiles: policy.StrictFiles})
		built, err := buildProofAttestation(artifacts.ProofGraph, graph, verify, proofAttestOptions{Release: firstNonEmpty(opts.Release, graph.Release), Key: opts.Key, Pub: opts.Pub, RequireSignature: true, StrictFiles: policy.StrictFiles})
		if err != nil {
			return releaseAutopilotReport{}, err
		}
		if err := signProofAttestation(&built, opts.Key); err != nil {
			return releaseAutopilotReport{}, err
		}
		attestation = &built
		if err := writeJSONFileEnsured(artifacts.Attestation, built); err != nil {
			return releaseAutopilotReport{}, fmt.Errorf("write autopilot attestation: %w", err)
		}
	}

	report := releaseAutopilotReport{
		APIVersion:  releaseAutopilotAPIVersion,
		GeneratedAt: now.Format(time.RFC3339Nano),
		Mode:        mapBool(opts.Execute, "execute", "evidence"),
		Source:      source,
		OutDir:      outDir,
		Release:     firstNonEmpty(opts.Release, graph.Release),
		Namespace:   firstNonEmpty(opts.Namespace, graph.Namespace),
		Chart:       firstNonEmpty(opts.Chart, graph.Chart),
		Artifacts:   artifacts,
		Apply:       applyReport,
		Gate:        gate,
		Score:       score,
		Flight:      flight,
		Replay:      replay,
		Explain:     explain,
		AgentPolicy: agentPolicy,
		AgentRun:    agentRun,
		Attestation: attestation,
	}
	report.Checks = buildReleaseAutopilotChecks(report, opts)
	report.Passed = true
	for _, check := range report.Checks {
		if !check.Passed {
			report.Passed = false
			break
		}
	}
	return report, nil
}

func runReleaseAutopilotApply(ctx context.Context, opts releaseAutopilotOptions, proofPath, capturePath string) (releaseAutopilotApplyReport, error) {
	args := []string{"apply", "--chart", strings.TrimSpace(opts.Chart), "--release", strings.TrimSpace(opts.Release), "--predict", "--proof-bundle", proofPath, "--capture", capturePath, "--yes"}
	if strings.TrimSpace(opts.Namespace) != "" {
		args = append(args, "--namespace", strings.TrimSpace(opts.Namespace))
	}
	if opts.AutoRollback {
		args = append(args, "--auto-rollback")
	}
	if strings.TrimSpace(opts.SLO) != "" {
		args = append(args, "--slo", strings.TrimSpace(opts.SLO))
	}
	if strings.TrimSpace(opts.RequireVerified) != "" {
		args = append(args, "--require-verified", strings.TrimSpace(opts.RequireVerified))
	}
	args = append(args, opts.ApplyArgs...)
	report := releaseAutopilotApplyReport{Executed: true, Command: append([]string{os.Args[0]}, args...)}
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Run()
	if err == nil {
		return report, nil
	}
	report.Error = err.Error()
	if exitErr, ok := err.(*exec.ExitError); ok {
		report.ExitCode = exitErr.ExitCode()
	}
	return report, fmt.Errorf("release autopilot apply failed: %w", err)
}

func applyReleaseAutopilotMetadata(graph *proofGraph, opts releaseAutopilotOptions) {
	if strings.TrimSpace(opts.Release) != "" {
		graph.Release = strings.TrimSpace(opts.Release)
	}
	if strings.TrimSpace(opts.Namespace) != "" {
		graph.Namespace = strings.TrimSpace(opts.Namespace)
	}
	if strings.TrimSpace(opts.Chart) != "" {
		graph.Chart = strings.TrimSpace(opts.Chart)
	}
}

func absolutizeProofGraphFilePaths(graph *proofGraph, source string) {
	sourceBase := proofSourceBaseDir(source)
	for i := range graph.Artifacts {
		path := strings.TrimSpace(graph.Artifacts[i].Path)
		if path == "" || filepath.IsAbs(path) {
			continue
		}
		graph.Artifacts[i].Path = filepath.ToSlash(resolveReleaseAutopilotArtifactPath(path, sourceBase))
	}
}

func proofSourceBaseDir(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "."
	}
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return source
	}
	return filepath.Dir(source)
}

func resolveReleaseAutopilotArtifactPath(path, sourceBase string) string {
	if sourceBase != "" {
		candidate := filepath.Join(sourceBase, filepath.FromSlash(path))
		if _, err := os.Stat(candidate); err == nil {
			if abs, absErr := filepath.Abs(candidate); absErr == nil {
				return abs
			}
			return candidate
		}
	}
	if _, err := os.Stat(path); err == nil {
		if abs, absErr := filepath.Abs(path); absErr == nil {
			return abs
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func releaseAutopilotAgentRequest(opts releaseAutopilotOptions, graph proofGraph, proofPath string) agentPolicyRequest {
	operation := normalizeAgentOperation(firstNonEmpty(opts.Operation, "apply"))
	return agentPolicyRequest{
		Version:     "v1",
		Actor:       firstNonEmpty(opts.Actor, "release-autopilot"),
		Operation:   operation,
		Command:     releaseAutopilotAgentCommand(opts, operation),
		Release:     firstNonEmpty(opts.Release, graph.Release),
		Namespace:   firstNonEmpty(opts.Namespace, graph.Namespace),
		Proof:       proofPath,
		RequireGate: true,
		Reason:      "release autopilot proof-backed operation",
	}
}

func releaseAutopilotAgentCommand(opts releaseAutopilotOptions, operation string) []string {
	if opts.Execute && strings.TrimSpace(opts.Chart) != "" && strings.TrimSpace(opts.Release) != "" {
		cmd := []string{"torque", "apply", "--chart", strings.TrimSpace(opts.Chart), "--release", strings.TrimSpace(opts.Release)}
		if strings.TrimSpace(opts.Namespace) != "" {
			cmd = append(cmd, "--namespace", strings.TrimSpace(opts.Namespace))
		}
		return cmd
	}
	if operation == "" {
		operation = "apply"
	}
	return []string{"torque", operation}
}

func buildReleaseAutopilotChecks(report releaseAutopilotReport, opts releaseAutopilotOptions) []releaseAutopilotCheck {
	checks := []releaseAutopilotCheck{
		{ID: "proof.graph", Passed: strings.TrimSpace(report.Artifacts.ProofGraph) != "", Message: "proof graph was written"},
		{ID: "proof.gate", Passed: report.Gate.Passed, Message: "release gate passed"},
		{ID: "release.score", Passed: opts.FailBelow <= 0 || report.Score.Score >= opts.FailBelow, Message: fmt.Sprintf("release score is at least %d", opts.FailBelow)},
		{ID: "flight.replay", Passed: report.Replay.Passed, Message: "release flight replay passed"},
	}
	if opts.Execute {
		passed := report.Apply != nil && report.Apply.Executed && report.Apply.Error == ""
		checks = append(checks, releaseAutopilotCheck{ID: "apply.executed", Passed: passed, Message: "torque apply completed"})
	}
	if report.AgentPolicy != nil {
		checks = append(checks, releaseAutopilotCheck{ID: "agent.policy", Passed: report.AgentPolicy.Allowed, Message: "agent policy authorized operation"})
	}
	if report.AgentRun != nil {
		checks = append(checks, releaseAutopilotCheck{ID: "agent.run", Passed: report.AgentRun.Authorized && !report.AgentRun.Executed, Message: "agent run authorization record was written"})
	}
	if strings.TrimSpace(opts.Key) != "" {
		checks = append(checks, releaseAutopilotCheck{ID: "proof.attestation", Passed: report.Attestation != nil && report.Attestation.Verified && report.Attestation.Signature != nil, Message: "signed release attestation was written"})
	}
	return checks
}

func detectReleaseAutopilotProofSource() (string, error) {
	for _, candidate := range []string{"apply-proof.json", "proof.graph.json", "torque-sim-proof"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("proof source is required; pass a proof graph/path or --proof-source")
}

func defaultReleaseAutopilotOutDir(opts releaseAutopilotOptions, now time.Time) string {
	slug := sanitizeFilename(firstNonEmpty(opts.Release, opts.Chart, "release"))
	if slug == "" {
		slug = "release"
	}
	return fmt.Sprintf("torque-autopilot-%s-%s", slug, now.Format("20060102-150405"))
}

func renderReleaseAutopilotText(out io.Writer, report releaseAutopilotReport) {
	fmt.Fprintf(out, "Release autopilot: %s\n", strings.ToUpper(passFail(report.Passed)))
	if report.Release != "" {
		fmt.Fprintf(out, "Release: %s\n", report.Release)
	}
	if report.Namespace != "" {
		fmt.Fprintf(out, "Namespace: %s\n", report.Namespace)
	}
	fmt.Fprintf(out, "Mode: %s\n", report.Mode)
	fmt.Fprintf(out, "Out dir: %s\n", report.OutDir)
	fmt.Fprintf(out, "Gate: %s\n", strings.ToUpper(passFail(report.Gate.Passed)))
	fmt.Fprintf(out, "Score: %d (%s)\n", report.Score.Score, report.Score.Grade)
	fmt.Fprintf(out, "Flight events: %d\n", len(report.Flight.Timeline))
	if report.Attestation != nil {
		fmt.Fprintf(out, "Attestation: %s\n", report.Artifacts.Attestation)
	}
	for _, check := range report.Checks {
		if check.Passed {
			continue
		}
		fmt.Fprintf(out, "Blocked: %s: %s\n", check.ID, check.Message)
	}
}

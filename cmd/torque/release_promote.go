package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	releasePromotionAPIVersion        = "torque.dev/release-promotion/v1"
	releasePromotionKind              = "ReleasePromotion"
	releasePromotionDecisionKind      = "ReleasePromotionDecision"
	releasePromotionTrafficAPIVersion = "torque.dev/release-traffic-state/v1"
	releasePromotionTrafficKind       = "ReleaseTrafficState"
)

type releasePromoteOptions struct {
	ProofSource    string
	OutDir         string
	Strategy       string
	Steps          string
	AnalysisWindow string
	SLO            string
	Smoke          string
	Policy         string
	Pub            string
	Key            string
	Release        string
	Namespace      string
	Actor          string
	Provider       string
	StateOut       string
	Allow          []string
	FailBelow      int
	RequireGate    bool
	RollbackOnFail bool
	Preview        bool
	SwitchTraffic  bool
	Execute        bool
	Yes            bool
	Format         string
}

type releasePromotionReport struct {
	APIVersion    string                         `json:"apiVersion"`
	Kind          string                         `json:"kind"`
	GeneratedAt   string                         `json:"generatedAt"`
	Mode          string                         `json:"mode"`
	Provider      string                         `json:"provider"`
	Source        string                         `json:"source"`
	OutDir        string                         `json:"outDir"`
	Strategy      string                         `json:"strategy"`
	Release       string                         `json:"release,omitempty"`
	Namespace     string                         `json:"namespace,omitempty"`
	Passed        bool                           `json:"passed"`
	Artifacts     releasePromotionArtifacts      `json:"artifacts"`
	Gate          proofGateReport                `json:"gate"`
	Score         releaseScoreReport             `json:"score"`
	Flight        releaseFlight                  `json:"flight"`
	SLO           *applyRollbackSLO              `json:"slo,omitempty"`
	Smoke         *releasePromotionSmoke         `json:"smoke,omitempty"`
	Canary        *releasePromotionCanary        `json:"canary,omitempty"`
	BlueGreen     *releasePromotionBlueGreen     `json:"blueGreen,omitempty"`
	ProviderState *releasePromotionProviderState `json:"providerState,omitempty"`
	AgentPolicy   *agentPolicyReport             `json:"agentPolicy,omitempty"`
	AgentRun      *agentRunReport                `json:"agentRun,omitempty"`
	Attestation   *proofAttestation              `json:"attestation,omitempty"`
	Checks        []releasePromotionCheck        `json:"checks"`
}

type releasePromotionArtifacts struct {
	Report        string `json:"report"`
	Decision      string `json:"decision"`
	PromotedGraph string `json:"promotedGraph"`
	Gate          string `json:"gate"`
	Score         string `json:"score"`
	Flight        string `json:"flight"`
	AgentRequest  string `json:"agentRequest,omitempty"`
	AgentPolicy   string `json:"agentPolicy,omitempty"`
	AgentRun      string `json:"agentRun,omitempty"`
	Attestation   string `json:"attestation,omitempty"`
	ProviderState string `json:"providerState,omitempty"`
}

type releasePromotionCheck struct {
	ID      string `json:"id"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

type releasePromotionCanary struct {
	Steps          []releasePromotionStep `json:"steps"`
	AnalysisWindow string                 `json:"analysisWindow"`
	RollbackOnFail bool                   `json:"rollbackOnFail"`
}

type releasePromotionBlueGreen struct {
	Preview       bool                   `json:"preview"`
	SwitchTraffic bool                   `json:"switchTraffic"`
	Phases        []releasePromotionStep `json:"phases"`
}

type releasePromotionStep struct {
	Index    int                      `json:"index"`
	Name     string                   `json:"name"`
	Status   string                   `json:"status"`
	Traffic  releasePromotionTraffic  `json:"traffic"`
	Analysis releasePromotionAnalysis `json:"analysis,omitempty"`
}

type releasePromotionTraffic struct {
	Stable int `json:"stable,omitempty"`
	Canary int `json:"canary,omitempty"`
	Blue   int `json:"blue,omitempty"`
	Green  int `json:"green,omitempty"`
}

type releasePromotionAnalysis struct {
	Window    string   `json:"window,omitempty"`
	SLOPassed bool     `json:"sloPassed"`
	Signals   []string `json:"signals,omitempty"`
}

type releasePromotionSmoke struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Status string `json:"status,omitempty"`
}

type releasePromotionProviderState struct {
	APIVersion   string                  `json:"apiVersion"`
	Kind         string                  `json:"kind"`
	GeneratedAt  string                  `json:"generatedAt"`
	Provider     string                  `json:"provider"`
	Applied      bool                    `json:"applied"`
	Strategy     string                  `json:"strategy"`
	Release      string                  `json:"release,omitempty"`
	Namespace    string                  `json:"namespace,omitempty"`
	Source       string                  `json:"source"`
	FinalTraffic releasePromotionTraffic `json:"finalTraffic"`
	Steps        []releasePromotionStep  `json:"steps,omitempty"`
}

type releasePromotionDecisionLog struct {
	APIVersion    string                     `json:"apiVersion"`
	Kind          string                     `json:"kind"`
	GeneratedAt   string                     `json:"generatedAt"`
	Mode          string                     `json:"mode"`
	Provider      string                     `json:"provider"`
	Strategy      string                     `json:"strategy"`
	Release       string                     `json:"release,omitempty"`
	Namespace     string                     `json:"namespace,omitempty"`
	Passed        bool                       `json:"passed"`
	Source        string                     `json:"source"`
	GraphSHA256   string                     `json:"graphSha256,omitempty"`
	Score         int                        `json:"score"`
	Grade         string                     `json:"grade"`
	Checks        []releasePromotionCheck    `json:"checks"`
	Canary        *releasePromotionCanary    `json:"canary,omitempty"`
	BlueGreen     *releasePromotionBlueGreen `json:"blueGreen,omitempty"`
	ProviderState string                     `json:"providerState,omitempty"`
}

func newReleasePromoteCommand() *cobra.Command {
	opts := releasePromoteOptions{
		Steps:          "5,25,50,100",
		AnalysisWindow: "5m",
		Actor:          "release-promote",
		Provider:       "evidence",
		Allow:          []string{"release-promote"},
		FailBelow:      90,
		RequireGate:    true,
		RollbackOnFail: true,
		Format:         "text",
	}
	cmd := &cobra.Command{
		Use:   "promote <proof-graph>",
		Short: "Promote a release with proof-backed canary or blue/green gates",
		Long:  "Promote a release using proof-backed progressive delivery. Canary and blue/green strategies evaluate the release gate, score, flight evidence, agent policy, SLO/smoke inputs, and write portable promotion proof before any traffic-changing execution.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if _, err := normalizeReleasePromotionStrategy(opts.Strategy); err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "text", "json":
			default:
				return fmt.Errorf("unsupported --format %q (expected text or json)", opts.Format)
			}
			if _, err := normalizeReleasePromotionProvider(opts.Provider); err != nil {
				return err
			}
			if _, err := time.ParseDuration(strings.TrimSpace(opts.AnalysisWindow)); err != nil {
				return fmt.Errorf("invalid --analysis-window: %w", err)
			}
			if opts.Execute && !opts.Yes {
				return fmt.Errorf("--execute requires --yes")
			}
			if opts.Execute && normalizeReleasePromotionProviderNoErr(opts.Provider) != "file" {
				return fmt.Errorf("--execute currently requires --provider file")
			}
			strategy := normalizeReleasePromotionStrategyNoErr(opts.Strategy)
			if strategy == "canary" {
				if _, err := parseReleasePromotionSteps(opts.Steps); err != nil {
					return err
				}
			}
			if strategy != "blue-green" && opts.SwitchTraffic {
				return fmt.Errorf("--switch-traffic is only supported with --strategy blue-green")
			}
			if strategy != "blue-green" && strings.TrimSpace(opts.Smoke) != "" {
				return fmt.Errorf("--smoke is only supported with --strategy blue-green")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ProofSource = args[0]
			report, err := runReleasePromote(opts)
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
				renderReleasePromotionText(cmd.OutOrStdout(), report)
			}
			if !report.Passed {
				return fmt.Errorf("release promotion blocked")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Strategy, "strategy", "", "Promotion strategy: canary or blue-green")
	cmd.Flags().StringVar(&opts.Steps, "steps", "5,25,50,100", "Canary traffic steps as percentages (comma-separated)")
	cmd.Flags().StringVar(&opts.AnalysisWindow, "analysis-window", "5m", "Per-step analysis window")
	cmd.Flags().StringVar(&opts.SLO, "slo", "", "Rollout SLO YAML used as promotion evidence")
	cmd.Flags().StringVar(&opts.Smoke, "smoke", "", "Smoke test result file for blue/green promotion evidence")
	cmd.Flags().StringVar(&opts.OutDir, "out-dir", "", "Directory for promotion artifacts (default: ./torque-promote-<strategy>-<release>-<timestamp>)")
	cmd.Flags().StringVar(&opts.Policy, "policy", "", "Optional proof gate policy file")
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON for graph verification")
	cmd.Flags().StringVar(&opts.Key, "key", "", "Sign promoted graph and attestation with an ed25519 key JSON file from torque stack keygen")
	cmd.Flags().StringVar(&opts.Release, "release", "", "Release name override")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "Release namespace override")
	cmd.Flags().StringVar(&opts.Actor, "actor", "release-promote", "Agent actor identity to record")
	cmd.Flags().StringVar(&opts.Provider, "provider", "evidence", "Traffic provider: evidence or file")
	cmd.Flags().StringVar(&opts.StateOut, "state-out", "", "File provider state output path (default: <out-dir>/traffic-state.json)")
	cmd.Flags().StringArrayVar(&opts.Allow, "allow", []string{"release-promote"}, "Allowed agent operation (repeatable or comma-separated)")
	cmd.Flags().IntVar(&opts.FailBelow, "fail-below", 90, "Block when release score is below this value")
	cmd.Flags().BoolVar(&opts.RequireGate, "require-gate", true, "Require proof gate success before promotion")
	cmd.Flags().BoolVar(&opts.RollbackOnFail, "rollback-on-fail", true, "Record rollback-on-failure intent for canary promotion")
	cmd.Flags().BoolVar(&opts.Preview, "preview", false, "Record blue/green preview before traffic switch")
	cmd.Flags().BoolVar(&opts.SwitchTraffic, "switch-traffic", false, "Record blue/green traffic switch to green")
	cmd.Flags().BoolVar(&opts.Execute, "execute", false, "Execute the selected traffic provider after proof checks pass")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Confirm --execute")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Release Promote Flags")
	return cmd
}

func runReleasePromote(opts releasePromoteOptions) (releasePromotionReport, error) {
	now := time.Now().UTC()
	strategy, err := normalizeReleasePromotionStrategy(opts.Strategy)
	if err != nil {
		return releasePromotionReport{}, err
	}
	provider, err := normalizeReleasePromotionProvider(opts.Provider)
	if err != nil {
		return releasePromotionReport{}, err
	}
	source := strings.TrimSpace(opts.ProofSource)
	graph, _, err := loadOrBuildProofGraph(source)
	if err != nil {
		return releasePromotionReport{}, err
	}
	applyReleasePromotionMetadata(&graph, opts)
	absolutizeProofGraphFilePaths(&graph, source)
	finalizeProofGraph(&graph)

	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		outDir = defaultReleasePromoteOutDir(strategy, graph, opts, now)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return releasePromotionReport{}, fmt.Errorf("create promotion output directory: %w", err)
	}
	artifacts := releasePromotionArtifacts{
		Report:        filepath.Join(outDir, "release-promotion.json"),
		Decision:      filepath.Join(outDir, "promotion-decision.json"),
		PromotedGraph: filepath.Join(outDir, "proof.promoted.graph.json"),
		Gate:          filepath.Join(outDir, "proof.gate.json"),
		Score:         filepath.Join(outDir, "release-score.json"),
		Flight:        filepath.Join(outDir, "release.flight.torque"),
		AgentRequest:  filepath.Join(outDir, "agent-request.json"),
		AgentPolicy:   filepath.Join(outDir, "agent-policy.json"),
		AgentRun:      filepath.Join(outDir, "agent-run.json"),
	}
	if strings.TrimSpace(opts.StateOut) != "" {
		artifacts.ProviderState = strings.TrimSpace(opts.StateOut)
	} else if provider == "file" {
		artifacts.ProviderState = filepath.Join(outDir, "traffic-state.json")
	}

	policy, err := loadProofGatePolicy(opts.Policy)
	if err != nil {
		return releasePromotionReport{}, err
	}
	gate, err := gateProofSource(source, policy, proofGateOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releasePromotionReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Gate, gate); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion gate: %w", err)
	}
	score, err := scoreProofSource(source, releaseScoreOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releasePromotionReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Score, score); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion score: %w", err)
	}
	flight, err := recordReleaseFlight(source, flightRecordOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releasePromotionReport{}, err
	}
	if err := writeJSONFileEnsured(artifacts.Flight, flight); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion flight: %w", err)
	}

	slo, err := loadApplyRollbackSLO(opts.SLO)
	if err != nil {
		return releasePromotionReport{}, err
	}
	smoke, err := loadReleasePromotionSmoke(opts.Smoke)
	if err != nil {
		return releasePromotionReport{}, err
	}

	report := releasePromotionReport{
		APIVersion:  releasePromotionAPIVersion,
		Kind:        releasePromotionKind,
		GeneratedAt: now.Format(time.RFC3339Nano),
		Mode:        mapBool(opts.Execute, "execute", "plan"),
		Provider:    provider,
		Source:      source,
		OutDir:      outDir,
		Strategy:    strategy,
		Release:     firstNonEmpty(opts.Release, graph.Release),
		Namespace:   firstNonEmpty(opts.Namespace, graph.Namespace),
		Artifacts:   artifacts,
		Gate:        gate,
		Score:       score,
		Flight:      flight,
		SLO:         slo,
		Smoke:       smoke,
	}
	if strategy == "canary" {
		steps, err := parseReleasePromotionSteps(opts.Steps)
		if err != nil {
			return releasePromotionReport{}, err
		}
		report.Canary = buildReleasePromotionCanary(steps, opts, graph)
	} else {
		report.BlueGreen = buildReleasePromotionBlueGreen(opts, graph)
	}

	request := releasePromotionAgentRequest(opts, report, source)
	if err := writeJSONFileEnsured(artifacts.AgentRequest, request); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion agent request: %w", err)
	}
	policyReport, err := evaluateAgentPolicy(request, agentPolicyOptions{Proof: source, Policy: opts.Policy, Pub: opts.Pub, Allow: opts.Allow, RequireGate: opts.RequireGate, Format: "json"})
	if err != nil {
		return releasePromotionReport{}, err
	}
	report.AgentPolicy = &policyReport
	if err := writeJSONFileEnsured(artifacts.AgentPolicy, policyReport); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion agent policy: %w", err)
	}
	runReport := agentRunReport{
		Version:     "v1",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Authorized:  policyReport.Allowed,
		Executed:    false,
		DryRun:      !opts.Execute,
		Request:     request,
		Policy:      policyReport,
		Message:     "authorized proof-backed release promotion; execution remains explicit",
	}
	if !policyReport.Allowed {
		runReport.Message = "release promotion denied by agent policy"
	}
	report.AgentRun = &runReport
	if err := writeJSONFileEnsured(artifacts.AgentRun, runReport); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion agent run: %w", err)
	}

	report.Checks = buildReleasePromotionChecks(report, opts, graph)
	report.Passed = releasePromotionChecksPassed(report.Checks)
	if provider == "file" {
		state := buildReleasePromotionProviderState(report, opts, source, now)
		if opts.Execute && report.Passed {
			state.Applied = true
		}
		report.ProviderState = &state
		if err := writeJSONFileEnsured(artifacts.ProviderState, state); err != nil {
			report.Checks = append(report.Checks, releasePromotionCheck{ID: "provider.file", Passed: false, Message: "file provider state could not be written: " + err.Error()})
		} else {
			report.Checks = append(report.Checks, releasePromotionCheck{ID: "provider.file", Passed: true, Message: "file provider state was written"})
		}
	}
	report.Passed = releasePromotionChecksPassed(report.Checks)

	decision := buildReleasePromotionDecisionLog(report, graph)
	if err := writeJSONFileEnsured(artifacts.Decision, decision); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion decision: %w", err)
	}
	promotedGraph := graph
	if err := attachReleasePromotionEvidence(&promotedGraph, report); err != nil {
		return releasePromotionReport{}, err
	}
	finalizeProofGraph(&promotedGraph)
	if strings.TrimSpace(opts.Key) != "" {
		if err := signProofGraph(&promotedGraph, opts.Key); err != nil {
			return releasePromotionReport{}, err
		}
	}
	if err := writeJSONFileEnsured(artifacts.PromotedGraph, promotedGraph); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promoted proof graph: %w", err)
	}

	if strings.TrimSpace(opts.Key) != "" {
		artifacts.Attestation = filepath.Join(outDir, "release-promotion.attestation.json")
		verify := verifyProofGraph(promotedGraph, "", proofVerifyOptions{Pub: opts.Pub, RequireSignature: true, StrictFiles: policy.StrictFiles})
		attestation, err := buildProofAttestation(artifacts.PromotedGraph, promotedGraph, verify, proofAttestOptions{
			Release:          firstNonEmpty(report.Release, promotedGraph.Release),
			Key:              opts.Key,
			Pub:              opts.Pub,
			RequireSignature: true,
			StrictFiles:      policy.StrictFiles,
		})
		if err != nil {
			return releasePromotionReport{}, err
		}
		if err := signProofAttestation(&attestation, opts.Key); err != nil {
			return releasePromotionReport{}, err
		}
		report.Attestation = &attestation
		report.Artifacts.Attestation = artifacts.Attestation
		if err := writeJSONFileEnsured(artifacts.Attestation, attestation); err != nil {
			return releasePromotionReport{}, fmt.Errorf("write promotion attestation: %w", err)
		}
		report.Checks = append(report.Checks, releasePromotionCheck{ID: "promotion.attestation", Passed: attestation.Verified && attestation.Signature != nil, Message: "signed promotion attestation was written"})
	}
	report.Artifacts = artifacts
	report.Passed = releasePromotionChecksPassed(report.Checks)
	if err := writeJSONFileEnsured(artifacts.Report, report); err != nil {
		return releasePromotionReport{}, fmt.Errorf("write promotion report: %w", err)
	}
	return report, nil
}

func normalizeReleasePromotionStrategy(value string) (string, error) {
	value = normalizeReleasePromotionStrategyNoErr(value)
	switch value {
	case "canary", "blue-green":
		return value, nil
	default:
		return "", fmt.Errorf("unsupported --strategy %q (expected canary or blue-green)", strings.TrimSpace(value))
	}
}

func normalizeReleasePromotionStrategyNoErr(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	switch value {
	case "bluegreen", "blue-green":
		return "blue-green"
	default:
		return value
	}
}

func normalizeReleasePromotionProvider(value string) (string, error) {
	value = normalizeReleasePromotionProviderNoErr(value)
	switch value {
	case "evidence", "file":
		return value, nil
	default:
		return "", fmt.Errorf("unsupported --provider %q (expected evidence or file)", strings.TrimSpace(value))
	}
}

func normalizeReleasePromotionProviderNoErr(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseReleasePromotionSteps(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("--steps is required for canary promotion")
	}
	parts := strings.Split(raw, ",")
	steps := make([]int, 0, len(parts))
	last := 0
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimSuffix(part, "%"))
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid canary step %q", part)
		}
		if n <= 0 || n > 100 {
			return nil, fmt.Errorf("canary step %d is outside 1..100", n)
		}
		if n <= last {
			return nil, fmt.Errorf("canary steps must be strictly increasing")
		}
		steps = append(steps, n)
		last = n
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("--steps did not contain any canary percentages")
	}
	if steps[len(steps)-1] != 100 {
		return nil, fmt.Errorf("canary steps must end at 100")
	}
	return steps, nil
}

func applyReleasePromotionMetadata(graph *proofGraph, opts releasePromoteOptions) {
	if strings.TrimSpace(opts.Release) != "" {
		graph.Release = strings.TrimSpace(opts.Release)
	}
	if strings.TrimSpace(opts.Namespace) != "" {
		graph.Namespace = strings.TrimSpace(opts.Namespace)
	}
}

func buildReleasePromotionCanary(steps []int, opts releasePromoteOptions, graph proofGraph) *releasePromotionCanary {
	status := "planned"
	if opts.Execute {
		status = "applied"
	}
	analysis := releasePromotionAnalysis{
		Window:    strings.TrimSpace(opts.AnalysisWindow),
		SLOPassed: !proofGateHasSLOFailure(graph) || graph.Summary.Rollback,
		Signals: []string{
			"proof gate passed",
			"release score threshold met",
			"runtime drift evidence present",
		},
	}
	out := &releasePromotionCanary{
		AnalysisWindow: strings.TrimSpace(opts.AnalysisWindow),
		RollbackOnFail: opts.RollbackOnFail,
	}
	for i, weight := range steps {
		stepStatus := status
		if weight < 100 {
			stepStatus = "advanced"
			if !opts.Execute {
				stepStatus = "planned"
			}
		}
		out.Steps = append(out.Steps, releasePromotionStep{
			Index:  i + 1,
			Name:   fmt.Sprintf("canary-%d", weight),
			Status: stepStatus,
			Traffic: releasePromotionTraffic{
				Stable: 100 - weight,
				Canary: weight,
			},
			Analysis: analysis,
		})
	}
	return out
}

func buildReleasePromotionBlueGreen(opts releasePromoteOptions, graph proofGraph) *releasePromotionBlueGreen {
	status := "planned"
	if opts.Execute {
		status = "applied"
	}
	analysis := releasePromotionAnalysis{
		Window:    strings.TrimSpace(opts.AnalysisWindow),
		SLOPassed: !proofGateHasSLOFailure(graph) || graph.Summary.Rollback,
		Signals: []string{
			"proof gate passed",
			"release score threshold met",
			"green environment available for preview",
		},
	}
	out := &releasePromotionBlueGreen{
		Preview:       opts.Preview,
		SwitchTraffic: opts.SwitchTraffic,
	}
	if opts.Preview {
		out.Phases = append(out.Phases, releasePromotionStep{
			Index:    len(out.Phases) + 1,
			Name:     "preview-green",
			Status:   status,
			Traffic:  releasePromotionTraffic{Blue: 100, Green: 0},
			Analysis: analysis,
		})
	}
	if strings.TrimSpace(opts.Smoke) != "" {
		out.Phases = append(out.Phases, releasePromotionStep{
			Index:    len(out.Phases) + 1,
			Name:     "smoke-green",
			Status:   status,
			Traffic:  releasePromotionTraffic{Blue: 100, Green: 0},
			Analysis: analysis,
		})
	}
	finalTraffic := releasePromotionTraffic{Blue: 100, Green: 0}
	if opts.SwitchTraffic {
		finalTraffic = releasePromotionTraffic{Blue: 0, Green: 100}
	}
	out.Phases = append(out.Phases, releasePromotionStep{
		Index:    len(out.Phases) + 1,
		Name:     "switch-traffic",
		Status:   status,
		Traffic:  finalTraffic,
		Analysis: analysis,
	})
	return out
}

func loadReleasePromotionSmoke(path string) (*releasePromotionSmoke, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	sha, size, err := sha256HexFileLocal(path)
	if err != nil {
		return nil, fmt.Errorf("read --smoke: %w", err)
	}
	return &releasePromotionSmoke{Path: path, SHA256: sha, Size: size, Status: firstNonEmpty(proofStatusForFile(path), "passed")}, nil
}

func releasePromotionAgentRequest(opts releasePromoteOptions, report releasePromotionReport, proofPath string) agentPolicyRequest {
	return agentPolicyRequest{
		Version:     "v1",
		Actor:       firstNonEmpty(opts.Actor, "release-promote"),
		Operation:   "release-promote",
		Command:     releasePromotionAgentCommand(opts, proofPath),
		Release:     report.Release,
		Namespace:   report.Namespace,
		Proof:       proofPath,
		RequireGate: opts.RequireGate,
		Reason:      "proof-backed progressive release promotion",
	}
}

func releasePromotionAgentCommand(opts releasePromoteOptions, proofPath string) []string {
	cmd := []string{"torque", "release", "promote", proofPath, "--strategy", normalizeReleasePromotionStrategyNoErr(opts.Strategy)}
	if normalizeReleasePromotionStrategyNoErr(opts.Strategy) == "canary" {
		cmd = append(cmd, "--steps", strings.TrimSpace(opts.Steps))
	}
	if strings.TrimSpace(opts.Namespace) != "" {
		cmd = append(cmd, "--namespace", strings.TrimSpace(opts.Namespace))
	}
	if opts.Execute {
		cmd = append(cmd, "--execute")
	}
	return cmd
}

func buildReleasePromotionChecks(report releasePromotionReport, opts releasePromoteOptions, graph proofGraph) []releasePromotionCheck {
	checks := []releasePromotionCheck{
		{ID: "strategy.valid", Passed: report.Strategy == "canary" || report.Strategy == "blue-green", Message: "promotion strategy is supported"},
		{ID: "proof.gate", Passed: !opts.RequireGate || report.Gate.Passed, Message: "release gate passed"},
		{ID: "release.score", Passed: opts.FailBelow <= 0 || report.Score.Score >= opts.FailBelow, Message: fmt.Sprintf("release score is at least %d", opts.FailBelow)},
		{ID: "flight.timeline", Passed: len(report.Flight.Timeline) > 0, Message: "release flight timeline is available"},
	}
	if report.AgentPolicy != nil {
		checks = append(checks, releasePromotionCheck{ID: "agent.policy", Passed: report.AgentPolicy.Allowed, Message: "agent policy authorized release promotion"})
	}
	if report.AgentRun != nil {
		checks = append(checks, releasePromotionCheck{ID: "agent.run", Passed: report.AgentRun.Authorized && !report.AgentRun.Executed, Message: "agent run authorization record was written"})
	}
	if report.SLO != nil {
		checks = append(checks, releasePromotionCheck{ID: "slo.evidence", Passed: report.SLO.SHA256 != "", Message: "SLO evidence was loaded"})
		checks = append(checks, releasePromotionCheck{ID: "slo.rollback", Passed: !proofGateHasSLOFailure(graph) || graph.Summary.Rollback, Message: "SLO failure has rollback proof"})
	}
	if report.Strategy == "canary" {
		checks = append(checks, releasePromotionCheck{ID: "canary.steps", Passed: report.Canary != nil && len(report.Canary.Steps) > 0 && report.Canary.Steps[len(report.Canary.Steps)-1].Traffic.Canary == 100, Message: "canary reaches 100 percent"})
		checks = append(checks, releasePromotionCheck{ID: "canary.rollback", Passed: opts.RollbackOnFail, Message: "canary records rollback-on-fail intent"})
	}
	if report.Strategy == "blue-green" {
		checks = append(checks, releasePromotionCheck{ID: "blue-green.phases", Passed: report.BlueGreen != nil && len(report.BlueGreen.Phases) > 0, Message: "blue/green phases were planned"})
		checks = append(checks, releasePromotionCheck{ID: "blue-green.switch", Passed: !opts.SwitchTraffic || blueGreenFinalTraffic(report).Green == 100, Message: "blue/green switch moves traffic to green"})
		if strings.TrimSpace(opts.Smoke) != "" {
			checks = append(checks, releasePromotionCheck{ID: "blue-green.smoke", Passed: report.Smoke != nil && report.Smoke.SHA256 != "" && !releasePromotionStatusFailed(report.Smoke.Status), Message: "blue/green smoke evidence passed"})
		}
	}
	if opts.Execute {
		checks = append(checks, releasePromotionCheck{ID: "execute.confirmed", Passed: opts.Yes, Message: "execution was explicitly confirmed"})
	}
	return checks
}

func releasePromotionStatusFailed(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "failed" || status == "blocked" || strings.Contains(status, "fail")
}

func releasePromotionChecksPassed(checks []releasePromotionCheck) bool {
	for _, check := range checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func buildReleasePromotionProviderState(report releasePromotionReport, opts releasePromoteOptions, source string, now time.Time) releasePromotionProviderState {
	var steps []releasePromotionStep
	if report.Canary != nil {
		steps = append(steps, report.Canary.Steps...)
	} else if report.BlueGreen != nil {
		steps = append(steps, report.BlueGreen.Phases...)
	}
	return releasePromotionProviderState{
		APIVersion:   releasePromotionTrafficAPIVersion,
		Kind:         releasePromotionTrafficKind,
		GeneratedAt:  now.Format(time.RFC3339Nano),
		Provider:     "file",
		Applied:      false,
		Strategy:     report.Strategy,
		Release:      report.Release,
		Namespace:    report.Namespace,
		Source:       source,
		FinalTraffic: releasePromotionFinalTraffic(report),
		Steps:        steps,
	}
}

func releasePromotionFinalTraffic(report releasePromotionReport) releasePromotionTraffic {
	if report.Canary != nil && len(report.Canary.Steps) > 0 {
		return report.Canary.Steps[len(report.Canary.Steps)-1].Traffic
	}
	return blueGreenFinalTraffic(report)
}

func blueGreenFinalTraffic(report releasePromotionReport) releasePromotionTraffic {
	if report.BlueGreen == nil || len(report.BlueGreen.Phases) == 0 {
		return releasePromotionTraffic{}
	}
	return report.BlueGreen.Phases[len(report.BlueGreen.Phases)-1].Traffic
}

func buildReleasePromotionDecisionLog(report releasePromotionReport, graph proofGraph) releasePromotionDecisionLog {
	_, sum, _ := proofGraphSigningBytes(graph)
	graphSHA := ""
	if sum != [32]byte{} {
		graphSHA = fmt.Sprintf("%x", sum[:])
	}
	return releasePromotionDecisionLog{
		APIVersion:    releasePromotionAPIVersion,
		Kind:          releasePromotionDecisionKind,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Mode:          report.Mode,
		Provider:      report.Provider,
		Strategy:      report.Strategy,
		Release:       report.Release,
		Namespace:     report.Namespace,
		Passed:        report.Passed,
		Source:        report.Source,
		GraphSHA256:   graphSHA,
		Score:         report.Score.Score,
		Grade:         report.Score.Grade,
		Checks:        report.Checks,
		Canary:        report.Canary,
		BlueGreen:     report.BlueGreen,
		ProviderState: report.Artifacts.ProviderState,
	}
}

func attachReleasePromotionEvidence(graph *proofGraph, report releasePromotionReport) error {
	decisionPath, err := filepath.Abs(report.Artifacts.Decision)
	if err != nil {
		decisionPath = report.Artifacts.Decision
	}
	addProofFileArtifact(graph, "release.promotion", "release-promotion", "Release promotion decision", decisionPath, false, passFail(report.Passed))
	addProofLink(graph, "apply.proof", "release.promotion", "promoted-by")
	if report.Artifacts.ProviderState != "" {
		statePath, err := filepath.Abs(report.Artifacts.ProviderState)
		if err != nil {
			statePath = report.Artifacts.ProviderState
		}
		addProofFileArtifact(graph, "traffic.state", "traffic-shift", "Traffic shift state", statePath, false, passFail(report.ProviderState != nil && report.ProviderState.Applied))
		addProofLink(graph, "release.promotion", "traffic.state", "planned")
	}
	if report.Smoke != nil {
		smokePath, err := filepath.Abs(report.Smoke.Path)
		if err != nil {
			smokePath = report.Smoke.Path
		}
		addProofFileArtifact(graph, proofID("smoke", smokePath), "smoke-test", "Smoke test result", smokePath, false, firstNonEmpty(report.Smoke.Status, "passed"))
		addProofLink(graph, proofID("smoke", smokePath), "release.promotion", "gates")
	}
	if report.SLO != nil && strings.TrimSpace(report.SLO.Path) != "" {
		sloPath, err := filepath.Abs(report.SLO.Path)
		if err != nil {
			sloPath = report.SLO.Path
		}
		addProofFileArtifact(graph, proofID("promotion-slo", sloPath), "slo-outcome", "Promotion SLO", sloPath, false, "configured")
		addProofLink(graph, proofID("promotion-slo", sloPath), "release.promotion", "gates")
	}
	return nil
}

func defaultReleasePromoteOutDir(strategy string, graph proofGraph, opts releasePromoteOptions, now time.Time) string {
	slug := sanitizeFilename(firstNonEmpty(opts.Release, graph.Release, "release"))
	if slug == "" {
		slug = "release"
	}
	return fmt.Sprintf("torque-promote-%s-%s-%s", strategy, slug, now.Format("20060102-150405"))
}

func renderReleasePromotionText(out io.Writer, report releasePromotionReport) {
	fmt.Fprintf(out, "Release promotion: %s\n", strings.ToUpper(passFail(report.Passed)))
	fmt.Fprintf(out, "Strategy: %s\n", report.Strategy)
	if report.Release != "" {
		fmt.Fprintf(out, "Release: %s\n", report.Release)
	}
	if report.Namespace != "" {
		fmt.Fprintf(out, "Namespace: %s\n", report.Namespace)
	}
	fmt.Fprintf(out, "Mode: %s\n", report.Mode)
	fmt.Fprintf(out, "Provider: %s\n", report.Provider)
	fmt.Fprintf(out, "Out dir: %s\n", report.OutDir)
	fmt.Fprintf(out, "Gate: %s\n", strings.ToUpper(passFail(report.Gate.Passed)))
	fmt.Fprintf(out, "Score: %d (%s)\n", report.Score.Score, report.Score.Grade)
	if report.Canary != nil {
		fmt.Fprintf(out, "Canary steps: %d\n", len(report.Canary.Steps))
	}
	if report.BlueGreen != nil {
		fmt.Fprintf(out, "Blue/green phases: %d\n", len(report.BlueGreen.Phases))
	}
	fmt.Fprintf(out, "Promotion graph: %s\n", report.Artifacts.PromotedGraph)
	for _, check := range report.Checks {
		if check.Passed {
			continue
		}
		fmt.Fprintf(out, "Blocked: %s: %s\n", check.ID, check.Message)
	}
}

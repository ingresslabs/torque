package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	releaseFlightAPIVersion = "torque.dev/release-flight/v1"
	releaseFlightKind       = "ReleaseFlight"
)

type flightRecordOptions struct {
	Out    string
	Policy string
	Pub    string
	Format string
}

type flightReplayOptions struct {
	Format string
}

type flightExplainOptions struct {
	Format string
}

type releaseFlight struct {
	APIVersion  string                       `json:"apiVersion"`
	Kind        string                       `json:"kind"`
	GeneratedAt string                       `json:"generatedAt"`
	Source      string                       `json:"source"`
	Release     string                       `json:"release,omitempty"`
	Namespace   string                       `json:"namespace,omitempty"`
	Status      string                       `json:"status,omitempty"`
	Blocked     bool                         `json:"blocked,omitempty"`
	GraphSHA256 string                       `json:"graphSha256"`
	Artifacts   int                          `json:"artifacts"`
	Files       int                          `json:"files"`
	Gate        proofGateVerificationSummary `json:"gate"`
	Score       int                          `json:"score"`
	Grade       string                       `json:"grade"`
	Timeline    []releaseFlightEvent         `json:"timeline"`
}

type releaseFlightEvent struct {
	Phase    string `json:"phase"`
	Type     string `json:"type"`
	Label    string `json:"label,omitempty"`
	Status   string `json:"status,omitempty"`
	Path     string `json:"path,omitempty"`
	Digest   string `json:"digest,omitempty"`
	Resource string `json:"resource,omitempty"`
}

type flightReplayReport struct {
	Version     string `json:"version"`
	Source      string `json:"source"`
	Passed      bool   `json:"passed"`
	Release     string `json:"release,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Events      int    `json:"events"`
	GraphSHA256 string `json:"graphSha256,omitempty"`
	Score       int    `json:"score,omitempty"`
	Grade       string `json:"grade,omitempty"`
	Error       string `json:"error,omitempty"`
}

type flightExplainReport struct {
	Version   string   `json:"version"`
	Source    string   `json:"source"`
	Release   string   `json:"release,omitempty"`
	Namespace string   `json:"namespace,omitempty"`
	Status    string   `json:"status,omitempty"`
	Blocked   bool     `json:"blocked,omitempty"`
	Score     int      `json:"score"`
	Grade     string   `json:"grade"`
	Phases    []string `json:"phases"`
	Summary   string   `json:"summary"`
}

func newFlightCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flight",
		Short: "Record and replay release flight evidence",
		Long:  "Record a release flight from a proof graph, replay the portable timeline, and explain the release path without mutating a cluster.",
	}
	cmd.AddCommand(newFlightRecordCommand())
	cmd.AddCommand(newFlightReplayCommand())
	cmd.AddCommand(newFlightExplainCommand())
	decorateCommandHelp(cmd, "Flight Commands")
	return cmd
}

func newFlightRecordCommand() *cobra.Command {
	opts := flightRecordOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "record <proof-graph>",
		Short: "Record a portable release flight from a proof graph",
		Long:  "Record a portable release flight timeline from a proof graph, release gate result, score, and evidence artifact order.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateFlightFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			flight, err := recordReleaseFlight(args[0], opts)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, flight); err != nil {
					return fmt.Errorf("write release flight: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(flight, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderFlightRecordText(cmd.OutOrStdout(), flight, opts.Out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write release flight JSON to this path")
	cmd.Flags().StringVar(&opts.Policy, "policy", "", "Optional proof gate policy file")
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON for graph verification")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Flight Record Flags")
	return cmd
}

func newFlightReplayCommand() *cobra.Command {
	opts := flightReplayOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "replay <flight.torque>",
		Short: "Replay and validate a release flight",
		Long:  "Replay and validate that a release flight contains a graph digest, score, and ordered evidence timeline.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateFlightFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := replayReleaseFlight(args[0])
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
				renderFlightReplayText(cmd.OutOrStdout(), report)
			}
			if !report.Passed {
				return fmt.Errorf("release flight replay failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Flight Replay Flags")
	return cmd
}

func newFlightExplainCommand() *cobra.Command {
	opts := flightExplainOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "explain <flight.torque>",
		Short: "Explain a release flight timeline",
		Long:  "Explain a recorded release flight as a compact timeline summary for reviews and incident follow-up.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateFlightFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := explainReleaseFlight(args[0])
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
				renderFlightExplainText(cmd.OutOrStdout(), report)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Flight Explain Flags")
	return cmd
}

func validateFlightFormat(format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text", "json":
		return nil
	default:
		return fmt.Errorf("unsupported --format %q (expected text or json)", format)
	}
}

func recordReleaseFlight(path string, opts flightRecordOptions) (releaseFlight, error) {
	graph, _, err := loadOrBuildProofGraph(path)
	if err != nil {
		return releaseFlight{}, err
	}
	finalizeProofGraph(&graph)
	score, err := scoreProofSource(path, releaseScoreOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releaseFlight{}, err
	}
	_, graphSum, err := proofGraphSigningBytes(graph)
	if err != nil {
		return releaseFlight{}, err
	}
	flight := releaseFlight{
		APIVersion:  releaseFlightAPIVersion,
		Kind:        releaseFlightKind,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Source:      strings.TrimSpace(path),
		Release:     graph.Release,
		Namespace:   graph.Namespace,
		Status:      graph.Status,
		Blocked:     graph.Blocked,
		GraphSHA256: fmt.Sprintf("%x", graphSum[:]),
		Artifacts:   len(graph.Artifacts),
		Files:       graph.Summary.Files,
		Gate: proofGateVerificationSummary{
			Passed:            score.GatePassed,
			SignatureVerified: score.Verified,
			FilesChecked:      score.Summary.Files,
		},
		Score:    score.Score,
		Grade:    score.Grade,
		Timeline: buildReleaseFlightTimeline(graph),
	}
	return flight, nil
}

func buildReleaseFlightTimeline(graph proofGraph) []releaseFlightEvent {
	phaseOrder := map[string]int{
		"source":   10,
		"build":    20,
		"render":   30,
		"verify":   40,
		"dry-run":  50,
		"runtime":  60,
		"rollout":  70,
		"slo":      80,
		"rollback": 90,
		"repair":   100,
	}
	var out []releaseFlightEvent
	for _, artifact := range graph.Artifacts {
		phase := releaseFlightPhase(artifact.Type)
		out = append(out, releaseFlightEvent{
			Phase:    phase,
			Type:     artifact.Type,
			Label:    artifact.Label,
			Status:   artifact.Status,
			Path:     artifact.Path,
			Digest:   firstNonEmpty(artifact.Digest, artifact.SHA256),
			Resource: artifact.Resource,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if phaseOrder[out[i].Phase] != phaseOrder[out[j].Phase] {
			return phaseOrder[out[i].Phase] < phaseOrder[out[j].Phase]
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func releaseFlightPhase(typ string) string {
	switch typ {
	case "git-commit", "apply-proof", "simulation-manifest":
		return "source"
	case "build-capture", "supply-chain-provenance", "provenance", "attestation", "sbom", "image-digest":
		return "build"
	case "helm-plan", "helm-render", "predicted-live-state":
		return "render"
	case "verifier-report", "admission-proof", "field-ownership-proof", "quota-proof":
		return "verify"
	case "server-dry-run":
		return "dry-run"
	case "runtime-drift", "rollout-events":
		return "runtime"
	case "rollout-capture", "logs-capture", "rollout-state", "rollout-prediction", "release-promotion", "traffic-shift", "smoke-test":
		return "rollout"
	case "slo-outcome":
		return "slo"
	case "rollback-proof":
		return "rollback"
	case "repair-pr", "repair-patch":
		return "repair"
	default:
		return "evidence"
	}
}

func loadReleaseFlight(path string) (releaseFlight, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return releaseFlight{}, fmt.Errorf("read release flight: %w", err)
	}
	var flight releaseFlight
	if err := json.Unmarshal(raw, &flight); err != nil {
		return releaseFlight{}, fmt.Errorf("parse release flight: %w", err)
	}
	if strings.TrimSpace(flight.APIVersion) != releaseFlightAPIVersion || strings.TrimSpace(flight.Kind) != releaseFlightKind {
		return releaseFlight{}, fmt.Errorf("%s does not look like a Torque release flight", path)
	}
	return flight, nil
}

func replayReleaseFlight(path string) (flightReplayReport, error) {
	flight, err := loadReleaseFlight(path)
	if err != nil {
		return flightReplayReport{}, err
	}
	report := flightReplayReport{
		Version:     "v1",
		Source:      path,
		Passed:      true,
		Release:     flight.Release,
		Namespace:   flight.Namespace,
		Events:      len(flight.Timeline),
		GraphSHA256: flight.GraphSHA256,
		Score:       flight.Score,
		Grade:       flight.Grade,
	}
	if strings.TrimSpace(flight.GraphSHA256) == "" {
		report.Passed = false
		report.Error = "missing graph SHA256"
	}
	if len(flight.Timeline) == 0 {
		report.Passed = false
		report.Error = firstNonEmpty(report.Error, "missing timeline")
	}
	return report, nil
}

func explainReleaseFlight(path string) (flightExplainReport, error) {
	flight, err := loadReleaseFlight(path)
	if err != nil {
		return flightExplainReport{}, err
	}
	seen := map[string]bool{}
	var phases []string
	for _, event := range flight.Timeline {
		if event.Phase == "" || seen[event.Phase] {
			continue
		}
		seen[event.Phase] = true
		phases = append(phases, event.Phase)
	}
	summary := fmt.Sprintf("%s/%s recorded %d evidence events with release score %d (%s).",
		firstNonEmpty(flight.Namespace, "-"),
		firstNonEmpty(flight.Release, "-"),
		len(flight.Timeline),
		flight.Score,
		flight.Grade,
	)
	return flightExplainReport{
		Version:   "v1",
		Source:    path,
		Release:   flight.Release,
		Namespace: flight.Namespace,
		Status:    flight.Status,
		Blocked:   flight.Blocked,
		Score:     flight.Score,
		Grade:     flight.Grade,
		Phases:    phases,
		Summary:   summary,
	}, nil
}

func renderFlightRecordText(out io.Writer, flight releaseFlight, outPath string) {
	fmt.Fprintf(out, "Release flight: %s\n", firstNonEmpty(flight.Release, "-"))
	fmt.Fprintf(out, "Events: %d\n", len(flight.Timeline))
	fmt.Fprintf(out, "Score: %d (%s)\n", flight.Score, flight.Grade)
	if strings.TrimSpace(outPath) != "" {
		fmt.Fprintf(out, "Flight JSON: %s\n", outPath)
	}
}

func renderFlightReplayText(out io.Writer, report flightReplayReport) {
	fmt.Fprintf(out, "Flight replay: %s\n", strings.ToUpper(passFail(report.Passed)))
	fmt.Fprintf(out, "Events: %d\n", report.Events)
	if report.Score > 0 {
		fmt.Fprintf(out, "Score: %d (%s)\n", report.Score, report.Grade)
	}
	if report.Error != "" {
		fmt.Fprintf(out, "Error: %s\n", report.Error)
	}
}

func renderFlightExplainText(out io.Writer, report flightExplainReport) {
	fmt.Fprintf(out, "Flight explain: %s\n", report.Summary)
	if len(report.Phases) > 0 {
		fmt.Fprintf(out, "Phases: %s\n", strings.Join(report.Phases, ", "))
	}
}

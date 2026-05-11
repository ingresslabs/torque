package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type releaseScoreOptions struct {
	Policy    string
	Pub       string
	Out       string
	Format    string
	FailBelow int
}

type releaseScoreReport struct {
	Version      string            `json:"version"`
	GeneratedAt  string            `json:"generatedAt"`
	Source       string            `json:"source"`
	Release      string            `json:"release,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Score        int               `json:"score"`
	Grade        string            `json:"grade"`
	Passed       bool              `json:"passed"`
	GatePassed   bool              `json:"gatePassed"`
	Verified     bool              `json:"verified"`
	Blocked      bool              `json:"blocked,omitempty"`
	Summary      proofGraphSummary `json:"summary"`
	Penalties    []releasePenalty  `json:"penalties,omitempty"`
	GateFailures []string          `json:"gateFailures,omitempty"`
}

type releasePenalty struct {
	ID     string `json:"id"`
	Points int    `json:"points"`
	Reason string `json:"reason"`
}

func newReleaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Evaluate release readiness from proof evidence",
		Long:  "Evaluate release readiness from signed proof graphs, release gates, and evidence coverage.",
	}
	cmd.AddCommand(newReleaseAutopilotCommand())
	cmd.AddCommand(newReleaseScoreCommand())
	decorateCommandHelp(cmd, "Release Commands")
	return cmd
}

func newReleaseScoreCommand() *cobra.Command {
	opts := releaseScoreOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "score <proof-graph>",
		Short: "Score release readiness from a proof graph",
		Long:  "Score release readiness from a signed proof graph and release gate checks. The score is designed for PRs, release notes, and agent policy decisions.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "text", "json":
				return nil
			default:
				return fmt.Errorf("unsupported --format %q (expected text or json)", opts.Format)
			}
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := scoreProofSource(args[0], opts)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, report); err != nil {
					return fmt.Errorf("write release score: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderReleaseScoreText(cmd.OutOrStdout(), report, opts.Out)
			}
			if opts.FailBelow > 0 && report.Score < opts.FailBelow {
				return fmt.Errorf("release score %d is below %d", report.Score, opts.FailBelow)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Policy, "policy", "", "Optional proof gate policy file")
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON for graph verification")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write score JSON to this path")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	cmd.Flags().IntVar(&opts.FailBelow, "fail-below", 0, "Exit non-zero when score is below this value")
	decorateCommandHelp(cmd, "Release Score Flags")
	return cmd
}

func scoreProofSource(path string, opts releaseScoreOptions) (releaseScoreReport, error) {
	graph, _, err := loadOrBuildProofGraph(path)
	if err != nil {
		return releaseScoreReport{}, err
	}
	finalizeProofGraph(&graph)
	policy, err := loadProofGatePolicy(opts.Policy)
	if err != nil {
		return releaseScoreReport{}, err
	}
	gate, err := gateProofSource(path, policy, proofGateOptions{Policy: opts.Policy, Pub: opts.Pub, Format: "json"})
	if err != nil {
		return releaseScoreReport{}, err
	}
	report := releaseScoreReport{
		Version:     "v1",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Source:      path,
		Release:     graph.Release,
		Namespace:   graph.Namespace,
		Passed:      gate.Passed,
		GatePassed:  gate.Passed,
		Verified:    gate.Verification.Passed,
		Blocked:     graph.Blocked,
		Summary:     graph.Summary,
	}
	for _, check := range gate.Checks {
		if check.Passed {
			continue
		}
		report.GateFailures = append(report.GateFailures, check.ID)
		report.Penalties = append(report.Penalties, releasePenaltyForGateCheck(check))
	}
	if graph.Blocked {
		report.Penalties = append(report.Penalties, releasePenalty{ID: "release.blocked", Points: 10, Reason: "release proof is blocked"})
	}
	report.Score = calculateReleaseScore(report.Penalties)
	report.Grade = releaseScoreGrade(report.Score)
	sortReleasePenalties(report.Penalties)
	return report, nil
}

func releasePenaltyForGateCheck(check proofGateCheck) releasePenalty {
	switch check.ID {
	case "graph.verification", "hash.integrity":
		return releasePenalty{ID: check.ID, Points: 25, Reason: check.Message}
	case "signature.required":
		return releasePenalty{ID: check.ID, Points: 20, Reason: check.Message}
	case "images.pinned", "verifier.passed":
		return releasePenalty{ID: check.ID, Points: 10, Reason: check.Message}
	case "rollback.blocked-release", "rollback.slo":
		return releasePenalty{ID: check.ID, Points: 10, Reason: check.Message}
	case "repair.pr":
		return releasePenalty{ID: check.ID, Points: 5, Reason: check.Message}
	default:
		if strings.HasPrefix(check.ID, "artifact.") {
			return releasePenalty{ID: check.ID, Points: 3, Reason: check.Message}
		}
		return releasePenalty{ID: check.ID, Points: 2, Reason: check.Message}
	}
}

func calculateReleaseScore(penalties []releasePenalty) int {
	score := 100
	for _, penalty := range penalties {
		score -= penalty.Points
	}
	return int(math.Max(0, float64(score)))
}

func releaseScoreGrade(score int) string {
	switch {
	case score >= 95:
		return "A+"
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

func sortReleasePenalties(penalties []releasePenalty) {
	for i := 0; i < len(penalties); i++ {
		for j := i + 1; j < len(penalties); j++ {
			if penalties[j].ID < penalties[i].ID {
				penalties[i], penalties[j] = penalties[j], penalties[i]
			}
		}
	}
}

func renderReleaseScoreText(out io.Writer, report releaseScoreReport, outPath string) {
	fmt.Fprintf(out, "Release score: %d (%s)\n", report.Score, report.Grade)
	if report.Release != "" {
		fmt.Fprintf(out, "Release: %s\n", report.Release)
	}
	fmt.Fprintf(out, "Gate: %s\n", strings.ToUpper(passFail(report.GatePassed)))
	fmt.Fprintf(out, "Verified: %t\n", report.Verified)
	if strings.TrimSpace(outPath) != "" {
		fmt.Fprintf(out, "Score JSON: %s\n", outPath)
	}
	for _, penalty := range report.Penalties {
		fmt.Fprintf(out, "Penalty: -%d %s (%s)\n", penalty.Points, penalty.ID, penalty.Reason)
	}
}

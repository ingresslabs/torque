package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type replayOptions struct {
	Lab           string
	Format        string
	FailOnBlocked bool
}

type replayReport struct {
	Version      string                 `json:"version"`
	Lab          string                 `json:"lab"`
	Source       string                 `json:"source"`
	Kind         string                 `json:"kind"`
	Passed       bool                   `json:"passed"`
	Blocked      bool                   `json:"blocked,omitempty"`
	Release      string                 `json:"release,omitempty"`
	Namespace    string                 `json:"namespace,omitempty"`
	FilesChecked int                    `json:"filesChecked,omitempty"`
	MissingFiles []string               `json:"missingFiles,omitempty"`
	Summary      map[string]interface{} `json:"summary,omitempty"`
}

func newReplayCommand() *cobra.Command {
	opts := replayOptions{Lab: "k3s", Format: "text"}
	cmd := &cobra.Command{
		Use:   "replay <proof-bundle>",
		Short: "Replay and validate a Torque proof bundle",
		Long:  "Validate a Torque simulation or apply proof bundle and report whether its evidence can be replayed in a lab workflow.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "text", "json":
			default:
				return fmt.Errorf("unsupported --format %q (expected text or json)", opts.Format)
			}
			if strings.TrimSpace(opts.Lab) == "" {
				return fmt.Errorf("--lab cannot be empty")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := replayProofBundle(args[0], opts)
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
				renderReplayText(cmd.OutOrStdout(), report)
			}
			if opts.FailOnBlocked && report.Blocked {
				return fmt.Errorf("proof bundle is blocked")
			}
			if !report.Passed {
				return fmt.Errorf("proof bundle replay validation failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Lab, "lab", "k3s", "Replay lab profile label")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&opts.FailOnBlocked, "fail-on-blocked", false, "Exit non-zero when the replayed proof is blocked")
	decorateCommandHelp(cmd, "Replay Flags")
	return cmd
}

func replayProofBundle(source string, opts replayOptions) (replayReport, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return replayReport{}, fmt.Errorf("proof bundle path is required")
	}
	info, err := os.Stat(source)
	if err != nil {
		return replayReport{}, fmt.Errorf("inspect proof bundle: %w", err)
	}
	if info.IsDir() {
		return replaySimulationProofDir(source, opts)
	}
	return replayApplyProofFile(source, opts)
}

func replaySimulationProofDir(dir string, opts replayOptions) (replayReport, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return replayReport{}, fmt.Errorf("read simulation manifest: %w", err)
	}
	var manifest applySimulationManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return replayReport{}, fmt.Errorf("parse simulation manifest: %w", err)
	}
	report := replayReport{
		Version:   "v1",
		Lab:       strings.TrimSpace(opts.Lab),
		Source:    dir,
		Kind:      "apply-simulation",
		Passed:    true,
		Blocked:   manifest.Blocked,
		Release:   manifest.Release,
		Namespace: manifest.Namespace,
		Summary: map[string]interface{}{
			"status":                  manifest.Status,
			"serverDryRunFailed":      manifest.Summary.ServerDryRunFailed,
			"admissionDenied":         manifest.Summary.AdmissionDenied,
			"fieldOwnershipConflicts": manifest.Summary.FieldOwnershipConflicts,
			"quotaFails":              manifest.Summary.QuotaFails,
			"risk":                    manifest.Summary.Risk,
		},
	}
	names := simulationFilesFromManifest(manifest)
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			if os.IsNotExist(err) {
				report.MissingFiles = append(report.MissingFiles, name)
				continue
			}
			return replayReport{}, fmt.Errorf("inspect %s: %w", name, err)
		}
		report.FilesChecked++
	}
	report.Passed = len(report.MissingFiles) == 0
	return report, nil
}

func replayApplyProofFile(path string, opts replayOptions) (replayReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return replayReport{}, fmt.Errorf("read apply proof: %w", err)
	}
	var proof applyProofBundle
	if err := json.Unmarshal(raw, &proof); err != nil {
		return replayReport{}, fmt.Errorf("parse apply proof: %w", err)
	}
	if strings.TrimSpace(proof.Release) == "" {
		return replayReport{}, fmt.Errorf("%s does not look like a Torque apply proof", path)
	}
	blocked := strings.EqualFold(proof.Status, "failed")
	report := replayReport{
		Version:   "v1",
		Lab:       strings.TrimSpace(opts.Lab),
		Source:    path,
		Kind:      "apply-proof",
		Passed:    true,
		Blocked:   blocked,
		Release:   proof.Release,
		Namespace: proof.Namespace,
		Summary: map[string]interface{}{
			"status":   proof.Status,
			"dryRun":   proof.DryRun,
			"evidence": len(proof.Evidence),
		},
	}
	if proof.Prediction != nil {
		report.Summary["risk"] = proof.Prediction.Risk
	}
	return report, nil
}

func simulationFilesFromManifest(manifest applySimulationManifest) []string {
	seen := map[string]struct{}{}
	for _, name := range manifest.Files {
		name = filepath.ToSlash(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		seen[name] = struct{}{}
	}
	var names []string
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderReplayText(out interface{ Write([]byte) (int, error) }, report replayReport) {
	fmt.Fprintf(out, "Replay: %s\n", strings.ToUpper(passFail(report.Passed)))
	fmt.Fprintf(out, "Lab: %s\n", report.Lab)
	fmt.Fprintf(out, "Kind: %s\n", report.Kind)
	if report.Release != "" {
		fmt.Fprintf(out, "Release: %s\n", report.Release)
	}
	if report.Namespace != "" {
		fmt.Fprintf(out, "Namespace: %s\n", report.Namespace)
	}
	if report.FilesChecked > 0 {
		fmt.Fprintf(out, "Files checked: %d\n", report.FilesChecked)
	}
	if report.Blocked {
		fmt.Fprintln(out, "Blocked: true")
	}
	if len(report.MissingFiles) > 0 {
		fmt.Fprintln(out, "Missing files:")
		for _, name := range report.MissingFiles {
			fmt.Fprintf(out, "  - %s\n", name)
		}
	}
}

func passFail(ok bool) string {
	if ok {
		return "passed"
	}
	return "failed"
}

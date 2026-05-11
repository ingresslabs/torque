package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/stack"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	proofAttestationAPIVersion = "torque.dev/proof-attestation/v1"
	proofAttestationKind       = "ProofAttestation"
)

type proofAttestOptions struct {
	Release          string
	Out              string
	Key              string
	Pub              string
	Format           string
	RequireSignature bool
	StrictFiles      bool
}

type proofGateOptions struct {
	Policy string
	Out    string
	Pub    string
	Format string
}

type proofAttestation struct {
	APIVersion      string                     `json:"apiVersion"`
	Kind            string                     `json:"kind"`
	GeneratedAt     string                     `json:"generatedAt"`
	Release         string                     `json:"release,omitempty"`
	Commit          string                     `json:"commit,omitempty"`
	Graph           string                     `json:"graph"`
	GraphSHA256     string                     `json:"graphSha256"`
	GraphFileSHA256 string                     `json:"graphFileSha256,omitempty"`
	GraphSignature  string                     `json:"graphSignature,omitempty"`
	SourceKind      string                     `json:"sourceKind"`
	Status          string                     `json:"status,omitempty"`
	Blocked         bool                       `json:"blocked,omitempty"`
	Verified        bool                       `json:"verified"`
	Artifacts       int                        `json:"artifacts"`
	FilesChecked    int                        `json:"filesChecked"`
	FilesFailed     int                        `json:"filesFailed,omitempty"`
	MissingFiles    []string                   `json:"missingFiles,omitempty"`
	MismatchedFiles []string                   `json:"mismatchedFiles,omitempty"`
	Summary         proofGraphSummary          `json:"summary"`
	Signature       *proofAttestationSignature `json:"signature,omitempty"`
}

type proofAttestationSignature struct {
	APIVersion        string `json:"apiVersion"`
	CreatedAt         string `json:"createdAt"`
	Algorithm         string `json:"algorithm"`
	PublicKey         string `json:"publicKey"`
	AttestationSHA256 string `json:"attestationSha256"`
	Signature         string `json:"signature"`
}

type proofGatePolicy struct {
	RequireSignature         bool     `json:"requireSignature" yaml:"requireSignature"`
	StrictFiles              bool     `json:"strictFiles" yaml:"strictFiles"`
	FailOnUnpinnedImages     bool     `json:"failOnUnpinnedImages" yaml:"failOnUnpinnedImages"`
	FailOnVerifierBlocked    bool     `json:"failOnVerifierBlocked" yaml:"failOnVerifierBlocked"`
	RequireRollbackOnBlocked bool     `json:"requireRollbackOnBlocked" yaml:"requireRollbackOnBlocked"`
	RequireRollbackOnSLO     bool     `json:"requireRollbackOnSLO" yaml:"requireRollbackOnSLO"`
	RequireRepairPR          bool     `json:"requireRepairPR" yaml:"requireRepairPR"`
	MinArtifacts             int      `json:"minArtifacts,omitempty" yaml:"minArtifacts,omitempty"`
	MinCheckedFiles          int      `json:"minCheckedFiles,omitempty" yaml:"minCheckedFiles,omitempty"`
	RequiredArtifacts        []string `json:"requiredArtifacts,omitempty" yaml:"requiredArtifacts,omitempty"`
}

type proofGatePolicyFile struct {
	RequireSignature         *bool    `json:"requireSignature" yaml:"requireSignature"`
	StrictFiles              *bool    `json:"strictFiles" yaml:"strictFiles"`
	FailOnUnpinnedImages     *bool    `json:"failOnUnpinnedImages" yaml:"failOnUnpinnedImages"`
	FailOnVerifierBlocked    *bool    `json:"failOnVerifierBlocked" yaml:"failOnVerifierBlocked"`
	RequireRollbackOnBlocked *bool    `json:"requireRollbackOnBlocked" yaml:"requireRollbackOnBlocked"`
	RequireRollbackOnSLO     *bool    `json:"requireRollbackOnSLO" yaml:"requireRollbackOnSLO"`
	RequireRepairPR          *bool    `json:"requireRepairPR" yaml:"requireRepairPR"`
	MinArtifacts             *int     `json:"minArtifacts" yaml:"minArtifacts"`
	MinCheckedFiles          *int     `json:"minCheckedFiles" yaml:"minCheckedFiles"`
	RequiredArtifacts        []string `json:"requiredArtifacts" yaml:"requiredArtifacts"`
}

type proofGateReport struct {
	Version      string                       `json:"version"`
	Source       string                       `json:"source"`
	Policy       string                       `json:"policy,omitempty"`
	Release      string                       `json:"release,omitempty"`
	Namespace    string                       `json:"namespace,omitempty"`
	Passed       bool                         `json:"passed"`
	Summary      proofGateSummary             `json:"summary"`
	Verification proofGateVerificationSummary `json:"verification"`
	GraphSummary proofGraphSummary            `json:"graphSummary"`
	Checks       []proofGateCheck             `json:"checks"`
}

type proofGateSummary struct {
	Checks int `json:"checks"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type proofGateVerificationSummary struct {
	Passed            bool `json:"passed"`
	SignatureVerified bool `json:"signatureVerified,omitempty"`
	FilesChecked      int  `json:"filesChecked"`
	FilesFailed       int  `json:"filesFailed,omitempty"`
}

type proofGateCheck struct {
	ID       string   `json:"id"`
	Passed   bool     `json:"passed"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Evidence []string `json:"evidence,omitempty"`
}

func newProofAttestCommand() *cobra.Command {
	opts := proofAttestOptions{Format: "text", RequireSignature: true}
	cmd := &cobra.Command{
		Use:   "attest <proof-graph>",
		Short: "Sign a compact release verdict from a verified proof graph",
		Long:  "Verify a proof graph, then write a signed release attestation that records the release, commit, graph digest, verification result, and artifact counts for PRs and release notes.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "text", "json":
			default:
				return fmt.Errorf("unsupported --format %q (expected text or json)", opts.Format)
			}
			if strings.TrimSpace(opts.Key) == "" {
				return fmt.Errorf("--key is required to sign the release attestation")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			graph, graphFile, err := loadOrBuildProofGraph(args[0])
			if err != nil {
				return err
			}
			baseDir := ""
			if graphFile {
				baseDir = filepath.Dir(args[0])
			}
			report := verifyProofGraph(graph, baseDir, proofVerifyOptions{
				Pub:              opts.Pub,
				RequireSignature: opts.RequireSignature,
				StrictFiles:      opts.StrictFiles,
			})
			attestation, err := buildProofAttestation(args[0], graph, report, opts)
			if err != nil {
				return err
			}
			if err := signProofAttestation(&attestation, opts.Key); err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, attestation); err != nil {
					return fmt.Errorf("write proof attestation: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(attestation, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderProofAttestationText(cmd.OutOrStdout(), attestation, opts.Out)
			}
			if !attestation.Verified {
				return fmt.Errorf("proof graph verification failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Release, "release", "", "Release name or tag to record in the attestation")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write signed attestation JSON to this path")
	cmd.Flags().StringVar(&opts.Key, "key", "", "Sign attestation with an ed25519 key JSON file from torque stack keygen")
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON for graph verification")
	cmd.Flags().BoolVar(&opts.RequireSignature, "require-signature", true, "Require the input proof graph to be signed")
	cmd.Flags().BoolVar(&opts.StrictFiles, "strict-files", false, "Fail graph verification when any referenced file is missing")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Proof Attest Flags")
	return cmd
}

func newProofGateCommand() *cobra.Command {
	opts := proofGateOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "gate <proof-graph>",
		Short: "Evaluate a release policy against a proof graph",
		Long:  "Block release promotion when proof is unsigned, hashes drift, required evidence is missing, images are unpinned, verifier evidence blocks, SLO failure lacks rollback proof, or repair PR evidence is absent.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "text", "json":
			default:
				return fmt.Errorf("unsupported --format %q (expected text or json)", opts.Format)
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			policy, err := loadProofGatePolicy(opts.Policy)
			if err != nil {
				return err
			}
			report, err := gateProofSource(args[0], policy, opts)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, report); err != nil {
					return fmt.Errorf("write proof gate report: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderProofGateText(cmd.OutOrStdout(), report, opts.Out)
			}
			if !report.Passed {
				return fmt.Errorf("proof gate failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Policy, "policy", "", "YAML or JSON release policy (defaults to the built-in release gate)")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write gate report JSON to this path")
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON for graph verification")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Proof Gate Flags")
	return cmd
}

func buildProofAttestation(source string, graph proofGraph, report proofVerifyReport, opts proofAttestOptions) (proofAttestation, error) {
	finalizeProofGraph(&graph)
	_, graphSum, err := proofGraphSigningBytes(graph)
	if err != nil {
		return proofAttestation{}, err
	}
	graphFileSHA := ""
	if info, err := os.Stat(source); err == nil && !info.IsDir() {
		if sha, _, err := sha256HexFileLocal(source); err == nil {
			graphFileSHA = sha
		}
	}
	release := firstNonEmpty(opts.Release, graph.Release)
	return proofAttestation{
		APIVersion:      proofAttestationAPIVersion,
		Kind:            proofAttestationKind,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Release:         release,
		Commit:          proofGraphCommit(graph),
		Graph:           strings.TrimSpace(source),
		GraphSHA256:     hex.EncodeToString(graphSum[:]),
		GraphFileSHA256: graphFileSHA,
		GraphSignature:  graphSignatureSHA(graph),
		SourceKind:      graph.SourceKind,
		Status:          graph.Status,
		Blocked:         graph.Blocked,
		Verified:        report.Passed,
		Artifacts:       len(graph.Artifacts),
		FilesChecked:    report.Artifacts.Checked,
		FilesFailed:     report.Artifacts.Failed,
		MissingFiles:    report.Artifacts.Missing,
		MismatchedFiles: report.Artifacts.Mismatched,
		Summary:         graph.Summary,
	}, nil
}

func signProofAttestation(attestation *proofAttestation, keyPath string) error {
	if attestation == nil {
		return fmt.Errorf("proof attestation is nil")
	}
	_, _, priv, err := stack.LoadBundleKey(keyPath)
	if err != nil {
		return err
	}
	if len(priv) == 0 {
		return fmt.Errorf("key %s does not contain a private key", keyPath)
	}
	_, sum, err := proofAttestationSigningBytes(*attestation)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(priv, sum[:])
	pub := priv.Public().(ed25519.PublicKey)
	attestation.Signature = &proofAttestationSignature{
		APIVersion:        proofAttestationAPIVersion + "/signature",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		Algorithm:         "ed25519",
		PublicKey:         base64.StdEncoding.EncodeToString(pub),
		AttestationSHA256: hex.EncodeToString(sum[:]),
		Signature:         base64.StdEncoding.EncodeToString(signature),
	}
	return nil
}

func proofAttestationSigningBytes(attestation proofAttestation) ([]byte, [32]byte, error) {
	attestation.Signature = nil
	raw, err := json.Marshal(attestation)
	if err != nil {
		return nil, [32]byte{}, err
	}
	sum := sha256.Sum256(raw)
	return raw, sum, nil
}

func defaultProofGatePolicy() proofGatePolicy {
	return proofGatePolicy{
		RequireSignature:         true,
		StrictFiles:              true,
		FailOnUnpinnedImages:     true,
		FailOnVerifierBlocked:    true,
		RequireRollbackOnBlocked: true,
		RequireRollbackOnSLO:     true,
		RequireRepairPR:          true,
		MinArtifacts:             1,
		MinCheckedFiles:          1,
		RequiredArtifacts: []string{
			"git-commit",
			"image-digest",
			"helm-render",
			"verifier-report",
			"build-capture",
			"sbom",
			"supply-chain-provenance",
			"server-dry-run",
			"runtime-drift",
			"rollout-events",
			"logs-capture",
			"slo-outcome",
			"repair-pr",
		},
	}
}

func loadProofGatePolicy(path string) (proofGatePolicy, error) {
	policy := defaultProofGatePolicy()
	path = strings.TrimSpace(path)
	if path == "" {
		return policy, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return proofGatePolicy{}, fmt.Errorf("read proof gate policy: %w", err)
	}
	var override proofGatePolicyFile
	if err := yaml.Unmarshal(raw, &override); err != nil {
		return proofGatePolicy{}, fmt.Errorf("parse proof gate policy: %w", err)
	}
	if override.RequireSignature != nil {
		policy.RequireSignature = *override.RequireSignature
	}
	if override.StrictFiles != nil {
		policy.StrictFiles = *override.StrictFiles
	}
	if override.FailOnUnpinnedImages != nil {
		policy.FailOnUnpinnedImages = *override.FailOnUnpinnedImages
	}
	if override.FailOnVerifierBlocked != nil {
		policy.FailOnVerifierBlocked = *override.FailOnVerifierBlocked
	}
	if override.RequireRollbackOnBlocked != nil {
		policy.RequireRollbackOnBlocked = *override.RequireRollbackOnBlocked
	}
	if override.RequireRollbackOnSLO != nil {
		policy.RequireRollbackOnSLO = *override.RequireRollbackOnSLO
	}
	if override.RequireRepairPR != nil {
		policy.RequireRepairPR = *override.RequireRepairPR
	}
	if override.MinArtifacts != nil {
		policy.MinArtifacts = *override.MinArtifacts
	}
	if override.MinCheckedFiles != nil {
		policy.MinCheckedFiles = *override.MinCheckedFiles
	}
	if override.RequiredArtifacts != nil {
		policy.RequiredArtifacts = normalizedProofTypes(override.RequiredArtifacts)
	}
	return policy, nil
}

func gateProofSource(path string, policy proofGatePolicy, opts proofGateOptions) (proofGateReport, error) {
	graph, graphFile, err := loadOrBuildProofGraph(path)
	if err != nil {
		return proofGateReport{}, err
	}
	baseDir := ""
	if graphFile {
		baseDir = filepath.Dir(path)
	}
	finalizeProofGraph(&graph)
	verify := verifyProofGraph(graph, baseDir, proofVerifyOptions{
		Pub:              opts.Pub,
		RequireSignature: policy.RequireSignature,
		StrictFiles:      policy.StrictFiles,
	})
	report := proofGateReport{
		Version:   "v1",
		Source:    strings.TrimSpace(path),
		Policy:    firstNonEmpty(opts.Policy, "builtin-release"),
		Release:   graph.Release,
		Namespace: graph.Namespace,
		Passed:    true,
		Verification: proofGateVerificationSummary{
			Passed:            verify.Passed,
			SignatureVerified: verify.Signature.Verified,
			FilesChecked:      verify.Artifacts.Checked,
			FilesFailed:       verify.Artifacts.Failed,
		},
		GraphSummary: graph.Summary,
	}
	addProofGateCheck(&report, "graph.verification", verify.Passed, "Proof graph signature and file hashes verify", proofGateEvidenceFromVerify(verify)...)
	if policy.RequireSignature {
		addProofGateCheck(&report, "signature.required", verify.Signature.Present && verify.Signature.Verified, "Proof graph has a verified ed25519 signature", verify.Signature.GraphSHA256)
	}
	addProofGateCheck(&report, "hash.integrity", verify.Artifacts.Failed == 0, "Referenced proof files match recorded SHA256 hashes")
	if policy.MinArtifacts > 0 {
		addProofGateCheck(&report, "artifacts.minimum", len(graph.Artifacts) >= policy.MinArtifacts, fmt.Sprintf("Proof graph has at least %d artifact(s)", policy.MinArtifacts), fmt.Sprintf("artifacts=%d", len(graph.Artifacts)))
	}
	if policy.MinCheckedFiles > 0 {
		addProofGateCheck(&report, "files.minimum", verify.Artifacts.Checked >= policy.MinCheckedFiles, fmt.Sprintf("Verification checked at least %d file(s)", policy.MinCheckedFiles), fmt.Sprintf("checked=%d", verify.Artifacts.Checked))
	}
	byType := proofArtifactsByType(graph)
	for _, typ := range normalizedProofTypes(policy.RequiredArtifacts) {
		addProofGateCheck(&report, "artifact."+proofID(typ), len(byType[typ]) > 0, "Required proof artifact is present: "+typ)
	}
	if policy.FailOnUnpinnedImages {
		addProofGateCheck(&report, "images.pinned", graph.Summary.UnpinnedImages == 0, "All referenced images are digest-pinned", fmt.Sprintf("unpinned=%d", graph.Summary.UnpinnedImages))
	}
	if policy.FailOnVerifierBlocked {
		addProofGateCheck(&report, "verifier.passed", graph.Summary.VerifierBlocked == 0, "Verifier evidence does not block the release", fmt.Sprintf("blocked=%d", graph.Summary.VerifierBlocked))
	}
	if policy.RequireRollbackOnBlocked {
		addProofGateCheck(&report, "rollback.blocked-release", !graph.Blocked || len(byType["rollback-proof"]) > 0, "Blocked releases include rollback proof")
	}
	if policy.RequireRollbackOnSLO {
		addProofGateCheck(&report, "rollback.slo", !proofGateHasSLOFailure(graph) || len(byType["rollback-proof"]) > 0, "Failed SLO gates include rollback proof")
	}
	if policy.RequireRepairPR {
		addProofGateCheck(&report, "repair.pr", len(byType["repair-pr"]) > 0, "Repair PR evidence is present")
	}
	sort.Slice(report.Checks, func(i, j int) bool { return report.Checks[i].ID < report.Checks[j].ID })
	report.Summary.Checks = len(report.Checks)
	report.Summary.Passed = 0
	report.Summary.Failed = 0
	report.Passed = true
	for _, check := range report.Checks {
		if check.Passed {
			report.Summary.Passed++
			continue
		}
		report.Summary.Failed++
		report.Passed = false
	}
	return report, nil
}

func addProofGateCheck(report *proofGateReport, id string, passed bool, message string, evidence ...string) {
	cleanEvidence := make([]string, 0, len(evidence))
	for _, item := range evidence {
		item = strings.TrimSpace(item)
		if item != "" {
			cleanEvidence = append(cleanEvidence, item)
		}
	}
	report.Checks = append(report.Checks, proofGateCheck{
		ID:       strings.TrimSpace(id),
		Passed:   passed,
		Severity: "block",
		Message:  strings.TrimSpace(message),
		Evidence: cleanEvidence,
	})
}

func proofGateEvidenceFromVerify(report proofVerifyReport) []string {
	var evidence []string
	if report.Signature.Error != "" {
		evidence = append(evidence, "signature: "+report.Signature.Error)
	}
	for _, path := range report.Artifacts.Missing {
		evidence = append(evidence, "missing: "+path)
	}
	for _, path := range report.Artifacts.Mismatched {
		evidence = append(evidence, "mismatched: "+path)
	}
	return evidence
}

func proofArtifactsByType(graph proofGraph) map[string][]proofGraphArtifact {
	out := make(map[string][]proofGraphArtifact)
	for _, artifact := range graph.Artifacts {
		typ := strings.TrimSpace(artifact.Type)
		if typ == "" {
			continue
		}
		out[typ] = append(out[typ], artifact)
	}
	return out
}

func normalizedProofTypes(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func proofGateHasSLOFailure(graph proofGraph) bool {
	for _, artifact := range graph.Artifacts {
		if artifact.Type != "slo-outcome" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(artifact.Status))
		if status == "failed" || status == "blocked" || strings.Contains(status, "fail") || strings.Contains(status, "violation") {
			return true
		}
	}
	return false
}

func proofGraphCommit(graph proofGraph) string {
	for _, artifact := range graph.Artifacts {
		if artifact.Type == "git-commit" && strings.TrimSpace(artifact.Digest) != "" {
			return strings.TrimSpace(artifact.Digest)
		}
	}
	return ""
}

func graphSignatureSHA(graph proofGraph) string {
	if graph.Signature == nil {
		return ""
	}
	return strings.TrimSpace(graph.Signature.GraphSHA256)
}

func renderProofAttestationText(out io.Writer, attestation proofAttestation, outPath string) {
	fmt.Fprintf(out, "release=%s commit=%s graph=%s verified=%t artifacts=%d checked=%d signed=%t",
		firstNonEmpty(attestation.Release, "-"),
		firstNonEmpty(attestation.Commit, "-"),
		firstNonEmpty(attestation.GraphSHA256, "-"),
		attestation.Verified,
		attestation.Artifacts,
		attestation.FilesChecked,
		attestation.Signature != nil,
	)
	if strings.TrimSpace(outPath) != "" {
		fmt.Fprintf(out, " out=%s", outPath)
	}
	fmt.Fprintln(out)
}

func renderProofGateText(out io.Writer, report proofGateReport, outPath string) {
	fmt.Fprintf(out, "Proof gate: %s\n", strings.ToUpper(passFail(report.Passed)))
	if report.Release != "" {
		fmt.Fprintf(out, "Release: %s\n", report.Release)
	}
	fmt.Fprintf(out, "Checks: %d passed, %d failed\n", report.Summary.Passed, report.Summary.Failed)
	fmt.Fprintf(out, "Verification: %s (%d file(s) checked, %d failed)\n", strings.ToUpper(passFail(report.Verification.Passed)), report.Verification.FilesChecked, report.Verification.FilesFailed)
	if strings.TrimSpace(outPath) != "" {
		fmt.Fprintf(out, "Gate JSON: %s\n", outPath)
	}
	if report.Summary.Failed == 0 {
		return
	}
	fmt.Fprintln(out, "Blocking checks:")
	for _, check := range report.Checks {
		if check.Passed {
			continue
		}
		fmt.Fprintf(out, "  - %s: %s\n", check.ID, check.Message)
		for _, item := range check.Evidence {
			fmt.Fprintf(out, "    %s\n", item)
		}
	}
}

package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/stack"
	"github.com/spf13/cobra"
)

const (
	proofGraphAPIVersion = "torque.dev/proof-graph/v1"
	proofGraphKind       = "ProofGraph"
)

type proofGraphOptions struct {
	Out    string
	HTML   string
	Key    string
	Attach []string
	Format string
}

type proofVerifyOptions struct {
	Pub              string
	Format           string
	RequireSignature bool
	StrictFiles      bool
}

type proofDiffOptions struct {
	Format string
}

type proofGraph struct {
	APIVersion  string               `json:"apiVersion"`
	Kind        string               `json:"kind"`
	GeneratedAt string               `json:"generatedAt"`
	Source      string               `json:"source"`
	SourceKind  string               `json:"sourceKind"`
	Release     string               `json:"release,omitempty"`
	Namespace   string               `json:"namespace,omitempty"`
	Chart       string               `json:"chart,omitempty"`
	Status      string               `json:"status,omitempty"`
	Blocked     bool                 `json:"blocked,omitempty"`
	Summary     proofGraphSummary    `json:"summary"`
	Artifacts   []proofGraphArtifact `json:"artifacts"`
	Links       []proofGraphLink     `json:"links,omitempty"`
	Signature   *proofGraphSignature `json:"signature,omitempty"`
}

type proofGraphSummary struct {
	Files           int  `json:"files"`
	MissingFiles    int  `json:"missingFiles,omitempty"`
	RequiredMissing int  `json:"requiredMissing,omitempty"`
	Images          int  `json:"images,omitempty"`
	UnpinnedImages  int  `json:"unpinnedImages,omitempty"`
	VerifierReports int  `json:"verifierReports,omitempty"`
	VerifierBlocked int  `json:"verifierBlocked,omitempty"`
	BuildCaptures   int  `json:"buildCaptures,omitempty"`
	SBOMs           int  `json:"sboms,omitempty"`
	Provenance      int  `json:"provenance,omitempty"`
	RuntimeDrift    int  `json:"runtimeDrift,omitempty"`
	RolloutEvidence int  `json:"rolloutEvidence,omitempty"`
	LogCaptures     int  `json:"logCaptures,omitempty"`
	Rollback        bool `json:"rollback,omitempty"`
	SLO             bool `json:"slo,omitempty"`
	RepairArtifacts int  `json:"repairArtifacts,omitempty"`
}

type proofGraphArtifact struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Label     string            `json:"label,omitempty"`
	Path      string            `json:"path,omitempty"`
	SHA256    string            `json:"sha256,omitempty"`
	Size      int64             `json:"size,omitempty"`
	Present   bool              `json:"present,omitempty"`
	Required  bool              `json:"required,omitempty"`
	Status    string            `json:"status,omitempty"`
	Digest    string            `json:"digest,omitempty"`
	Resource  string            `json:"resource,omitempty"`
	Container string            `json:"container,omitempty"`
	Count     int               `json:"count,omitempty"`
	Summary   map[string]any    `json:"summary,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type proofGraphLink struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

type proofGraphSignature struct {
	APIVersion  string `json:"apiVersion"`
	CreatedAt   string `json:"createdAt"`
	Algorithm   string `json:"algorithm"`
	PublicKey   string `json:"publicKey"`
	GraphSHA256 string `json:"graphSha256"`
	Signature   string `json:"signature"`
}

type proofVerifyReport struct {
	Version      string                     `json:"version"`
	Source       string                     `json:"source"`
	SourceKind   string                     `json:"sourceKind"`
	Release      string                     `json:"release,omitempty"`
	Namespace    string                     `json:"namespace,omitempty"`
	Passed       bool                       `json:"passed"`
	Signature    proofSignatureVerifyReport `json:"signature"`
	Artifacts    proofArtifactVerifyReport  `json:"artifacts"`
	GraphSummary proofGraphSummary          `json:"graphSummary"`
}

type proofSignatureVerifyReport struct {
	Present     bool   `json:"present"`
	Required    bool   `json:"required,omitempty"`
	Verified    bool   `json:"verified,omitempty"`
	Algorithm   string `json:"algorithm,omitempty"`
	GraphSHA256 string `json:"graphSha256,omitempty"`
	Error       string `json:"error,omitempty"`
}

type proofArtifactVerifyReport struct {
	Checked    int      `json:"checked"`
	Failed     int      `json:"failed,omitempty"`
	Missing    []string `json:"missing,omitempty"`
	Mismatched []string `json:"mismatched,omitempty"`
}

type proofDiffReport struct {
	Version      string           `json:"version"`
	From         string           `json:"from"`
	To           string           `json:"to"`
	Changed      bool             `json:"changed"`
	Summary      proofDiffSummary `json:"summary"`
	Added        []proofDiffItem  `json:"added,omitempty"`
	Removed      []proofDiffItem  `json:"removed,omitempty"`
	ChangedItems []proofDiffItem  `json:"changedItems,omitempty"`
}

type proofDiffSummary struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}

type proofDiffItem struct {
	ID     string `json:"id"`
	Type   string `json:"type,omitempty"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

func newProofCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proof",
		Short: "Build, verify, and diff release proof graphs",
		Long:  "Build a signed release proof graph from Torque proof bundles, verify graph integrity, and diff proof evidence across releases.",
	}
	cmd.AddCommand(newProofGraphCommand())
	cmd.AddCommand(newProofVerifyCommand())
	cmd.AddCommand(newProofDiffCommand())
	cmd.AddCommand(newProofAttestCommand())
	cmd.AddCommand(newProofGateCommand())
	decorateCommandHelp(cmd, "Proof Commands")
	return cmd
}

func newProofGraphCommand() *cobra.Command {
	opts := proofGraphOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "graph <proof-source>",
		Short: "Build a release proof graph from a Torque proof bundle",
		Long:  "Build a release proof graph by hashing and linking apply proof, build capture, verifier report, dry-run, drift, rollout, SLO, rollback, and repair evidence. Use --key to sign the graph with an ed25519 key from torque stack keygen.",
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
			graph, err := buildProofGraph(args[0], opts.Attach)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Key) != "" {
				if err := signProofGraph(&graph, opts.Key); err != nil {
					return err
				}
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, graph); err != nil {
					return fmt.Errorf("write proof graph: %w", err)
				}
			}
			if strings.TrimSpace(opts.HTML) != "" {
				if err := writeProofGraphHTML(opts.HTML, graph); err != nil {
					return err
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(graph, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
				return nil
			}
			renderProofGraphText(cmd.OutOrStdout(), graph, opts.Out, opts.HTML)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write proof graph JSON to this path")
	cmd.Flags().StringVar(&opts.HTML, "html", "", "Write a browser-readable proof graph report to this path")
	cmd.Flags().StringVar(&opts.Key, "key", "", "Sign graph with an ed25519 key JSON file from torque stack keygen")
	cmd.Flags().StringArrayVar(&opts.Attach, "attach", nil, "Attach additional evidence files or directories (repeatable)")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Proof Graph Flags")
	return cmd
}

func newProofVerifyCommand() *cobra.Command {
	opts := proofVerifyOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "verify <proof-source-or-graph>",
		Short: "Verify proof graph hashes and signatures",
		Long:  "Verify a saved proof graph JSON, or build and verify a graph directly from a Torque proof source. Signed graphs are checked with their embedded public key unless --pub is provided.",
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
			report, err := verifyProofSource(args[0], opts)
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
				renderProofVerifyText(cmd.OutOrStdout(), report)
			}
			if !report.Passed {
				return fmt.Errorf("proof verification failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Pub, "pub", "", "Optional trusted ed25519 public/private key JSON; overrides embedded public key")
	cmd.Flags().BoolVar(&opts.RequireSignature, "require-signature", false, "Fail when the proof graph is unsigned")
	cmd.Flags().BoolVar(&opts.StrictFiles, "strict-files", false, "Fail when any referenced file is missing, even if it was optional")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Proof Verify Flags")
	return cmd
}

func newProofDiffCommand() *cobra.Command {
	opts := proofDiffOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "diff <old-proof> <new-proof>",
		Short: "Diff two release proof graphs",
		Long:  "Compare two saved proof graphs or proof sources and report added, removed, and changed evidence artifacts.",
		Args:  cobra.ExactArgs(2),
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
			report, err := diffProofSources(args[0], args[1])
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
				return nil
			}
			renderProofDiffText(cmd.OutOrStdout(), report)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	decorateCommandHelp(cmd, "Proof Diff Flags")
	return cmd
}

func buildProofGraph(source string, attachments []string) (proofGraph, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return proofGraph{}, fmt.Errorf("proof source is required")
	}
	info, err := os.Stat(source)
	if err != nil {
		return proofGraph{}, fmt.Errorf("inspect proof source: %w", err)
	}
	var graph proofGraph
	if info.IsDir() {
		graph, err = buildProofGraphFromDir(source)
	} else {
		graph, err = buildProofGraphFromFile(source)
	}
	if err != nil {
		return proofGraph{}, err
	}
	for _, attach := range attachments {
		if err := attachProofEvidence(&graph, attach); err != nil {
			return proofGraph{}, err
		}
	}
	finalizeProofGraph(&graph)
	return graph, nil
}

func buildProofGraphFromDir(dir string) (proofGraph, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return proofGraph{}, fmt.Errorf("read proof manifest: %w", err)
	}
	var manifest applySimulationManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return proofGraph{}, fmt.Errorf("parse simulation manifest: %w", err)
	}
	if strings.TrimSpace(manifest.Tool) != "torque-apply-simulate" {
		return proofGraph{}, fmt.Errorf("%s does not look like a torque apply simulate proof directory", dir)
	}
	graph := newProofGraph(dir, "apply-simulation", manifest.Release, manifest.Namespace, manifest.Chart, manifest.Status, manifest.Blocked)
	addProofFileArtifact(&graph, "simulation.manifest", "simulation-manifest", "Simulation manifest", manifestPath, true, "")
	names := simulationFilesFromManifest(manifest)
	for _, name := range names {
		cleanName := filepath.ToSlash(strings.TrimSpace(name))
		if cleanName == "apply.proof.json" || strings.HasPrefix(cleanName, "fixes/") {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		addProofFileArtifact(&graph, proofID("simulation", name), classifyProofFileType(name), proofLabelForFile(name), path, true, proofStatusForFile(path))
	}
	applyPath := filepath.Join(dir, "apply.proof.json")
	if proof, err := loadApplyProof(applyPath); err == nil {
		addApplyProofArtifacts(&graph, proof, applyPath)
	}
	addAdjacentRepairArtifacts(&graph, dir)
	return graph, nil
}

func buildProofGraphFromFile(path string) (proofGraph, error) {
	if graph, ok, err := loadProofGraphFile(path); err != nil {
		return proofGraph{}, err
	} else if ok {
		graph.Source = firstNonEmpty(graph.Source, path)
		return graph, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return proofGraph{}, fmt.Errorf("read proof source: %w", err)
	}
	if proof, ok := decodeApplyProof(raw); ok {
		graph := newProofGraph(path, "apply-proof", proof.Release, proof.Namespace, proof.Chart, proof.Status, strings.EqualFold(proof.Status, "failed"))
		addApplyProofArtifacts(&graph, proof, path)
		addAdjacentRepairArtifacts(&graph, filepath.Dir(path))
		return graph, nil
	}
	if proof, ok := decodeGuardianDiffProof(raw); ok {
		graph := newProofGraph(path, "guardian-drift", proof.Release, proof.Namespace, proof.Chart, proof.Status, proof.Blocked)
		addProofFileArtifact(&graph, "guardian.drift", "runtime-drift", "Guardian drift proof", path, true, proof.Status)
		addProofArtifact(&graph, proofGraphArtifact{
			ID:     "helm.render",
			Type:   "helm-render",
			Label:  "Rendered manifest digest",
			Digest: proof.RenderedManifestSHA256,
			Status: proof.Status,
			Summary: map[string]any{
				"changed":                 proof.Summary.Changed,
				"missing":                 proof.Summary.Missing,
				"runtimeBoundaryFindings": proof.Summary.RuntimeBoundary,
				"warningEvents":           proof.Summary.WarningEvents,
			},
		})
		return graph, nil
	}
	if proof, ok := decodeGuardianRuntimeProof(raw); ok {
		graph := newProofGraph(path, "guardian-runtime", "", proof.Namespace, "", passFail(!proofHasGuardianWarnings(proof)), proofHasGuardianWarnings(proof))
		addProofFileArtifact(&graph, "guardian.runtime", "rollout-events", "Guardian runtime proof", path, true, passFail(!proofHasGuardianWarnings(proof)))
		return graph, nil
	}
	if proof, ok := decodeIncidentReplayProof(raw); ok {
		graph := newProofGraph(path, "incident-replay", proof.Release, proof.Namespace, "", passFail(proof.Passed), proof.Blocked)
		addProofFileArtifact(&graph, "incident.replay", "incident-replay", "Incident replay proof", path, true, passFail(proof.Passed))
		return graph, nil
	}
	if proof, ok := decodeRuntimeContractProof(raw); ok {
		graph := newProofGraph(path, "runtime-contract", proof.Release, proof.Namespace, "", passFail(proof.Passed), proof.Blocked)
		addProofFileArtifact(&graph, "runtime.contract", "runtime-contract", "Runtime contract proof", path, true, passFail(proof.Passed))
		return graph, nil
	}
	return proofGraph{}, fmt.Errorf("%s does not look like a supported Torque proof source", path)
}

func newProofGraph(source, sourceKind, release, namespace, chart, status string, blocked bool) proofGraph {
	return proofGraph{
		APIVersion:  proofGraphAPIVersion,
		Kind:        proofGraphKind,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Source:      strings.TrimSpace(source),
		SourceKind:  strings.TrimSpace(sourceKind),
		Release:     strings.TrimSpace(release),
		Namespace:   strings.TrimSpace(namespace),
		Chart:       strings.TrimSpace(chart),
		Status:      strings.TrimSpace(status),
		Blocked:     blocked,
	}
}

func addApplyProofArtifacts(graph *proofGraph, proof applyProofBundle, sourcePath string) {
	addProofFileArtifact(graph, "apply.proof", "apply-proof", "Apply proof bundle", sourcePath, true, proof.Status)
	if proof.Status != "" {
		graph.Status = firstNonEmpty(graph.Status, proof.Status)
		graph.Blocked = graph.Blocked || strings.EqualFold(proof.Status, "failed")
	}
	graph.Release = firstNonEmpty(graph.Release, proof.Release)
	graph.Namespace = firstNonEmpty(graph.Namespace, proof.Namespace)
	graph.Chart = firstNonEmpty(graph.Chart, proof.Chart)
	if commit, dirty := gitProofCommit(); commit != "" {
		status := "clean"
		if dirty {
			status = "dirty"
		}
		addProofArtifact(graph, proofGraphArtifact{
			ID:     "git.commit",
			Type:   "git-commit",
			Label:  "Git commit",
			Digest: commit,
			Status: status,
		})
		addProofLink(graph, "git.commit", "apply.proof", "recorded-by")
	}
	if proof.Plan != nil {
		addProofArtifact(graph, proofGraphArtifact{
			ID:     "helm.plan",
			Type:   "helm-plan",
			Label:  "Helm plan",
			Status: proof.Status,
			Summary: map[string]any{
				"creates":   proof.Plan.Summary.Creates,
				"updates":   proof.Plan.Summary.Updates,
				"deletes":   proof.Plan.Summary.Deletes,
				"unchanged": proof.Plan.Summary.Unchanged,
			},
		})
		addProofLink(graph, "apply.proof", "helm.plan", "contains")
		if proof.Plan.RenderedSHA256 != "" {
			addProofArtifact(graph, proofGraphArtifact{
				ID:     "helm.render",
				Type:   "helm-render",
				Label:  "Rendered manifest digest",
				Digest: proof.Plan.RenderedSHA256,
				Status: proof.Status,
			})
			addProofLink(graph, "helm.plan", "helm.render", "rendered")
		}
		addImageArtifacts(graph, proof.Plan.Images)
		for _, report := range proof.Plan.VerifyReports {
			id := proofID("verifier", report.Path, report.Tool)
			status := "passed"
			if report.Blocked || !report.Passed {
				status = "blocked"
			}
			if strings.TrimSpace(report.Path) != "" {
				addProofFileArtifact(graph, id, "verifier-report", "Verifier report", report.Path, false, status)
			} else {
				addProofArtifact(graph, proofGraphArtifact{ID: id, Type: "verifier-report", Label: "Verifier report", Status: status})
			}
			addProofLink(graph, id, "helm.render", "verifies")
		}
		for _, build := range proof.Plan.BuildProvenance {
			id := proofID("build", build.Source, build.Digest)
			if strings.TrimSpace(build.Source) != "" {
				addProofFileArtifact(graph, id, "build-capture", "BuildKit capture", build.Source, false, build.Verdict)
			} else {
				addProofArtifact(graph, proofGraphArtifact{ID: id, Type: "build-capture", Label: "BuildKit capture", Digest: build.Digest, Status: build.Verdict})
			}
			addProofArtifact(graph, proofGraphArtifact{
				ID:     proofID("provenance", build.Digest, build.Source),
				Type:   "supply-chain-provenance",
				Label:  "Build provenance",
				Digest: build.Digest,
				Status: build.Verdict,
				Count:  len(build.Attestations),
				Summary: map[string]any{
					"tags":       build.Tags,
					"platforms":  build.Platforms,
					"referenced": build.Referenced,
				},
			})
			if build.Digest != "" {
				addProofLink(graph, id, proofID("image", build.Digest), "produced")
			}
		}
	} else if proof.Prediction != nil {
		addImageArtifacts(graph, proof.Prediction.Images)
		if proof.Prediction.RenderedSHA256 != "" {
			addProofArtifact(graph, proofGraphArtifact{ID: "helm.render", Type: "helm-render", Label: "Rendered manifest digest", Digest: proof.Prediction.RenderedSHA256, Status: proof.Status})
		}
	}
	if proof.Prediction != nil {
		addProofArtifact(graph, proofGraphArtifact{
			ID:     "rollout.prediction",
			Type:   "rollout-prediction",
			Label:  "Rollout prediction",
			Status: proof.Prediction.Risk,
			Summary: map[string]any{
				"risk":                proof.Prediction.Risk,
				"missingDependencies": proof.Prediction.Summary.MissingDependencies,
				"restartingWorkloads": proof.Prediction.Summary.RestartingWorkloads,
				"rollbackConfidence":  proof.Prediction.Rollback.Confidence,
			},
		})
		addProofLink(graph, "rollout.prediction", "helm.plan", "scores")
	}
	if strings.TrimSpace(proof.CapturePath) != "" {
		addProofFileArtifact(graph, proofID("capture", proof.CapturePath), "rollout-capture", "Apply SQLite capture", proof.CapturePath, false, proof.Status)
	}
	if len(proof.ResourceSnapshot) > 0 {
		addProofArtifact(graph, proofGraphArtifact{
			ID:     "rollout.resources",
			Type:   "rollout-state",
			Label:  "Resource readiness snapshot",
			Status: proof.Status,
			Count:  len(proof.ResourceSnapshot),
		})
	}
	if proof.RollbackProof != nil {
		addRollbackProofArtifact(graph, *proof.RollbackProof)
	}
}

func addImageArtifacts(graph *proofGraph, images []planImageRef) {
	for _, image := range images {
		idSource := firstNonEmpty(image.Digest, image.Image, image.Resource)
		if strings.TrimSpace(idSource) == "" {
			continue
		}
		status := "pinned"
		if !image.Pinned {
			status = "unpinned"
		}
		addProofArtifact(graph, proofGraphArtifact{
			ID:        proofID("image", idSource),
			Type:      "image-digest",
			Label:     "Container image",
			Digest:    image.Digest,
			Resource:  image.Resource,
			Container: image.Container,
			Status:    status,
			Metadata: map[string]string{
				"image": image.Image,
			},
		})
		addProofLink(graph, "helm.render", proofID("image", idSource), "references")
	}
}

func addRollbackProofArtifact(graph *proofGraph, proof applyRollbackProof) {
	addProofArtifact(graph, proofGraphArtifact{
		ID:     "rollback.proof",
		Type:   "rollback-proof",
		Label:  "Rollback proof",
		Status: proof.Outcome,
		Summary: map[string]any{
			"mode":                 proof.Mode,
			"trigger":              proof.Trigger.Reason,
			"rolledBackToRevision": proof.RolledBackToRevision,
		},
	})
	addProofLink(graph, "rollback.proof", "apply.proof", "explains-failure")
	if proof.SLO != nil {
		status := "attached"
		if proof.Trigger.Source == "slo" || strings.Contains(strings.ToLower(proof.Trigger.Reason), "slo") {
			status = "failed"
		}
		if strings.TrimSpace(proof.SLO.Path) != "" {
			id := proofID("slo", proof.SLO.Path)
			addProofFileArtifact(graph, id, "slo-outcome", "Rollout SLO", proof.SLO.Path, false, status)
			addProofLink(graph, id, "rollback.proof", "triggered")
		} else {
			addProofArtifact(graph, proofGraphArtifact{
				ID:     "slo.outcome",
				Type:   "slo-outcome",
				Label:  "Rollout SLO",
				Digest: proof.SLO.SHA256,
				Status: status,
			})
		}
	}
}

func attachProofEvidence(graph *proofGraph, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect attached evidence %s: %w", path, err)
	}
	if info.IsDir() {
		attached, err := buildProofGraphFromDir(path)
		if err != nil {
			return err
		}
		mergeProofGraph(graph, attached, "attached")
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read attached evidence %s: %w", path, err)
	}
	if proof, ok := decodeGuardianDiffProof(raw); ok {
		id := proofID("guardian", path)
		addProofFileArtifact(graph, id, "runtime-drift", "Guardian drift proof", path, false, proof.Status)
		addProofLink(graph, id, "helm.render", "compares-live")
		graph.Blocked = graph.Blocked || proof.Blocked
		return nil
	}
	if proof, ok := decodeGuardianRuntimeProof(raw); ok {
		status := passFail(!proofHasGuardianWarnings(proof))
		addProofFileArtifact(graph, proofID("guardian-runtime", path), "rollout-events", "Guardian runtime proof", path, false, status)
		return nil
	}
	if proof, ok := decodeIncidentReplayProof(raw); ok {
		addProofFileArtifact(graph, proofID("incident", path), "incident-replay", "Incident replay proof", path, false, passFail(proof.Passed))
		graph.Blocked = graph.Blocked || proof.Blocked
		return nil
	}
	if proof, ok := decodeRuntimeContractProof(raw); ok {
		addProofFileArtifact(graph, proofID("contract", path), "runtime-contract", "Runtime contract proof", path, false, passFail(proof.Passed))
		graph.Blocked = graph.Blocked || proof.Blocked
		return nil
	}
	addProofFileArtifact(graph, proofID("attach", path), classifyProofFileType(path), proofLabelForFile(path), path, false, proofStatusForFile(path))
	return nil
}

func addAdjacentRepairArtifacts(graph *proofGraph, dir string) {
	for _, rel := range []string{
		filepath.Join("fixes", "pr.md"),
		filepath.Join("fixes", "fix.patch"),
		filepath.Join("fix", "pr.md"),
		filepath.Join("fix", "fix.patch"),
		"repair-pr.md",
		"root-cause.json",
	} {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); err == nil {
			typ := classifyProofFileType(path)
			addProofFileArtifact(graph, proofID("repair", rel), typ, proofLabelForFile(path), path, false, "ready")
		}
	}
}

func mergeProofGraph(dst *proofGraph, src proofGraph, relation string) {
	for _, artifact := range src.Artifacts {
		addProofArtifact(dst, artifact)
		if relation != "" {
			addProofLink(dst, "apply.proof", artifact.ID, relation)
		}
	}
	for _, link := range src.Links {
		addProofLink(dst, link.From, link.To, link.Relation)
	}
	dst.Blocked = dst.Blocked || src.Blocked
}

func addProofFileArtifact(graph *proofGraph, id, typ, label, path string, required bool, status string) string {
	artifact := proofGraphArtifact{
		ID:       id,
		Type:     typ,
		Label:    label,
		Path:     filepath.ToSlash(strings.TrimSpace(path)),
		Required: required,
		Status:   strings.TrimSpace(status),
	}
	if path != "" {
		if sha, size, err := sha256HexFileLocal(path); err == nil {
			artifact.Present = true
			artifact.SHA256 = sha
			artifact.Size = size
		}
	}
	return addProofArtifact(graph, artifact)
}

func addProofArtifact(graph *proofGraph, artifact proofGraphArtifact) string {
	if graph == nil {
		return ""
	}
	artifact.ID = strings.TrimSpace(artifact.ID)
	if artifact.ID == "" {
		artifact.ID = proofID(artifact.Type, artifact.Path, artifact.Digest, artifact.Label)
	}
	artifact.Type = strings.TrimSpace(artifact.Type)
	if artifact.Type == "" {
		artifact.Type = "evidence"
	}
	for i := range graph.Artifacts {
		if graph.Artifacts[i].ID != artifact.ID {
			continue
		}
		graph.Artifacts[i] = mergeProofArtifact(graph.Artifacts[i], artifact)
		return artifact.ID
	}
	graph.Artifacts = append(graph.Artifacts, artifact)
	return artifact.ID
}

func mergeProofArtifact(existing, incoming proofGraphArtifact) proofGraphArtifact {
	if existing.Type == "" {
		existing.Type = incoming.Type
	}
	existing.Label = firstNonEmpty(existing.Label, incoming.Label)
	existing.Path = firstNonEmpty(existing.Path, incoming.Path)
	existing.SHA256 = firstNonEmpty(existing.SHA256, incoming.SHA256)
	if existing.Size == 0 {
		existing.Size = incoming.Size
	}
	existing.Present = existing.Present || incoming.Present
	existing.Required = existing.Required || incoming.Required
	existing.Status = firstNonEmpty(existing.Status, incoming.Status)
	existing.Digest = firstNonEmpty(existing.Digest, incoming.Digest)
	existing.Resource = firstNonEmpty(existing.Resource, incoming.Resource)
	existing.Container = firstNonEmpty(existing.Container, incoming.Container)
	if existing.Count == 0 {
		existing.Count = incoming.Count
	}
	if existing.Summary == nil {
		existing.Summary = incoming.Summary
	}
	if existing.Metadata == nil {
		existing.Metadata = incoming.Metadata
	}
	return existing
}

func addProofLink(graph *proofGraph, from, to, relation string) {
	if graph == nil {
		return
	}
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	relation = strings.TrimSpace(relation)
	if from == "" || to == "" || from == to {
		return
	}
	if relation == "" {
		relation = "related"
	}
	for _, link := range graph.Links {
		if link.From == from && link.To == to && link.Relation == relation {
			return
		}
	}
	graph.Links = append(graph.Links, proofGraphLink{From: from, To: to, Relation: relation})
}

func finalizeProofGraph(graph *proofGraph) {
	if graph == nil {
		return
	}
	sort.Slice(graph.Artifacts, func(i, j int) bool {
		return graph.Artifacts[i].ID < graph.Artifacts[j].ID
	})
	sort.Slice(graph.Links, func(i, j int) bool {
		if graph.Links[i].From != graph.Links[j].From {
			return graph.Links[i].From < graph.Links[j].From
		}
		if graph.Links[i].To != graph.Links[j].To {
			return graph.Links[i].To < graph.Links[j].To
		}
		return graph.Links[i].Relation < graph.Links[j].Relation
	})
	graph.Summary = proofGraphSummary{}
	for _, artifact := range graph.Artifacts {
		if artifact.Path != "" {
			graph.Summary.Files++
			if !artifact.Present {
				graph.Summary.MissingFiles++
				if artifact.Required {
					graph.Summary.RequiredMissing++
				}
			}
		}
		switch artifact.Type {
		case "image-digest":
			graph.Summary.Images++
			if strings.EqualFold(artifact.Status, "unpinned") {
				graph.Summary.UnpinnedImages++
			}
		case "verifier-report":
			graph.Summary.VerifierReports++
			if strings.EqualFold(artifact.Status, "blocked") || strings.EqualFold(artifact.Status, "failed") {
				graph.Summary.VerifierBlocked++
			}
		case "build-capture":
			graph.Summary.BuildCaptures++
		case "sbom":
			graph.Summary.SBOMs++
		case "supply-chain-provenance", "provenance", "attestation":
			graph.Summary.Provenance++
		case "runtime-drift":
			graph.Summary.RuntimeDrift++
		case "rollout-capture", "rollout-events", "rollout-state", "server-dry-run":
			graph.Summary.RolloutEvidence++
		case "logs-capture":
			graph.Summary.LogCaptures++
		case "rollback-proof":
			graph.Summary.Rollback = true
		case "slo-outcome":
			graph.Summary.SLO = true
		case "repair-pr", "repair-patch":
			graph.Summary.RepairArtifacts++
		}
	}
}

func signProofGraph(graph *proofGraph, keyPath string) error {
	if graph == nil {
		return fmt.Errorf("proof graph is nil")
	}
	_, _, priv, err := stack.LoadBundleKey(keyPath)
	if err != nil {
		return err
	}
	if len(priv) == 0 {
		return fmt.Errorf("key %s does not contain a private key", keyPath)
	}
	_, sum, err := proofGraphSigningBytes(*graph)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(priv, sum[:])
	pub := priv.Public().(ed25519.PublicKey)
	graph.Signature = &proofGraphSignature{
		APIVersion:  proofGraphAPIVersion + "/signature",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Algorithm:   "ed25519",
		PublicKey:   base64.StdEncoding.EncodeToString(pub),
		GraphSHA256: hex.EncodeToString(sum[:]),
		Signature:   base64.StdEncoding.EncodeToString(signature),
	}
	return nil
}

func proofGraphSigningBytes(graph proofGraph) ([]byte, [32]byte, error) {
	graph.Signature = nil
	raw, err := json.Marshal(graph)
	if err != nil {
		return nil, [32]byte{}, err
	}
	sum := sha256.Sum256(raw)
	return raw, sum, nil
}

func verifyProofSource(path string, opts proofVerifyOptions) (proofVerifyReport, error) {
	graph, graphFile, err := loadOrBuildProofGraph(path)
	if err != nil {
		return proofVerifyReport{}, err
	}
	baseDir := ""
	if graphFile {
		baseDir = filepath.Dir(path)
	}
	return verifyProofGraph(graph, baseDir, opts), nil
}

func verifyProofGraph(graph proofGraph, baseDir string, opts proofVerifyOptions) proofVerifyReport {
	finalizeProofGraph(&graph)
	report := proofVerifyReport{
		Version:      "v1",
		Source:       graph.Source,
		SourceKind:   graph.SourceKind,
		Release:      graph.Release,
		Namespace:    graph.Namespace,
		Passed:       true,
		GraphSummary: graph.Summary,
	}
	report.Signature.Required = opts.RequireSignature
	if graph.Signature == nil {
		report.Signature.Present = false
		if opts.RequireSignature {
			report.Signature.Error = "signature is required but missing"
			report.Passed = false
		}
	} else {
		report.Signature.Present = true
		report.Signature.Algorithm = graph.Signature.Algorithm
		report.Signature.GraphSHA256 = graph.Signature.GraphSHA256
		if err := verifyProofGraphSignature(graph, opts.Pub); err != nil {
			report.Signature.Error = err.Error()
			report.Passed = false
		} else {
			report.Signature.Verified = true
		}
	}
	for _, artifact := range graph.Artifacts {
		if strings.TrimSpace(artifact.Path) == "" || strings.TrimSpace(artifact.SHA256) == "" {
			continue
		}
		path := resolveProofArtifactPath(baseDir, artifact.Path)
		got, _, err := sha256HexFileLocal(path)
		if err != nil {
			if artifact.Required || artifact.Present || opts.StrictFiles {
				report.Artifacts.Missing = append(report.Artifacts.Missing, artifact.Path)
				report.Artifacts.Failed++
				report.Passed = false
			}
			continue
		}
		report.Artifacts.Checked++
		if got != artifact.SHA256 {
			report.Artifacts.Mismatched = append(report.Artifacts.Mismatched, artifact.Path)
			report.Artifacts.Failed++
			report.Passed = false
		}
	}
	sort.Strings(report.Artifacts.Missing)
	sort.Strings(report.Artifacts.Mismatched)
	return report
}

func verifyProofGraphSignature(graph proofGraph, pubPath string) error {
	if graph.Signature == nil {
		return fmt.Errorf("signature missing")
	}
	if strings.TrimSpace(graph.Signature.Algorithm) != "ed25519" {
		return fmt.Errorf("unsupported signature algorithm %q", graph.Signature.Algorithm)
	}
	_, sum, err := proofGraphSigningBytes(graph)
	if err != nil {
		return err
	}
	gotSHA := hex.EncodeToString(sum[:])
	if strings.TrimSpace(graph.Signature.GraphSHA256) != "" && strings.TrimSpace(graph.Signature.GraphSHA256) != gotSHA {
		return fmt.Errorf("graph sha256 mismatch (want %s got %s)", graph.Signature.GraphSHA256, gotSHA)
	}
	pubB64 := strings.TrimSpace(graph.Signature.PublicKey)
	if strings.TrimSpace(pubPath) != "" {
		_, pub, _, err := stack.LoadBundleKey(pubPath)
		if err != nil {
			return err
		}
		pubB64 = base64.StdEncoding.EncodeToString(pub)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return fmt.Errorf("decode publicKey: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("publicKey has length %d (want %d)", len(pubBytes), ed25519.PublicKeySize)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(graph.Signature.Signature))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), sum[:], sigBytes) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func diffProofSources(oldPath, newPath string) (proofDiffReport, error) {
	oldGraph, _, err := loadOrBuildProofGraph(oldPath)
	if err != nil {
		return proofDiffReport{}, err
	}
	newGraph, _, err := loadOrBuildProofGraph(newPath)
	if err != nil {
		return proofDiffReport{}, err
	}
	finalizeProofGraph(&oldGraph)
	finalizeProofGraph(&newGraph)
	report := proofDiffReport{Version: "v1", From: oldPath, To: newPath}
	oldMap := proofArtifactMap(oldGraph.Artifacts)
	newMap := proofArtifactMap(newGraph.Artifacts)
	for id, artifact := range newMap {
		if _, ok := oldMap[id]; !ok {
			report.Added = append(report.Added, proofDiffItem{ID: id, Type: artifact.Type, After: proofArtifactFingerprint(artifact)})
		}
	}
	for id, artifact := range oldMap {
		if _, ok := newMap[id]; !ok {
			report.Removed = append(report.Removed, proofDiffItem{ID: id, Type: artifact.Type, Before: proofArtifactFingerprint(artifact)})
		}
	}
	for id, oldArtifact := range oldMap {
		newArtifact, ok := newMap[id]
		if !ok {
			continue
		}
		before := proofArtifactFingerprint(oldArtifact)
		after := proofArtifactFingerprint(newArtifact)
		if before != after {
			report.ChangedItems = append(report.ChangedItems, proofDiffItem{ID: id, Type: newArtifact.Type, Before: before, After: after})
		}
	}
	sortProofDiffItems(report.Added)
	sortProofDiffItems(report.Removed)
	sortProofDiffItems(report.ChangedItems)
	report.Summary = proofDiffSummary{Added: len(report.Added), Removed: len(report.Removed), Changed: len(report.ChangedItems)}
	report.Changed = report.Summary.Added+report.Summary.Removed+report.Summary.Changed > 0 || oldGraph.Status != newGraph.Status || oldGraph.Blocked != newGraph.Blocked
	if oldGraph.Status != newGraph.Status {
		report.ChangedItems = append(report.ChangedItems, proofDiffItem{ID: "release.status", Type: "summary", Before: oldGraph.Status, After: newGraph.Status})
		report.Summary.Changed++
	}
	if oldGraph.Blocked != newGraph.Blocked {
		report.ChangedItems = append(report.ChangedItems, proofDiffItem{ID: "release.blocked", Type: "summary", Before: fmt.Sprintf("%t", oldGraph.Blocked), After: fmt.Sprintf("%t", newGraph.Blocked)})
		report.Summary.Changed++
	}
	return report, nil
}

func loadOrBuildProofGraph(path string) (proofGraph, bool, error) {
	if graph, ok, err := loadProofGraphFile(path); err != nil {
		return proofGraph{}, false, err
	} else if ok {
		return graph, true, nil
	}
	graph, err := buildProofGraph(path, nil)
	return graph, false, err
}

func loadProofGraphFile(path string) (proofGraph, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return proofGraph{}, false, nil
	}
	var marker struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		return proofGraph{}, false, nil
	}
	if strings.TrimSpace(marker.APIVersion) != proofGraphAPIVersion && strings.TrimSpace(marker.Kind) != proofGraphKind {
		return proofGraph{}, false, nil
	}
	var graph proofGraph
	if err := json.Unmarshal(raw, &graph); err != nil {
		return proofGraph{}, true, fmt.Errorf("parse proof graph: %w", err)
	}
	return graph, true, nil
}

func decodeApplyProof(raw []byte) (applyProofBundle, bool) {
	var proof applyProofBundle
	if err := json.Unmarshal(raw, &proof); err != nil {
		return applyProofBundle{}, false
	}
	if strings.TrimSpace(proof.Release) == "" || proof.Version == 0 {
		return applyProofBundle{}, false
	}
	return proof, true
}

func loadApplyProof(path string) (applyProofBundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return applyProofBundle{}, err
	}
	proof, ok := decodeApplyProof(raw)
	if !ok {
		return applyProofBundle{}, fmt.Errorf("%s does not look like an apply proof", path)
	}
	return proof, nil
}

func decodeGuardianDiffProof(raw []byte) (guardianDiffProof, bool) {
	var proof guardianDiffProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return guardianDiffProof{}, false
	}
	if proof.Tool != guardianTool || proof.Version == "" || strings.TrimSpace(proof.Status) == "" {
		return guardianDiffProof{}, false
	}
	return proof, true
}

func decodeGuardianRuntimeProof(raw []byte) (guardianRuntimeProof, bool) {
	var proof guardianRuntimeProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return guardianRuntimeProof{}, false
	}
	if proof.Tool != guardianTool || proof.Version == "" || proof.EventsTimeline.Version == "" {
		return guardianRuntimeProof{}, false
	}
	return proof, true
}

func decodeIncidentReplayProof(raw []byte) (incidentReplayProof, bool) {
	var proof incidentReplayProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return incidentReplayProof{}, false
	}
	if proof.Tool != incidentTool || proof.Version == "" {
		return incidentReplayProof{}, false
	}
	return proof, true
}

func decodeRuntimeContractProof(raw []byte) (runtimeContractProof, bool) {
	var proof runtimeContractProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return runtimeContractProof{}, false
	}
	if proof.Tool != contractTool || proof.Version == "" {
		return runtimeContractProof{}, false
	}
	return proof, true
}

func proofHasGuardianWarnings(proof guardianRuntimeProof) bool {
	return proof.Summary.Warnings > 0
}

func classifyProofFileType(path string) string {
	name := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(name, "server-dry-run"):
		return "server-dry-run"
	case strings.Contains(name, "admission"):
		return "admission-proof"
	case strings.Contains(name, "field-ownership"):
		return "field-ownership-proof"
	case strings.Contains(name, "quota"):
		return "quota-proof"
	case strings.Contains(name, "rollout.prediction"):
		return "rollout-prediction"
	case strings.Contains(name, "verifier") || strings.Contains(name, "verify"):
		return "verifier-report"
	case strings.Contains(name, "slo"):
		return "slo-outcome"
	case strings.Contains(name, "predicted-live-state"):
		return "predicted-live-state"
	case strings.Contains(name, "apply.proof") || strings.Contains(name, "apply-proof"):
		return "apply-proof"
	case strings.Contains(name, "drift"):
		return "runtime-drift"
	case strings.Contains(name, "event") || strings.Contains(name, "aftercare"):
		return "rollout-events"
	case strings.Contains(name, "logs") && strings.HasSuffix(name, ".sqlite"):
		return "logs-capture"
	case strings.HasSuffix(name, ".sqlite"):
		return "rollout-capture"
	case strings.Contains(name, "sbom"):
		return "sbom"
	case strings.Contains(name, "provenance") || strings.Contains(name, "attestation"):
		return "supply-chain-provenance"
	case base == "pr.md":
		return "repair-pr"
	case strings.HasSuffix(name, ".patch"):
		return "repair-patch"
	case base == "manifest.json":
		return "manifest"
	default:
		return "evidence"
	}
}

func proofLabelForFile(path string) string {
	switch classifyProofFileType(path) {
	case "server-dry-run":
		return "Server-side dry-run"
	case "admission-proof":
		return "Admission results"
	case "field-ownership-proof":
		return "Field ownership conflicts"
	case "quota-proof":
		return "Quota capacity risk"
	case "rollout-prediction":
		return "Rollout prediction"
	case "verifier-report":
		return "Verifier report"
	case "slo-outcome":
		return "SLO gate outcome"
	case "predicted-live-state":
		return "Predicted live state"
	case "apply-proof":
		return "Apply proof bundle"
	case "runtime-drift":
		return "Runtime drift proof"
	case "rollout-events":
		return "Rollout events"
	case "logs-capture":
		return "Logs capture"
	case "rollout-capture":
		return "SQLite capture"
	case "sbom":
		return "SBOM"
	case "supply-chain-provenance":
		return "Supply-chain provenance"
	case "repair-pr":
		return "Repair PR"
	case "repair-patch":
		return "Repair patch"
	case "manifest":
		return "Bundle manifest"
	default:
		return filepath.Base(path)
	}
}

func proofStatusForFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	for _, key := range []string{"status", "outcome", "verdict"} {
		if value, ok := fields[key].(string); ok {
			return value
		}
	}
	if blocked, ok := fields["blocked"].(bool); ok && blocked {
		return "blocked"
	}
	if passed, ok := fields["passed"].(bool); ok {
		return passFail(passed)
	}
	return ""
}

func resolveProofArtifactPath(baseDir, path string) string {
	path = filepath.FromSlash(strings.TrimSpace(path))
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if strings.TrimSpace(baseDir) != "" {
		return filepath.Join(baseDir, path)
	}
	return path
}

func sha256HexFileLocal(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func proofArtifactMap(artifacts []proofGraphArtifact) map[string]proofGraphArtifact {
	out := make(map[string]proofGraphArtifact, len(artifacts))
	for _, artifact := range artifacts {
		out[artifact.ID] = artifact
	}
	return out
}

func proofArtifactFingerprint(artifact proofGraphArtifact) string {
	summary, _ := json.Marshal(artifact.Summary)
	metadata, _ := json.Marshal(artifact.Metadata)
	parts := []string{
		artifact.Type,
		artifact.Status,
		artifact.Digest,
		artifact.SHA256,
		artifact.Path,
		fmt.Sprintf("count=%d", artifact.Count),
		fmt.Sprintf("present=%t", artifact.Present),
		string(summary),
		string(metadata),
	}
	return strings.Join(parts, "|")
}

func sortProofDiffItems(items []proofDiffItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
}

func proofID(parts ...string) string {
	var joined []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			joined = append(joined, part)
		}
	}
	if len(joined) == 0 {
		return "evidence"
	}
	id := strings.ToLower(strings.Join(joined, "."))
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('.')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "evidence"
	}
	if len(out) > 96 {
		sum := sha256.Sum256([]byte(id))
		out = strings.TrimRight(out[:72], ".") + "." + hex.EncodeToString(sum[:])[:16]
	}
	return out
}

func gitProofCommit() (string, bool) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	raw, err := cmd.Output()
	if err != nil {
		return "", false
	}
	commit := strings.TrimSpace(string(raw))
	if commit == "" {
		return "", false
	}
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusRaw, err := statusCmd.Output()
	return commit, err == nil && strings.TrimSpace(string(statusRaw)) != ""
}

func writeProofGraphHTML(path string, graph proofGraph) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, []byte(renderProofGraphHTML(graph)), 0o644)
}

func renderProofGraphText(out io.Writer, graph proofGraph, outPath, htmlPath string) {
	fmt.Fprintf(out, "Proof graph: %s\n", strings.ToUpper(firstNonEmpty(graph.Status, "unknown")))
	if graph.Release != "" {
		fmt.Fprintf(out, "Release: %s\n", graph.Release)
	}
	if graph.Namespace != "" {
		fmt.Fprintf(out, "Namespace: %s\n", graph.Namespace)
	}
	fmt.Fprintf(out, "Evidence: %d artifact(s), %d file(s), %d missing\n", len(graph.Artifacts), graph.Summary.Files, graph.Summary.MissingFiles)
	if graph.Signature != nil {
		fmt.Fprintf(out, "Signature: ed25519 %s\n", graph.Signature.GraphSHA256)
	} else {
		fmt.Fprintln(out, "Signature: unsigned")
	}
	if strings.TrimSpace(outPath) != "" {
		fmt.Fprintf(out, "Graph JSON: %s\n", outPath)
	}
	if strings.TrimSpace(htmlPath) != "" {
		fmt.Fprintf(out, "Graph HTML: %s\n", htmlPath)
	}
}

func renderProofVerifyText(out io.Writer, report proofVerifyReport) {
	fmt.Fprintf(out, "Proof verify: %s\n", strings.ToUpper(passFail(report.Passed)))
	fmt.Fprintf(out, "Kind: %s\n", report.SourceKind)
	if report.Release != "" {
		fmt.Fprintf(out, "Release: %s\n", report.Release)
	}
	if report.Signature.Present {
		status := "verified"
		if !report.Signature.Verified {
			status = "failed"
		}
		fmt.Fprintf(out, "Signature: %s", status)
		if report.Signature.GraphSHA256 != "" {
			fmt.Fprintf(out, " (%s)", report.Signature.GraphSHA256)
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "Signature: unsigned")
	}
	fmt.Fprintf(out, "Artifacts checked: %d\n", report.Artifacts.Checked)
	if len(report.Artifacts.Missing) > 0 {
		fmt.Fprintln(out, "Missing files:")
		for _, path := range report.Artifacts.Missing {
			fmt.Fprintf(out, "  - %s\n", path)
		}
	}
	if len(report.Artifacts.Mismatched) > 0 {
		fmt.Fprintln(out, "Hash mismatches:")
		for _, path := range report.Artifacts.Mismatched {
			fmt.Fprintf(out, "  - %s\n", path)
		}
	}
	if report.Signature.Error != "" {
		fmt.Fprintf(out, "Signature error: %s\n", report.Signature.Error)
	}
}

func renderProofDiffText(out io.Writer, report proofDiffReport) {
	fmt.Fprintf(out, "Proof diff: %s\n", strings.ToUpper(mapBool(!report.Changed, "unchanged", "changed")))
	fmt.Fprintf(out, "Added: %d, removed: %d, changed: %d\n", report.Summary.Added, report.Summary.Removed, report.Summary.Changed)
	writeProofDiffSection(out, "Added", report.Added)
	writeProofDiffSection(out, "Removed", report.Removed)
	writeProofDiffSection(out, "Changed", report.ChangedItems)
}

func writeProofDiffSection(out io.Writer, title string, items []proofDiffItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "%s:\n", title)
	for _, item := range items {
		fmt.Fprintf(out, "  - %s (%s)", item.ID, item.Type)
		if item.Before != "" || item.After != "" {
			fmt.Fprintf(out, ": %s -> %s", item.Before, item.After)
		}
		fmt.Fprintln(out)
	}
}

func renderProofGraphHTML(graph proofGraph) string {
	status := firstNonEmpty(graph.Status, "unknown")
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>Torque Proof Graph</title>")
	b.WriteString(`<style>
:root{color-scheme:light;--bg:#f7f8fb;--ink:#18212f;--muted:#5d6878;--line:#d8dee8;--panel:#ffffff;--ok:#13795b;--warn:#a15c00;--bad:#b42318;--blue:#2457c5}
body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
header{background:#101828;color:#fff;padding:28px max(24px,calc((100vw - 1120px)/2));}
main{max-width:1120px;margin:0 auto;padding:24px}
h1{font-size:28px;line-height:1.15;margin:0 0 8px}
h2{font-size:18px;margin:28px 0 10px}
p{margin:0;color:#d4dae5}
.meta{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;margin-top:18px}
.metric{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:12px}
.metric strong{display:block;font-size:20px}
.metric span{color:var(--muted)}
table{width:100%;border-collapse:collapse;background:var(--panel);border:1px solid var(--line);border-radius:8px;overflow:hidden}
th,td{text-align:left;border-bottom:1px solid var(--line);padding:9px 10px;vertical-align:top;word-break:break-word}
th{font-size:12px;color:var(--muted);text-transform:uppercase;background:#eef2f7}
tr:last-child td{border-bottom:0}
.pill{display:inline-block;border-radius:999px;padding:2px 8px;font-size:12px;background:#eef2f7;color:var(--muted)}
.ok{color:var(--ok)}.warn{color:var(--warn)}.bad{color:var(--bad)}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px}
</style></head><body>`)
	fmt.Fprintf(&b, "<header><h1>Torque Proof Graph</h1><p>%s / %s / %s</p></header>", html.EscapeString(firstNonEmpty(graph.Release, "-")), html.EscapeString(firstNonEmpty(graph.Namespace, "-")), html.EscapeString(status))
	b.WriteString("<main>")
	b.WriteString("<section class=\"meta\">")
	writeHTMLMetric(&b, "Artifacts", fmt.Sprintf("%d", len(graph.Artifacts)))
	writeHTMLMetric(&b, "Files", fmt.Sprintf("%d", graph.Summary.Files))
	writeHTMLMetric(&b, "Images", fmt.Sprintf("%d", graph.Summary.Images))
	writeHTMLMetric(&b, "Missing", fmt.Sprintf("%d", graph.Summary.MissingFiles))
	if graph.Signature != nil {
		writeHTMLMetric(&b, "Signature", "signed")
	} else {
		writeHTMLMetric(&b, "Signature", "unsigned")
	}
	b.WriteString("</section>")
	b.WriteString("<h2>Artifacts</h2><table><thead><tr><th>ID</th><th>Type</th><th>Status</th><th>Path / Digest</th><th>SHA256</th></tr></thead><tbody>")
	for _, artifact := range graph.Artifacts {
		statusClass := "warn"
		if strings.EqualFold(artifact.Status, "passed") || strings.EqualFold(artifact.Status, "succeeded") || strings.EqualFold(artifact.Status, "clean") || strings.EqualFold(artifact.Status, "safe") || strings.EqualFold(artifact.Status, "pinned") || strings.EqualFold(artifact.Status, "ready") {
			statusClass = "ok"
		}
		if strings.EqualFold(artifact.Status, "failed") || strings.EqualFold(artifact.Status, "blocked") || strings.EqualFold(artifact.Status, "drifted") || strings.EqualFold(artifact.Status, "unpinned") {
			statusClass = "bad"
		}
		ref := firstNonEmpty(artifact.Path, artifact.Digest, artifact.Resource, "-")
		fmt.Fprintf(&b, "<tr><td class=\"mono\">%s</td><td>%s</td><td class=\"%s\">%s</td><td class=\"mono\">%s</td><td class=\"mono\">%s</td></tr>",
			html.EscapeString(artifact.ID),
			html.EscapeString(artifact.Type),
			statusClass,
			html.EscapeString(firstNonEmpty(artifact.Status, "-")),
			html.EscapeString(ref),
			html.EscapeString(shortSHA(artifact.SHA256)),
		)
	}
	b.WriteString("</tbody></table>")
	if len(graph.Links) > 0 {
		b.WriteString("<h2>Links</h2><table><thead><tr><th>From</th><th>Relation</th><th>To</th></tr></thead><tbody>")
		for _, link := range graph.Links {
			fmt.Fprintf(&b, "<tr><td class=\"mono\">%s</td><td><span class=\"pill\">%s</span></td><td class=\"mono\">%s</td></tr>", html.EscapeString(link.From), html.EscapeString(link.Relation), html.EscapeString(link.To))
		}
		b.WriteString("</tbody></table>")
	}
	if graph.Signature != nil {
		b.WriteString("<h2>Signature</h2><table><tbody>")
		fmt.Fprintf(&b, "<tr><th>Algorithm</th><td>%s</td></tr>", html.EscapeString(graph.Signature.Algorithm))
		fmt.Fprintf(&b, "<tr><th>Graph SHA256</th><td class=\"mono\">%s</td></tr>", html.EscapeString(graph.Signature.GraphSHA256))
		fmt.Fprintf(&b, "<tr><th>Public key</th><td class=\"mono\">%s</td></tr>", html.EscapeString(graph.Signature.PublicKey))
		b.WriteString("</tbody></table>")
	}
	b.WriteString("</main></body></html>")
	return b.String()
}

func writeHTMLMetric(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "<div class=\"metric\"><strong>%s</strong><span>%s</span></div>", html.EscapeString(value), html.EscapeString(label))
}

func shortSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}

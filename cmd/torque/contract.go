package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const contractTool = "torque-contract"

type contractSynthesizeOptions struct {
	From     string
	Guardian string
	Out      string
	Format   string
}

type contractTestOptions struct {
	Contract      string
	From          string
	Guardian      string
	Out           string
	Format        string
	FailOnBlocked bool
}

type contractPROptions struct {
	Contract string
	Proof    string
	Branch   string
	OutDir   string
	Format   string
}

type runtimeContract struct {
	APIVersion string                  `json:"apiVersion"`
	Kind       string                  `json:"kind"`
	Metadata   runtimeContractMetadata `json:"metadata"`
	Spec       runtimeContractSpec     `json:"spec"`
}

type runtimeContractMetadata struct {
	Name        string            `json:"name"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type runtimeContractSpec struct {
	Release     string                  `json:"release,omitempty"`
	Namespace   string                  `json:"namespace,omitempty"`
	ObserveOnly bool                    `json:"observeOnly"`
	Sources     []runtimeContractSource `json:"sources,omitempty"`
	Invariants  []runtimeInvariant      `json:"invariants"`
}

type runtimeContractSource struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Status    string `json:"status,omitempty"`
	Blocked   bool   `json:"blocked,omitempty"`
	Generated string `json:"generatedAt,omitempty"`
}

type runtimeInvariant struct {
	ID          string               `json:"id"`
	Type        string               `json:"type"`
	Severity    string               `json:"severity"`
	Description string               `json:"description"`
	Value       string               `json:"value,omitempty"`
	Resource    *guardianResourceRef `json:"resource,omitempty"`
	Required    bool                 `json:"required"`
}

type runtimeContractProof struct {
	Version     string                 `json:"version"`
	Tool        string                 `json:"tool"`
	GeneratedAt string                 `json:"generatedAt"`
	Contract    string                 `json:"contract,omitempty"`
	From        string                 `json:"from,omitempty"`
	Guardian    string                 `json:"guardian,omitempty"`
	Release     string                 `json:"release,omitempty"`
	Namespace   string                 `json:"namespace,omitempty"`
	Passed      bool                   `json:"passed"`
	Blocked     bool                   `json:"blocked"`
	Summary     runtimeContractSummary `json:"summary"`
	Results     []runtimeContractCheck `json:"results,omitempty"`
}

type runtimeContractSummary struct {
	Invariants int `json:"invariants"`
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	Missing    int `json:"missingEvidence"`
	Critical   int `json:"criticalFailures"`
}

type runtimeContractCheck struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Severity string               `json:"severity"`
	Passed   bool                 `json:"passed"`
	Missing  bool                 `json:"missingEvidence,omitempty"`
	Message  string               `json:"message"`
	Resource *guardianResourceRef `json:"resource,omitempty"`
	Evidence []string             `json:"evidence,omitempty"`
}

type contractEvidence struct {
	IncidentPath string
	GuardianPath string
	Bundle       incidentBundle
	RootCause    incidentRootCause
	Replay       incidentReplayProof
	Guardian     guardianDiffProof
	HasIncident  bool
	HasRootCause bool
	HasReplay    bool
	HasGuardian  bool
}

func newContractCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contract",
		Short: "Synthesize and test runtime contracts from proof",
		Long:  "Runtime Contract turns Guardian drift proof and Incident replay proof into observe-only recurrence rules, machine-checkable test proof, and PR-ready artifacts.",
	}
	cmd.AddCommand(newContractSynthesizeCommand())
	cmd.AddCommand(newContractTestCommand())
	cmd.AddCommand(newContractPRCommand())
	decorateCommandHelp(cmd, "Runtime Contract Commands")
	return cmd
}

func newContractSynthesizeCommand() *cobra.Command {
	opts := contractSynthesizeOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "synthesize",
		Short: "Synthesize a RuntimeContract from proof evidence",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.From) == "" && strings.TrimSpace(opts.Guardian) == "" {
				return fmt.Errorf("at least one of --from or --guardian is required")
			}
			return validateContractFormat(opts.Format, true)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			evidence, err := loadContractEvidence(opts.From, opts.Guardian)
			if err != nil {
				return err
			}
			contract := synthesizeRuntimeContract(evidence)
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeRuntimeContract(opts.Out, contract); err != nil {
					return fmt.Errorf("write runtime contract: %w", err)
				}
			}
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "json":
				return renderContractJSON(cmd.OutOrStdout(), contract)
			case "yaml":
				return renderContractYAML(cmd.OutOrStdout(), contract)
			default:
				renderContractSynthesizeText(cmd.OutOrStdout(), contract, opts.Out)
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&opts.From, "from", "", "Incident bundle, root cause JSON, or incident replay proof directory")
	cmd.Flags().StringVar(&opts.Guardian, "guardian", "", "Guardian drift proof JSON or runtime proof directory")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write RuntimeContract YAML/JSON to this path")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text, json, or yaml")
	decorateCommandHelp(cmd, "Runtime Contract Synthesize Flags")
	return cmd
}

func newContractTestCommand() *cobra.Command {
	opts := contractTestOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test a RuntimeContract against proof evidence",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Contract) == "" {
				return fmt.Errorf("--contract is required")
			}
			if strings.TrimSpace(opts.From) == "" && strings.TrimSpace(opts.Guardian) == "" {
				return fmt.Errorf("at least one of --from or --guardian is required")
			}
			return validateContractFormat(opts.Format, false)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			contract, err := loadRuntimeContract(opts.Contract)
			if err != nil {
				return err
			}
			evidence, err := loadContractEvidence(opts.From, opts.Guardian)
			if err != nil {
				return err
			}
			proof := testRuntimeContract(contract, evidence, contractTestOptions{Contract: opts.Contract, From: opts.From, Guardian: opts.Guardian})
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFileEnsured(opts.Out, proof); err != nil {
					return fmt.Errorf("write contract proof: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				if err := renderContractJSON(cmd.OutOrStdout(), proof); err != nil {
					return err
				}
			} else {
				renderContractTestText(cmd.OutOrStdout(), proof, opts.Out)
			}
			if opts.FailOnBlocked && proof.Blocked {
				return fmt.Errorf("runtime contract failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Contract, "contract", "", "RuntimeContract YAML/JSON file")
	cmd.Flags().StringVar(&opts.From, "from", "", "Incident bundle, root cause JSON, or incident replay proof directory")
	cmd.Flags().StringVar(&opts.Guardian, "guardian", "", "Guardian drift proof JSON or runtime proof directory")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write contract test proof JSON to this path")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	cmd.Flags().BoolVar(&opts.FailOnBlocked, "fail-on-blocked", false, "Exit non-zero when the contract test fails")
	decorateCommandHelp(cmd, "Runtime Contract Test Flags")
	return cmd
}

func newContractPRCommand() *cobra.Command {
	opts := contractPROptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Generate PR-ready artifacts from a RuntimeContract proof",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Contract) == "" {
				return fmt.Errorf("--contract is required")
			}
			if strings.TrimSpace(opts.Proof) == "" {
				return fmt.Errorf("--proof is required")
			}
			return validateContractFormat(opts.Format, false)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			contract, err := loadRuntimeContract(opts.Contract)
			if err != nil {
				return err
			}
			proof, err := loadRuntimeContractProof(opts.Proof)
			if err != nil {
				return err
			}
			paths, err := writeContractPRArtifacts(contract, proof, opts)
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				return renderContractJSON(cmd.OutOrStdout(), paths)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Runtime Contract PR artifacts written:\n  %s\n  %s\n", paths["patch"], paths["pr"])
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Contract, "contract", "", "RuntimeContract YAML/JSON file")
	cmd.Flags().StringVar(&opts.Proof, "proof", "", "RuntimeContract proof JSON from torque contract test")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Suggested contract branch name")
	cmd.Flags().StringVar(&opts.OutDir, "out", "", "Directory for PR artifacts (default: ./fix beside --proof)")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	decorateCommandHelp(cmd, "Runtime Contract PR Flags")
	return cmd
}

func synthesizeRuntimeContract(evidence contractEvidence) runtimeContract {
	release := firstNonEmpty(evidence.RootCause.Release, evidence.Bundle.Release, evidence.Replay.Release, evidence.Guardian.Release, "runtime")
	namespace := firstNonEmpty(evidence.RootCause.Namespace, evidence.Bundle.Namespace, evidence.Replay.Namespace, evidence.Guardian.Namespace)
	contract := runtimeContract{
		APIVersion: "torque.dev/v1",
		Kind:       "RuntimeContract",
		Metadata: runtimeContractMetadata{
			Name:      sanitizeFilename(firstNonEmpty(release, "runtime")) + "-runtime-contract",
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Labels: map[string]string{
				"torque.dev/observe-only": "true",
			},
		},
		Spec: runtimeContractSpec{
			Release:     release,
			Namespace:   namespace,
			ObserveOnly: true,
		},
	}
	if contract.Metadata.Name == "-runtime-contract" {
		contract.Metadata.Name = "runtime-contract"
	}
	if evidence.HasIncident {
		contract.Spec.Sources = append(contract.Spec.Sources, runtimeContractSource{Kind: "incident", Path: evidence.IncidentPath, Status: evidence.RootCause.Status, Blocked: evidence.RootCause.Blocked, Generated: evidence.Bundle.GeneratedAt})
	}
	if evidence.HasGuardian {
		contract.Spec.Sources = append(contract.Spec.Sources, runtimeContractSource{Kind: "guardian", Path: evidence.GuardianPath, Status: evidence.Guardian.Status, Blocked: evidence.Guardian.Blocked, Generated: evidence.Guardian.GeneratedAt})
	}

	invariants := newInvariantSet()
	if evidence.HasReplay {
		invariants.add(runtimeInvariant{
			ID:          "incident-replay-must-pass",
			Type:        "incident.replay.passed",
			Severity:    "high",
			Description: "Incident replay proof must remain valid and complete.",
			Required:    true,
		})
	}
	if evidence.HasRootCause && evidence.RootCause.PrimaryCause != "" && evidence.RootCause.Blocked {
		invariants.add(runtimeInvariant{
			ID:          "no-" + sanitizeFilename(evidence.RootCause.PrimaryCause),
			Type:        "incident.rootCause.absent",
			Severity:    "critical",
			Description: "The incident primary cause must not recur.",
			Value:       evidence.RootCause.PrimaryCause,
			Required:    true,
		})
	}
	for _, item := range contractImagePullEvidence(evidence) {
		invariants.add(runtimeInvariant{
			ID:          "no-image-pull-failure",
			Type:        "incident.rootCause.absent",
			Severity:    "critical",
			Description: "Kubernetes must not report image pull failures for the release.",
			Value:       "image_pull_failure",
			Resource:    contractResourcePtr(item),
			Required:    true,
		})
	}
	if contractBoundaryFindings(evidence) > 0 {
		invariants.add(runtimeInvariant{
			ID:          "no-runtime-secret-boundary-findings",
			Type:        "runtime.secretBoundary.none",
			Severity:    "critical",
			Description: "Secret-like values must not reach ConfigMap, metadata, annotation, env.value, log-facing, or report surfaces.",
			Required:    true,
		})
	}
	if contractAftercareIssues(evidence) > 0 || contractUnhealthyResources(evidence) > 0 {
		invariants.add(runtimeInvariant{
			ID:          "rollout-aftercare-must-pass",
			Type:        "rollout.aftercare.passed",
			Severity:    "high",
			Description: "Runtime rollout aftercare must have no availability, warning-event, or unhealthy-resource regressions.",
			Required:    true,
		})
		for _, ref := range contractUnavailableResources(evidence) {
			invariants.add(runtimeInvariant{
				ID:          "resource-available-" + sanitizeFilename(ref.Kind+"-"+firstNonEmpty(ref.Namespace, "cluster")+"-"+ref.Name),
				Type:        "resource.available",
				Severity:    "high",
				Description: "The resource must be available in runtime evidence.",
				Resource:    contractResourcePtr(ref),
				Required:    true,
			})
		}
	}
	if evidence.HasGuardian && (!evidence.Guardian.PredictedVsLive.Passed || evidence.Guardian.Summary.Changed > 0 || evidence.Guardian.Summary.Missing > 0 || evidence.Guardian.Summary.FetchErrors > 0) {
		invariants.add(runtimeInvariant{
			ID:          "predicted-live-state-must-match",
			Type:        "guardian.drift.none",
			Severity:    "high",
			Description: "Live objects must match the Torque simulation proof after Kubernetes API defaults are normalized.",
			Required:    true,
		})
	}
	for _, reason := range contractWarningReasons(evidence) {
		invariants.add(runtimeInvariant{
			ID:          "no-warning-event-" + sanitizeFilename(reason),
			Type:        "events.warningReason.absent",
			Severity:    "high",
			Description: "The runtime event stream must not contain this Warning reason.",
			Value:       reason,
			Required:    true,
		})
	}
	if contractSuspiciousOwners(evidence) > 0 {
		invariants.add(runtimeInvariant{
			ID:          "no-suspicious-managed-field-owners",
			Type:        "managedFields.noSuspiciousOwners",
			Severity:    "medium",
			Description: "Managed fields must not show manual edits, suspicious patch managers, or unexpected mutation owners.",
			Required:    true,
		})
	}
	if contractLogFailureSignals(evidence) > 0 {
		invariants.add(runtimeInvariant{
			ID:          "no-log-failure-signals",
			Type:        "logs.failureSignals.absent",
			Severity:    "medium",
			Description: "Captured logs must not include failure-like runtime signals.",
			Required:    true,
		})
	}
	contract.Spec.Invariants = invariants.sorted()
	if len(contract.Spec.Invariants) == 0 {
		contract.Spec.Invariants = append(contract.Spec.Invariants, runtimeInvariant{
			ID:          "runtime-proof-must-be-clean",
			Type:        "runtime.proof.clean",
			Severity:    "high",
			Description: "Runtime proof must have no drift, warning events, secret-boundary findings, aftercare issues, or blocking incident root cause.",
			Required:    true,
		})
	}
	return contract
}

func testRuntimeContract(contract runtimeContract, evidence contractEvidence, opts contractTestOptions) runtimeContractProof {
	proof := runtimeContractProof{
		Version:     "v1",
		Tool:        contractTool,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Contract:    opts.Contract,
		From:        opts.From,
		Guardian:    opts.Guardian,
		Release:     firstNonEmpty(contract.Spec.Release, evidence.RootCause.Release, evidence.Bundle.Release, evidence.Guardian.Release),
		Namespace:   firstNonEmpty(contract.Spec.Namespace, evidence.RootCause.Namespace, evidence.Bundle.Namespace, evidence.Guardian.Namespace),
		Passed:      true,
	}
	for _, inv := range contract.Spec.Invariants {
		check := evaluateRuntimeInvariant(inv, evidence)
		proof.Results = append(proof.Results, check)
		proof.Summary.Invariants++
		if check.Passed {
			proof.Summary.Passed++
			continue
		}
		proof.Passed = false
		proof.Summary.Failed++
		if check.Missing {
			proof.Summary.Missing++
		}
		if strings.EqualFold(check.Severity, "critical") {
			proof.Summary.Critical++
		}
	}
	proof.Blocked = !proof.Passed
	return proof
}

func evaluateRuntimeInvariant(inv runtimeInvariant, evidence contractEvidence) runtimeContractCheck {
	check := runtimeContractCheck{
		ID:       inv.ID,
		Type:     inv.Type,
		Severity: firstNonEmpty(inv.Severity, "high"),
		Passed:   true,
		Resource: inv.Resource,
	}
	fail := func(message string, evidenceRows ...string) runtimeContractCheck {
		check.Passed = false
		check.Message = message
		check.Evidence = append(check.Evidence, evidenceRows...)
		return check
	}
	missing := func(message string) runtimeContractCheck {
		check.Passed = false
		check.Missing = true
		check.Message = message
		return check
	}
	pass := func(message string) runtimeContractCheck {
		check.Message = message
		return check
	}
	switch inv.Type {
	case "incident.replay.passed":
		if !evidence.HasReplay {
			return missing("incident replay proof is required")
		}
		if !evidence.Replay.Passed {
			return fail("incident replay proof did not pass", evidence.Replay.Source)
		}
		return pass("incident replay proof passed")
	case "incident.rootCause.absent":
		if !evidence.HasRootCause {
			return missing("incident root cause evidence is required")
		}
		if evidence.RootCause.Blocked && strings.EqualFold(evidence.RootCause.PrimaryCause, inv.Value) {
			return fail("blocked root cause recurred: "+inv.Value, evidence.RootCause.Summary)
		}
		return pass("blocked root cause is absent")
	case "runtime.secretBoundary.none":
		if !evidence.HasIncident && !evidence.HasGuardian {
			return missing("runtime boundary evidence is required")
		}
		rows := contractBoundaryEvidence(evidence)
		if len(rows) > 0 {
			return fail(fmt.Sprintf("%d runtime secret-boundary finding(s)", len(rows)), rows...)
		}
		return pass("no runtime secret-boundary findings")
	case "rollout.aftercare.passed":
		if !evidence.HasIncident && !evidence.HasGuardian {
			return missing("rollout aftercare evidence is required")
		}
		rows := contractAftercareEvidence(evidence)
		if evidence.HasIncident && evidence.Bundle.Summary.Unhealthy > 0 {
			rows = append(rows, fmt.Sprintf("incident unhealthy resources: %d", evidence.Bundle.Summary.Unhealthy))
		}
		if len(rows) > 0 {
			return fail(fmt.Sprintf("%d rollout aftercare issue(s)", len(rows)), rows...)
		}
		return pass("rollout aftercare passed")
	case "guardian.drift.none":
		if !evidence.HasGuardian {
			return missing("Guardian drift proof is required")
		}
		rows := contractDriftEvidence(evidence.Guardian)
		if len(rows) > 0 {
			return fail(fmt.Sprintf("%d drift finding(s)", len(rows)), rows...)
		}
		return pass("Guardian drift proof passed")
	case "events.warningReason.absent":
		if !evidence.HasIncident && !evidence.HasGuardian {
			return missing("runtime event evidence is required")
		}
		rows := contractWarningEvidence(evidence, inv.Value)
		if len(rows) > 0 {
			return fail(fmt.Sprintf("%d Warning event(s) with reason %s", len(rows), inv.Value), rows...)
		}
		return pass("Warning event reason is absent")
	case "managedFields.noSuspiciousOwners":
		if !evidence.HasIncident && !evidence.HasGuardian {
			return missing("managed-field owner evidence is required")
		}
		rows := contractManagedOwnerEvidence(evidence)
		if len(rows) > 0 {
			return fail(fmt.Sprintf("%d suspicious managed-field owner(s)", len(rows)), rows...)
		}
		return pass("no suspicious managed-field owners")
	case "resource.available":
		if !evidence.HasIncident && !evidence.HasGuardian {
			return missing("resource availability evidence is required")
		}
		if inv.Resource == nil {
			return missing("resource availability invariant has no resource target")
		}
		rows := contractResourceAvailabilityEvidence(evidence, inv.Resource)
		if len(rows) > 0 {
			return fail("resource is not available", rows...)
		}
		return pass("resource is available")
	case "logs.failureSignals.absent":
		if !evidence.HasIncident {
			return missing("incident log evidence is required")
		}
		rows := contractLogFailureEvidence(evidence)
		if len(rows) > 0 {
			return fail(fmt.Sprintf("%d log failure signal(s)", len(rows)), rows...)
		}
		return pass("no log failure signals")
	case "runtime.proof.clean":
		if !evidence.HasIncident && !evidence.HasGuardian {
			return missing("runtime proof evidence is required")
		}
		var rows []string
		rows = append(rows, contractBoundaryEvidence(evidence)...)
		rows = append(rows, contractAftercareEvidence(evidence)...)
		if evidence.HasGuardian {
			rows = append(rows, contractDriftEvidence(evidence.Guardian)...)
		}
		if evidence.HasRootCause && evidence.RootCause.Blocked {
			rows = append(rows, "blocked root cause: "+evidence.RootCause.PrimaryCause)
		}
		for _, reason := range contractWarningReasons(evidence) {
			rows = append(rows, contractWarningEvidence(evidence, reason)...)
		}
		if len(rows) > 0 {
			return fail(fmt.Sprintf("%d runtime proof issue(s)", len(rows)), rows...)
		}
		return pass("runtime proof is clean")
	default:
		return missing("unsupported invariant type: " + inv.Type)
	}
}

func loadContractEvidence(fromPath, guardianPath string) (contractEvidence, error) {
	var evidence contractEvidence
	fromPath = strings.TrimSpace(fromPath)
	guardianPath = strings.TrimSpace(guardianPath)
	if fromPath != "" {
		evidence.IncidentPath = fromPath
		if bundle, err := loadIncidentBundle(fromPath); err == nil {
			evidence.Bundle = bundle
			evidence.HasIncident = true
			evidence.RootCause = bundle.RootCause
			if evidence.RootCause.Version == "" {
				evidence.RootCause = buildIncidentRootCause(bundle)
			}
			evidence.HasRootCause = evidence.RootCause.Version != ""
		} else {
			root, rootErr := loadIncidentRootCause(fromPath)
			if rootErr != nil {
				return evidence, err
			}
			evidence.RootCause = root
			evidence.HasRootCause = root.Version != ""
			evidence.HasIncident = evidence.HasRootCause
		}
		if replay, err := loadIncidentReplayProof(fromPath); err == nil {
			evidence.Replay = replay
			evidence.HasReplay = true
			if !evidence.HasRootCause && replay.RootCause.Version != "" {
				evidence.RootCause = replay.RootCause
				evidence.HasRootCause = true
			}
		}
	}
	if guardianPath != "" {
		guardian, err := loadGuardianDiffProof(guardianPath)
		if err != nil {
			return evidence, err
		}
		evidence.GuardianPath = guardianPath
		evidence.Guardian = guardian
		evidence.HasGuardian = true
	}
	return evidence, nil
}

func loadIncidentReplayProof(path string) (incidentReplayProof, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return incidentReplayProof{}, fmt.Errorf("incident replay path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return incidentReplayProof{}, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "replay.result.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return incidentReplayProof{}, err
	}
	var proof incidentReplayProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return incidentReplayProof{}, err
	}
	if proof.Tool != incidentTool || proof.Version == "" {
		return incidentReplayProof{}, fmt.Errorf("%s does not look like an incident replay proof", path)
	}
	return proof, nil
}

func loadRuntimeContract(path string) (runtimeContract, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return runtimeContract{}, fmt.Errorf("runtime contract path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return runtimeContract{}, fmt.Errorf("read runtime contract: %w", err)
	}
	var contract runtimeContract
	if err := yaml.Unmarshal(raw, &contract); err != nil {
		return runtimeContract{}, fmt.Errorf("parse runtime contract: %w", err)
	}
	if contract.Kind != "RuntimeContract" || contract.APIVersion == "" {
		return runtimeContract{}, fmt.Errorf("%s does not look like a RuntimeContract", path)
	}
	return contract, nil
}

func loadRuntimeContractProof(path string) (runtimeContractProof, error) {
	path = strings.TrimSpace(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return runtimeContractProof{}, fmt.Errorf("read runtime contract proof: %w", err)
	}
	var proof runtimeContractProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return runtimeContractProof{}, fmt.Errorf("parse runtime contract proof: %w", err)
	}
	if proof.Tool != contractTool || proof.Version == "" {
		return runtimeContractProof{}, fmt.Errorf("%s does not look like a RuntimeContract proof", path)
	}
	return proof, nil
}

func writeRuntimeContract(path string, contract runtimeContract) error {
	path = strings.TrimSpace(path)
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return writeJSONFileEnsured(path, contract)
	}
	raw, err := yaml.Marshal(contract)
	if err != nil {
		return err
	}
	return writeBytesEnsured(path, raw)
}

func writeRuntimeContractProof(path string, proof runtimeContractProof) error {
	return writeJSONFileEnsured(path, proof)
}

func writeJSONFileEnsured(path string, v any) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return writeJSONFile(path, v)
}

func writeBytesEnsured(path string, raw []byte) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, raw, 0o644)
}

func writeContractPRArtifacts(contract runtimeContract, proof runtimeContractProof, opts contractPROptions) (map[string]string, error) {
	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		base := strings.TrimSpace(opts.Proof)
		if base == "" {
			base = "."
		}
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			outDir = filepath.Join(base, "fix")
		} else {
			outDir = filepath.Join(filepath.Dir(base), "fix")
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create fix dir: %w", err)
	}
	patchPath := filepath.Join(outDir, "runtime-contract.patch")
	prPath := filepath.Join(outDir, "pr.md")
	body := renderContractPRMarkdown(contract, proof, opts.Branch)
	if err := os.WriteFile(patchPath, []byte(renderContractPatch(contract, body)), 0o644); err != nil {
		return nil, fmt.Errorf("write runtime contract patch: %w", err)
	}
	if err := os.WriteFile(prPath, []byte(body+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write runtime contract PR body: %w", err)
	}
	return map[string]string{"patch": patchPath, "pr": prPath}, nil
}

func renderContractPatch(contract runtimeContract, body string) string {
	name := sanitizeFilename(firstNonEmpty(contract.Metadata.Name, contract.Spec.Release, "runtime-contract"))
	if name == "" {
		name = "runtime-contract"
	}
	filename := ".torque/contracts/" + name + ".md"
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", filename, filename)
	fmt.Fprintf(&b, "new file mode 100644\n")
	fmt.Fprintf(&b, "--- /dev/null\n")
	fmt.Fprintf(&b, "+++ b/%s\n", filename)
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		fmt.Fprintf(&b, "+%s\n", line)
	}
	return b.String()
}

func renderContractPRMarkdown(contract runtimeContract, proof runtimeContractProof, branch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Torque Runtime Contract\n\n")
	fmt.Fprintf(&b, "- Contract: `%s`\n", firstNonEmpty(contract.Metadata.Name, "-"))
	fmt.Fprintf(&b, "- Release: `%s`\n", firstNonEmpty(proof.Release, contract.Spec.Release, "-"))
	fmt.Fprintf(&b, "- Namespace: `%s`\n", firstNonEmpty(proof.Namespace, contract.Spec.Namespace, "-"))
	fmt.Fprintf(&b, "- Status: `%s`\n", mapBool(proof.Passed, "passed", "blocked"))
	fmt.Fprintf(&b, "- Invariants: `%d`\n", proof.Summary.Invariants)
	fmt.Fprintf(&b, "- Failed: `%d`\n", proof.Summary.Failed)
	fmt.Fprintf(&b, "- Missing evidence: `%d`\n", proof.Summary.Missing)
	if branch != "" {
		fmt.Fprintf(&b, "- Branch: `%s`\n", branch)
	}
	if len(proof.Results) > 0 {
		fmt.Fprintf(&b, "\n## Contract Results\n\n")
		for _, result := range proof.Results {
			status := mapBool(result.Passed, "PASS", "FAIL")
			if result.Missing {
				status = "MISSING"
			}
			fmt.Fprintf(&b, "- `%s` `%s` `%s`: %s\n", status, result.Severity, result.ID, result.Message)
		}
	}
	fmt.Fprintf(&b, "\n## Validation\n\n```bash\n")
	fmt.Fprintf(&b, "torque contract test --contract %s", shellQuoteToken(firstNonEmpty(proof.Contract, "torque-contract.yaml")))
	if proof.From != "" {
		fmt.Fprintf(&b, " --from %s", shellQuoteToken(proof.From))
	}
	if proof.Guardian != "" {
		fmt.Fprintf(&b, " --guardian %s", shellQuoteToken(proof.Guardian))
	}
	fmt.Fprintf(&b, " --out contract-proof.json --fail-on-blocked\n")
	fmt.Fprintf(&b, "```\n")
	return strings.TrimRight(b.String(), "\n")
}

func renderContractSynthesizeText(out interface{ Write([]byte) (int, error) }, contract runtimeContract, path string) {
	fmt.Fprintf(out, "RuntimeContract synthesized: %s\n", contract.Metadata.Name)
	fmt.Fprintf(out, "Release: %s\n", firstNonEmpty(contract.Spec.Release, "-"))
	fmt.Fprintf(out, "Namespace: %s\n", firstNonEmpty(contract.Spec.Namespace, "-"))
	fmt.Fprintf(out, "Invariants: %d\n", len(contract.Spec.Invariants))
	if strings.TrimSpace(path) != "" {
		fmt.Fprintf(out, "Contract written to %s\n", path)
	}
}

func renderContractTestText(out interface{ Write([]byte) (int, error) }, proof runtimeContractProof, path string) {
	fmt.Fprintf(out, "Runtime Contract: %s\n", strings.ToUpper(mapBool(proof.Passed, "passed", "blocked")))
	fmt.Fprintf(out, "Invariants: %d passed, %d failed, %d missing evidence\n", proof.Summary.Passed, proof.Summary.Failed, proof.Summary.Missing)
	if strings.TrimSpace(path) != "" {
		fmt.Fprintf(out, "Proof written to %s\n", path)
	}
}

func renderContractJSON(out interface{ Write([]byte) (int, error) }, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\n", raw)
	return nil
}

func renderContractYAML(out interface{ Write([]byte) (int, error) }, value any) error {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	_, err = out.Write(raw)
	return err
}

func validateContractFormat(format string, allowYAML bool) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text", "json":
		return nil
	case "yaml":
		if allowYAML {
			return nil
		}
	}
	if allowYAML {
		return fmt.Errorf("unsupported --format %q (expected text, json, or yaml)", format)
	}
	return fmt.Errorf("unsupported --format %q (expected text or json)", format)
}

func contractBoundaryFindings(evidence contractEvidence) int {
	return len(contractBoundaryEvidence(evidence))
}

func contractBoundaryEvidence(evidence contractEvidence) []string {
	var rows []string
	for _, f := range evidence.Bundle.RuntimeSecretBoundary.Findings {
		rows = append(rows, guardianRedactText(fmt.Sprintf("incident %s %s/%s %s: %s", f.Resource.Kind, firstNonEmpty(f.Resource.Namespace, "cluster"), f.Resource.Name, f.Surface, f.Message)))
	}
	for _, f := range evidence.Guardian.RuntimeSecretBoundary.Findings {
		rows = append(rows, guardianRedactText(fmt.Sprintf("guardian %s %s/%s %s: %s", f.Resource.Kind, firstNonEmpty(f.Resource.Namespace, "cluster"), f.Resource.Name, f.Surface, f.Message)))
	}
	return uniqueContractRows(rows)
}

func contractAftercareIssues(evidence contractEvidence) int {
	return len(contractAftercareEvidence(evidence))
}

func contractAftercareEvidence(evidence contractEvidence) []string {
	var rows []string
	for _, item := range evidence.Bundle.RolloutAftercare.Items {
		rows = append(rows, guardianRedactText(fmt.Sprintf("incident %s %s/%s %s: %s", item.Resource.Kind, firstNonEmpty(item.Resource.Namespace, "cluster"), item.Resource.Name, item.Reason, item.Message)))
	}
	for _, item := range evidence.Guardian.RolloutAftercare.Items {
		rows = append(rows, guardianRedactText(fmt.Sprintf("guardian %s %s/%s %s: %s", item.Resource.Kind, firstNonEmpty(item.Resource.Namespace, "cluster"), item.Resource.Name, item.Reason, item.Message)))
	}
	if evidence.HasGuardian && evidence.Guardian.Summary.WarningEvents > 0 {
		rows = append(rows, fmt.Sprintf("guardian warning events: %d", evidence.Guardian.Summary.WarningEvents))
	}
	if evidence.HasIncident && evidence.Bundle.Summary.WarningEvents > 0 {
		rows = append(rows, fmt.Sprintf("incident warning events: %d", evidence.Bundle.Summary.WarningEvents))
	}
	return uniqueContractRows(rows)
}

func contractDriftEvidence(proof guardianDiffProof) []string {
	var rows []string
	for _, item := range proof.PredictedVsLive.Changes {
		rows = append(rows, guardianRedactText(fmt.Sprintf("%s %s/%s: %s", item.Resource.Kind, firstNonEmpty(item.Resource.Namespace, "cluster"), item.Resource.Name, item.Reason)))
	}
	if proof.Summary.FetchErrors > 0 {
		rows = append(rows, fmt.Sprintf("fetch errors: %d", proof.Summary.FetchErrors))
	}
	return uniqueContractRows(rows)
}

func contractWarningReasons(evidence contractEvidence) []string {
	seen := map[string]struct{}{}
	for _, ev := range append(append([]guardianEventRow{}, evidence.Bundle.EventsTimeline.Events...), evidence.Guardian.EventsTimeline.Events...) {
		if !strings.EqualFold(ev.Type, "Warning") {
			continue
		}
		reason := firstNonEmpty(ev.Reason, "Warning")
		seen[reason] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for reason := range seen {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

func contractWarningEvidence(evidence contractEvidence, reason string) []string {
	var rows []string
	for _, ev := range append(append([]guardianEventRow{}, evidence.Bundle.EventsTimeline.Events...), evidence.Guardian.EventsTimeline.Events...) {
		if !strings.EqualFold(ev.Type, "Warning") {
			continue
		}
		if reason != "" && !strings.EqualFold(ev.Reason, reason) {
			continue
		}
		rows = append(rows, guardianRedactText(fmt.Sprintf("%s %s/%s: %s", firstNonEmpty(ev.Reason, "Warning"), firstNonEmpty(ev.Resource.Namespace, "cluster"), ev.Resource.Name, ev.Message)))
	}
	return uniqueContractRows(rows)
}

func contractSuspiciousOwners(evidence contractEvidence) int {
	return len(contractManagedOwnerEvidence(evidence))
}

func contractManagedOwnerEvidence(evidence contractEvidence) []string {
	var rows []string
	for _, owner := range append(append([]guardianManagedOwnerRow{}, evidence.Bundle.ManagedFields.Owners...), evidence.Guardian.ManagedFields.Owners...) {
		if !owner.Suspicious {
			continue
		}
		rows = append(rows, fmt.Sprintf("%s %s/%s manager=%s", owner.Resource.Kind, firstNonEmpty(owner.Resource.Namespace, "cluster"), owner.Resource.Name, owner.Manager))
	}
	return uniqueContractRows(rows)
}

func contractUnhealthyResources(evidence contractEvidence) int {
	n := 0
	for _, resource := range evidence.Bundle.Resources {
		if incidentUnhealthyStatus(resource.Status) {
			n++
		}
	}
	return n
}

func contractUnavailableResources(evidence contractEvidence) []guardianResourceRef {
	seen := map[string]guardianResourceRef{}
	for _, resource := range evidence.Bundle.Resources {
		if incidentUnhealthyStatus(resource.Status) {
			seen[incidentResourceKey(resource.Resource)] = resource.Resource
		}
	}
	for _, item := range evidence.Bundle.RolloutAftercare.Items {
		seen[incidentResourceKey(item.Resource)] = item.Resource
	}
	for _, item := range evidence.Guardian.RolloutAftercare.Items {
		seen[incidentResourceKey(item.Resource)] = item.Resource
	}
	out := make([]guardianResourceRef, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool { return incidentResourceKey(out[i]) < incidentResourceKey(out[j]) })
	return out
}

func contractResourceAvailabilityEvidence(evidence contractEvidence, ref *guardianResourceRef) []string {
	var rows []string
	if ref == nil {
		return nil
	}
	for _, resource := range evidence.Bundle.Resources {
		if !contractSameResource(resource.Resource, *ref) || !incidentUnhealthyStatus(resource.Status) {
			continue
		}
		rows = append(rows, guardianRedactText(fmt.Sprintf("incident status %s: %s", firstNonEmpty(resource.Status.Reason, resource.Status.Status), resource.Status.Message)))
	}
	for _, item := range append(append([]guardianAftercareFinding{}, evidence.Bundle.RolloutAftercare.Items...), evidence.Guardian.RolloutAftercare.Items...) {
		if contractSameResource(item.Resource, *ref) {
			rows = append(rows, guardianRedactText(fmt.Sprintf("aftercare %s: %s", item.Reason, item.Message)))
		}
	}
	return uniqueContractRows(rows)
}

func contractImagePullEvidence(evidence contractEvidence) []guardianResourceRef {
	seen := map[string]guardianResourceRef{}
	if evidence.HasRootCause && evidence.RootCause.PrimaryCause == "image_pull_failure" {
		for _, item := range evidence.RootCause.Evidence {
			if item.Resource.Name != "" {
				seen[incidentResourceKey(item.Resource)] = item.Resource
			}
		}
	}
	for _, row := range evidence.Bundle.CausalTimeline.Items {
		msg := strings.ToLower(row.Reason + " " + row.Message)
		if strings.Contains(msg, "imagepullbackoff") || strings.Contains(msg, "errimagepull") || strings.Contains(msg, "failed to pull") || strings.Contains(msg, "pull access denied") {
			seen[incidentResourceKey(row.Resource)] = row.Resource
		}
	}
	if len(seen) == 0 && evidence.HasRootCause && evidence.RootCause.PrimaryCause == "image_pull_failure" {
		seen[""] = guardianResourceRef{}
	}
	out := make([]guardianResourceRef, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool { return incidentResourceKey(out[i]) < incidentResourceKey(out[j]) })
	return out
}

func contractLogFailureSignals(evidence contractEvidence) int {
	return len(contractLogFailureEvidence(evidence))
}

func contractLogFailureEvidence(evidence contractEvidence) []string {
	var rows []string
	for _, row := range evidence.Bundle.CausalTimeline.Items {
		if row.Source == "logs" && row.Reason == "log_failure_signal" {
			rows = append(rows, guardianRedactText(fmt.Sprintf("%s/%s: %s", firstNonEmpty(row.Resource.Namespace, "cluster"), row.Resource.Name, row.Message)))
		}
	}
	return uniqueContractRows(rows)
}

func contractSameResource(a, b guardianResourceRef) bool {
	if !strings.EqualFold(a.Kind, b.Kind) || !strings.EqualFold(a.Name, b.Name) {
		return false
	}
	if b.Namespace != "" && !strings.EqualFold(a.Namespace, b.Namespace) {
		return false
	}
	return true
}

func contractResourcePtr(ref guardianResourceRef) *guardianResourceRef {
	if ref.Group == "" && ref.Version == "" && ref.Kind == "" && ref.Namespace == "" && ref.Name == "" {
		return nil
	}
	cp := ref
	return &cp
}

type invariantSet struct {
	byID map[string]runtimeInvariant
}

func newInvariantSet() *invariantSet {
	return &invariantSet{byID: map[string]runtimeInvariant{}}
}

func (set *invariantSet) add(inv runtimeInvariant) {
	if set == nil {
		return
	}
	inv.ID = sanitizeFilename(inv.ID)
	if inv.ID == "" {
		inv.ID = "runtime-invariant"
	}
	if inv.Severity == "" {
		inv.Severity = "high"
	}
	if inv.Description == "" {
		inv.Description = inv.Type
	}
	set.byID[inv.ID] = inv
}

func (set *invariantSet) sorted() []runtimeInvariant {
	if set == nil || len(set.byID) == 0 {
		return nil
	}
	ids := make([]string, 0, len(set.byID))
	for id := range set.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]runtimeInvariant, 0, len(ids))
	for _, id := range ids {
		out = append(out, set.byID[id])
	}
	return out
}

func uniqueContractRows(rows []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		if _, ok := seen[row]; ok {
			continue
		}
		seen[row] = struct{}{}
		out = append(out, row)
	}
	sort.Strings(out)
	return out
}

func mapBool(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
	"github.com/ingresslabs/torque/internal/kube"
	"github.com/ingresslabs/torque/internal/secretstore"
	"github.com/ingresslabs/torque/internal/telemetry"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"sigs.k8s.io/yaml"
)

type applySimulationOptions struct {
	Chart            string
	Release          string
	Version          string
	Namespace        string
	ValuesFiles      []string
	SetValues        []string
	SetStringValues  []string
	SetFileValues    []string
	SecretProvider   string
	SecretConfig     string
	IncludeCRDs      bool
	FromLive         bool
	SLOPath          string
	SecurityEvidence string
	OutDir           string
	Format           string
	FailOnBlocked    bool
}

type applySimulationResult struct {
	Manifest       applySimulationManifest
	Plan           *deployPlanResult
	Prediction     *applyPrediction
	ServerDryRun   *deploy.ServerDryRunReport
	Admission      simulationResultSet
	FieldConflicts simulationResultSet
	Quota          simulationQuotaRiskReport
	Proof          applyProofBundle
	OutDir         string
	Blocked        bool
}

type applySimulationManifest struct {
	Version                string                 `json:"version"`
	Tool                   string                 `json:"tool"`
	GeneratedAt            string                 `json:"generatedAt"`
	Release                string                 `json:"release"`
	Namespace              string                 `json:"namespace"`
	Chart                  string                 `json:"chart,omitempty"`
	ChartVersion           string                 `json:"chartVersion,omitempty"`
	Status                 string                 `json:"status"`
	Blocked                bool                   `json:"blocked"`
	RenderedManifestSHA256 string                 `json:"renderedManifestSha256,omitempty"`
	ClusterHost            string                 `json:"clusterHost,omitempty"`
	Summary                applySimulationSummary `json:"summary"`
	Files                  map[string]string      `json:"files"`
	Evidence               []string               `json:"evidence"`
}

type applySimulationSummary struct {
	Creates                 int    `json:"creates"`
	Updates                 int    `json:"updates"`
	Deletes                 int    `json:"deletes"`
	Unchanged               int    `json:"unchanged"`
	Risk                    string `json:"risk,omitempty"`
	ServerDryRunPassed      int    `json:"serverDryRunPassed"`
	ServerDryRunFailed      int    `json:"serverDryRunFailed"`
	ServerDryRunSkipped     int    `json:"serverDryRunSkipped"`
	AdmissionDenied         int    `json:"admissionDenied"`
	FieldOwnershipConflicts int    `json:"fieldOwnershipConflicts"`
	ImmutableFields         int    `json:"immutableFields"`
	QuotaFails              int    `json:"quotaFails"`
	QuotaWarnings           int    `json:"quotaWarnings"`
	RepairFixes             int    `json:"repairFixes"`
}

type predictedLiveStateReport struct {
	Version                string            `json:"version"`
	RenderedManifestSHA256 string            `json:"renderedManifestSha256,omitempty"`
	Resources              map[string]string `json:"resources,omitempty"`
	Redacted               bool              `json:"redacted"`
}

type simulationResultSet struct {
	Version string                      `json:"version"`
	Passed  bool                        `json:"passed"`
	Count   int                         `json:"count"`
	Results []deploy.ServerDryRunResult `json:"results,omitempty"`
}

type simulationQuotaRiskReport struct {
	Version    string                  `json:"version"`
	Passed     bool                    `json:"passed"`
	Namespaces map[string]*quotaReport `json:"namespaces,omitempty"`
	Summary    simulationQuotaSummary  `json:"summary"`
}

type simulationQuotaSummary struct {
	Namespaces int `json:"namespaces"`
	Warnings   int `json:"warnings"`
	Fails      int `json:"fails"`
}

func newApplySimulateCommand(namespace *string, kubeconfig *string, kubeContext *string, helpSection string) *cobra.Command {
	ownNamespaceFlag := false
	if namespace == nil {
		namespace = new(string)
		ownNamespaceFlag = true
	}
	opts := applySimulationOptions{FromLive: true, Format: "text"}
	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Simulate a Helm apply against live API behavior and write a proof bundle",
		Long:  "Render a Helm release, diff it against live state, run Kubernetes server-side dry-run checks, score rollout risk, and write a replayable simulation proof bundle without applying changes.",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			opts.Namespace = strings.TrimSpace(*namespace)
			if strings.TrimSpace(opts.Chart) == "" {
				return fmt.Errorf("--chart is required")
			}
			if strings.TrimSpace(opts.Release) == "" {
				return fmt.Errorf("--release is required")
			}
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
			result, err := runApplySimulation(cmd.Context(), cmd, kubeconfig, kubeContext, opts)
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(result.Manifest, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderApplySimulationText(cmd.OutOrStdout(), result)
			}
			if result.Blocked && opts.FailOnBlocked {
				return fmt.Errorf("simulation blocked release %s (dry-run failures=%d admission=%d field-conflicts=%d quota-fails=%d)", opts.Release, result.Manifest.Summary.ServerDryRunFailed, result.Manifest.Summary.AdmissionDenied, result.Manifest.Summary.FieldOwnershipConflicts, result.Manifest.Summary.QuotaFails)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Chart, "chart", "", "Chart reference (path, repo/name, or OCI ref)")
	cmd.Flags().StringVar(&opts.Release, "release", "", "Helm release name")
	cmd.Flags().StringVar(&opts.Version, "version", "", "Chart version (default: latest)")
	cmd.Flags().StringSliceVarP(&opts.ValuesFiles, "values", "f", nil, "Values files to apply (can be repeated)")
	cmd.Flags().StringArrayVar(&opts.SetValues, "set", nil, "Set values on the command line (key=val)")
	cmd.Flags().StringArrayVar(&opts.SetStringValues, "set-string", nil, "Set STRING values on the command line")
	cmd.Flags().StringArrayVar(&opts.SetFileValues, "set-file", nil, "Set values from files (key=path)")
	cmd.Flags().StringVar(&opts.SecretProvider, "secret-provider", "", "Secret provider name for secret:// references")
	cmd.Flags().StringVar(&opts.SecretConfig, "secret-config", "", "Secrets provider config file (defaults to ~/.torque/config.yaml and repo .torque.yaml)")
	cmd.Flags().BoolVar(&opts.IncludeCRDs, "include-crds", false, "Render CRDs in addition to the main chart objects")
	cmd.Flags().BoolVar(&opts.FromLive, "from-live", true, "Use live cluster state and server-side dry-run behavior")
	cmd.Flags().StringVar(&opts.SLOPath, "slo", "", "Rollout SLO YAML to attach to the proof bundle")
	cmd.Flags().StringVar(&opts.SecurityEvidence, "security-evidence", "", "Existing verifier/security evidence directory to attach")
	cmd.Flags().StringVar(&opts.OutDir, "out", "", "Proof bundle directory (default: ./torque-sim-proof-<release>-<timestamp>)")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&opts.FailOnBlocked, "fail-on-blocked", false, "Exit non-zero when the simulation proof is blocked")
	_ = cmd.MarkFlagRequired("chart")
	_ = cmd.MarkFlagRequired("release")
	if ownNamespaceFlag {
		cmd.Flags().StringVarP(namespace, "namespace", "n", "", "Namespace for the Helm release (defaults to active context)")
	}
	section := strings.TrimSpace(helpSection)
	if section == "" {
		section = "Apply Simulation Flags"
	}
	decorateCommandHelp(cmd, section)
	return cmd
}

func runApplySimulation(ctx context.Context, cmd *cobra.Command, kubeconfig *string, kubeContext *string, opts applySimulationOptions) (*applySimulationResult, error) {
	startedAt := time.Now().UTC()
	kubeClient, err := kube.New(ctx, derefString(kubeconfig), derefString(kubeContext))
	if err != nil {
		return nil, err
	}
	if opts.Namespace == "" {
		opts.Namespace = kubeClient.Namespace
	}
	settings := cli.New()
	if derefString(kubeconfig) != "" {
		settings.KubeConfig = derefString(kubeconfig)
	}
	if derefString(kubeContext) != "" {
		settings.KubeContext = derefString(kubeContext)
	}
	if opts.Namespace != "" {
		settings.SetNamespace(opts.Namespace)
	}
	attachKubeTelemetry(settings, kubeClient)
	actionCfg := new(action.Configuration)
	if err := actionCfg.Init(settings.RESTClientGetter(), opts.Namespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		fmt.Fprintf(cmd.ErrOrStderr(), "[helm] "+format+"\n", v...)
	}); err != nil {
		return nil, fmt.Errorf("init helm action config: %w", err)
	}
	var secretAudit secretstore.AuditReport
	secretResolver, secretAuditSink, err := buildDeploySecretResolver(ctx, deploySecretConfig{
		Chart:      opts.Chart,
		ConfigPath: opts.SecretConfig,
		Provider:   opts.SecretProvider,
		Mode:       secretstore.ResolveModeMask,
		ErrOut:     cmd.ErrOrStderr(),
	})
	if err != nil {
		return nil, err
	}
	secretOptions := &deploy.SecretOptions{Resolver: secretResolver, AuditSink: func(report secretstore.AuditReport) {
		secretAudit = report
		if secretAuditSink != nil {
			secretAuditSink(report)
		}
	}, Validate: true}
	timer := telemetry.NewPhaseTimer()
	plan, err := executeDeployPlan(ctx, actionCfg, settings, kubeClient, deployPlanOptions{
		Chart:           opts.Chart,
		Release:         opts.Release,
		Version:         opts.Version,
		Namespace:       opts.Namespace,
		ValuesFiles:     opts.ValuesFiles,
		SetValues:       opts.SetValues,
		SetStringValues: opts.SetStringValues,
		SetFileValues:   opts.SetFileValues,
		Secrets:         secretOptions,
		IncludeCRDs:     opts.IncludeCRDs,
	}, timer)
	if err != nil {
		return nil, err
	}
	plan.Secrets = planSecretsFromAudit(secretAudit)
	if timer != nil {
		summary := telemetry.Summary{Total: timer.Total(), Phases: timer.Snapshot()}
		if kubeClient.APIStats != nil {
			metrics := kubeClient.APIStats.Snapshot()
			summary.KubeRequests = metrics.Count
			summary.KubeAvg = metrics.Avg()
			summary.KubeMax = metrics.Max
		}
		plan.Telemetry = buildPlanTelemetry(summary)
	}
	history, lastSuccessful, err := deploy.ReleaseHistoryBreadcrumbs(actionCfg, opts.Release, historyBreadcrumbLimit)
	if err != nil {
		return nil, fmt.Errorf("load release history: %w", err)
	}
	prediction := buildApplyPrediction(plan, history, lastSuccessful)
	manifestText := renderedManifestFromPlan(plan)
	serverReport := &deploy.ServerDryRunReport{Version: "v1", FieldManager: "torque-simulate"}
	if opts.FromLive {
		serverReport, err = deploy.RunServerDryRunReport(ctx, kubeClient, manifestText, deploy.ServerPlanOptions{FieldManager: "torque-simulate", Force: false, DefaultNamespace: opts.Namespace})
		if err != nil {
			return nil, fmt.Errorf("server dry-run simulation: %w", err)
		}
	}
	admission := filterServerDryRunResults(serverReport, deploy.ServerDryRunClassAdmissionDenied)
	conflicts := filterServerDryRunResults(serverReport, deploy.ServerDryRunClassFieldOwnershipConflict)
	quota := buildSimulationQuotaRiskReport(plan)
	blocked := simulationBlocked(serverReport, quota)
	proofErr := error(nil)
	if blocked {
		proofErr = errors.New("simulation blocked before apply")
	}
	proof := buildApplyProofBundle(applyProofBundleInput{
		StartedAt:            startedAt,
		Command:              os.Args,
		Release:              opts.Release,
		Namespace:            opts.Namespace,
		Chart:                opts.Chart,
		ChartVersion:         plan.ChartVersion,
		DryRun:               true,
		Err:                  proofErr,
		Prediction:           prediction,
		Plan:                 plan,
		HistoryBefore:        history,
		LastSuccessfulBefore: lastSuccessful,
		PhaseDurations:       phaseDurationsFromPlan(plan),
	})
	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		outDir = defaultApplySimulationOutputDir(opts.Release, startedAt)
	}
	result := &applySimulationResult{
		Plan:           plan,
		Prediction:     prediction,
		ServerDryRun:   serverReport,
		Admission:      admission,
		FieldConflicts: conflicts,
		Quota:          quota,
		Proof:          proof,
		OutDir:         outDir,
		Blocked:        blocked,
	}
	result.Manifest = buildApplySimulationManifest(result, opts)
	if err := writeApplySimulationBundle(ctx, result, opts); err != nil {
		return nil, err
	}
	return result, nil
}

func buildApplySimulationManifest(result *applySimulationResult, opts applySimulationOptions) applySimulationManifest {
	files := map[string]string{
		"predictedLiveState":      "predicted-live-state.json",
		"serverDryRun":            "server-dry-run.json",
		"admissionResults":        "admission.results.json",
		"fieldOwnershipConflicts": "field-ownership.conflicts.json",
		"quotaCapacityRisk":       "quota.capacity.risk.json",
		"rolloutPrediction":       "rollout.prediction.json",
		"verifierReport":          "verifier.report.json",
		"applyProof":              "apply.proof.json",
		"fixPatch":                filepath.ToSlash(filepath.Join("fixes", "fix.patch")),
		"fixPr":                   filepath.ToSlash(filepath.Join("fixes", "pr.md")),
	}
	summary := applySimulationSummary{}
	if result != nil && result.Plan != nil {
		summary.Creates = result.Plan.Summary.Creates
		summary.Updates = result.Plan.Summary.Updates
		summary.Deletes = result.Plan.Summary.Deletes
		summary.Unchanged = result.Plan.Summary.Unchanged
	}
	if result != nil && result.Prediction != nil {
		summary.Risk = result.Prediction.Risk
	}
	if result != nil && result.ServerDryRun != nil {
		summary.ServerDryRunPassed = result.ServerDryRun.Summary.Passed
		summary.ServerDryRunFailed = result.ServerDryRun.Summary.Failed
		summary.ServerDryRunSkipped = result.ServerDryRun.Summary.Skipped
		summary.AdmissionDenied = result.ServerDryRun.Summary.AdmissionDenied
		summary.FieldOwnershipConflicts = result.ServerDryRun.Summary.FieldOwnershipConflicts
		summary.ImmutableFields = result.ServerDryRun.Summary.ImmutableFields
	}
	if result != nil {
		summary.QuotaFails = result.Quota.Summary.Fails
		summary.QuotaWarnings = result.Quota.Summary.Warnings
		summary.RepairFixes = countAutoRepairFixes(buildRepairReport(&result.Proof, filepath.Join(result.OutDir, "apply.proof.json"), opts.Chart, "").Fixes)
	}
	status := "passed"
	blocked := result != nil && result.Blocked
	if blocked {
		status = "blocked"
	}
	manifest := applySimulationManifest{
		Version:     "v1",
		Tool:        "torque-apply-simulate",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Release:     strings.TrimSpace(opts.Release),
		Namespace:   strings.TrimSpace(opts.Namespace),
		Chart:       strings.TrimSpace(opts.Chart),
		Status:      status,
		Blocked:     blocked,
		Summary:     summary,
		Files:       files,
		Evidence: []string{
			"Helm render and live diff",
			"Kubernetes server-side apply dry-run",
			"Admission and field-ownership classification",
			"Quota capacity risk report",
			"Rollout prediction and rollback confidence",
			"Repair artifacts",
		},
	}
	if result != nil && result.Plan != nil {
		manifest.ChartVersion = result.Plan.ChartVersion
		manifest.RenderedManifestSHA256 = result.Plan.RenderedSHA256
		manifest.ClusterHost = result.Plan.ClusterHost
	}
	return manifest
}

func writeApplySimulationBundle(_ context.Context, result *applySimulationResult, opts applySimulationOptions) error {
	if result == nil {
		return nil
	}
	dir := strings.TrimSpace(result.OutDir)
	if dir == "" {
		return fmt.Errorf("simulation output directory is empty")
	}
	if err := os.MkdirAll(filepath.Join(dir, "fixes"), 0o755); err != nil {
		return fmt.Errorf("create simulation proof dir: %w", err)
	}
	predicted := predictedLiveStateReport{
		Version:                "v1",
		RenderedManifestSHA256: result.Manifest.RenderedManifestSHA256,
		Resources:              sanitizePredictedLiveState(result.Plan),
		Redacted:               true,
	}
	if err := writeJSONFile(filepath.Join(dir, "manifest.json"), result.Manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "predicted-live-state.json"), predicted); err != nil {
		return fmt.Errorf("write predicted live state: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "server-dry-run.json"), result.ServerDryRun); err != nil {
		return fmt.Errorf("write server dry-run: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "admission.results.json"), result.Admission); err != nil {
		return fmt.Errorf("write admission results: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "field-ownership.conflicts.json"), result.FieldConflicts); err != nil {
		return fmt.Errorf("write field ownership conflicts: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "quota.capacity.risk.json"), result.Quota); err != nil {
		return fmt.Errorf("write quota risk: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "rollout.prediction.json"), result.Prediction); err != nil {
		return fmt.Errorf("write rollout prediction: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "apply.proof.json"), result.Proof); err != nil {
		return fmt.Errorf("write apply proof: %w", err)
	}
	if err := writeSimulationFixArtifacts(dir, result); err != nil {
		return err
	}
	if err := attachSimulationSLO(dir, opts.SLOPath); err != nil {
		return err
	}
	if err := attachSimulationSecurityEvidence(dir, opts.SecurityEvidence); err != nil {
		return err
	}
	return nil
}

func renderedManifestFromPlan(plan *deployPlanResult) string {
	if plan == nil || len(plan.ManifestBlobs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(plan.ManifestBlobs))
	for key := range plan.ManifestBlobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		body := strings.TrimSpace(plan.ManifestBlobs[key])
		if body == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

func sanitizePredictedLiveState(plan *deployPlanResult) map[string]string {
	if plan == nil || len(plan.ManifestBlobs) == 0 {
		return nil
	}
	out := make(map[string]string, len(plan.ManifestBlobs))
	for id, body := range plan.ManifestBlobs {
		out[id] = sanitizePredictedManifestYAML(body)
	}
	return out
}

func sanitizePredictedManifestYAML(body string) string {
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(body), &obj); err != nil || len(obj) == 0 {
		return body
	}
	redactSensitiveMap(obj)
	raw, err := yaml.Marshal(obj)
	if err != nil {
		return body
	}
	return string(raw)
}

func redactSensitiveMap(m map[string]any) {
	kind, _ := m["kind"].(string)
	if strings.EqualFold(strings.TrimSpace(kind), "Secret") {
		for _, key := range []string{"data", "stringData"} {
			if data, ok := m[key].(map[string]any); ok {
				for dataKey := range data {
					data[dataKey] = "<redacted>"
				}
			}
		}
	}
	for key, value := range m {
		lower := strings.ToLower(key)
		if valueIsSensitiveKey(lower) {
			switch value.(type) {
			case string, []byte:
				m[key] = "<redacted>"
				continue
			}
		}
		switch child := value.(type) {
		case map[string]any:
			redactSensitiveMap(child)
		case []any:
			for _, item := range child {
				if itemMap, ok := item.(map[string]any); ok {
					redactSensitiveMap(itemMap)
				}
			}
		}
	}
}

func valueIsSensitiveKey(key string) bool {
	switch {
	case strings.Contains(key, "password"):
		return true
	case strings.Contains(key, "token"):
		return true
	case strings.Contains(key, "apikey"), strings.Contains(key, "api_key"), strings.Contains(key, "api-key"):
		return true
	case strings.Contains(key, "clientsecret"), strings.Contains(key, "client_secret"), strings.Contains(key, "client-secret"):
		return true
	case strings.Contains(key, "accesskey"), strings.Contains(key, "access_key"), strings.Contains(key, "access-key"):
		return true
	default:
		return false
	}
}

func filterServerDryRunResults(report *deploy.ServerDryRunReport, class string) simulationResultSet {
	out := simulationResultSet{Version: "v1", Passed: true}
	if report == nil {
		return out
	}
	for _, result := range report.Results {
		if strings.EqualFold(result.ErrorClass, class) {
			out.Results = append(out.Results, result)
		}
	}
	out.Count = len(out.Results)
	out.Passed = out.Count == 0
	return out
}

func buildSimulationQuotaRiskReport(plan *deployPlanResult) simulationQuotaRiskReport {
	out := simulationQuotaRiskReport{Version: "v1", Passed: true}
	if plan == nil || len(plan.DesiredQuotaByNS) == 0 {
		return out
	}
	out.Namespaces = plan.DesiredQuotaByNS
	out.Summary.Namespaces = len(plan.DesiredQuotaByNS)
	for _, report := range plan.DesiredQuotaByNS {
		if report == nil {
			continue
		}
		out.Summary.Warnings += len(report.Warnings)
		for _, row := range report.Headroom {
			switch strings.ToLower(strings.TrimSpace(row.Status)) {
			case "fail":
				out.Summary.Fails++
			case "warn":
				out.Summary.Warnings++
			}
		}
	}
	out.Passed = out.Summary.Fails == 0
	return out
}

func simulationBlocked(serverReport *deploy.ServerDryRunReport, quota simulationQuotaRiskReport) bool {
	if serverReport != nil && serverReport.Summary.Failed > 0 {
		return true
	}
	return quota.Summary.Fails > 0
}

func writeSimulationFixArtifacts(dir string, result *applySimulationResult) error {
	if result == nil {
		return nil
	}
	source := filepath.Join(dir, "apply.proof.json")
	report := buildRepairReport(&result.Proof, source, result.Proof.Chart, "")
	if err := os.WriteFile(filepath.Join(dir, "fixes", "pr.md"), []byte(renderRepairMarkdown(report)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write repair PR body: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixes", "fix.patch"), []byte(renderRepairPatch(report, source)), 0o644); err != nil {
		return fmt.Errorf("write repair patch: %w", err)
	}
	return nil
}

func attachSimulationSLO(dir, sloPath string) error {
	slo, err := loadApplyRollbackSLO(sloPath)
	if err != nil {
		return err
	}
	if slo == nil {
		return nil
	}
	if err := writeJSONFile(filepath.Join(dir, "slo.gates.json"), slo); err != nil {
		return fmt.Errorf("write SLO gates: %w", err)
	}
	return nil
}

func attachSimulationSecurityEvidence(outDir, evidenceDir string) error {
	evidenceDir = strings.TrimSpace(evidenceDir)
	if evidenceDir == "" {
		return writeJSONFile(filepath.Join(outDir, "verifier.report.json"), map[string]any{
			"version":  "v1",
			"attached": false,
			"message":  "No verifier report attached. Run verifier with --security-evidence and pass --security-evidence to apply simulate.",
		})
	}
	names := []string{
		"verifier.report.json",
		"secrets.report.json",
		"secret.flow.graph.json",
		"boundary.matrix.json",
		"redaction.proof.json",
	}
	for _, name := range names {
		src := filepath.Join(evidenceDir, name)
		raw, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read security evidence %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, name), raw, 0o644); err != nil {
			return fmt.Errorf("attach security evidence %s: %w", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "verifier.report.json")); os.IsNotExist(err) {
		return writeJSONFile(filepath.Join(outDir, "verifier.report.json"), map[string]any{
			"version":  "v1",
			"attached": false,
			"source":   evidenceDir,
			"message":  "Security evidence directory did not contain verifier.report.json.",
		})
	}
	return nil
}

func phaseDurationsFromPlan(plan *deployPlanResult) map[string]string {
	if plan == nil || plan.Telemetry == nil || len(plan.Telemetry.PhasesMs) == 0 {
		return nil
	}
	out := map[string]string{}
	for name, ms := range plan.Telemetry.PhasesMs {
		if strings.TrimSpace(name) == "" {
			continue
		}
		out[name] = fmt.Sprintf("%dms", ms)
	}
	return out
}

func defaultApplySimulationOutputDir(release string, now time.Time) string {
	slug := sanitizeFilename(release)
	if slug == "" {
		slug = "release"
	}
	return fmt.Sprintf("torque-sim-proof-%s-%s", slug, now.UTC().Format("20060102-150405"))
}

func renderApplySimulationText(out interface{ Write([]byte) (int, error) }, result *applySimulationResult) {
	if out == nil || result == nil {
		return
	}
	fmt.Fprintf(out, "Simulation: %s\n", strings.ToUpper(result.Manifest.Status))
	fmt.Fprintf(out, "Proof bundle: %s\n", result.OutDir)
	fmt.Fprintf(out, "Plan: %d create, %d update, %d delete, %d unchanged\n", result.Manifest.Summary.Creates, result.Manifest.Summary.Updates, result.Manifest.Summary.Deletes, result.Manifest.Summary.Unchanged)
	fmt.Fprintf(out, "Server dry-run: %d passed, %d failed, %d skipped\n", result.Manifest.Summary.ServerDryRunPassed, result.Manifest.Summary.ServerDryRunFailed, result.Manifest.Summary.ServerDryRunSkipped)
	if result.Manifest.Summary.AdmissionDenied > 0 || result.Manifest.Summary.FieldOwnershipConflicts > 0 || result.Manifest.Summary.ImmutableFields > 0 {
		fmt.Fprintf(out, "API blockers: %d admission, %d field conflict, %d immutable\n", result.Manifest.Summary.AdmissionDenied, result.Manifest.Summary.FieldOwnershipConflicts, result.Manifest.Summary.ImmutableFields)
	}
	if result.Manifest.Summary.QuotaFails > 0 || result.Manifest.Summary.QuotaWarnings > 0 {
		fmt.Fprintf(out, "Quota: %d fail, %d warning\n", result.Manifest.Summary.QuotaFails, result.Manifest.Summary.QuotaWarnings)
	}
	if result.Prediction != nil {
		fmt.Fprintf(out, "Rollout risk: %s\n", result.Prediction.Risk)
	}
}

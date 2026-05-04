package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ingresslabs/ktl/internal/appconfig"
	"github.com/ingresslabs/ktl/internal/deploy"
	"github.com/ingresslabs/ktl/internal/kube"
	"github.com/ingresslabs/ktl/internal/secretstore"
	"github.com/ingresslabs/ktl/internal/verify"
	"github.com/ingresslabs/ktl/internal/workflows/buildsvc"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

type shipOptions struct {
	chart           string
	release         string
	namespace       string
	version         string
	valuesFiles     []string
	setValues       []string
	setStringValues []string
	setFileValues   []string
	secretProvider  string
	secretConfig    string

	buildContext    string
	dockerfile      string
	tags            []string
	platforms       []string
	buildArgs       []string
	buildSecrets    []string
	cacheFrom       []string
	cacheTo         []string
	push            bool
	load            bool
	noCache         bool
	attest          bool
	sbom            bool
	provenance      bool
	attestDir       string
	hermetic        bool
	allowNetwork    bool
	allowUnpinned   bool
	buildPolicy     string
	buildPolicyMode string
	builder         string
	authFile        string
	sandbox         bool
	sandboxConfig   string
	buildOutput     string

	verifyMode   string
	verifyFailOn string

	evidenceDir   string
	buildCapture  string
	applyCapture  string
	verifyReport  string
	planOutput    string
	explainOutput string
	explainFormat string
	captureTags   []string
	noCapture     bool

	createNamespace bool
	wait            bool
	atomic          bool
	yes             bool
	nonInteractive  bool
	timeout         time.Duration
	watch           time.Duration

	planOnly    bool
	skipBuild   bool
	skipVerify  bool
	skipExplain bool
}

type shipPaths struct {
	EvidenceDir      string `json:"evidenceDir"`
	BuildCapture     string `json:"buildCapture,omitempty"`
	ApplyCapture     string `json:"applyCapture,omitempty"`
	VerifyReport     string `json:"verifyReport,omitempty"`
	PlanOutput       string `json:"planOutput,omitempty"`
	ExplainOutput    string `json:"explainOutput,omitempty"`
	SummaryOutput    string `json:"summaryOutput,omitempty"`
	RenderedManifest string `json:"renderedManifest,omitempty"`
	AttestDir        string `json:"attestDir,omitempty"`
}

type shipRunner interface {
	RunBuild(cmd *cobra.Command, opts shipOptions, paths shipPaths) error
	RunVerify(cmd *cobra.Command, opts shipOptions, paths shipPaths) error
	RunPlan(cmd *cobra.Command, opts shipOptions, paths shipPaths) error
	RunApply(cmd *cobra.Command, opts shipOptions, paths shipPaths) error
	RunExplain(cmd *cobra.Command, opts shipOptions, paths shipPaths) error
}

type defaultShipRunner struct {
	buildService   buildsvc.Service
	globalProfile  *string
	globalLogLevel *string
	kubeconfig     *string
	kubeContext    *string
	remoteAgent    *string
}

func newShipCommand(service buildsvc.Service, globalProfile *string, globalLogLevel *string, kubeconfig *string, kubeContext *string, remoteAgent *string) *cobra.Command {
	return newShipCommandWithRunner(service, globalProfile, globalLogLevel, kubeconfig, kubeContext, remoteAgent, nil)
}

func newShipCommandWithRunner(service buildsvc.Service, globalProfile *string, globalLogLevel *string, kubeconfig *string, kubeContext *string, remoteAgent *string, runner shipRunner) *cobra.Command {
	if service == nil {
		service = defaultBuildService
	}
	if globalProfile == nil {
		fallback := "dev"
		globalProfile = &fallback
	}
	if globalLogLevel == nil {
		fallback := "info"
		globalLogLevel = &fallback
	}
	if runner == nil {
		runner = defaultShipRunner{
			buildService:   service,
			globalProfile:  globalProfile,
			globalLogLevel: globalLogLevel,
			kubeconfig:     kubeconfig,
			kubeContext:    kubeContext,
			remoteAgent:    remoteAgent,
		}
	}

	opts := shipOptions{
		dockerfile:      "Dockerfile",
		push:            true,
		attest:          true,
		verifyMode:      "block",
		verifyFailOn:    "high",
		explainFormat:   "markdown",
		wait:            true,
		atomic:          true,
		timeout:         5 * time.Minute,
		buildPolicyMode: "enforce",
		buildOutput:     "auto",
	}

	cmd := &cobra.Command{
		Use:   "ship",
		Short: "Build, verify, plan, apply, capture, and explain one release",
		Long:  "Run the focused build-to-deploy workflow and write a portable evidence directory for the release.",
		Example: `  # Build an image, verify the chart, plan, apply, capture, and explain
  ktl ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --yes

  # Stop after build, verify, and PR-ready plan generation
  ktl ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --plan-only`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateShipOptions(opts)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShipCommand(cmd, runner, opts)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&opts.chart, "chart", "", "Chart reference (path, repo/name, or OCI ref)")
	cmd.Flags().StringVar(&opts.release, "release", "", "Helm release name")
	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "", "Namespace for the Helm release")
	cmd.Flags().StringVar(&opts.version, "version", "", "Chart version (default: latest)")
	cmd.Flags().StringSliceVarP(&opts.valuesFiles, "values", "f", nil, "Values files to apply (can be repeated)")
	cmd.Flags().StringArrayVar(&opts.setValues, "set", nil, "Set values on the command line (key=val)")
	cmd.Flags().StringArrayVar(&opts.setStringValues, "set-string", nil, "Set STRING values on the command line")
	cmd.Flags().StringArrayVar(&opts.setFileValues, "set-file", nil, "Set values from files (key=path)")
	cmd.Flags().StringVar(&opts.secretProvider, "secret-provider", "", "Secret provider name for secret:// references")
	cmd.Flags().StringVar(&opts.secretConfig, "secret-config", "", "Secrets provider config file")

	cmd.Flags().StringVar(&opts.buildContext, "build", "", "Build context directory to build before planning/applying")
	cmd.Flags().StringVar(&opts.dockerfile, "dockerfile", opts.dockerfile, "Path to the Dockerfile within the build context")
	cmd.Flags().StringSliceVarP(&opts.tags, "tag", "t", nil, "Image tag(s) to apply to the build result")
	cmd.Flags().StringSliceVar(&opts.platforms, "platform", nil, "Target platforms (comma-separated values like linux/amd64)")
	cmd.Flags().StringArrayVar(&opts.buildArgs, "build-arg", nil, "Add a build-time variable (KEY=VALUE)")
	cmd.Flags().StringArrayVar(&opts.buildSecrets, "build-secret", nil, "Expose an environment variable as a BuildKit secret (NAME)")
	cmd.Flags().StringArrayVar(&opts.cacheFrom, "cache-from", nil, "BuildKit cache import source")
	cmd.Flags().StringArrayVar(&opts.cacheTo, "cache-to", nil, "BuildKit cache export destination")
	cmd.Flags().BoolVar(&opts.push, "push", opts.push, "Push built image tags to their registries")
	cmd.Flags().BoolVar(&opts.load, "load", false, "Load the resulting image into the local container runtime")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Disable BuildKit cache usage")
	cmd.Flags().BoolVar(&opts.attest, "attest", opts.attest, "Write SBOM/provenance attestations into the evidence directory")
	cmd.Flags().BoolVar(&opts.sbom, "sbom", false, "Generate an SBOM attestation during the build")
	cmd.Flags().BoolVar(&opts.provenance, "provenance", false, "Generate a SLSA provenance attestation during the build")
	cmd.Flags().StringVar(&opts.attestDir, "attest-dir", "", "Write generated attestations to this directory (defaults inside --evidence-dir)")
	cmd.Flags().BoolVar(&opts.hermetic, "hermetic", false, "Enable hermetic/locked build mode")
	cmd.Flags().BoolVar(&opts.hermetic, "locked", false, "Alias for --hermetic")
	cmd.Flags().BoolVar(&opts.allowNetwork, "allow-network", false, "Allow network egress when --hermetic is enabled")
	cmd.Flags().BoolVar(&opts.allowUnpinned, "allow-unpinned-bases", false, "Allow unpinned base images when --hermetic is enabled")
	cmd.Flags().StringVar(&opts.buildPolicy, "build-policy", "", "Build policy bundle path or https URL to evaluate before/after build")
	cmd.Flags().Var(newEnumStringValue(&opts.buildPolicyMode, "enforce", "enforce", "warn"), "build-policy-mode", "Build policy enforcement mode: enforce or warn")
	cmd.Flags().StringVar(&opts.builder, "builder", "", "BuildKit address")
	cmd.Flags().StringVar(&opts.authFile, "authfile", "", "Path to Docker auth config.json")
	cmd.Flags().BoolVar(&opts.sandbox, "sandbox", false, "Require executing the build inside the sandbox")
	cmd.Flags().StringVar(&opts.sandboxConfig, "sandbox-config", "", "Path to a sandbox runtime config file")
	cmd.Flags().Var(newEnumStringValue(&opts.buildOutput, "auto", "auto", "tty", "logs"), "build-output", "Build output mode: auto, tty, or logs")

	cmd.Flags().Var(newEnumStringValue(&opts.verifyMode, "block", "warn", "block", "off"), "verify-mode", "Verifier mode: warn, block, or off")
	cmd.Flags().Var(newEnumStringValue(&opts.verifyFailOn, "high", "info", "low", "medium", "high", "critical"), "verify-fail-on", "Fail threshold for verifier findings")

	cmd.Flags().StringVar(&opts.evidenceDir, "evidence-dir", "", "Directory for build/apply captures, reports, and ship summary")
	cmd.Flags().StringVar(&opts.buildCapture, "build-capture", "", "Build capture path (defaults inside --evidence-dir)")
	cmd.Flags().StringVar(&opts.applyCapture, "apply-capture", "", "Apply capture path (defaults inside --evidence-dir)")
	cmd.Flags().StringVar(&opts.verifyReport, "verify-report", "", "Verifier JSON report path (defaults inside --evidence-dir)")
	cmd.Flags().StringVar(&opts.planOutput, "plan-output", "", "Plan JSON path (defaults inside --evidence-dir)")
	cmd.Flags().StringVar(&opts.explainOutput, "explain-output", "", "Explain report path (defaults inside --evidence-dir)")
	cmd.Flags().Var(newEnumStringValue(&opts.explainFormat, "markdown", "text", "markdown", "json"), "explain-format", "Explain output format: text, markdown, or json")
	cmd.Flags().StringArrayVar(&opts.captureTags, "capture-tag", nil, "Tag build/apply capture sessions (KEY=VALUE). Repeatable.")
	cmd.Flags().BoolVar(&opts.noCapture, "no-capture", false, "Do not write build/apply capture SQLite files")

	cmd.Flags().BoolVar(&opts.createNamespace, "create-namespace", false, "Create the release namespace if it does not exist")
	cmd.Flags().BoolVar(&opts.wait, "wait", opts.wait, "Wait for resources to be ready")
	cmd.Flags().BoolVar(&opts.atomic, "atomic", opts.atomic, "Rollback changes if the upgrade fails")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip interactive confirmation prompts")
	cmd.Flags().BoolVar(&opts.nonInteractive, "non-interactive", false, "Fail instead of prompting (requires --yes)")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "Time to wait for Kubernetes operations")
	cmd.Flags().DurationVar(&opts.watch, "watch", 0, "After a successful deploy, stream logs/events for this long")

	cmd.Flags().BoolVar(&opts.planOnly, "plan-only", false, "Run build, verify, and plan, then stop before apply")
	cmd.Flags().BoolVar(&opts.skipBuild, "skip-build", false, "Skip the build step and plan/apply the chart as-is")
	cmd.Flags().BoolVar(&opts.skipVerify, "skip-verify", false, "Skip the verifier step and do not enforce --require-verified on apply")
	cmd.Flags().BoolVar(&opts.skipExplain, "skip-explain", false, "Skip the post-apply explain report")

	_ = cmd.MarkFlagRequired("chart")
	_ = cmd.MarkFlagRequired("release")
	decorateCommandHelp(cmd, "Ship Flags")
	return cmd
}

func validateShipOptions(opts shipOptions) error {
	if strings.TrimSpace(opts.chart) == "" {
		return fmt.Errorf("--chart is required")
	}
	if strings.TrimSpace(opts.release) == "" {
		return fmt.Errorf("--release is required")
	}
	if !opts.skipBuild {
		if strings.TrimSpace(opts.buildContext) == "" {
			return fmt.Errorf("--build is required unless --skip-build is set")
		}
		if len(opts.tags) == 0 {
			return fmt.Errorf("--tag is required when --build is set")
		}
	}
	if opts.timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0")
	}
	if opts.watch > 0 && opts.planOnly {
		return fmt.Errorf("--watch cannot be combined with --plan-only")
	}
	if opts.noCapture && !opts.skipExplain && !opts.planOnly {
		return fmt.Errorf("--no-capture requires --skip-explain or --plan-only")
	}
	if _, err := parseCaptureTags(opts.captureTags); err != nil {
		return err
	}
	return nil
}

func runShipCommand(cmd *cobra.Command, runner shipRunner, opts shipOptions) (runErr error) {
	paths, err := resolveShipPaths(opts, time.Now())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.EvidenceDir, 0o755); err != nil {
		return fmt.Errorf("create evidence dir: %w", err)
	}

	executed := []string{}
	defer func() {
		status := "success"
		errText := ""
		if runErr != nil {
			status = "failed"
			errText = runErr.Error()
		}
		if werr := writeShipSummary(paths, opts, executed, status, errText); werr != nil && runErr == nil {
			runErr = werr
		}
	}()

	errOut := cmd.ErrOrStderr()
	fmt.Fprintf(errOut, "Shipping release %s in namespace %s\n", opts.release, firstNonEmpty(opts.namespace, "default"))
	fmt.Fprintf(errOut, "Evidence directory: %s\n", paths.EvidenceDir)

	if !opts.skipBuild {
		executed = append(executed, "build")
		fmt.Fprintln(errOut, "ship: build")
		if err := runner.RunBuild(cmd, opts, paths); err != nil {
			return err
		}
	}

	if !opts.skipVerify && !strings.EqualFold(strings.TrimSpace(opts.verifyMode), "off") {
		executed = append(executed, "verify")
		fmt.Fprintln(errOut, "ship: verify")
		if err := runner.RunVerify(cmd, opts, paths); err != nil {
			return err
		}
	}

	executed = append(executed, "plan")
	fmt.Fprintln(errOut, "ship: plan")
	if err := runner.RunPlan(cmd, opts, paths); err != nil {
		return err
	}

	if opts.planOnly {
		fmt.Fprintf(errOut, "ship: plan-only complete. Plan: %s\n", paths.PlanOutput)
		return nil
	}

	executed = append(executed, "apply")
	fmt.Fprintln(errOut, "ship: apply")
	applyErr := runner.RunApply(cmd, opts, paths)

	if !opts.skipExplain && !opts.noCapture {
		executed = append(executed, "explain")
		fmt.Fprintln(errOut, "ship: explain")
		if err := runner.RunExplain(cmd, opts, paths); err != nil {
			if applyErr != nil {
				fmt.Fprintf(errOut, "ship: explain failed after apply failure: %v\n", err)
			} else {
				return err
			}
		}
	}
	if applyErr != nil {
		return applyErr
	}

	fmt.Fprintf(errOut, "ship: complete. Summary: %s\n", paths.SummaryOutput)
	return nil
}

func resolveShipPaths(opts shipOptions, now time.Time) (shipPaths, error) {
	slug := sanitizeFilename(opts.release)
	if slug == "" {
		slug = "release"
	}
	dir := strings.TrimSpace(opts.evidenceDir)
	if dir == "" {
		dir = filepath.Join("dist", fmt.Sprintf("ktl-ship-%s-%s", slug, now.UTC().Format("20060102-150405")))
	}
	dir = filepath.Clean(os.ExpandEnv(dir))
	paths := shipPaths{
		EvidenceDir:      dir,
		VerifyReport:     defaultPath(opts.verifyReport, dir, "verify.json"),
		PlanOutput:       defaultPath(opts.planOutput, dir, "plan.json"),
		ExplainOutput:    defaultPath(opts.explainOutput, dir, "explain.md"),
		SummaryOutput:    filepath.Join(dir, "ship.json"),
		RenderedManifest: filepath.Join(dir, "rendered.yaml"),
		AttestDir:        defaultPath(opts.attestDir, dir, "attest"),
	}
	if !opts.noCapture {
		paths.BuildCapture = defaultPath(opts.buildCapture, dir, "build.sqlite")
		paths.ApplyCapture = defaultPath(opts.applyCapture, dir, "apply.sqlite")
	}
	return paths, nil
}

func defaultPath(value, dir, name string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(os.ExpandEnv(value))
	}
	return filepath.Join(dir, name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r defaultShipRunner) RunBuild(cmd *cobra.Command, opts shipOptions, paths shipPaths) error {
	buildCmd := newBuildCommandWithService(r.buildService, r.globalProfile, r.globalLogLevel, r.kubeconfig, r.kubeContext)
	return executeShipChild(cmd, buildCmd, shipBuildArgs(opts, paths), r.globalProfile)
}

func (r defaultShipRunner) RunPlan(cmd *cobra.Command, opts shipOptions, paths shipPaths) error {
	applyCmd := newApplyCommand(r.kubeconfig, r.kubeContext, r.globalLogLevel, r.remoteAgent)
	return executeShipChild(cmd, applyCmd, append([]string{"plan"}, shipPlanArgs(opts, paths)...), nil)
}

func (r defaultShipRunner) RunApply(cmd *cobra.Command, opts shipOptions, paths shipPaths) error {
	applyCmd := newDeployApplyCommand(nil, r.kubeconfig, r.kubeContext, r.globalLogLevel, r.remoteAgent, "Ship Apply Flags")
	return executeShipChild(cmd, applyCmd, shipApplyArgs(opts, paths), nil)
}

func (r defaultShipRunner) RunVerify(cmd *cobra.Command, opts shipOptions, paths shipPaths) error {
	ctx := cmd.Context()
	kubeconfig := derefString(r.kubeconfig)
	kubeContext := derefString(r.kubeContext)
	kubeClient, err := kube.New(ctx, kubeconfig, kubeContext)
	if err != nil {
		return err
	}
	namespace := strings.TrimSpace(opts.namespace)
	if namespace == "" {
		namespace = kubeClient.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}
	settings := cli.New()
	if kubeconfig != "" {
		settings.KubeConfig = kubeconfig
	}
	if kubeContext != "" {
		settings.KubeContext = kubeContext
	}
	settings.SetNamespace(namespace)
	attachKubeTelemetry(settings, kubeClient)

	actionCfg := new(action.Configuration)
	if err := actionCfg.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
		return fmt.Errorf("init helm action config: %w", err)
	}

	secretResolver, secretAuditSink, err := buildDeploySecretResolver(ctx, deploySecretConfig{
		Chart:      opts.chart,
		ConfigPath: opts.secretConfig,
		Provider:   opts.secretProvider,
		Mode:       secretstore.ResolveModeValue,
		ErrOut:     cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	secretOptions := &deploy.SecretOptions{Resolver: secretResolver, AuditSink: secretAuditSink, Validate: true}
	rendered, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
		Chart:           opts.chart,
		Version:         opts.version,
		ReleaseName:     opts.release,
		Namespace:       namespace,
		ValuesFiles:     opts.valuesFiles,
		SetValues:       opts.setValues,
		SetStringValues: opts.setStringValues,
		SetFileValues:   opts.setFileValues,
		Secrets:         secretOptions,
		IncludeCRDs:     true,
		UseCluster:      true,
	})
	if err != nil {
		return err
	}
	if err := writeTextFile(paths.RenderedManifest, rendered.Manifest); err != nil {
		return err
	}
	objects, err := verify.DecodeK8SYAMLWithHelmSources(rendered.Manifest)
	if err != nil {
		return err
	}
	rulesDir := defaultShipRulesDir()
	report, err := verify.VerifyObjectsWithEmitter(ctx, fmt.Sprintf("chart %s (release=%s ns=%s)", opts.chart, opts.release, namespace), objects, verify.Options{
		Mode:     verify.Mode(strings.ToLower(strings.TrimSpace(opts.verifyMode))),
		FailOn:   verify.Severity(strings.ToLower(strings.TrimSpace(opts.verifyFailOn))),
		Format:   verify.OutputJSON,
		RulesDir: rulesDir,
	}, nil)
	if err != nil {
		return err
	}
	report.Inputs = append(report.Inputs, verify.Input{
		Kind:           "chart",
		Chart:          strings.TrimSpace(opts.chart),
		Release:        strings.TrimSpace(opts.release),
		Namespace:      namespace,
		RenderedSHA256: verify.ManifestDigestSHA256(rendered.Manifest),
	})
	report.Findings = verify.AnnotateFindingsWithRenderedSource(paths.RenderedManifest, rendered.Manifest, report.Findings)
	if err := writeVerifyReport(paths.VerifyReport, report); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Verify report: %s\n", paths.VerifyReport)
	if report.Blocked {
		return fmt.Errorf("verify blocked (fail-on=%s)", opts.verifyFailOn)
	}
	return nil
}

func defaultShipRulesDir() string {
	candidates := []string{
		filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 5 && dir != "" && dir != string(filepath.Separator); i++ {
			candidates = append(candidates, filepath.Join(dir, "internal", "verify", "rules", "builtin"))
			candidates = append(candidates, filepath.Join(dir, "..", "internal", "verify", "rules", "builtin"))
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			dir = next
		}
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

func (r defaultShipRunner) RunExplain(cmd *cobra.Command, opts shipOptions, paths shipPaths) error {
	if strings.TrimSpace(paths.ApplyCapture) == "" {
		return nil
	}
	out, closer, err := openShipOutput(paths.ExplainOutput)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	report := map[string]any{
		"tool":         "ktl ship",
		"generatedAt":  time.Now().UTC(),
		"release":      strings.TrimSpace(opts.release),
		"namespace":    strings.TrimSpace(opts.namespace),
		"chart":        strings.TrimSpace(opts.chart),
		"applyCapture": strings.TrimSpace(paths.ApplyCapture),
		"summary":      "Apply capture written; inspect the SQLite evidence file for timeline, events, logs, and manifest artifacts.",
	}
	switch strings.ToLower(strings.TrimSpace(opts.explainFormat)) {
	case "text", "":
		_, err = fmt.Fprintf(out, "ktl ship evidence\n\nRelease: %s\nNamespace: %s\nChart: %s\nApply capture: %s\n\nInspect the SQLite evidence file for timeline, events, logs, and manifest artifacts.\n",
			report["release"], firstNonEmpty(report["namespace"].(string), "default"), report["chart"], report["applyCapture"])
	case "markdown", "md":
		_, err = fmt.Fprintf(out, "# ktl ship evidence\n\n- Release: `%s`\n- Namespace: `%s`\n- Chart: `%s`\n- Apply capture: `%s`\n\nInspect the SQLite evidence file for timeline, events, logs, and manifest artifacts.\n",
			report["release"], firstNonEmpty(report["namespace"].(string), "default"), report["chart"], report["applyCapture"])
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		err = enc.Encode(report)
	default:
		err = fmt.Errorf("unsupported --explain-format %q", opts.explainFormat)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(paths.ExplainOutput) != "-" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Explain report: %s\n", paths.ExplainOutput)
	}
	return nil
}

func executeShipChild(parent *cobra.Command, child *cobra.Command, args []string, profile *string) error {
	root := &cobra.Command{
		Use:           "ktl",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	if profile != nil && strings.TrimSpace(*profile) != "" && strings.TrimSpace(*profile) != "dev" {
		root.PersistentFlags().String("profile", *profile, "")
		_ = root.PersistentFlags().Set("profile", *profile)
	}
	root.SetIn(parent.InOrStdin())
	root.SetOut(parent.OutOrStdout())
	root.SetErr(parent.ErrOrStderr())
	root.AddCommand(child)
	root.SetArgs(append([]string{child.Name()}, args...))
	return root.ExecuteContext(parent.Context())
}

func shipBuildArgs(opts shipOptions, paths shipPaths) []string {
	args := []string{}
	for _, tag := range opts.tags {
		args = append(args, "--tag", tag)
	}
	if strings.TrimSpace(opts.dockerfile) != "" && strings.TrimSpace(opts.dockerfile) != "Dockerfile" {
		args = append(args, "--file", opts.dockerfile)
	}
	for _, platform := range opts.platforms {
		args = append(args, "--platform", platform)
	}
	for _, value := range opts.buildArgs {
		args = append(args, "--build-arg", value)
	}
	for _, value := range opts.buildSecrets {
		args = append(args, "--secret", value)
	}
	for _, value := range opts.cacheFrom {
		args = append(args, "--cache-from", value)
	}
	for _, value := range opts.cacheTo {
		args = append(args, "--cache-to", value)
	}
	if opts.push {
		args = append(args, "--push")
	}
	if opts.load {
		args = append(args, "--load")
	}
	if opts.noCache {
		args = append(args, "--no-cache")
	}
	if opts.attest || opts.sbom {
		args = append(args, "--sbom")
	}
	if opts.attest || opts.provenance {
		args = append(args, "--provenance")
	}
	if opts.attest && strings.TrimSpace(paths.AttestDir) != "" {
		args = append(args, "--attest-dir", paths.AttestDir)
	}
	if opts.hermetic {
		args = append(args, "--hermetic")
	}
	if opts.allowNetwork {
		args = append(args, "--allow-network")
	}
	if opts.allowUnpinned {
		args = append(args, "--allow-unpinned-bases")
	}
	if strings.TrimSpace(opts.buildPolicy) != "" {
		args = append(args, "--policy", opts.buildPolicy)
	}
	if strings.TrimSpace(opts.buildPolicyMode) != "" {
		args = append(args, "--policy-mode", opts.buildPolicyMode)
	}
	if strings.TrimSpace(opts.builder) != "" {
		args = append(args, "--builder", opts.builder)
	}
	if strings.TrimSpace(opts.authFile) != "" {
		args = append(args, "--authfile", opts.authFile)
	}
	if opts.sandbox {
		args = append(args, "--sandbox")
	}
	if strings.TrimSpace(opts.sandboxConfig) != "" {
		args = append(args, "--sandbox-config", opts.sandboxConfig)
	}
	if strings.TrimSpace(opts.buildOutput) != "" {
		args = append(args, "--output", opts.buildOutput)
	}
	if strings.TrimSpace(paths.BuildCapture) != "" {
		args = append(args, "--capture="+paths.BuildCapture)
		for _, tag := range shipCaptureTags(opts) {
			args = append(args, "--capture-tag", tag)
		}
	}
	args = append(args, opts.buildContext)
	return args
}

func shipPlanArgs(opts shipOptions, paths shipPaths) []string {
	args := shipDeployBaseArgs(opts)
	args = append(args, "--include-crds", "--format", "json", "--output", paths.PlanOutput)
	return args
}

func shipApplyArgs(opts shipOptions, paths shipPaths) []string {
	args := shipDeployBaseArgs(opts)
	if opts.createNamespace {
		args = append(args, "--create-namespace")
	}
	if !opts.wait {
		args = append(args, "--wait=false")
	}
	if !opts.atomic {
		args = append(args, "--atomic=false")
	}
	if opts.yes {
		args = append(args, "--yes")
	}
	if opts.nonInteractive {
		args = append(args, "--non-interactive")
	}
	if opts.timeout > 0 {
		args = append(args, "--timeout", opts.timeout.String())
	}
	if opts.watch > 0 {
		args = append(args, "--watch", opts.watch.String())
	}
	if strings.TrimSpace(paths.ApplyCapture) != "" {
		args = append(args, "--capture="+paths.ApplyCapture)
		for _, tag := range shipCaptureTags(opts) {
			args = append(args, "--capture-tag", tag)
		}
	}
	if !opts.skipVerify && !strings.EqualFold(strings.TrimSpace(opts.verifyMode), "off") && strings.TrimSpace(paths.VerifyReport) != "" {
		args = append(args, "--require-verified", paths.VerifyReport)
	}
	return args
}

func shipDeployBaseArgs(opts shipOptions) []string {
	args := []string{"--chart", opts.chart, "--release", opts.release}
	if strings.TrimSpace(opts.namespace) != "" {
		args = append(args, "--namespace", opts.namespace)
	}
	if strings.TrimSpace(opts.version) != "" {
		args = append(args, "--version", opts.version)
	}
	for _, value := range opts.valuesFiles {
		args = append(args, "--values", value)
	}
	for _, value := range opts.setValues {
		args = append(args, "--set", value)
	}
	for _, value := range opts.setStringValues {
		args = append(args, "--set-string", value)
	}
	for _, value := range opts.setFileValues {
		args = append(args, "--set-file", value)
	}
	if strings.TrimSpace(opts.secretProvider) != "" {
		args = append(args, "--secret-provider", opts.secretProvider)
	}
	if strings.TrimSpace(opts.secretConfig) != "" {
		args = append(args, "--secret-config", opts.secretConfig)
	}
	return args
}

func shipCaptureTags(opts shipOptions) []string {
	tags := append([]string(nil), opts.captureTags...)
	tags = append(tags, "workflow=ship")
	if strings.TrimSpace(opts.release) != "" {
		tags = append(tags, "release="+strings.TrimSpace(opts.release))
	}
	if strings.TrimSpace(opts.namespace) != "" {
		tags = append(tags, "namespace="+strings.TrimSpace(opts.namespace))
	}
	return tags
}

func writeVerifyReport(path string, report *verify.Report) error {
	out, closer, err := openShipOutput(path)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	return verify.WriteReport(out, report, verify.OutputJSON)
}

func writeTextFile(path string, text string) error {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(path) == "-" {
		if strings.TrimSpace(path) == "-" {
			fmt.Println(text)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func openShipOutput(path string) (io.Writer, io.Closer, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return os.Stdout, nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f, nil
}

type shipSummary struct {
	Tool        string    `json:"tool"`
	GeneratedAt time.Time `json:"generatedAt"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	Steps       []string  `json:"steps"`
	Chart       string    `json:"chart"`
	Release     string    `json:"release"`
	Namespace   string    `json:"namespace,omitempty"`
	Build       struct {
		Context string   `json:"context,omitempty"`
		Tags    []string `json:"tags,omitempty"`
	} `json:"build,omitempty"`
	Paths shipPaths `json:"paths"`
}

func writeShipSummary(paths shipPaths, opts shipOptions, steps []string, status string, errText string) error {
	if strings.TrimSpace(paths.SummaryOutput) == "" {
		return nil
	}
	summary := shipSummary{
		Tool:        "ktl ship",
		GeneratedAt: time.Now().UTC(),
		Status:      status,
		Error:       errText,
		Steps:       append([]string(nil), steps...),
		Chart:       strings.TrimSpace(opts.chart),
		Release:     strings.TrimSpace(opts.release),
		Namespace:   strings.TrimSpace(opts.namespace),
		Paths:       paths,
	}
	summary.Build.Context = strings.TrimSpace(opts.buildContext)
	summary.Build.Tags = append([]string(nil), opts.tags...)
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.SummaryOutput), 0o755); err != nil {
		return err
	}
	return os.WriteFile(paths.SummaryOutput, append(raw, '\n'), 0o644)
}

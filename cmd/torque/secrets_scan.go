package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/verify"
	cfgpkg "github.com/ingresslabs/torque/internal/verify/config"
	"github.com/spf13/cobra"
)

func newSecretsScanCommand(kubeconfig, kubeContext *string) *cobra.Command {
	var scope string
	var root string
	var chartPath string
	var release string
	var namespace string
	var manifestPath string
	var valuesFiles []string
	var setValues []string
	var reportPath string
	var format string
	var mode string
	var failOn string
	var securityProfile string
	var flowGraph bool
	var evaluatedAt string

	cmd := &cobra.Command{
		Use:   "scan [ARTIFACT]",
		Short: "Scan source, rendered manifests, or artifacts for secret evidence",
		Long:  "Scan source files, rendered Kubernetes manifests, or text artifacts for secret-like values and write a redacted evidence-first report.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			scope = strings.ToLower(strings.TrimSpace(scope))
			if scope == "" {
				scope = "repo"
			}
			switch scope {
			case "repo", "render", "artifact", "build":
			default:
				return fmt.Errorf("unsupported --scope %q (expected repo, render, artifact, or build)", scope)
			}
			now := time.Now().UTC()
			if strings.TrimSpace(evaluatedAt) != "" {
				parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(evaluatedAt))
				if err != nil {
					return fmt.Errorf("--evaluated-at must be RFC3339: %w", err)
				}
				now = parsed.UTC()
			}
			opts := verify.SecretScanOptions{
				Mode:        verify.Mode(strings.ToLower(strings.TrimSpace(mode))),
				FailOn:      verify.Severity(strings.ToLower(strings.TrimSpace(failOn))),
				Profile:     strings.ToLower(strings.TrimSpace(securityProfile)),
				Surface:     "torque.secrets.scan",
				FlowGraph:   flowGraph,
				EvaluatedAt: now,
			}
			var report *verify.SecretScanReport
			var err error
			switch scope {
			case "render":
				report, err = runRenderSecretScan(ctx, kubeconfig, kubeContext, chartPath, release, namespace, manifestPath, valuesFiles, setValues, opts)
			case "artifact":
				target := ""
				if len(args) > 0 {
					target = args[0]
				}
				report, err = runTextSecretScan(scope, firstScanValue(target, root), opts)
			case "build":
				report, err = runBuildSecretScan(root, opts)
			default:
				report, err = runTextSecretScan(scope, root, opts)
			}
			if err != nil {
				return err
			}
			if strings.TrimSpace(reportPath) != "" && strings.TrimSpace(reportPath) != "-" {
				out, closer, err := cfgpkg.OpenOutput(cmd.OutOrStdout(), reportPath)
				if err != nil {
					return err
				}
				if err := verify.WriteSecretScanReport(out, report); err != nil {
					if closer != nil {
						_ = closer.Close()
					}
					return err
				}
				if closer != nil {
					_ = closer.Close()
				}
			}
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "table", "text":
				verify.RenderSecretScanText(cmd.OutOrStdout(), report)
			case "json":
				if strings.TrimSpace(reportPath) == "" || strings.TrimSpace(reportPath) == "-" {
					return verify.WriteSecretScanReport(cmd.OutOrStdout(), report)
				}
			default:
				return fmt.Errorf("unsupported --format %q (expected table or json)", format)
			}
			if report.Blocked {
				return fmt.Errorf("secret scan blocked (fail-on=%s)", report.FailOn)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "repo", "Scan scope: repo, render, build, or artifact")
	cmd.Flags().StringVar(&root, "root", ".", "Root directory or artifact path to scan")
	cmd.Flags().StringVar(&chartPath, "chart", "", "Helm chart path for --scope render")
	cmd.Flags().StringVar(&release, "release", "", "Helm release name for --scope render")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace for --scope render")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Rendered manifest path for --scope render")
	cmd.Flags().StringSliceVar(&valuesFiles, "values", nil, "Values file(s) for --scope render chart mode")
	cmd.Flags().StringSliceVar(&setValues, "set", nil, "Set value(s) for --scope render chart mode")
	cmd.Flags().StringVar(&reportPath, "report", "", "Write JSON report to this path")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table or json")
	cmd.Flags().StringVar(&mode, "mode", "warn", "Scan mode: warn|block|off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Fail threshold: info|low|medium|high|critical")
	cmd.Flags().StringVar(&securityProfile, "security-profile", "", "Security profile to apply (default or enterprise)")
	cmd.Flags().BoolVar(&flowGraph, "flow-graph", false, "Add a redacted secret flow graph to the JSON report")
	cmd.Flags().StringVar(&evaluatedAt, "evaluated-at", "", "Override evaluation time (RFC3339) for deterministic reports/tests")
	decorateCommandHelp(cmd, "Scan Flags")
	return cmd
}

func runRenderSecretScan(ctx context.Context, kubeconfig, kubeContext *string, chartPath, release, namespace, manifestPath string, valuesFiles, setValues []string, opts verify.SecretScanOptions) (*verify.SecretScanReport, error) {
	cwd, _ := os.Getwd()
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	built, err := cfgpkg.BuildFromParams(cfgpkg.Params{
		ChartPath:   chartPath,
		Release:     release,
		Namespace:   namespace,
		Manifest:    manifestPath,
		ValuesFiles: valuesFiles,
		SetValues:   setValues,
		Format:      "json",
		Report:      "-",
	})
	if err != nil {
		return nil, err
	}
	cfg := &built
	cfg.ResolvePaths(cwd)
	if err := cfg.Validate(cwd); err != nil {
		return nil, err
	}
	kcfg, kctx := "", ""
	if kubeconfig != nil {
		kcfg = *kubeconfig
	}
	if kubeContext != nil {
		kctx = *kubeContext
	}
	objects, renderedManifest, _, err := cfg.LoadObjects(ctx, cwd, kcfg, kctx, nil)
	if err != nil {
		return nil, err
	}
	opts.Stage = "render"
	if strings.EqualFold(strings.TrimSpace(cfg.Target.Kind), "namespace") {
		opts.Stage = "live"
	}
	opts.Source = cfg.TargetLabel()
	opts.TargetKind = strings.ToLower(strings.TrimSpace(cfg.Target.Kind))
	opts.ValuesFiles = append([]string(nil), cfg.Target.Chart.ValuesFiles...)
	opts.RenderedSource = renderedManifest
	if strings.EqualFold(strings.TrimSpace(cfg.Target.Kind), "manifest") {
		opts.RenderedPath = cfg.Target.Manifest
	}
	return verify.ScanRenderedSecrets(objects, opts)
}

func runTextSecretScan(scope string, root string, opts verify.SecretScanOptions) (*verify.SecretScanReport, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	inputs, err := collectSecretScanInputs(root, scope)
	if err != nil {
		return nil, err
	}
	opts.Stage = scope
	opts.Source = root
	return verify.ScanTextSecrets(inputs, opts)
}

func runBuildSecretScan(root string, opts verify.SecretScanOptions) (*verify.SecretScanReport, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	all, err := collectSecretScanInputs(root, "build")
	if err != nil {
		return nil, err
	}
	var inputs []verify.SecretTextInput
	for _, input := range all {
		name := strings.ToLower(filepath.Base(input.Path))
		if name == "dockerfile" || strings.Contains(name, "compose") || strings.HasSuffix(name, ".env") {
			inputs = append(inputs, input)
		}
	}
	opts.Stage = "build"
	opts.Source = root
	return verify.ScanTextSecrets(inputs, opts)
}

func collectSecretScanInputs(root string, stage string) ([]verify.SecretTextInput, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		raw, err := os.ReadFile(root)
		if err != nil {
			return nil, err
		}
		if !looksText(raw) {
			return nil, fmt.Errorf("%s does not look like a text artifact", root)
		}
		return []verify.SecretTextInput{{Path: root, Content: string(raw), Stage: stage}}, nil
	}
	var inputs []verify.SecretTextInput
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "node_modules", "vendor", "bin", "dist", ".terraform":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !scanFileName(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 2*1024*1024 {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !looksText(raw) {
			return nil
		}
		inputs = append(inputs, verify.SecretTextInput{Path: path, Content: string(raw), Stage: stage})
		return nil
	})
	return inputs, err
}

func scanFileName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	switch lower {
	case "dockerfile", ".env", ".npmrc", ".pypirc", ".netrc", "config.json":
		return true
	}
	for _, ext := range []string{".yaml", ".yml", ".json", ".toml", ".tf", ".tfvars", ".env", ".sh", ".bash", ".zsh", ".go", ".js", ".ts", ".py", ".txt", ".md"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func looksText(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return false
	}
	check := raw
	if len(check) > 4096 {
		check = check[:4096]
	}
	printable := 0
	for _, b := range check {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 32 && b <= 126) {
			printable++
		}
	}
	return float64(printable)/float64(len(check)) >= 0.85
}

func firstScanValue(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

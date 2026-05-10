package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type repairDependency struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

type repairFix struct {
	Kind        string           `json:"kind"`
	Namespace   string           `json:"namespace,omitempty"`
	Name        string           `json:"name"`
	Action      string           `json:"action"`
	Path        string           `json:"path,omitempty"`
	Applied     bool             `json:"applied,omitempty"`
	Manual      bool             `json:"manual,omitempty"`
	Description string           `json:"description"`
	Dependency  repairDependency `json:"dependency"`
}

type repairReport struct {
	Version      int               `json:"version"`
	GeneratedAt  string            `json:"generatedAt"`
	Source       string            `json:"source"`
	Release      string            `json:"release,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Chart        string            `json:"chart,omitempty"`
	Branch       string            `json:"branch,omitempty"`
	RootCause    string            `json:"rootCause"`
	Confidence   int               `json:"confidence"`
	Reasons      []string          `json:"reasons,omitempty"`
	Fixes        []repairFix       `json:"fixes,omitempty"`
	Commands     []string          `json:"commands,omitempty"`
	ProofSignals map[string]string `json:"proofSignals,omitempty"`
}

type repairOptions struct {
	SourcePath     string
	ChartPath      string
	Branch         string
	Apply          bool
	PRBodyPath     string
	Format         string
	Yes            bool
	NonInteractive bool
}

func newRepairCommand() *cobra.Command {
	var opts repairOptions
	cmd := &cobra.Command{
		Use:     "repair",
		Aliases: []string{"fix"},
		Short:   "Turn failed apply proof into a chart repair plan",
		Long:    "Read a Torque apply or simulation proof bundle, diagnose likely rollout root cause, and optionally write PR-ready chart repair files.",
		Args:    cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.SourcePath) == "" {
				return fmt.Errorf("--from is required")
			}
			if opts.Apply && strings.TrimSpace(opts.ChartPath) == "" {
				return fmt.Errorf("--chart is required with --apply")
			}
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "text", "json", "markdown":
			default:
				return fmt.Errorf("unsupported --format %q (expected text, json, or markdown)", opts.Format)
			}
			if err := validateNonInteractive(cmd, opts.NonInteractive, opts.Yes); err != nil {
				return err
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := loadRepairProofBundle(opts.SourcePath)
			if err != nil {
				return err
			}
			report := buildRepairReport(bundle, opts.SourcePath, opts.ChartPath, opts.Branch)
			if opts.Apply {
				dec, err := approvalMode(cmd, opts.Yes, opts.NonInteractive)
				if err != nil {
					return err
				}
				if err := confirmAction(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), dec, fmt.Sprintf("Apply %d repair file(s) to chart %s? Only 'yes' will be accepted:", countAutoRepairFixes(report.Fixes), opts.ChartPath), confirmModeYes, ""); err != nil {
					return err
				}
				if err := applyRepairReport(cmd.Context(), &report, opts); err != nil {
					return err
				}
			}
			if path := strings.TrimSpace(opts.PRBodyPath); path != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
					return fmt.Errorf("create PR body dir: %w", err)
				}
				if err := os.WriteFile(path, []byte(renderRepairMarkdown(report)+"\n"), 0o644); err != nil {
					return fmt.Errorf("write PR body: %w", err)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "PR body written to %s\n", path)
			}
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "json":
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			case "markdown":
				fmt.Fprintln(cmd.OutOrStdout(), renderRepairMarkdown(report))
			default:
				renderRepairText(cmd.OutOrStdout(), report)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.SourcePath, "from", "", "Apply proof bundle JSON to diagnose")
	cmd.Flags().StringVar(&opts.ChartPath, "chart", "", "Chart directory to patch")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Repair branch name to create when --apply is used in a clean git worktree")
	cmd.Flags().BoolVar(&opts.Apply, "apply", false, "Write repair files into the chart")
	cmd.Flags().StringVar(&opts.PRBodyPath, "pr-body", "", "Write a Markdown PR body to this path")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format: text, json, or markdown")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Auto-approve repair file writes")
	cmd.Flags().BoolVar(&opts.NonInteractive, "non-interactive", false, "Fail instead of prompting (requires --yes)")
	decorateCommandHelp(cmd, "Repair Flags")
	return cmd
}

func loadRepairProofBundle(path string) (*applyProofBundle, error) {
	path = strings.TrimSpace(path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "apply.proof.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --from: %w", err)
	}
	var bundle applyProofBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, fmt.Errorf("parse proof bundle: %w", err)
	}
	if strings.TrimSpace(bundle.Release) == "" && bundle.Prediction == nil && bundle.RollbackProof == nil {
		return nil, fmt.Errorf("%s does not look like a Torque apply proof bundle", path)
	}
	return &bundle, nil
}

func buildRepairReport(bundle *applyProofBundle, sourcePath, chartPath, branch string) repairReport {
	now := time.Now().UTC()
	report := repairReport{
		Version:      1,
		GeneratedAt:  now.Format(time.RFC3339Nano),
		Source:       strings.TrimSpace(sourcePath),
		Chart:        strings.TrimSpace(chartPath),
		Branch:       strings.TrimSpace(branch),
		RootCause:    "unknown rollout failure",
		Confidence:   35,
		ProofSignals: map[string]string{},
	}
	if bundle == nil {
		report.Reasons = []string{"No proof bundle was loaded."}
		return report
	}
	report.Release = bundle.Release
	report.Namespace = bundle.Namespace
	if report.Chart == "" {
		report.Chart = bundle.Chart
	}
	if bundle.Status != "" {
		report.ProofSignals["status"] = bundle.Status
	}
	if bundle.Error != "" {
		report.ProofSignals["error"] = bundle.Error
	}
	if bundle.Prediction != nil {
		report.ProofSignals["predictionRisk"] = bundle.Prediction.Risk
	}
	missing := repairMissingDependencies(bundle)
	if len(missing) > 0 {
		report.RootCause = summarizeMissingRootCause(missing)
		report.Confidence = 92
		report.Reasons = append(report.Reasons, fmt.Sprintf("Prediction found %d missing dependency resource(s).", len(missing)))
		report.Fixes = append(report.Fixes, buildMissingDependencyFixes(missing, report.Namespace, report.Chart)...)
	} else if reason, confidence := classifySnapshotFailure(bundle); reason != "" {
		report.RootCause = reason
		report.Confidence = confidence
		report.Reasons = append(report.Reasons, "Resource snapshot and rollback proof contain matching failure signals.")
	} else if bundle.Error != "" {
		report.RootCause = bundle.Error
		report.Reasons = append(report.Reasons, "No automatic chart patch is known for this failure class yet.")
	}
	if len(report.Fixes) == 0 {
		report.Reasons = append(report.Reasons, "Torque did not find a safe automatic chart patch; use this report as the PR investigation body.")
	}
	report.Commands = buildRepairCommands(report)
	return report
}

func repairMissingDependencies(bundle *applyProofBundle) []repairDependency {
	if bundle == nil || bundle.Prediction == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var deps []repairDependency
	for _, dep := range bundle.Prediction.MissingDependencies {
		kind := strings.TrimSpace(dep.Kind)
		name := strings.TrimSpace(dep.Name)
		if kind == "" || name == "" {
			continue
		}
		item := repairDependency{Kind: kind, Namespace: firstNonEmpty(dep.Namespace, bundle.Namespace), Name: name}
		key := strings.ToLower(item.Kind) + "/" + item.Namespace + "/" + item.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deps = append(deps, item)
	}
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].Kind != deps[j].Kind {
			return deps[i].Kind < deps[j].Kind
		}
		if deps[i].Namespace != deps[j].Namespace {
			return deps[i].Namespace < deps[j].Namespace
		}
		return deps[i].Name < deps[j].Name
	})
	return deps
}

func summarizeMissingRootCause(deps []repairDependency) string {
	if len(deps) == 0 {
		return "missing dependency"
	}
	if len(deps) == 1 {
		dep := deps[0]
		return fmt.Sprintf("missing %s %s", dep.Kind, dep.Name)
	}
	return fmt.Sprintf("%d missing dependency resources", len(deps))
}

func buildMissingDependencyFixes(deps []repairDependency, namespace, chartPath string) []repairFix {
	var fixes []repairFix
	for _, dep := range deps {
		action := "manual"
		manual := true
		description := fmt.Sprintf("Create or reference %s %s before retrying the rollout.", dep.Kind, dep.Name)
		path := ""
		if canGenerateDependency(dep.Kind) && strings.TrimSpace(chartPath) != "" {
			action = "add-chart-template"
			manual = false
			path = filepath.Join(chartPath, "templates", repairTemplateFilename(dep))
			description = fmt.Sprintf("Add a minimal %s template for missing dependency %s.", dep.Kind, dep.Name)
		}
		fixes = append(fixes, repairFix{
			Kind:        dep.Kind,
			Namespace:   firstNonEmpty(dep.Namespace, namespace),
			Name:        dep.Name,
			Action:      action,
			Path:        path,
			Manual:      manual,
			Description: description,
			Dependency:  dep,
		})
	}
	return fixes
}

func canGenerateDependency(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "configmap", "secret", "serviceaccount":
		return true
	default:
		return false
	}
}

func repairTemplateFilename(dep repairDependency) string {
	return fmt.Sprintf("torque-repair-%s-%s.yaml", strings.ToLower(sanitizeFilename(dep.Kind)), sanitizeFilename(dep.Name))
}

func classifySnapshotFailure(bundle *applyProofBundle) (string, int) {
	var rows []deploy.ResourceStatus
	if bundle != nil {
		rows = append(rows, bundle.ResourceSnapshot...)
		if bundle.RollbackProof != nil {
			rows = append(rows, bundle.RollbackProof.ResourceSnapshot...)
		}
	}
	joined := strings.ToLower(bundleErrorText(bundle))
	for _, row := range rows {
		joined += "\n" + strings.ToLower(row.Status+" "+row.Message)
	}
	switch {
	case strings.Contains(joined, "imagepullbackoff") || strings.Contains(joined, "errimagepull") || strings.Contains(joined, "pull image"):
		return "image pull failure", 86
	case strings.Contains(joined, "createcontainerconfigerror") || strings.Contains(joined, "not found"):
		return "container configuration dependency failure", 78
	case strings.Contains(joined, "failedscheduling") || strings.Contains(joined, "unschedulable"):
		return "scheduling capacity or placement failure", 75
	case strings.Contains(joined, "slo failed"):
		return "rollout SLO failed", 70
	default:
		return "", 0
	}
}

func bundleErrorText(bundle *applyProofBundle) string {
	if bundle == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(bundle.Error)
	if bundle.RollbackProof != nil {
		b.WriteString("\n")
		b.WriteString(bundle.RollbackProof.Trigger.Error)
		b.WriteString("\n")
		b.WriteString(bundle.RollbackProof.Trigger.Reason)
	}
	return b.String()
}

func buildRepairCommands(report repairReport) []string {
	if report.Release == "" {
		return nil
	}
	var cmds []string
	if report.Chart != "" {
		cmd := fmt.Sprintf("torque apply plan --chart %s --release %s", shellQuoteToken(report.Chart), shellQuoteToken(report.Release))
		if report.Namespace != "" {
			cmd += " -n " + shellQuoteToken(report.Namespace)
		}
		cmds = append(cmds, cmd)
		cmd = fmt.Sprintf("torque apply --chart %s --release %s --predict --proof-bundle ./repair-proof.json --yes", shellQuoteToken(report.Chart), shellQuoteToken(report.Release))
		if report.Namespace != "" {
			cmd += " -n " + shellQuoteToken(report.Namespace)
		}
		cmds = append(cmds, cmd)
	}
	return cmds
}

func applyRepairReport(ctx context.Context, report *repairReport, opts repairOptions) error {
	if report == nil {
		return nil
	}
	if strings.TrimSpace(opts.Branch) != "" {
		msg, err := ensureRepairBranch(ctx, opts.ChartPath, opts.Branch)
		if err != nil {
			return err
		}
		if msg != "" {
			report.Reasons = append(report.Reasons, msg)
		}
	}
	for i := range report.Fixes {
		fix := &report.Fixes[i]
		if fix.Manual || fix.Path == "" {
			continue
		}
		text, err := renderRepairTemplate(*fix, opts.SourcePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(fix.Path), 0o755); err != nil {
			return fmt.Errorf("create repair template dir: %w", err)
		}
		if _, err := os.Stat(fix.Path); err == nil {
			return fmt.Errorf("repair template already exists: %s", fix.Path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect repair template %s: %w", fix.Path, err)
		}
		if err := os.WriteFile(fix.Path, []byte(text), 0o644); err != nil {
			return fmt.Errorf("write repair template %s: %w", fix.Path, err)
		}
		fix.Applied = true
	}
	return nil
}

func ensureRepairBranch(ctx context.Context, chartPath, branch string) (string, error) {
	chartPath = strings.TrimSpace(chartPath)
	if chartPath == "" {
		chartPath = "."
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", nil
	}
	rootCmd := exec.CommandContext(ctx, "git", "-C", chartPath, "rev-parse", "--show-toplevel")
	rootRaw, err := rootCmd.Output()
	if err != nil {
		return fmt.Sprintf("Branch %s requested, but %s is not inside a git worktree.", branch, chartPath), nil
	}
	root := strings.TrimSpace(string(rootRaw))
	statusCmd := exec.CommandContext(ctx, "git", "-C", root, "status", "--short")
	statusRaw, err := statusCmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(string(statusRaw)) != "" {
		return "", fmt.Errorf("cannot create repair branch %s: git worktree is not clean", branch)
	}
	currentCmd := exec.CommandContext(ctx, "git", "-C", root, "branch", "--show-current")
	currentRaw, _ := currentCmd.Output()
	if strings.TrimSpace(string(currentRaw)) == branch {
		return fmt.Sprintf("Using existing repair branch %s.", branch), nil
	}
	checkCmd := exec.CommandContext(ctx, "git", "-C", root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err := checkCmd.Run(); err == nil {
		if out, err := runGitRepair(ctx, root, "switch", branch); err != nil {
			return "", err
		} else if out != "" {
			return strings.TrimSpace(out), nil
		}
		return fmt.Sprintf("Switched to repair branch %s.", branch), nil
	}
	if out, err := runGitRepair(ctx, root, "switch", "-c", branch); err != nil {
		return "", err
	} else if out != "" {
		return strings.TrimSpace(out), nil
	}
	return fmt.Sprintf("Created repair branch %s.", branch), nil
}

func runGitRepair(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

func renderRepairTemplate(fix repairFix, sourcePath string) (string, error) {
	source := filepath.Base(strings.TrimSpace(sourcePath))
	if source == "" || source == "." {
		source = "proof-bundle"
	}
	metadata := map[string]any{
		"name": fix.Name,
		"labels": map[string]string{
			"app.kubernetes.io/managed-by": "torque-repair",
		},
		"annotations": map[string]string{
			"torque.ingresslabs.dev/repair-source": source,
		},
	}
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       canonicalRepairKind(fix.Kind),
		"metadata":   metadata,
	}
	switch strings.ToLower(strings.TrimSpace(fix.Kind)) {
	case "configmap":
		obj["data"] = map[string]string{}
	case "secret":
		obj["type"] = "Opaque"
		obj["stringData"] = map[string]string{"placeholder": "replace-me"}
	case "serviceaccount":
	default:
		return "", fmt.Errorf("no automatic template for %s", fix.Kind)
	}
	raw, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return "# Generated by torque repair. Review before merging.\n" + string(raw), nil
}

func canonicalRepairKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "configmap":
		return "ConfigMap"
	case "secret":
		return "Secret"
	case "serviceaccount":
		return "ServiceAccount"
	default:
		return strings.TrimSpace(kind)
	}
}

func renderRepairText(out io.Writer, report repairReport) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "Root cause: %s\n", report.RootCause)
	fmt.Fprintf(out, "Confidence: %d%%\n", report.Confidence)
	if report.Branch != "" {
		fmt.Fprintf(out, "Branch: %s\n", report.Branch)
	}
	if len(report.Reasons) > 0 {
		fmt.Fprintln(out, "\nWhy:")
		for _, reason := range report.Reasons {
			fmt.Fprintf(out, "  - %s\n", reason)
		}
	}
	if len(report.Fixes) > 0 {
		fmt.Fprintln(out, "\nPatch plan:")
		for _, fix := range report.Fixes {
			status := "planned"
			if fix.Applied {
				status = "applied"
			} else if fix.Manual {
				status = "manual"
			}
			line := fmt.Sprintf("  - [%s] %s", status, fix.Description)
			if fix.Path != "" {
				line += " -> " + fix.Path
			}
			fmt.Fprintln(out, line)
		}
	}
	if len(report.Commands) > 0 {
		fmt.Fprintln(out, "\nValidate:")
		for _, cmd := range report.Commands {
			fmt.Fprintf(out, "  %s\n", cmd)
		}
	}
}

func renderRepairMarkdown(report repairReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Torque Repair: %s\n\n", firstNonEmpty(report.Release, "release"))
	fmt.Fprintf(&b, "- Root cause: `%s`\n", report.RootCause)
	fmt.Fprintf(&b, "- Confidence: `%d%%`\n", report.Confidence)
	if report.Namespace != "" {
		fmt.Fprintf(&b, "- Namespace: `%s`\n", report.Namespace)
	}
	if report.Source != "" {
		fmt.Fprintf(&b, "- Source proof: `%s`\n", report.Source)
	}
	if report.Branch != "" {
		fmt.Fprintf(&b, "- Branch: `%s`\n", report.Branch)
	}
	if len(report.Reasons) > 0 {
		fmt.Fprintf(&b, "\n## Evidence\n\n")
		for _, reason := range report.Reasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
	}
	if len(report.Fixes) > 0 {
		fmt.Fprintf(&b, "\n## Patch Plan\n\n")
		for _, fix := range report.Fixes {
			status := "planned"
			if fix.Applied {
				status = "applied"
			} else if fix.Manual {
				status = "manual"
			}
			fmt.Fprintf(&b, "- `%s` %s", status, fix.Description)
			if fix.Path != "" {
				fmt.Fprintf(&b, " (`%s`)", fix.Path)
			}
			fmt.Fprintln(&b)
		}
	}
	if len(report.Commands) > 0 {
		fmt.Fprintf(&b, "\n## Validation\n\n```bash\n")
		for _, cmd := range report.Commands {
			fmt.Fprintf(&b, "%s\n", cmd)
		}
		fmt.Fprintf(&b, "```\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderRepairPatch(report repairReport, sourcePath string) string {
	var b strings.Builder
	for _, fix := range report.Fixes {
		if fix.Manual || strings.TrimSpace(fix.Path) == "" {
			continue
		}
		body, err := renderRepairTemplate(fix, sourcePath)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		path := filepath.ToSlash(strings.TrimPrefix(filepath.Clean(fix.Path), string(filepath.Separator)))
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
		fmt.Fprintf(&b, "new file mode 100644\n")
		fmt.Fprintf(&b, "--- /dev/null\n")
		fmt.Fprintf(&b, "+++ b/%s\n", path)
		lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, line := range lines {
			fmt.Fprintf(&b, "+%s\n", line)
		}
	}
	return b.String()
}

func countAutoRepairFixes(fixes []repairFix) int {
	count := 0
	for _, fix := range fixes {
		if !fix.Manual && fix.Path != "" {
			count++
		}
	}
	return count
}

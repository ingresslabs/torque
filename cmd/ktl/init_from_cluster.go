// File: cmd/ktl/init_from_cluster.go
// Brief: "ktl init from-cluster" onboarding flow.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ingresslabs/ktl/internal/clusteradopt"
	"github.com/ingresslabs/ktl/internal/kube"
	"github.com/spf13/cobra"
)

type initFromClusterOptions struct {
	output          string
	stackName       string
	clusterName     string
	namespaces      []string
	allNamespaces   bool
	includeSystem   bool
	includeHelm     bool
	includeGitOps   bool
	includeWorkload bool
	writeCharts     bool
	chartsDir       string
	writeValues     bool
	valuesDir       string
	force           bool
	dryRun          bool
}

func newInitFromClusterCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	opts := initFromClusterOptions{
		output:          "stack.yaml",
		includeHelm:     true,
		includeGitOps:   true,
		includeWorkload: true,
		writeCharts:     true,
		chartsDir:       "charts/adopted",
		valuesDir:       "values/adopted",
	}

	cmd := &cobra.Command{
		Use:   "from-cluster [path]",
		Short: "Generate a starter stack.yaml from an existing cluster",
		Long: strings.TrimSpace(`
Inspects the current cluster for Helm releases, namespaces, GitOps resources,
local Helmfile files, and current workloads, then writes a starter stack.yaml.

The generated stack is executable for discovered Helm releases. Argo CD, Flux,
Helmfile, and non-Helm workloads are preserved as adoption notes so teams can
move incrementally instead of rewriting their cluster before ktl is useful.`),
		Example: `  # Generate stack.yaml from the active namespace
  ktl init from-cluster

  # Inspect every non-system namespace
  ktl init from-cluster --all-namespaces

  # Preview without writing
  ktl init from-cluster --all-namespaces --dry-run

  # Export installed charts and current Helm values
  ktl init from-cluster --write-values`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitFromCluster(cmd.Context(), cmd, kubeconfig, kubeContext, opts, args)
		},
	}

	cmd.Flags().StringVar(&opts.output, "output", opts.output, "Output stack path, or - for stdout")
	cmd.Flags().StringVar(&opts.stackName, "name", "", "Stack name to write")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster target name to write in stack defaults")
	cmd.Flags().StringArrayVarP(&opts.namespaces, "namespace", "n", nil, "Namespace to inspect (repeatable or comma-separated; defaults to current namespace)")
	cmd.Flags().BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "Inspect every namespace")
	cmd.Flags().BoolVar(&opts.includeSystem, "include-system", false, "Include kube-* namespaces when using --all-namespaces")
	cmd.Flags().BoolVar(&opts.includeHelm, "helm", opts.includeHelm, "Discover Helm releases")
	cmd.Flags().BoolVar(&opts.includeGitOps, "gitops", opts.includeGitOps, "Discover Argo CD and Flux resources")
	cmd.Flags().BoolVar(&opts.includeWorkload, "workloads", opts.includeWorkload, "Discover current Kubernetes workloads")
	cmd.Flags().BoolVar(&opts.writeCharts, "write-charts", opts.writeCharts, "Export installed Helm chart archives and reference them from stack.yaml")
	cmd.Flags().StringVar(&opts.chartsDir, "charts-dir", opts.chartsDir, "Directory for --write-charts output")
	cmd.Flags().BoolVar(&opts.writeValues, "write-values", false, "Write current Helm values and reference them from stack.yaml")
	cmd.Flags().StringVar(&opts.valuesDir, "values-dir", opts.valuesDir, "Directory for --write-values output")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Overwrite existing stack.yaml and generated values files")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print the generated stack.yaml without writing files")
	decorateCommandHelp(cmd, "Onboarding")
	return cmd
}

func runInitFromCluster(ctx context.Context, cmd *cobra.Command, kubeconfig *string, kubeContext *string, opts initFromClusterOptions, args []string) error {
	if opts.allNamespaces && len(opts.namespaces) > 0 {
		return fmt.Errorf("--namespace cannot be combined with --all-namespaces")
	}
	if !opts.includeHelm && !opts.includeGitOps && !opts.includeWorkload {
		return fmt.Errorf("at least one discovery source must be enabled")
	}

	repoRoot, _, err := resolveInitPaths(args)
	if err != nil {
		return err
	}
	outputPath, writeStdout, err := resolveFromClusterOutputPath(repoRoot, opts.output)
	if err != nil {
		return err
	}
	if !opts.dryRun && !writeStdout && !opts.force {
		if fi, err := os.Stat(outputPath); err == nil {
			if fi.IsDir() {
				return fmt.Errorf("%s exists and is a directory", outputPath)
			}
			return fmt.Errorf("%s already exists (use --force)", outputPath)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	kubeconfigValue := ""
	if kubeconfig != nil {
		kubeconfigValue = strings.TrimSpace(*kubeconfig)
	}
	contextValue := ""
	if kubeContext != nil {
		contextValue = strings.TrimSpace(*kubeContext)
	}
	strictContext := rootFlagChanged(cmd, "kubeconfig") || rootFlagChanged(cmd, "context")
	info, note, err := detectKubeContext(kubeconfig, kubeContext, strictContext)
	if err != nil {
		return err
	}
	if info != nil {
		if contextValue == "" {
			contextValue = info.Context
		}
		if kubeconfigValue == "" {
			kubeconfigValue = info.Kubeconfig
		}
	}

	kClient, err := kube.New(ctx, kubeconfigValue, contextValue)
	if err != nil {
		return fmt.Errorf("init kube client: %w", err)
	}

	snapshot, err := clusteradopt.Discover(ctx, kClient, clusteradopt.DiscoverOptions{
		Kubeconfig:      kubeconfigValue,
		Context:         contextValue,
		Namespaces:      opts.namespaces,
		AllNamespaces:   opts.allNamespaces,
		IncludeSystem:   opts.includeSystem,
		IncludeHelm:     opts.includeHelm,
		IncludeGitOps:   opts.includeGitOps,
		IncludeWorkload: opts.includeWorkload,
	})
	if err != nil {
		return err
	}
	if info != nil {
		snapshot.Cluster.Name = firstNonEmpty(opts.clusterName, info.Context, info.Cluster)
		snapshot.Cluster.Context = info.Context
		snapshot.Cluster.Kubeconfig = info.Kubeconfig
	}
	snapshot.Helmfiles = clusteradopt.DiscoverHelmfiles(repoRoot)

	stackYAML, valuesFiles, chartFiles, err := clusteradopt.RenderStackYAML(snapshot, clusteradopt.RenderOptions{
		StackName:   opts.stackName,
		ClusterName: firstNonEmpty(opts.clusterName, snapshot.Cluster.Name, contextValue),
		Kubeconfig:  kubeconfigValue,
		Context:     contextValue,
		ChartsDir:   opts.chartsDir,
		WriteCharts: opts.writeCharts,
		ValuesDir:   opts.valuesDir,
		WriteValues: opts.writeValues,
	})
	if err != nil {
		return err
	}

	valuesResult := clusteradopt.ValuesWriteResult{}
	chartsResult := clusteradopt.ValuesWriteResult{}
	if opts.writeCharts {
		chartsResult, err = clusteradopt.WriteChartFiles(repoRoot, chartFiles, opts.force, opts.dryRun)
		if err != nil {
			return err
		}
	}
	if opts.writeValues {
		valuesResult, err = clusteradopt.WriteValuesFiles(repoRoot, valuesFiles, opts.force, opts.dryRun)
		if err != nil {
			return err
		}
	}

	if opts.dryRun || writeStdout {
		if _, err := cmd.OutOrStdout().Write([]byte(stackYAML)); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, []byte(stackYAML), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", outputPath, err)
		}
	}

	summaryOut := cmd.OutOrStdout()
	if opts.dryRun || writeStdout {
		summaryOut = cmd.ErrOrStderr()
	}
	printInitFromClusterSummary(summaryOut, initFromClusterSummary{
		RepoRoot:     repoRoot,
		OutputPath:   outputPath,
		Stdout:       writeStdout,
		DryRun:       opts.dryRun,
		KubeNote:     note,
		Snapshot:     snapshot,
		ChartsResult: chartsResult,
		WriteCharts:  opts.writeCharts,
		ValuesResult: valuesResult,
		WriteValues:  opts.writeValues,
	})
	return nil
}

type initFromClusterSummary struct {
	RepoRoot     string
	OutputPath   string
	Stdout       bool
	DryRun       bool
	KubeNote     string
	Snapshot     *clusteradopt.Snapshot
	ChartsResult clusteradopt.ValuesWriteResult
	WriteCharts  bool
	ValuesResult clusteradopt.ValuesWriteResult
	WriteValues  bool
}

func printInitFromClusterSummary(out interface{ Write([]byte) (int, error) }, s initFromClusterSummary) {
	mode := "created"
	if s.DryRun {
		mode = "previewed"
	}
	target := s.OutputPath
	if s.Stdout {
		target = "stdout"
	}
	fmt.Fprintf(out, "ktl init from-cluster %s %s\n", mode, target)
	if s.KubeNote != "" {
		fmt.Fprintf(out, "note: %s\n", s.KubeNote)
	}
	if s.Snapshot != nil {
		fmt.Fprintf(out, "discovered: %d namespaces, %d Helm releases, %d GitOps resources, %d workloads, %d Helmfile files\n",
			len(s.Snapshot.Namespaces), len(s.Snapshot.HelmReleases), len(s.Snapshot.GitOps), len(s.Snapshot.Workloads), len(s.Snapshot.Helmfiles))
		for _, warning := range s.Snapshot.Warnings {
			fmt.Fprintf(out, "warning: %s\n", warning)
		}
	}
	if s.WriteValues {
		if len(s.ValuesResult.Created) > 0 {
			label := "values written"
			if s.DryRun {
				label = "values planned"
			}
			fmt.Fprintf(out, "%s: %s\n", label, strings.Join(s.ValuesResult.Created, ", "))
		}
		if len(s.ValuesResult.Skipped) > 0 {
			fmt.Fprintf(out, "values skipped: %s\n", strings.Join(s.ValuesResult.Skipped, ", "))
		}
	}
	if s.WriteCharts {
		if len(s.ChartsResult.Created) > 0 {
			label := "charts written"
			if s.DryRun {
				label = "charts planned"
			}
			fmt.Fprintf(out, "%s: %s\n", label, strings.Join(s.ChartsResult.Created, ", "))
		}
		if len(s.ChartsResult.Skipped) > 0 {
			fmt.Fprintf(out, "charts skipped: %s\n", strings.Join(s.ChartsResult.Skipped, ", "))
		}
	}
	fmt.Fprintln(out, "next: ktl stack plan --config stack.yaml")
	fmt.Fprintln(out, "next: ktl stack verify --config stack.yaml")
}

func resolveFromClusterOutputPath(repoRoot string, output string) (string, bool, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		output = "stack.yaml"
	}
	if output == "-" {
		return "", true, nil
	}
	if filepath.IsAbs(output) {
		return filepath.Clean(output), false, nil
	}
	return filepath.Join(repoRoot, output), false, nil
}

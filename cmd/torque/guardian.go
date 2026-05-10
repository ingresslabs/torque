package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/kube"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const (
	guardianTool        = "torque-guardian"
	guardianInstallName = "torque-guardian"
)

var guardianSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{20,}`),
	regexp.MustCompile(`(?i)(password|token|secret|client_secret|access_key)\s*[:=]\s*([^&\s]+)`),
}

type guardianRuntimeProof struct {
	Version        string                 `json:"version"`
	Tool           string                 `json:"tool"`
	GeneratedAt    string                 `json:"generatedAt"`
	ClusterHost    string                 `json:"clusterHost,omitempty"`
	Namespace      string                 `json:"namespace,omitempty"`
	AllNamespaces  bool                   `json:"allNamespaces,omitempty"`
	Since          string                 `json:"since,omitempty"`
	StartedAt      string                 `json:"startedAt,omitempty"`
	EventsTimeline guardianEventsTimeline `json:"eventsTimeline"`
	Summary        guardianRuntimeSummary `json:"summary"`
}

type guardianRuntimeSummary struct {
	Events   int `json:"events"`
	Warnings int `json:"warnings"`
}

type guardianDiffProof struct {
	Version                string                         `json:"version"`
	Tool                   string                         `json:"tool"`
	GeneratedAt            string                         `json:"generatedAt"`
	Source                 string                         `json:"source,omitempty"`
	Release                string                         `json:"release,omitempty"`
	Namespace              string                         `json:"namespace,omitempty"`
	Chart                  string                         `json:"chart,omitempty"`
	RenderedManifestSHA256 string                         `json:"renderedManifestSha256,omitempty"`
	ClusterHost            string                         `json:"clusterHost,omitempty"`
	Status                 string                         `json:"status"`
	Blocked                bool                           `json:"blocked"`
	Summary                guardianDiffSummary            `json:"summary"`
	PredictedVsLive        guardianPredictedVsLiveDiff    `json:"predictedVsLive"`
	ManagedFields          guardianManagedFieldsReport    `json:"managedFields"`
	DriftTimeline          guardianDriftTimeline          `json:"driftTimeline"`
	EventsTimeline         guardianEventsTimeline         `json:"eventsTimeline"`
	RuntimeSecretBoundary  guardianRuntimeBoundaryReport  `json:"runtimeSecretBoundary"`
	RolloutAftercare       guardianRolloutAftercareReport `json:"rolloutAftercare"`
	Fix                    guardianFixPlan                `json:"fix"`
}

type guardianDiffSummary struct {
	Resources          int `json:"resources"`
	Unchanged          int `json:"unchanged"`
	Changed            int `json:"changed"`
	Missing            int `json:"missing"`
	FetchErrors        int `json:"fetchErrors"`
	ManagedFieldOwners int `json:"managedFieldOwners"`
	RuntimeBoundary    int `json:"runtimeBoundaryFindings"`
	WarningEvents      int `json:"warningEvents"`
	AftercareIssues    int `json:"aftercareIssues"`
}

type guardianPredictedVsLiveDiff struct {
	Version string              `json:"version"`
	Passed  bool                `json:"passed"`
	Changes []guardianDriftItem `json:"changes,omitempty"`
}

type guardianResourceRef struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

type guardianDriftItem struct {
	Resource guardianResourceRef `json:"resource"`
	Reason   string              `json:"reason"`
	Diff     string              `json:"diff,omitempty"`
}

type guardianManagedFieldsReport struct {
	Version string                    `json:"version"`
	Owners  []guardianManagedOwnerRow `json:"owners,omitempty"`
}

type guardianManagedOwnerRow struct {
	Resource   guardianResourceRef `json:"resource"`
	Manager    string              `json:"manager,omitempty"`
	Operation  string              `json:"operation,omitempty"`
	APIVersion string              `json:"apiVersion,omitempty"`
	Time       string              `json:"time,omitempty"`
	FieldCount int                 `json:"fieldCount,omitempty"`
	Suspicious bool                `json:"suspicious,omitempty"`
}

type guardianDriftTimeline struct {
	Version string                `json:"version"`
	Items   []guardianTimelineRow `json:"items,omitempty"`
}

type guardianTimelineRow struct {
	Time      string              `json:"time"`
	Resource  guardianResourceRef `json:"resource"`
	Reason    string              `json:"reason"`
	Manager   string              `json:"manager,omitempty"`
	EventType string              `json:"eventType,omitempty"`
	Message   string              `json:"message,omitempty"`
}

type guardianEventsTimeline struct {
	Version string             `json:"version"`
	Events  []guardianEventRow `json:"events,omitempty"`
}

type guardianEventRow struct {
	Time     string              `json:"time,omitempty"`
	Type     string              `json:"type,omitempty"`
	Reason   string              `json:"reason,omitempty"`
	Message  string              `json:"message,omitempty"`
	Resource guardianResourceRef `json:"resource"`
	Count    int32               `json:"count,omitempty"`
	Source   string              `json:"source,omitempty"`
}

type guardianRuntimeBoundaryReport struct {
	Version  string                    `json:"version"`
	Passed   bool                      `json:"passed"`
	Findings []guardianBoundaryFinding `json:"findings,omitempty"`
}

type guardianBoundaryFinding struct {
	Resource guardianResourceRef `json:"resource"`
	Surface  string              `json:"surface"`
	Boundary string              `json:"boundary"`
	Severity string              `json:"severity"`
	Message  string              `json:"message"`
}

type guardianRolloutAftercareReport struct {
	Version string                     `json:"version"`
	Passed  bool                       `json:"passed"`
	Items   []guardianAftercareFinding `json:"items,omitempty"`
}

type guardianAftercareFinding struct {
	Resource guardianResourceRef `json:"resource"`
	Severity string              `json:"severity"`
	Reason   string              `json:"reason"`
	Message  string              `json:"message"`
}

type guardianFixPlan struct {
	Branch         string   `json:"branch,omitempty"`
	Commands       []string `json:"commands,omitempty"`
	PatchPath      string   `json:"patchPath,omitempty"`
	PRBodyPath     string   `json:"prBodyPath,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
}

type guardianSimulationSource struct {
	Dir                    string
	Release                string
	Namespace              string
	Chart                  string
	RenderedManifestSHA256 string
	Resources              map[string]string
}

type guardianLiveGetter func(ctx context.Context, desired *unstructured.Unstructured, defaultNamespace string) (*unstructured.Unstructured, error)

type guardianInstallOptions struct {
	Namespace string
	Mode      string
	Output    string
	DryRun    bool
}

type guardianReportOptions struct {
	Namespace     string
	AllNamespaces bool
	Since         string
	Out           string
	Format        string
}

type guardianDiffOptions struct {
	Source    string
	Live      bool
	Out       string
	Namespace string
	Since     string
	Format    string
}

type guardianPROptions struct {
	From   string
	Branch string
	OutDir string
	Format string
}

func newGuardianCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guardian",
		Short: "Observe runtime drift and produce repair evidence",
		Long:  "Torque Guardian connects simulation proof, live runtime state, Kubernetes events, managed fields, secret-boundary checks, and PR-ready repair artifacts in observe-only mode.",
	}
	cmd.AddCommand(newGuardianInstallCommand(kubeconfig, kubeContext))
	cmd.AddCommand(newGuardianReportCommand(kubeconfig, kubeContext))
	cmd.AddCommand(newGuardianDiffCommand(kubeconfig, kubeContext))
	cmd.AddCommand(newGuardianPRCommand())
	decorateCommandHelp(cmd, "Guardian Commands")
	return cmd
}

func newGuardianInstallCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	opts := guardianInstallOptions{Namespace: "torque-system", Mode: "observe"}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install observe-only Guardian RBAC and config",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Namespace) == "" {
				return fmt.Errorf("--namespace cannot be empty")
			}
			if !strings.EqualFold(strings.TrimSpace(opts.Mode), "observe") {
				return fmt.Errorf("unsupported --mode %q (only observe is implemented)", opts.Mode)
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest := renderGuardianInstallManifest(opts.Namespace, opts.Mode)
			if opts.DryRun || strings.TrimSpace(opts.Output) != "" {
				return writeGuardianBytes(cmd.OutOrStdout(), opts.Output, []byte(manifest))
			}
			client, err := kube.New(cmd.Context(), derefString(kubeconfig), derefString(kubeContext))
			if err != nil {
				return err
			}
			if err := installGuardianResources(cmd.Context(), client, opts); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Guardian installed in %s (mode=%s)\n", opts.Namespace, opts.Mode)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Namespace, "namespace", opts.Namespace, "Namespace for Guardian observe config")
	cmd.Flags().StringVar(&opts.Mode, "mode", opts.Mode, "Guardian mode (observe)")
	cmd.Flags().StringVar(&opts.Output, "output", "", "Write install manifest to this path instead of applying it")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Print the install manifest without applying it")
	decorateCommandHelp(cmd, "Guardian Install Flags")
	return cmd
}

func newGuardianReportCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	opts := guardianReportOptions{Since: "24h", Format: "text"}
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Write a runtime proof from live events",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if _, err := time.ParseDuration(opts.Since); err != nil {
				return fmt.Errorf("invalid --since %q: %w", opts.Since, err)
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
			client, err := kube.New(cmd.Context(), derefString(kubeconfig), derefString(kubeContext))
			if err != nil {
				return err
			}
			if opts.Namespace == "" {
				opts.Namespace = client.Namespace
			}
			proof, err := buildGuardianRuntimeProof(cmd.Context(), client, opts)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFile(opts.Out, proof); err != nil {
					return fmt.Errorf("write runtime proof: %w", err)
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") || strings.TrimSpace(opts.Out) == "" {
				return renderGuardianRuntimeProof(cmd.OutOrStdout(), proof, opts.Format)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Runtime proof written to %s\n", opts.Out)
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "Namespace to inspect (defaults to active context)")
	cmd.Flags().BoolVar(&opts.AllNamespaces, "all-namespaces", false, "Inspect events across all namespaces")
	cmd.Flags().StringVar(&opts.Since, "since", opts.Since, "Runtime event window, for example 24h or 30m")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write runtime proof JSON to this path")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	decorateCommandHelp(cmd, "Guardian Report Flags")
	return cmd
}

func newGuardianDiffCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	opts := guardianDiffOptions{Live: true, Since: "24h", Format: "text"}
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare simulation proof with live runtime state",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Source) == "" {
				return fmt.Errorf("--source is required")
			}
			if !opts.Live {
				return fmt.Errorf("--live=false is not supported; Guardian diff compares simulation proof against live Kubernetes objects")
			}
			if _, err := time.ParseDuration(opts.Since); err != nil {
				return fmt.Errorf("invalid --since %q: %w", opts.Since, err)
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
			client, err := kube.New(cmd.Context(), derefString(kubeconfig), derefString(kubeContext))
			if err != nil {
				return err
			}
			source, err := loadGuardianSimulationSource(opts.Source)
			if err != nil {
				return err
			}
			if opts.Namespace == "" {
				opts.Namespace = firstNonEmpty(source.Namespace, client.Namespace)
			}
			events, err := guardianCollectEvents(cmd.Context(), client, opts.Namespace, false, opts.Since)
			if err != nil {
				return err
			}
			proof, err := buildGuardianDiffProof(cmd.Context(), source, guardianKubeGetter(client), events, guardianDiffBuildOptions{
				ClusterHost: client.RESTConfig.Host,
				Namespace:   opts.Namespace,
				Since:       opts.Since,
			})
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeGuardianDiffOutput(opts.Out, proof); err != nil {
					return err
				}
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") || strings.TrimSpace(opts.Out) == "" {
				raw, err := json.MarshalIndent(proof, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderGuardianDiffText(cmd.OutOrStdout(), proof, opts.Out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Source, "source", "", "Simulation proof directory from torque apply simulate")
	cmd.Flags().BoolVar(&opts.Live, "live", true, "Compare against live Kubernetes objects")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "Namespace override for namespaced resources")
	cmd.Flags().StringVar(&opts.Since, "since", opts.Since, "Runtime event window, for example 24h or 30m")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write drift proof JSON or proof directory")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	decorateCommandHelp(cmd, "Guardian Diff Flags")
	return cmd
}

func newGuardianPRCommand() *cobra.Command {
	opts := guardianPROptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Generate PR-ready artifacts from Guardian drift proof",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.From) == "" {
				return fmt.Errorf("--from is required")
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
			proof, err := loadGuardianDiffProof(opts.From)
			if err != nil {
				return err
			}
			paths, err := writeGuardianPRArtifacts(proof, opts)
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(paths, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Guardian PR artifacts written:\n  %s\n  %s\n", paths["patch"], paths["pr"])
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.From, "from", "", "Guardian drift proof JSON or runtime proof directory")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Suggested repair branch name")
	cmd.Flags().StringVar(&opts.OutDir, "out", "", "Directory for fix artifacts (default: ./fix beside the proof)")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	decorateCommandHelp(cmd, "Guardian PR Flags")
	return cmd
}

type guardianDiffBuildOptions struct {
	ClusterHost string
	Namespace   string
	Since       string
}

func buildGuardianDiffProof(ctx context.Context, source guardianSimulationSource, get guardianLiveGetter, events []guardianEventRow, opts guardianDiffBuildOptions) (guardianDiffProof, error) {
	events = guardianSanitizeEvents(events)
	proof := guardianDiffProof{
		Version:                "v1",
		Tool:                   guardianTool,
		GeneratedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		Source:                 source.Dir,
		Release:                source.Release,
		Namespace:              firstNonEmpty(opts.Namespace, source.Namespace),
		Chart:                  source.Chart,
		RenderedManifestSHA256: source.RenderedManifestSHA256,
		ClusterHost:            opts.ClusterHost,
		PredictedVsLive:        guardianPredictedVsLiveDiff{Version: "v1", Passed: true},
		ManagedFields:          guardianManagedFieldsReport{Version: "v1"},
		DriftTimeline:          guardianDriftTimeline{Version: "v1"},
		EventsTimeline:         guardianEventsTimeline{Version: "v1", Events: events},
		RuntimeSecretBoundary:  guardianRuntimeBoundaryReport{Version: "v1", Passed: true},
		RolloutAftercare:       guardianRolloutAftercareReport{Version: "v1", Passed: true},
	}
	if get == nil {
		return proof, fmt.Errorf("live getter is required")
	}
	keys := make([]string, 0, len(source.Resources))
	for key := range source.Resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		desired, err := guardianObjectFromYAML(source.Resources[key])
		if err != nil {
			proof.Summary.FetchErrors++
			proof.PredictedVsLive.Changes = append(proof.PredictedVsLive.Changes, guardianDriftItem{
				Resource: guardianResourceFromGraphID(key),
				Reason:   "parse_error: " + err.Error(),
			})
			continue
		}
		resource := guardianResourceFromObject(desired, proof.Namespace)
		live, err := get(ctx, desired, proof.Namespace)
		if err != nil {
			proof.Summary.FetchErrors++
			proof.PredictedVsLive.Changes = append(proof.PredictedVsLive.Changes, guardianDriftItem{
				Resource: resource,
				Reason:   "fetch_error: " + err.Error(),
			})
			continue
		}
		proof.Summary.Resources++
		if live == nil {
			proof.Summary.Missing++
			item := guardianDriftItem{Resource: resource, Reason: "missing"}
			proof.PredictedVsLive.Changes = append(proof.PredictedVsLive.Changes, item)
			proof.DriftTimeline.Items = append(proof.DriftTimeline.Items, guardianTimelineRow{Time: proof.GeneratedAt, Resource: resource, Reason: "missing"})
			continue
		}
		liveResource := guardianResourceFromObject(live, proof.Namespace)
		proof.ManagedFields.Owners = append(proof.ManagedFields.Owners, guardianManagedOwners(liveResource, live)...)
		boundary := guardianRuntimeBoundaryFindings(liveResource, live)
		proof.RuntimeSecretBoundary.Findings = append(proof.RuntimeSecretBoundary.Findings, boundary...)
		proof.RolloutAftercare.Items = append(proof.RolloutAftercare.Items, guardianAftercareFindings(liveResource, live)...)
		expected, actual := guardianComparableYAMLPair(desired, live)
		if strings.TrimSpace(expected) == strings.TrimSpace(actual) {
			proof.Summary.Unchanged++
			continue
		}
		diff := guardianDiffStrings(expected, actual)
		item := guardianDriftItem{Resource: liveResource, Reason: "changed", Diff: guardianRedactText(diff)}
		proof.PredictedVsLive.Changes = append(proof.PredictedVsLive.Changes, item)
		proof.DriftTimeline.Items = append(proof.DriftTimeline.Items, guardianTimelineRow{
			Time:     guardianLatestManagedTime(live),
			Resource: liveResource,
			Reason:   "changed",
			Manager:  guardianLatestManager(live),
		})
		proof.Summary.Changed++
	}
	if len(proof.PredictedVsLive.Changes) > 0 {
		proof.PredictedVsLive.Passed = false
	}
	proof.Summary.ManagedFieldOwners = len(proof.ManagedFields.Owners)
	proof.Summary.RuntimeBoundary = len(proof.RuntimeSecretBoundary.Findings)
	proof.Summary.WarningEvents = guardianCountWarningEvents(events)
	proof.Summary.AftercareIssues = len(proof.RolloutAftercare.Items)
	proof.RuntimeSecretBoundary.Passed = proof.Summary.RuntimeBoundary == 0
	proof.RolloutAftercare.Passed = proof.Summary.AftercareIssues == 0 && proof.Summary.WarningEvents == 0
	proof.DriftTimeline.Items = append(proof.DriftTimeline.Items, guardianEventTimelineRows(events)...)
	sortGuardianTimeline(proof.DriftTimeline.Items)
	proof.Blocked = !proof.PredictedVsLive.Passed || !proof.RuntimeSecretBoundary.Passed || !proof.RolloutAftercare.Passed
	proof.Status = "passed"
	if proof.Blocked {
		proof.Status = "drifted"
	}
	proof.Fix = guardianBuildFixPlan(proof, "")
	return proof, nil
}

func guardianSanitizeEvents(events []guardianEventRow) []guardianEventRow {
	if len(events) == 0 {
		return nil
	}
	out := make([]guardianEventRow, len(events))
	copy(out, events)
	for i := range out {
		out[i].Message = guardianRedactText(out[i].Message)
	}
	return out
}

func buildGuardianRuntimeProof(ctx context.Context, client *kube.Client, opts guardianReportOptions) (guardianRuntimeProof, error) {
	events, err := guardianCollectEvents(ctx, client, opts.Namespace, opts.AllNamespaces, opts.Since)
	if err != nil {
		return guardianRuntimeProof{}, err
	}
	startedAt := ""
	if d, err := time.ParseDuration(opts.Since); err == nil {
		startedAt = time.Now().UTC().Add(-d).Format(time.RFC3339Nano)
	}
	proof := guardianRuntimeProof{
		Version:       "v1",
		Tool:          guardianTool,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Namespace:     opts.Namespace,
		AllNamespaces: opts.AllNamespaces,
		Since:         opts.Since,
		StartedAt:     startedAt,
		EventsTimeline: guardianEventsTimeline{
			Version: "v1",
			Events:  events,
		},
		Summary: guardianRuntimeSummary{
			Events:   len(events),
			Warnings: guardianCountWarningEvents(events),
		},
	}
	if client != nil && client.RESTConfig != nil {
		proof.ClusterHost = client.RESTConfig.Host
	}
	return proof, nil
}

func loadGuardianSimulationSource(path string) (guardianSimulationSource, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return guardianSimulationSource{}, fmt.Errorf("simulation proof source is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return guardianSimulationSource{}, fmt.Errorf("inspect simulation proof: %w", err)
	}
	if !info.IsDir() {
		return guardianSimulationSource{}, fmt.Errorf("%s must be a torque apply simulate proof directory", path)
	}
	manifestRaw, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return guardianSimulationSource{}, fmt.Errorf("read simulation manifest: %w", err)
	}
	var manifest applySimulationManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return guardianSimulationSource{}, fmt.Errorf("parse simulation manifest: %w", err)
	}
	stateRaw, err := os.ReadFile(filepath.Join(path, "predicted-live-state.json"))
	if err != nil {
		return guardianSimulationSource{}, fmt.Errorf("read predicted live state: %w", err)
	}
	var state predictedLiveStateReport
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		return guardianSimulationSource{}, fmt.Errorf("parse predicted live state: %w", err)
	}
	return guardianSimulationSource{
		Dir:                    path,
		Release:                manifest.Release,
		Namespace:              manifest.Namespace,
		Chart:                  manifest.Chart,
		RenderedManifestSHA256: firstNonEmpty(manifest.RenderedManifestSHA256, state.RenderedManifestSHA256),
		Resources:              state.Resources,
	}, nil
}

func guardianKubeGetter(client *kube.Client) guardianLiveGetter {
	return func(ctx context.Context, desired *unstructured.Unstructured, defaultNamespace string) (*unstructured.Unstructured, error) {
		live, _, err := fetchLiveResource(ctx, client, desired, defaultNamespace)
		return live, err
	}
}

func guardianObjectFromYAML(body string) (*unstructured.Unstructured, error) {
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(body), &obj); err != nil {
		return nil, err
	}
	if len(obj) == 0 {
		return nil, fmt.Errorf("empty object")
	}
	u := &unstructured.Unstructured{Object: obj}
	if strings.TrimSpace(u.GetKind()) == "" || strings.TrimSpace(u.GetName()) == "" {
		return nil, fmt.Errorf("object missing kind or name")
	}
	return u, nil
}

func guardianResourceFromObject(obj *unstructured.Unstructured, defaultNamespace string) guardianResourceRef {
	if obj == nil {
		return guardianResourceRef{}
	}
	gvk := obj.GroupVersionKind()
	ns := strings.TrimSpace(obj.GetNamespace())
	if ns == "" {
		ns = strings.TrimSpace(defaultNamespace)
	}
	return guardianResourceRef{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      obj.GetKind(),
		Namespace: ns,
		Name:      obj.GetName(),
	}
}

func guardianResourceFromGraphID(id string) guardianResourceRef {
	parts := strings.Split(id, "|")
	if len(parts) != 3 {
		return guardianResourceRef{Name: id}
	}
	ns := parts[0]
	if ns == "cluster" {
		ns = ""
	}
	return guardianResourceRef{Namespace: ns, Kind: parts[1], Name: parts[2]}
}

func guardianComparableYAMLPair(desired, live *unstructured.Unstructured) (string, string) {
	expected := guardianComparableObject(desired)
	actual := guardianComparableObject(live)
	if expected != nil && actual != nil {
		guardianDropLiveOnlyDefaults(expected.Object, actual.Object)
	}
	return guardianMarshalComparableObject(expected), guardianMarshalComparableObject(actual)
}

func guardianComparableYAML(obj *unstructured.Unstructured) string {
	return guardianMarshalComparableObject(guardianComparableObject(obj))
}

func guardianComparableObject(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	cp := obj.DeepCopy()
	unstructured.RemoveNestedField(cp.Object, "status")
	unstructured.RemoveNestedField(cp.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(cp.Object, "metadata", "uid")
	unstructured.RemoveNestedField(cp.Object, "metadata", "generation")
	unstructured.RemoveNestedField(cp.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(cp.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(cp.Object, "metadata", "namespace")
	guardianRemoveNestedKeys(cp.Object, []string{"metadata", "annotations"},
		"kubectl.kubernetes.io/last-applied-configuration",
		"meta.helm.sh/release-name",
		"meta.helm.sh/release-namespace",
		"deployment.kubernetes.io/revision",
	)
	guardianRemoveNestedKeys(cp.Object, []string{"metadata", "labels"}, "app.kubernetes.io/managed-by")
	if strings.EqualFold(cp.GetKind(), "Secret") {
		unstructured.RemoveNestedField(cp.Object, "data")
		unstructured.RemoveNestedField(cp.Object, "stringData")
	}
	guardianRedactObject(cp.Object)
	return cp
}

func guardianMarshalComparableObject(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	raw, err := yaml.Marshal(obj.Object)
	if err != nil {
		return ""
	}
	return string(raw)
}

func guardianRemoveNestedKeys(obj map[string]any, path []string, keys ...string) {
	values, ok, _ := unstructured.NestedMap(obj, path...)
	if !ok {
		return
	}
	for _, key := range keys {
		delete(values, key)
	}
	if len(values) == 0 {
		unstructured.RemoveNestedField(obj, path...)
		return
	}
	_ = unstructured.SetNestedMap(obj, values, path...)
}

func guardianDropLiveOnlyDefaults(desired, live map[string]any) {
	kind, _ := live["kind"].(string)
	switch strings.ToLower(kind) {
	case "deployment":
		guardianRemoveLiveDefault(live, desired, int64(600), "spec", "progressDeadlineSeconds")
		guardianRemoveLiveDefault(live, desired, int64(10), "spec", "revisionHistoryLimit")
		guardianRemoveLiveDefault(live, desired, map[string]any{
			"rollingUpdate": map[string]any{"maxSurge": "25%", "maxUnavailable": "25%"},
			"type":          "RollingUpdate",
		}, "spec", "strategy")
		guardianDropLivePodSpecDefaults(desired, live, "spec", "template", "spec")
	case "pod":
		guardianDropLivePodSpecDefaults(desired, live, "spec")
	case "service":
		guardianDropLiveServiceDefaults(desired, live)
	}
}

func guardianDropLiveServiceDefaults(desired, live map[string]any) {
	guardianRemoveLiveDefault(live, desired, "ClusterIP", "spec", "type")
	guardianRemoveLiveDefault(live, desired, "None", "spec", "sessionAffinity")
	guardianRemoveLiveDefault(live, desired, "Cluster", "spec", "internalTrafficPolicy")
	guardianRemoveLiveDefault(live, desired, "SingleStack", "spec", "ipFamilyPolicy")
	for _, field := range []string{"clusterIP", "clusterIPs", "ipFamilies"} {
		if _, found, _ := unstructured.NestedFieldNoCopy(desired, "spec", field); !found {
			unstructured.RemoveNestedField(live, "spec", field)
		}
	}
	desiredPorts, desiredOK, _ := unstructured.NestedSlice(desired, "spec", "ports")
	livePorts, liveOK, _ := unstructured.NestedSlice(live, "spec", "ports")
	if !liveOK {
		return
	}
	for i, value := range livePorts {
		port, ok := value.(map[string]any)
		if !ok {
			continue
		}
		desiredHasProtocol := false
		if desiredOK && i < len(desiredPorts) {
			if desiredPort, ok := desiredPorts[i].(map[string]any); ok {
				_, desiredHasProtocol = desiredPort["protocol"]
			}
		}
		if !desiredHasProtocol && fmt.Sprint(port["protocol"]) == "TCP" {
			delete(port, "protocol")
			livePorts[i] = port
		}
	}
	_ = unstructured.SetNestedSlice(live, livePorts, "spec", "ports")
}

func guardianDropLivePodSpecDefaults(desired, live map[string]any, path ...string) {
	desiredSpec, desiredOK, _ := unstructured.NestedMap(desired, path...)
	liveSpec, liveOK, _ := unstructured.NestedMap(live, path...)
	if !liveOK {
		return
	}
	guardianRemoveLiveDefaultFromMaps(liveSpec, desiredSpec, desiredOK, "ClusterFirst", "dnsPolicy")
	guardianRemoveLiveDefaultFromMaps(liveSpec, desiredSpec, desiredOK, "Always", "restartPolicy")
	guardianRemoveLiveDefaultFromMaps(liveSpec, desiredSpec, desiredOK, "default-scheduler", "schedulerName")
	guardianRemoveLiveDefaultFromMaps(liveSpec, desiredSpec, desiredOK, int64(30), "terminationGracePeriodSeconds")
	guardianRemoveLiveEmptyMapFromMaps(liveSpec, desiredSpec, desiredOK, "securityContext")
	guardianRemoveLiveServiceAccountAlias(liveSpec, desiredSpec, desiredOK)
	guardianDropLiveContainerDefaults(liveSpec, desiredSpec, desiredOK, "containers")
	guardianDropLiveContainerDefaults(liveSpec, desiredSpec, desiredOK, "initContainers")
	_ = unstructured.SetNestedMap(live, liveSpec, path...)
}

func guardianDropLiveContainerDefaults(liveSpec, desiredSpec map[string]any, desiredOK bool, key string) {
	liveContainers, ok := liveSpec[key].([]any)
	if !ok {
		return
	}
	desiredContainers := map[string]map[string]any{}
	if desiredOK {
		if values, ok := desiredSpec[key].([]any); ok {
			for _, value := range values {
				container, ok := value.(map[string]any)
				if !ok {
					continue
				}
				name, _ := container["name"].(string)
				if name != "" {
					desiredContainers[name] = container
				}
			}
		}
	}
	for _, value := range liveContainers {
		container, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name, _ := container["name"].(string)
		desiredContainer, desiredContainerOK := desiredContainers[name]
		guardianRemoveLiveDefaultFromMaps(container, desiredContainer, desiredContainerOK, "IfNotPresent", "imagePullPolicy")
		guardianRemoveLiveDefaultFromMaps(container, desiredContainer, desiredContainerOK, "/dev/termination-log", "terminationMessagePath")
		guardianRemoveLiveDefaultFromMaps(container, desiredContainer, desiredContainerOK, "File", "terminationMessagePolicy")
		guardianRemoveLiveEmptyMapFromMaps(container, desiredContainer, desiredContainerOK, "resources")
	}
	liveSpec[key] = liveContainers
}

func guardianRemoveLiveServiceAccountAlias(liveSpec, desiredSpec map[string]any, desiredOK bool) {
	if _, found := desiredSpec["serviceAccount"]; desiredOK && found {
		return
	}
	serviceAccount, ok := liveSpec["serviceAccount"].(string)
	if !ok || serviceAccount == "" {
		return
	}
	serviceAccountName, _ := liveSpec["serviceAccountName"].(string)
	if serviceAccount == serviceAccountName {
		delete(liveSpec, "serviceAccount")
	}
}

func guardianRemoveLiveDefault(live, desired map[string]any, expected any, path ...string) {
	if _, found, _ := unstructured.NestedFieldNoCopy(desired, path...); found {
		return
	}
	value, found, _ := unstructured.NestedFieldNoCopy(live, path...)
	if found && guardianValuesEqual(value, expected) {
		unstructured.RemoveNestedField(live, path...)
	}
}

func guardianRemoveLiveDefaultFromMaps(live, desired map[string]any, desiredOK bool, expected any, key string) {
	if desiredOK {
		if _, found := desired[key]; found {
			return
		}
	}
	if value, found := live[key]; found && guardianValuesEqual(value, expected) {
		delete(live, key)
	}
}

func guardianRemoveLiveEmptyMapFromMaps(live, desired map[string]any, desiredOK bool, key string) {
	if desiredOK {
		if _, found := desired[key]; found {
			return
		}
	}
	if value, found := live[key]; found && guardianEmptyMap(value) {
		delete(live, key)
	}
}

func guardianEmptyMap(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func guardianValuesEqual(actual, expected any) bool {
	switch want := expected.(type) {
	case int64:
		got, ok := guardianInt64(actual)
		return ok && got == want
	case map[string]any:
		actualMap, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		if len(actualMap) != len(want) {
			return false
		}
		for key, expectedValue := range want {
			actualValue, ok := actualMap[key]
			if !ok || !guardianValuesEqual(actualValue, expectedValue) {
				return false
			}
		}
		return true
	default:
		return fmt.Sprint(actual) == fmt.Sprint(expected)
	}
}

func guardianInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case float64:
		if typed == float64(int64(typed)) {
			return int64(typed), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func guardianRedactObject(v any) {
	switch typed := v.(type) {
	case map[string]any:
		if name, ok := typed["name"].(string); ok && valueIsSensitiveKey(strings.ToLower(name)) {
			if _, ok := typed["value"].(string); ok {
				typed["value"] = "<redacted>"
			}
		}
		for key, value := range typed {
			if valueIsSensitiveKey(strings.ToLower(key)) {
				if _, ok := value.(string); ok {
					typed[key] = "<redacted>"
					continue
				}
			}
			if s, ok := value.(string); ok && guardianLooksSecretLike(s) {
				typed[key] = guardianRedactText(s)
				continue
			}
			guardianRedactObject(value)
		}
	case []any:
		for _, item := range typed {
			guardianRedactObject(item)
		}
	}
}

func guardianDiffStrings(expected, live string) string {
	if expected == live {
		return ""
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(expected),
		B:        difflib.SplitLines(live),
		FromFile: "predicted",
		ToFile:   "live",
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return fmt.Sprintf("failed to render diff: %v", err)
	}
	return text
}

func guardianManagedOwners(resource guardianResourceRef, obj *unstructured.Unstructured) []guardianManagedOwnerRow {
	if obj == nil {
		return nil
	}
	fields := obj.GetManagedFields()
	out := make([]guardianManagedOwnerRow, 0, len(fields))
	for _, f := range fields {
		manager := strings.TrimSpace(f.Manager)
		row := guardianManagedOwnerRow{
			Resource:   resource,
			Manager:    manager,
			Operation:  string(f.Operation),
			APIVersion: f.APIVersion,
			Suspicious: guardianSuspiciousManager(manager),
		}
		if f.Time != nil {
			row.Time = f.Time.UTC().Format(time.RFC3339Nano)
		}
		if f.FieldsV1 != nil && f.FieldsV1.Raw != nil {
			row.FieldCount = strings.Count(string(f.FieldsV1.Raw), `"f:`)
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Resource.Kind != out[j].Resource.Kind {
			return out[i].Resource.Kind < out[j].Resource.Kind
		}
		if out[i].Resource.Name != out[j].Resource.Name {
			return out[i].Resource.Name < out[j].Resource.Name
		}
		return out[i].Manager < out[j].Manager
	})
	return out
}

func guardianSuspiciousManager(manager string) bool {
	m := strings.ToLower(strings.TrimSpace(manager))
	if m == "" {
		return false
	}
	for _, allowed := range []string{"helm", "torque", "kube-controller-manager", "k3s", "deployment-controller", "replicaset-controller", "horizontal-pod-autoscaler"} {
		if strings.Contains(m, allowed) {
			return false
		}
	}
	return strings.Contains(m, "kubectl") || strings.Contains(m, "edit") || strings.Contains(m, "patch") || strings.Contains(m, "controller") || strings.Contains(m, "webhook")
}

func guardianLatestManagedTime(obj *unstructured.Unstructured) string {
	latest := time.Time{}
	for _, f := range obj.GetManagedFields() {
		if f.Time != nil && f.Time.After(latest) {
			latest = f.Time.Time
		}
	}
	if latest.IsZero() {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return latest.UTC().Format(time.RFC3339Nano)
}

func guardianLatestManager(obj *unstructured.Unstructured) string {
	latest := time.Time{}
	manager := ""
	for _, f := range obj.GetManagedFields() {
		if f.Time != nil && f.Time.After(latest) {
			latest = f.Time.Time
			manager = f.Manager
		}
	}
	return manager
}

func guardianRuntimeBoundaryFindings(resource guardianResourceRef, obj *unstructured.Unstructured) []guardianBoundaryFinding {
	if obj == nil {
		return nil
	}
	var out []guardianBoundaryFinding
	kind := strings.ToLower(obj.GetKind())
	if kind == "secret" {
		return nil
	}
	if kind == "configmap" {
		if data, ok, _ := unstructured.NestedStringMap(obj.Object, "data"); ok {
			for key, value := range data {
				if valueIsSensitiveKey(strings.ToLower(key)) || guardianLooksSecretLike(value) {
					out = append(out, guardianBoundaryFinding{
						Resource: resource,
						Surface:  "ConfigMap.data." + key,
						Boundary: "blocked",
						Severity: "high",
						Message:  "Secret-like material is present in ConfigMap data.",
					})
				}
			}
		}
	}
	metadataFindings := func(surface string, values map[string]string) {
		for key, value := range values {
			if valueIsSensitiveKey(strings.ToLower(key)) || guardianLooksSecretLike(value) {
				out = append(out, guardianBoundaryFinding{
					Resource: resource,
					Surface:  surface + "." + key,
					Boundary: "blocked",
					Severity: "high",
					Message:  "Secret-like material is present in metadata.",
				})
			}
		}
	}
	metadataFindings("metadata.labels", obj.GetLabels())
	metadataFindings("metadata.annotations", obj.GetAnnotations())
	out = append(out, guardianEnvBoundaryFindings(resource, obj)...)
	return out
}

func guardianEnvBoundaryFindings(resource guardianResourceRef, obj *unstructured.Unstructured) []guardianBoundaryFinding {
	specs := guardianPodSpecs(obj)
	var out []guardianBoundaryFinding
	for _, spec := range specs {
		containers, _ := spec["containers"].([]any)
		containers = append(containers, guardianSlice(spec["initContainers"])...)
		for _, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			envs, _ := cm["env"].([]any)
			for _, env := range envs {
				em, ok := env.(map[string]any)
				if !ok {
					continue
				}
				name, _ := em["name"].(string)
				value, _ := em["value"].(string)
				if strings.TrimSpace(value) == "" {
					continue
				}
				if valueIsSensitiveKey(strings.ToLower(name)) || guardianLooksSecretLike(value) {
					out = append(out, guardianBoundaryFinding{
						Resource: resource,
						Surface:  "env." + name,
						Boundary: "blocked",
						Severity: "critical",
						Message:  "Secret-like material is present in env.value instead of a Secret reference.",
					})
				}
			}
		}
	}
	return out
}

func guardianPodSpecs(obj *unstructured.Unstructured) []map[string]any {
	if obj == nil {
		return nil
	}
	if strings.EqualFold(obj.GetKind(), "Pod") {
		if spec, ok, _ := unstructured.NestedMap(obj.Object, "spec"); ok {
			return []map[string]any{spec}
		}
		return nil
	}
	if spec, ok, _ := unstructured.NestedMap(obj.Object, "spec", "template", "spec"); ok {
		return []map[string]any{spec}
	}
	return nil
}

func guardianSlice(v any) []any {
	if values, ok := v.([]any); ok {
		return values
	}
	return nil
}

func guardianAftercareFindings(resource guardianResourceRef, obj *unstructured.Unstructured) []guardianAftercareFinding {
	if obj == nil {
		return nil
	}
	switch strings.ToLower(obj.GetKind()) {
	case "deployment":
		var out []guardianAftercareFinding
		replicas := guardianNestedInt64(obj.Object, "status", "replicas")
		available := guardianNestedInt64(obj.Object, "status", "availableReplicas")
		unavailable := guardianNestedInt64(obj.Object, "status", "unavailableReplicas")
		if replicas > 0 && available < replicas {
			out = append(out, guardianAftercareFinding{Resource: resource, Severity: "high", Reason: "deployment_not_fully_available", Message: fmt.Sprintf("%d/%d replicas available", available, replicas)})
		}
		if unavailable > 0 {
			out = append(out, guardianAftercareFinding{Resource: resource, Severity: "high", Reason: "deployment_unavailable_replicas", Message: fmt.Sprintf("%d unavailable replica(s)", unavailable)})
		}
		return out
	default:
		return nil
	}
}

func guardianNestedInt64(obj map[string]any, fields ...string) int64 {
	value, found, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	if !found {
		return 0
	}
	if n, ok := guardianInt64(value); ok {
		return n
	}
	return 0
}

func guardianCollectEvents(ctx context.Context, client *kube.Client, namespace string, allNamespaces bool, since string) ([]guardianEventRow, error) {
	if client == nil || client.Clientset == nil {
		return nil, nil
	}
	d, err := time.ParseDuration(since)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-d)
	ns := strings.TrimSpace(namespace)
	if allNamespaces {
		ns = ""
	}
	list, err := client.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []guardianEventRow
	for _, ev := range list.Items {
		ts := guardianEventTime(ev)
		if !ts.IsZero() && ts.Before(cutoff) {
			continue
		}
		ref := guardianResourceRef{
			Kind:      ev.InvolvedObject.Kind,
			Namespace: firstNonEmpty(ev.InvolvedObject.Namespace, ev.Namespace),
			Name:      ev.InvolvedObject.Name,
		}
		out = append(out, guardianEventRow{
			Time:     ts.UTC().Format(time.RFC3339Nano),
			Type:     ev.Type,
			Reason:   ev.Reason,
			Message:  guardianRedactText(ev.Message),
			Resource: ref,
			Count:    ev.Count,
			Source:   ev.Source.Component,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out, nil
}

func guardianEventTime(ev corev1.Event) time.Time {
	if !ev.EventTime.IsZero() {
		return ev.EventTime.Time
	}
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.FirstTimestamp.IsZero() {
		return ev.FirstTimestamp.Time
	}
	return ev.CreationTimestamp.Time
}

func guardianEventTimelineRows(events []guardianEventRow) []guardianTimelineRow {
	var rows []guardianTimelineRow
	for _, ev := range events {
		if !strings.EqualFold(ev.Type, corev1.EventTypeWarning) {
			continue
		}
		rows = append(rows, guardianTimelineRow{
			Time:      ev.Time,
			Resource:  ev.Resource,
			Reason:    ev.Reason,
			EventType: ev.Type,
			Message:   ev.Message,
		})
	}
	return rows
}

func guardianCountWarningEvents(events []guardianEventRow) int {
	n := 0
	for _, ev := range events {
		if strings.EqualFold(ev.Type, corev1.EventTypeWarning) {
			n++
		}
	}
	return n
}

func sortGuardianTimeline(rows []guardianTimelineRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Time < rows[j].Time
	})
}

func guardianBuildFixPlan(proof guardianDiffProof, branch string) guardianFixPlan {
	cmd := fmt.Sprintf("torque guardian diff --source %s --live --out drift-proof.json", shellQuoteToken(proof.Source))
	if proof.Source == "" {
		cmd = "torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json"
	}
	return guardianFixPlan{
		Branch: branch,
		Commands: []string{
			"torque apply simulate --chart " + shellQuoteToken(firstNonEmpty(proof.Chart, "./chart")) + " --release " + shellQuoteToken(proof.Release) + " -n " + shellQuoteToken(firstNonEmpty(proof.Namespace, "default")) + " --out ./torque-sim-proof",
			cmd,
			"torque guardian pr --from drift-proof.json",
		},
		Recommendation: "Review the drift proof, decide whether source or live state is authoritative, then reconcile through Torque rather than kubectl edit.",
	}
}

func renderGuardianInstallManifest(namespace, mode string) string {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = "torque-system"
	}
	mode = firstNonEmpty(strings.TrimSpace(mode), "observe")
	return strings.TrimSpace(fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: %[2]s
  namespace: %[1]s
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: %[2]s
  namespace: %[1]s
data:
  mode: %[3]s
  mutation: disabled
  purpose: observe runtime drift, events, managed fields, and repair evidence
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: %[2]s
rules:
  - apiGroups: ["", "apps", "batch", "networking.k8s.io", "autoscaling", "policy"]
    resources: ["pods", "services", "configmaps", "secrets", "events", "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "cronjobs", "ingresses", "horizontalpodautoscalers", "poddisruptionbudgets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["admissionregistration.k8s.io"]
    resources: ["mutatingwebhookconfigurations", "validatingwebhookconfigurations"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %[2]s
subjects:
  - kind: ServiceAccount
    name: %[2]s
    namespace: %[1]s
roleRef:
  kind: ClusterRole
  name: %[2]s
  apiGroup: rbac.authorization.k8s.io
`, ns, guardianInstallName, mode)) + "\n"
}

func installGuardianResources(ctx context.Context, client *kube.Client, opts guardianInstallOptions) error {
	ns := strings.TrimSpace(opts.Namespace)
	if ns == "" {
		ns = "torque-system"
	}
	if _, err := client.Clientset.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err := client.Clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create namespace: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get namespace: %w", err)
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: guardianInstallName, Namespace: ns}}
	if _, err := client.Clientset.CoreV1().ServiceAccounts(ns).Create(ctx, sa, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create serviceaccount: %w", err)
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: guardianInstallName, Namespace: ns}, Data: map[string]string{"mode": "observe", "mutation": "disabled", "purpose": "observe runtime drift, events, managed fields, and repair evidence"}}
	if existing, err := client.Clientset.CoreV1().ConfigMaps(ns).Get(ctx, guardianInstallName, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err := client.Clientset.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create configmap: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get configmap: %w", err)
	} else {
		cm.ResourceVersion = existing.ResourceVersion
		if _, err := client.Clientset.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update configmap: %w", err)
		}
	}
	role := guardianClusterRole()
	if existing, err := client.Clientset.RbacV1().ClusterRoles().Get(ctx, guardianInstallName, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err := client.Clientset.RbacV1().ClusterRoles().Create(ctx, role, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create clusterrole: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get clusterrole: %w", err)
	} else {
		role.ResourceVersion = existing.ResourceVersion
		if _, err := client.Clientset.RbacV1().ClusterRoles().Update(ctx, role, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update clusterrole: %w", err)
		}
	}
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: guardianInstallName},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: guardianInstallName, Namespace: ns}},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: guardianInstallName, APIGroup: rbacv1.GroupName},
	}
	if existing, err := client.Clientset.RbacV1().ClusterRoleBindings().Get(ctx, guardianInstallName, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err := client.Clientset.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create clusterrolebinding: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get clusterrolebinding: %w", err)
	} else {
		binding.ResourceVersion = existing.ResourceVersion
		if _, err := client.Clientset.RbacV1().ClusterRoleBindings().Update(ctx, binding, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update clusterrolebinding: %w", err)
		}
	}
	return nil
}

func guardianClusterRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: guardianInstallName},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"", "apps", "batch", "networking.k8s.io", "autoscaling", "policy"},
				Resources: []string{"pods", "services", "configmaps", "secrets", "events", "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "cronjobs", "ingresses", "horizontalpodautoscalers", "poddisruptionbudgets"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"roles", "rolebindings", "clusterroles", "clusterrolebindings"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"admissionregistration.k8s.io"},
				Resources: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"apiextensions.k8s.io"},
				Resources: []string{"customresourcedefinitions"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

func writeGuardianDiffOutput(path string, proof guardianDiffProof) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return writeJSONFile(path, proof)
	}
	if err := os.MkdirAll(filepath.Join(path, "fix"), 0o755); err != nil {
		return err
	}
	files := map[string]any{
		"manifest.json":                guardianRuntimeBundleManifest(proof),
		"predicted-vs-live.diff.json":  proof.PredictedVsLive,
		"managed-fields.owners.json":   proof.ManagedFields,
		"drift.timeline.json":          proof.DriftTimeline,
		"events.timeline.json":         proof.EventsTimeline,
		"runtime.secret.boundary.json": proof.RuntimeSecretBoundary,
		"rollout.aftercare.json":       proof.RolloutAftercare,
	}
	for name, value := range files {
		if err := writeJSONFile(filepath.Join(path, name), value); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	_, err := writeGuardianPRArtifacts(proof, guardianPROptions{From: path, OutDir: filepath.Join(path, "fix")})
	return err
}

func guardianRuntimeBundleManifest(proof guardianDiffProof) map[string]any {
	return map[string]any{
		"version":     "v1",
		"tool":        guardianTool,
		"generatedAt": proof.GeneratedAt,
		"source":      proof.Source,
		"release":     proof.Release,
		"namespace":   proof.Namespace,
		"status":      proof.Status,
		"blocked":     proof.Blocked,
		"summary":     proof.Summary,
		"files": map[string]string{
			"predictedVsLive":       "predicted-vs-live.diff.json",
			"managedFieldsOwners":   "managed-fields.owners.json",
			"driftTimeline":         "drift.timeline.json",
			"eventsTimeline":        "events.timeline.json",
			"runtimeSecretBoundary": "runtime.secret.boundary.json",
			"rolloutAftercare":      "rollout.aftercare.json",
			"fixPatch":              "fix/drift-fix.patch",
			"fixPR":                 "fix/pr.md",
		},
	}
}

type guardianDiffBundleManifest struct {
	Version     string              `json:"version"`
	Tool        string              `json:"tool"`
	GeneratedAt string              `json:"generatedAt"`
	Source      string              `json:"source"`
	Release     string              `json:"release"`
	Namespace   string              `json:"namespace"`
	Status      string              `json:"status"`
	Blocked     bool                `json:"blocked"`
	Summary     guardianDiffSummary `json:"summary"`
}

func loadGuardianDiffProof(path string) (guardianDiffProof, error) {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return guardianDiffProof{}, fmt.Errorf("inspect guardian proof: %w", err)
	}
	if info.IsDir() {
		var p guardianDiffProof
		p.Version = "v1"
		p.Tool = guardianTool
		p.Source = path
		p.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
		p.ManagedFields = guardianManagedFieldsReport{Version: "v1"}
		p.DriftTimeline = guardianDriftTimeline{Version: "v1"}
		p.EventsTimeline = guardianEventsTimeline{Version: "v1"}
		p.RuntimeSecretBoundary = guardianRuntimeBoundaryReport{Version: "v1", Passed: true}
		p.RolloutAftercare = guardianRolloutAftercareReport{Version: "v1", Passed: true}
		if raw, err := os.ReadFile(filepath.Join(path, "manifest.json")); err == nil {
			var manifest guardianDiffBundleManifest
			if err := json.Unmarshal(raw, &manifest); err != nil {
				return guardianDiffProof{}, fmt.Errorf("parse guardian bundle manifest: %w", err)
			}
			p.Version = firstNonEmpty(manifest.Version, p.Version)
			p.Tool = firstNonEmpty(manifest.Tool, p.Tool)
			p.GeneratedAt = firstNonEmpty(manifest.GeneratedAt, p.GeneratedAt)
			p.Source = firstNonEmpty(manifest.Source, p.Source)
			p.Release = manifest.Release
			p.Namespace = manifest.Namespace
			p.Status = manifest.Status
			p.Blocked = manifest.Blocked
			p.Summary = manifest.Summary
		} else if !os.IsNotExist(err) {
			return guardianDiffProof{}, fmt.Errorf("read guardian bundle manifest: %w", err)
		}
		diffRaw, err := os.ReadFile(filepath.Join(path, "predicted-vs-live.diff.json"))
		if err != nil {
			return guardianDiffProof{}, fmt.Errorf("read predicted-vs-live diff: %w", err)
		}
		if err := json.Unmarshal(diffRaw, &p.PredictedVsLive); err != nil {
			return guardianDiffProof{}, fmt.Errorf("parse predicted-vs-live diff: %w", err)
		}
		if err := readGuardianBundleJSON(path, "managed-fields.owners.json", &p.ManagedFields); err != nil {
			return p, err
		}
		if err := readGuardianBundleJSON(path, "drift.timeline.json", &p.DriftTimeline); err != nil {
			return p, err
		}
		if err := readGuardianBundleJSON(path, "events.timeline.json", &p.EventsTimeline); err != nil {
			return p, err
		}
		if err := readGuardianBundleJSON(path, "runtime.secret.boundary.json", &p.RuntimeSecretBoundary); err != nil {
			return p, err
		}
		if err := readGuardianBundleJSON(path, "rollout.aftercare.json", &p.RolloutAftercare); err != nil {
			return p, err
		}
		if p.Summary.Changed == 0 && p.Summary.Missing == 0 && p.Summary.FetchErrors == 0 {
			for _, item := range p.PredictedVsLive.Changes {
				switch {
				case item.Reason == "changed":
					p.Summary.Changed++
				case item.Reason == "missing":
					p.Summary.Missing++
				case strings.HasPrefix(item.Reason, "fetch_error"):
					p.Summary.FetchErrors++
				}
			}
		}
		if p.Summary.RuntimeBoundary == 0 {
			p.Summary.RuntimeBoundary = len(p.RuntimeSecretBoundary.Findings)
		}
		if p.Summary.WarningEvents == 0 {
			p.Summary.WarningEvents = guardianCountWarningEvents(p.EventsTimeline.Events)
		}
		if p.Summary.AftercareIssues == 0 {
			p.Summary.AftercareIssues = len(p.RolloutAftercare.Items)
		}
		if p.Status == "" {
			p.Blocked = !p.PredictedVsLive.Passed || !p.RuntimeSecretBoundary.Passed || !p.RolloutAftercare.Passed
			p.Status = "passed"
			if p.Blocked {
				p.Status = "drifted"
			}
		}
		return p, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return guardianDiffProof{}, fmt.Errorf("read guardian proof: %w", err)
	}
	var proof guardianDiffProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return guardianDiffProof{}, fmt.Errorf("parse guardian proof: %w", err)
	}
	if strings.TrimSpace(proof.Tool) == "" {
		return guardianDiffProof{}, fmt.Errorf("%s does not look like a Guardian drift proof", path)
	}
	return proof, nil
}

func readGuardianBundleJSON(dir, name string, dest any) error {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	return nil
}

func writeGuardianPRArtifacts(proof guardianDiffProof, opts guardianPROptions) (map[string]string, error) {
	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		base := strings.TrimSpace(opts.From)
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
	proof.Fix = guardianBuildFixPlan(proof, opts.Branch)
	patchPath := filepath.Join(outDir, "drift-fix.patch")
	prPath := filepath.Join(outDir, "pr.md")
	if err := os.WriteFile(patchPath, []byte(renderGuardianDriftPatch(proof)), 0o644); err != nil {
		return nil, fmt.Errorf("write drift patch: %w", err)
	}
	if err := os.WriteFile(prPath, []byte(renderGuardianPRMarkdown(proof)+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write PR markdown: %w", err)
	}
	return map[string]string{"patch": patchPath, "pr": prPath}, nil
}

func renderGuardianDriftPatch(proof guardianDiffProof) string {
	filename := ".torque/guardian/" + sanitizeFilename(firstNonEmpty(proof.Release, "runtime-drift")) + "-drift.md"
	body := renderGuardianPRMarkdown(proof)
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

func renderGuardianPRMarkdown(proof guardianDiffProof) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Torque Guardian Drift Repair\n\n")
	fmt.Fprintf(&b, "- Release: `%s`\n", firstNonEmpty(proof.Release, "-"))
	fmt.Fprintf(&b, "- Namespace: `%s`\n", firstNonEmpty(proof.Namespace, "-"))
	fmt.Fprintf(&b, "- Status: `%s`\n", firstNonEmpty(proof.Status, "-"))
	fmt.Fprintf(&b, "- Changed: `%d`\n", proof.Summary.Changed)
	fmt.Fprintf(&b, "- Missing: `%d`\n", proof.Summary.Missing)
	fmt.Fprintf(&b, "- Runtime boundary findings: `%d`\n", proof.Summary.RuntimeBoundary)
	fmt.Fprintf(&b, "- Warning events: `%d`\n", proof.Summary.WarningEvents)
	if proof.Fix.Branch != "" {
		fmt.Fprintf(&b, "- Branch: `%s`\n", proof.Fix.Branch)
	}
	if len(proof.PredictedVsLive.Changes) > 0 {
		fmt.Fprintf(&b, "\n## Drift\n\n")
		for _, item := range proof.PredictedVsLive.Changes {
			fmt.Fprintf(&b, "- `%s` `%s/%s` in `%s`: %s\n", item.Resource.Kind, firstNonEmpty(item.Resource.Namespace, "cluster"), item.Resource.Name, firstNonEmpty(item.Resource.Group, "core"), item.Reason)
		}
	}
	if len(proof.RuntimeSecretBoundary.Findings) > 0 {
		fmt.Fprintf(&b, "\n## Runtime Secret Boundary\n\n")
		for _, f := range proof.RuntimeSecretBoundary.Findings {
			fmt.Fprintf(&b, "- `%s` %s: %s\n", f.Severity, f.Surface, f.Message)
		}
	}
	if len(proof.Fix.Commands) > 0 {
		fmt.Fprintf(&b, "\n## Validation\n\n```bash\n")
		for _, cmd := range proof.Fix.Commands {
			fmt.Fprintf(&b, "%s\n", cmd)
		}
		fmt.Fprintf(&b, "```\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGuardianRuntimeProof(out io.Writer, proof guardianRuntimeProof, format string) error {
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		raw, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s\n", raw)
		return nil
	}
	fmt.Fprintf(out, "Guardian runtime proof: %d event(s), %d warning(s)\n", proof.Summary.Events, proof.Summary.Warnings)
	if proof.Namespace != "" {
		fmt.Fprintf(out, "Namespace: %s\n", proof.Namespace)
	}
	if proof.Since != "" {
		fmt.Fprintf(out, "Since: %s\n", proof.Since)
	}
	return nil
}

func renderGuardianDiffText(out io.Writer, proof guardianDiffProof, path string) {
	fmt.Fprintf(out, "Guardian diff: %s\n", strings.ToUpper(firstNonEmpty(proof.Status, "unknown")))
	if strings.TrimSpace(path) != "" {
		fmt.Fprintf(out, "Proof written to %s\n", path)
	}
	fmt.Fprintf(out, "Drift: %d changed, %d missing, %d fetch errors\n", proof.Summary.Changed, proof.Summary.Missing, proof.Summary.FetchErrors)
	fmt.Fprintf(out, "Runtime: %d boundary finding(s), %d warning event(s), %d aftercare issue(s)\n", proof.Summary.RuntimeBoundary, proof.Summary.WarningEvents, proof.Summary.AftercareIssues)
}

func writeGuardianBytes(out io.Writer, path string, data []byte) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		_, err := out.Write(data)
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

func guardianLooksSecretLike(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return false
	}
	for _, re := range guardianSecretPatterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

func guardianRedactText(text string) string {
	out := text
	for _, re := range guardianSecretPatterns {
		out = re.ReplaceAllStringFunc(out, func(match string) string {
			if len(match) <= 8 {
				return "<redacted>"
			}
			return match[:minInt(4, len(match))] + "...<redacted>"
		})
	}
	return out
}

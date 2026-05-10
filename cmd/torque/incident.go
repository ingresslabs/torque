package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
	"github.com/ingresslabs/torque/internal/kube"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	incidentTool = "torque-incident"
)

type incidentCaptureOptions struct {
	Release   string
	Namespace string
	Since     string
	Out       string
	Format    string
	LogTail   int64
}

type incidentReplayOptions struct {
	Lab           string
	Out           string
	Format        string
	FailOnBlocked bool
}

type incidentExplainOptions struct {
	From   string
	Out    string
	Format string
}

type incidentPROptions struct {
	From   string
	Branch string
	OutDir string
	Format string
}

type incidentBundle struct {
	Version               string                         `json:"version"`
	Tool                  string                         `json:"tool"`
	GeneratedAt           string                         `json:"generatedAt"`
	ClusterHost           string                         `json:"clusterHost,omitempty"`
	Release               string                         `json:"release"`
	Namespace             string                         `json:"namespace"`
	Since                 string                         `json:"since"`
	StartedAt             string                         `json:"startedAt,omitempty"`
	ObserveOnly           bool                           `json:"observeOnly"`
	Summary               incidentSummary                `json:"summary"`
	Resources             []incidentResourceSnapshot     `json:"resources,omitempty"`
	EventsTimeline        guardianEventsTimeline         `json:"eventsTimeline"`
	Logs                  []incidentLogSet               `json:"logs,omitempty"`
	ManagedFields         guardianManagedFieldsReport    `json:"managedFields"`
	RuntimeSecretBoundary guardianRuntimeBoundaryReport  `json:"runtimeSecretBoundary"`
	RolloutAftercare      guardianRolloutAftercareReport `json:"rolloutAftercare"`
	CausalTimeline        incidentCausalTimeline         `json:"causalTimeline"`
	RootCause             incidentRootCause              `json:"rootCause"`
}

type incidentSummary struct {
	Resources        int `json:"resources"`
	Unhealthy        int `json:"unhealthy"`
	Events           int `json:"events"`
	WarningEvents    int `json:"warningEvents"`
	LogStreams       int `json:"logStreams"`
	LogErrors        int `json:"logErrors"`
	ManagedOwners    int `json:"managedOwners"`
	BoundaryFindings int `json:"boundaryFindings"`
	AftercareIssues  int `json:"aftercareIssues"`
}

type incidentResourceSnapshot struct {
	Resource guardianResourceRef     `json:"resource"`
	Status   deploy.ResourceStatus   `json:"status,omitempty"`
	Object   map[string]any          `json:"object,omitempty"`
	Signals  []incidentSignalSummary `json:"signals,omitempty"`
}

type incidentSignalSummary struct {
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
}

type incidentLogSet struct {
	Pod       string   `json:"pod"`
	Namespace string   `json:"namespace"`
	Container string   `json:"container"`
	Previous  bool     `json:"previous,omitempty"`
	Lines     []string `json:"lines,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type incidentCausalTimeline struct {
	Version string                `json:"version"`
	Items   []incidentTimelineRow `json:"items,omitempty"`
}

type incidentTimelineRow struct {
	Time     string              `json:"time,omitempty"`
	Source   string              `json:"source"`
	Severity string              `json:"severity"`
	Resource guardianResourceRef `json:"resource"`
	Reason   string              `json:"reason"`
	Message  string              `json:"message"`
	Evidence string              `json:"evidence,omitempty"`
}

type incidentRootCause struct {
	Version         string             `json:"version"`
	Status          string             `json:"status"`
	Blocked         bool               `json:"blocked"`
	PrimaryCause    string             `json:"primaryCause,omitempty"`
	Confidence      string             `json:"confidence,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	Evidence        []incidentEvidence `json:"evidence,omitempty"`
	Recommendations []string           `json:"recommendations,omitempty"`
	Release         string             `json:"release,omitempty"`
	Namespace       string             `json:"namespace,omitempty"`
	GeneratedAt     string             `json:"generatedAt,omitempty"`
}

type incidentEvidence struct {
	Source   string              `json:"source"`
	Severity string              `json:"severity"`
	Resource guardianResourceRef `json:"resource"`
	Reason   string              `json:"reason"`
	Message  string              `json:"message"`
}

type incidentReplayProof struct {
	Version        string                 `json:"version"`
	Tool           string                 `json:"tool"`
	GeneratedAt    string                 `json:"generatedAt"`
	Lab            string                 `json:"lab"`
	Source         string                 `json:"source"`
	Release        string                 `json:"release"`
	Namespace      string                 `json:"namespace"`
	ObserveOnly    bool                   `json:"observeOnly"`
	Passed         bool                   `json:"passed"`
	Blocked        bool                   `json:"blocked"`
	FilesWritten   []string               `json:"filesWritten,omitempty"`
	Summary        incidentSummary        `json:"summary"`
	RootCause      incidentRootCause      `json:"rootCause"`
	CausalTimeline incidentCausalTimeline `json:"causalTimeline"`
}

type incidentReplayManifest struct {
	Version     string            `json:"version"`
	Tool        string            `json:"tool"`
	GeneratedAt string            `json:"generatedAt"`
	Lab         string            `json:"lab"`
	Source      string            `json:"source"`
	Release     string            `json:"release"`
	Namespace   string            `json:"namespace"`
	ObserveOnly bool              `json:"observeOnly"`
	Passed      bool              `json:"passed"`
	Blocked     bool              `json:"blocked"`
	Files       map[string]string `json:"files"`
}

func newIncidentCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "incident",
		Short: "Capture and replay observe-only incident evidence",
		Long:  "Torque Incident captures release runtime evidence, replays it as a portable lab proof, explains likely root cause, and writes PR-ready artifacts without mutating the cluster.",
	}
	cmd.AddCommand(newIncidentCaptureCommand(kubeconfig, kubeContext))
	cmd.AddCommand(newIncidentReplayCommand())
	cmd.AddCommand(newIncidentExplainCommand())
	cmd.AddCommand(newIncidentPRCommand())
	decorateCommandHelp(cmd, "Incident Commands")
	return cmd
}

func newIncidentCaptureCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	opts := incidentCaptureOptions{Since: "1h", Format: "text", LogTail: 80}
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture observe-only incident evidence for a release",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Release) == "" {
				return fmt.Errorf("--release is required")
			}
			if _, err := time.ParseDuration(opts.Since); err != nil {
				return fmt.Errorf("invalid --since %q: %w", opts.Since, err)
			}
			if opts.LogTail < 0 {
				return fmt.Errorf("--log-tail must be >= 0")
			}
			return validateIncidentFormat(opts.Format)
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
			if opts.Namespace == "" {
				opts.Namespace = metav1.NamespaceDefault
			}
			bundle, err := buildIncidentCapture(cmd.Context(), client, opts)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFile(opts.Out, bundle); err != nil {
					return fmt.Errorf("write incident bundle: %w", err)
				}
			}
			if strings.EqualFold(opts.Format, "json") || strings.TrimSpace(opts.Out) == "" {
				return renderIncidentJSON(cmd.OutOrStdout(), bundle)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Incident capture written to %s\n", opts.Out)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Release, "release", "", "Helm release name to capture")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "Release namespace (defaults to active context)")
	cmd.Flags().StringVar(&opts.Since, "since", opts.Since, "Incident evidence window, for example 1h or 30m")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write incident bundle JSON to this path")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	cmd.Flags().Int64Var(&opts.LogTail, "log-tail", opts.LogTail, "Log lines per container to include (0 disables logs)")
	decorateCommandHelp(cmd, "Incident Capture Flags")
	return cmd
}

func newIncidentReplayCommand() *cobra.Command {
	opts := incidentReplayOptions{Lab: "k3s", Format: "text"}
	cmd := &cobra.Command{
		Use:   "replay <incident-bundle>",
		Short: "Replay an incident bundle as a lab proof",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Lab) == "" {
				return fmt.Errorf("--lab cannot be empty")
			}
			return validateIncidentFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			proof, err := replayIncidentBundle(args[0], opts)
			if err != nil {
				return err
			}
			if strings.EqualFold(opts.Format, "json") || strings.TrimSpace(opts.Out) == "" {
				if err := renderIncidentJSON(cmd.OutOrStdout(), proof); err != nil {
					return err
				}
			} else {
				renderIncidentReplayText(cmd.OutOrStdout(), proof, opts.Out)
			}
			if opts.FailOnBlocked && proof.Blocked {
				return fmt.Errorf("incident replay is blocked")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Lab, "lab", opts.Lab, "Replay lab profile label")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write incident replay proof directory")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	cmd.Flags().BoolVar(&opts.FailOnBlocked, "fail-on-blocked", false, "Exit non-zero when replayed incident remains blocked")
	decorateCommandHelp(cmd, "Incident Replay Flags")
	return cmd
}

func newIncidentExplainCommand() *cobra.Command {
	opts := incidentExplainOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain root cause from incident proof",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.From) == "" {
				return fmt.Errorf("--from is required")
			}
			return validateIncidentFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := loadIncidentRootCause(opts.From)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.Out) != "" {
				if err := writeJSONFile(opts.Out, root); err != nil {
					return fmt.Errorf("write root cause: %w", err)
				}
			}
			if strings.EqualFold(opts.Format, "json") || strings.TrimSpace(opts.Out) == "" {
				return renderIncidentJSON(cmd.OutOrStdout(), root)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Incident root cause written to %s\n", opts.Out)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.From, "from", "", "Incident bundle or replay proof directory")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write root cause JSON to this path")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	decorateCommandHelp(cmd, "Incident Explain Flags")
	return cmd
}

func newIncidentPRCommand() *cobra.Command {
	opts := incidentPROptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Generate PR-ready artifacts from incident root cause",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.From) == "" {
				return fmt.Errorf("--from is required")
			}
			return validateIncidentFormat(opts.Format)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := loadIncidentRootCause(opts.From)
			if err != nil {
				return err
			}
			paths, err := writeIncidentPRArtifacts(root, opts)
			if err != nil {
				return err
			}
			if strings.EqualFold(opts.Format, "json") {
				return renderIncidentJSON(cmd.OutOrStdout(), paths)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Incident PR artifacts written:\n  %s\n  %s\n", paths["patch"], paths["pr"])
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.From, "from", "", "Root cause JSON, incident bundle, or replay proof directory")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Suggested repair branch name")
	cmd.Flags().StringVar(&opts.OutDir, "out", "", "Directory for fix artifacts (default: ./fix beside --from)")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	decorateCommandHelp(cmd, "Incident PR Flags")
	return cmd
}

func buildIncidentCapture(ctx context.Context, client *kube.Client, opts incidentCaptureOptions) (incidentBundle, error) {
	if client == nil {
		return incidentBundle{}, fmt.Errorf("kubernetes client is required")
	}
	since, _ := time.ParseDuration(opts.Since)
	startedAt := time.Now().UTC().Add(-since).Format(time.RFC3339Nano)
	resources, err := incidentCollectReleaseResources(ctx, client, opts.Namespace, opts.Release)
	if err != nil {
		return incidentBundle{}, err
	}
	statuses := incidentStatusByResource(deploy.NewResourceTracker(client, opts.Namespace, opts.Release, "", nil).Snapshot(ctx))
	events, err := guardianCollectEvents(ctx, client, opts.Namespace, false, opts.Since)
	if err != nil {
		return incidentBundle{}, err
	}
	events = incidentFilterEvents(events, opts.Release, resources)
	logs, err := incidentCollectLogs(ctx, client, opts.Namespace, opts.Release, opts.LogTail)
	if err != nil {
		return incidentBundle{}, err
	}
	in := incidentBuildInput{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ClusterHost: client.RESTConfig.Host,
		Release:     opts.Release,
		Namespace:   opts.Namespace,
		Since:       opts.Since,
		StartedAt:   startedAt,
		Resources:   resources,
		Statuses:    statuses,
		Events:      events,
		Logs:        logs,
	}
	return buildIncidentBundle(in), nil
}

type incidentBuildInput struct {
	GeneratedAt string
	ClusterHost string
	Release     string
	Namespace   string
	Since       string
	StartedAt   string
	Resources   []*unstructured.Unstructured
	Statuses    map[string]deploy.ResourceStatus
	Events      []guardianEventRow
	Logs        []incidentLogSet
}

func buildIncidentBundle(in incidentBuildInput) incidentBundle {
	events := guardianSanitizeEvents(in.Events)
	bundle := incidentBundle{
		Version:     "v1",
		Tool:        incidentTool,
		GeneratedAt: firstNonEmpty(in.GeneratedAt, time.Now().UTC().Format(time.RFC3339Nano)),
		ClusterHost: in.ClusterHost,
		Release:     in.Release,
		Namespace:   in.Namespace,
		Since:       in.Since,
		StartedAt:   in.StartedAt,
		ObserveOnly: true,
		EventsTimeline: guardianEventsTimeline{
			Version: "v1",
			Events:  events,
		},
		ManagedFields:         guardianManagedFieldsReport{Version: "v1"},
		RuntimeSecretBoundary: guardianRuntimeBoundaryReport{Version: "v1", Passed: true},
		RolloutAftercare:      guardianRolloutAftercareReport{Version: "v1", Passed: true},
		CausalTimeline:        incidentCausalTimeline{Version: "v1"},
		Logs:                  incidentRedactLogs(in.Logs),
	}
	for _, obj := range in.Resources {
		if obj == nil {
			continue
		}
		ref := guardianResourceFromObject(obj, in.Namespace)
		status := incidentStatusForResource(ref, in.Statuses)
		snapshot := incidentResourceSnapshot{
			Resource: ref,
			Status:   status,
			Object:   incidentRedactedObject(obj),
		}
		if incidentUnhealthyStatus(status) {
			snapshot.Signals = append(snapshot.Signals, incidentSignalSummary{Severity: "high", Reason: status.Reason, Message: status.Message})
		}
		bundle.Resources = append(bundle.Resources, snapshot)
		bundle.ManagedFields.Owners = append(bundle.ManagedFields.Owners, guardianManagedOwners(ref, obj)...)
		bundle.RuntimeSecretBoundary.Findings = append(bundle.RuntimeSecretBoundary.Findings, guardianRuntimeBoundaryFindings(ref, obj)...)
		bundle.RolloutAftercare.Items = append(bundle.RolloutAftercare.Items, guardianAftercareFindings(ref, obj)...)
	}
	sort.Slice(bundle.Resources, func(i, j int) bool {
		return incidentResourceKey(bundle.Resources[i].Resource) < incidentResourceKey(bundle.Resources[j].Resource)
	})
	bundle.Summary.Resources = len(bundle.Resources)
	for _, resource := range bundle.Resources {
		if incidentUnhealthyStatus(resource.Status) {
			bundle.Summary.Unhealthy++
		}
	}
	bundle.Summary.Events = len(events)
	bundle.Summary.WarningEvents = guardianCountWarningEvents(events)
	bundle.Summary.LogStreams = len(bundle.Logs)
	for _, log := range bundle.Logs {
		if log.Error != "" {
			bundle.Summary.LogErrors++
		}
	}
	bundle.Summary.ManagedOwners = len(bundle.ManagedFields.Owners)
	bundle.Summary.BoundaryFindings = len(bundle.RuntimeSecretBoundary.Findings)
	bundle.Summary.AftercareIssues = len(bundle.RolloutAftercare.Items)
	bundle.RuntimeSecretBoundary.Passed = bundle.Summary.BoundaryFindings == 0
	bundle.RolloutAftercare.Passed = bundle.Summary.AftercareIssues == 0 && bundle.Summary.WarningEvents == 0 && bundle.Summary.Unhealthy == 0
	bundle.CausalTimeline = buildIncidentTimeline(bundle)
	bundle.RootCause = buildIncidentRootCause(bundle)
	return bundle
}

func incidentCollectReleaseResources(ctx context.Context, client *kube.Client, namespace, release string) ([]*unstructured.Unstructured, error) {
	if client == nil || client.Clientset == nil {
		return nil, nil
	}
	selector := labels.Set(map[string]string{"app.kubernetes.io/instance": release}).AsSelector().String()
	opts := metav1.ListOptions{LabelSelector: selector}
	var out []*unstructured.Unstructured
	appendObj := func(obj runtime.Object, apiVersion, kind string) error {
		u, err := incidentUnstructured(obj, apiVersion, kind)
		if err != nil {
			return err
		}
		out = append(out, u)
		return nil
	}
	if list, err := client.Clientset.AppsV1().Deployments(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "apps/v1", "Deployment"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.AppsV1().StatefulSets(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "apps/v1", "StatefulSet"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.AppsV1().DaemonSets(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "apps/v1", "DaemonSet"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "apps/v1", "ReplicaSet"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.BatchV1().Jobs(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "batch/v1", "Job"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.BatchV1().CronJobs(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "batch/v1", "CronJob"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.CoreV1().Pods(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "v1", "Pod"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.CoreV1().Services(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "v1", "Service"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.CoreV1().ConfigMaps(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "v1", "ConfigMap"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.CoreV1().Secrets(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "v1", "Secret"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.CoreV1().ServiceAccounts(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "v1", "ServiceAccount"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.RbacV1().Roles(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "rbac.authorization.k8s.io/v1", "Role"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.RbacV1().RoleBindings(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "rbac.authorization.k8s.io/v1", "RoleBinding"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "autoscaling/v2", "HorizontalPodAutoscaler"); err != nil {
				return nil, err
			}
		}
	}
	if list, err := client.Clientset.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, opts); err == nil {
		for i := range list.Items {
			if err := appendObj(&list.Items[i], "policy/v1", "PodDisruptionBudget"); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return incidentResourceKey(guardianResourceFromObject(out[i], namespace)) < incidentResourceKey(guardianResourceFromObject(out[j], namespace))
	})
	return out, nil
}

func incidentUnstructured(obj runtime.Object, apiVersion, kind string) (*unstructured.Unstructured, error) {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: raw}
	u.SetGroupVersionKind(schema.FromAPIVersionAndKind(apiVersion, kind))
	return u, nil
}

func incidentStatusByResource(rows []deploy.ResourceStatus) map[string]deploy.ResourceStatus {
	out := map[string]deploy.ResourceStatus{}
	for _, row := range rows {
		ref := guardianResourceRef{Kind: row.Kind, Namespace: row.Namespace, Name: row.Name}
		out[incidentResourceKey(ref)] = row
	}
	return out
}

func incidentStatusForResource(ref guardianResourceRef, statuses map[string]deploy.ResourceStatus) deploy.ResourceStatus {
	if len(statuses) == 0 {
		return deploy.ResourceStatus{}
	}
	if status, ok := statuses[incidentResourceKey(ref)]; ok {
		return status
	}
	ref.Group = ""
	ref.Version = ""
	return statuses[incidentResourceKey(ref)]
}

func incidentCollectLogs(ctx context.Context, client *kube.Client, namespace, release string, tail int64) ([]incidentLogSet, error) {
	if tail == 0 || client == nil || client.Clientset == nil {
		return nil, nil
	}
	selector := labels.Set(map[string]string{"app.kubernetes.io/instance": release}).AsSelector().String()
	pods, err := client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	var out []incidentLogSet
	for _, pod := range pods.Items {
		for _, container := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
			out = append(out, incidentReadPodLogs(ctx, client, pod.Namespace, pod.Name, container.Name, tail, false))
			prev := incidentReadPodLogs(ctx, client, pod.Namespace, pod.Name, container.Name, tail, true)
			if prev.Error == "" && len(prev.Lines) > 0 {
				out = append(out, prev)
			}
		}
	}
	return out, nil
}

func incidentReadPodLogs(ctx context.Context, client *kube.Client, namespace, pod, container string, tail int64, previous bool) incidentLogSet {
	row := incidentLogSet{Namespace: namespace, Pod: pod, Container: container, Previous: previous}
	raw, err := client.Clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container, TailLines: &tail, Previous: previous}).Do(ctx).Raw()
	if err != nil {
		row.Error = guardianRedactText(err.Error())
		return row
	}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		row.Lines = append(row.Lines, guardianRedactText(line))
	}
	return row
}

func incidentFilterEvents(events []guardianEventRow, release string, resources []*unstructured.Unstructured) []guardianEventRow {
	resourceNames := map[string]struct{}{}
	for _, obj := range resources {
		if obj != nil {
			resourceNames[obj.GetName()] = struct{}{}
		}
	}
	var out []guardianEventRow
	for _, ev := range events {
		if strings.Contains(ev.Resource.Name, release) || strings.Contains(ev.Message, release) {
			out = append(out, ev)
			continue
		}
		if _, ok := resourceNames[ev.Resource.Name]; ok {
			out = append(out, ev)
		}
	}
	return out
}

func incidentRedactedObject(obj *unstructured.Unstructured) map[string]any {
	if obj == nil {
		return nil
	}
	cp := obj.DeepCopy()
	unstructured.RemoveNestedField(cp.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(cp.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(cp.Object, "metadata", "uid")
	if strings.EqualFold(cp.GetKind(), "Secret") {
		unstructured.RemoveNestedField(cp.Object, "data")
		unstructured.RemoveNestedField(cp.Object, "stringData")
	}
	guardianRedactObject(cp.Object)
	return cp.Object
}

func incidentRedactLogs(logs []incidentLogSet) []incidentLogSet {
	if len(logs) == 0 {
		return nil
	}
	out := make([]incidentLogSet, len(logs))
	for i, log := range logs {
		out[i] = log
		out[i].Error = guardianRedactText(log.Error)
		out[i].Lines = append([]string(nil), log.Lines...)
		for j := range out[i].Lines {
			out[i].Lines[j] = guardianRedactText(out[i].Lines[j])
		}
	}
	return out
}

func buildIncidentTimeline(bundle incidentBundle) incidentCausalTimeline {
	timeline := incidentCausalTimeline{Version: "v1"}
	for _, resource := range bundle.Resources {
		if incidentUnhealthyStatus(resource.Status) {
			timeline.Items = append(timeline.Items, incidentTimelineRow{
				Time:     bundle.GeneratedAt,
				Source:   "resource-status",
				Severity: "high",
				Resource: resource.Resource,
				Reason:   firstNonEmpty(resource.Status.Reason, resource.Status.Status),
				Message:  resource.Status.Message,
				Evidence: "resource status snapshot",
			})
		}
	}
	for _, ev := range bundle.EventsTimeline.Events {
		severity := "info"
		if strings.EqualFold(ev.Type, corev1.EventTypeWarning) {
			severity = "high"
		}
		timeline.Items = append(timeline.Items, incidentTimelineRow{
			Time:     ev.Time,
			Source:   "event",
			Severity: severity,
			Resource: ev.Resource,
			Reason:   ev.Reason,
			Message:  ev.Message,
			Evidence: "kubernetes event",
		})
	}
	for _, finding := range bundle.RuntimeSecretBoundary.Findings {
		timeline.Items = append(timeline.Items, incidentTimelineRow{
			Time:     bundle.GeneratedAt,
			Source:   "secret-boundary",
			Severity: finding.Severity,
			Resource: finding.Resource,
			Reason:   finding.Surface,
			Message:  finding.Message,
			Evidence: "runtime boundary scan",
		})
	}
	for _, item := range bundle.RolloutAftercare.Items {
		timeline.Items = append(timeline.Items, incidentTimelineRow{
			Time:     bundle.GeneratedAt,
			Source:   "rollout-aftercare",
			Severity: item.Severity,
			Resource: item.Resource,
			Reason:   item.Reason,
			Message:  item.Message,
			Evidence: "rollout aftercare",
		})
	}
	for _, log := range bundle.Logs {
		if log.Error != "" {
			timeline.Items = append(timeline.Items, incidentTimelineRow{
				Time:     bundle.GeneratedAt,
				Source:   "logs",
				Severity: "medium",
				Resource: guardianResourceRef{Kind: "Pod", Namespace: log.Namespace, Name: log.Pod},
				Reason:   "log_read_error",
				Message:  log.Error,
				Evidence: log.Container,
			})
		}
		for _, line := range log.Lines {
			if incidentLineLooksFailure(line) {
				timeline.Items = append(timeline.Items, incidentTimelineRow{
					Time:     bundle.GeneratedAt,
					Source:   "logs",
					Severity: "medium",
					Resource: guardianResourceRef{Kind: "Pod", Namespace: log.Namespace, Name: log.Pod},
					Reason:   "log_failure_signal",
					Message:  line,
					Evidence: log.Container,
				})
			}
		}
	}
	sort.SliceStable(timeline.Items, func(i, j int) bool {
		return timeline.Items[i].Time < timeline.Items[j].Time
	})
	return timeline
}

func buildIncidentRootCause(bundle incidentBundle) incidentRootCause {
	root := incidentRootCause{
		Version:     "v1",
		Status:      "passed",
		Confidence:  "low",
		Release:     bundle.Release,
		Namespace:   bundle.Namespace,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	addEvidence := func(row incidentTimelineRow) {
		if len(root.Evidence) >= 8 {
			return
		}
		root.Evidence = append(root.Evidence, incidentEvidence{
			Source:   row.Source,
			Severity: row.Severity,
			Resource: row.Resource,
			Reason:   row.Reason,
			Message:  row.Message,
		})
	}
	for _, row := range bundle.CausalTimeline.Items {
		msg := strings.ToLower(row.Reason + " " + row.Message)
		if strings.Contains(msg, "imagepullbackoff") || strings.Contains(msg, "errimagepull") || strings.Contains(msg, "failed to pull") || strings.Contains(msg, "pull access denied") {
			root.Status = "blocked"
			root.Blocked = true
			root.PrimaryCause = "image_pull_failure"
			root.Confidence = "high"
			root.Summary = "The rollout is blocked because Kubernetes cannot pull a container image."
			root.Recommendations = []string{"Verify the image reference and registry credentials.", "Rebuild or retag the image, then rerun torque apply with capture enabled.", "Use torque guardian diff after repair to prove live state matches the simulated release."}
			addEvidence(row)
			return root
		}
	}
	for _, row := range bundle.CausalTimeline.Items {
		msg := strings.ToLower(row.Reason + " " + row.Message)
		if strings.Contains(msg, "crashloopbackoff") || strings.Contains(msg, "back-off restarting failed container") {
			root.Status = "blocked"
			root.Blocked = true
			root.PrimaryCause = "crash_loop"
			root.Confidence = "high"
			root.Summary = "The rollout is blocked by a crashing container."
			root.Recommendations = []string{"Inspect the captured container logs and fix the application startup failure.", "Rerun incident capture after the next rollout to confirm the crash loop is gone."}
			addEvidence(row)
			return root
		}
	}
	if len(bundle.RuntimeSecretBoundary.Findings) > 0 {
		root.Status = "blocked"
		root.Blocked = true
		root.PrimaryCause = "secret_boundary_violation"
		root.Confidence = "medium"
		root.Summary = "Secret-like material reached a forbidden runtime surface."
		root.Recommendations = []string{"Move literal secret values into Kubernetes Secrets or secret:// references.", "Rerun verifier and Guardian boundary checks before applying again."}
		for _, finding := range bundle.RuntimeSecretBoundary.Findings {
			addEvidence(incidentTimelineRow{Source: "secret-boundary", Severity: finding.Severity, Resource: finding.Resource, Reason: finding.Surface, Message: finding.Message})
		}
		return root
	}
	for _, row := range bundle.CausalTimeline.Items {
		if row.Severity == "high" {
			root.Status = "blocked"
			root.Blocked = true
			root.PrimaryCause = "runtime_health_regression"
			root.Confidence = "medium"
			root.Summary = "Runtime health evidence contains high-severity rollout or event failures."
			root.Recommendations = []string{"Inspect the high-severity timeline rows and fix the failing resource.", "Run torque incident replay and torque guardian diff before declaring the incident resolved."}
			addEvidence(row)
		}
	}
	if root.Blocked {
		return root
	}
	root.Summary = "No blocking incident evidence was found in the captured window."
	root.Recommendations = []string{"If the incident is still active, capture a wider --since window or include fresh logs."}
	return root
}

func replayIncidentBundle(path string, opts incidentReplayOptions) (incidentReplayProof, error) {
	bundle, err := loadIncidentBundle(path)
	if err != nil {
		return incidentReplayProof{}, err
	}
	root := bundle.RootCause
	if root.Version == "" {
		root = buildIncidentRootCause(bundle)
	}
	proof := incidentReplayProof{
		Version:        "v1",
		Tool:           incidentTool,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Lab:            opts.Lab,
		Source:         path,
		Release:        bundle.Release,
		Namespace:      bundle.Namespace,
		ObserveOnly:    true,
		Passed:         bundle.Tool == incidentTool && bundle.Version != "" && len(bundle.Resources)+len(bundle.EventsTimeline.Events)+len(bundle.Logs) > 0,
		Blocked:        root.Blocked,
		Summary:        bundle.Summary,
		RootCause:      root,
		CausalTimeline: bundle.CausalTimeline,
	}
	if strings.TrimSpace(opts.Out) != "" {
		files, err := writeIncidentReplayProof(opts.Out, bundle, proof)
		if err != nil {
			return incidentReplayProof{}, err
		}
		proof.FilesWritten = files
	}
	return proof, nil
}

func writeIncidentReplayProof(dir string, bundle incidentBundle, proof incidentReplayProof) ([]string, error) {
	if err := os.MkdirAll(filepath.Join(dir, "fix"), 0o755); err != nil {
		return nil, err
	}
	manifest := incidentReplayManifest{
		Version:     "v1",
		Tool:        incidentTool,
		GeneratedAt: proof.GeneratedAt,
		Lab:         proof.Lab,
		Source:      proof.Source,
		Release:     proof.Release,
		Namespace:   proof.Namespace,
		ObserveOnly: true,
		Passed:      proof.Passed,
		Blocked:     proof.Blocked,
		Files: map[string]string{
			"capture":               "capture.bundle.json",
			"replay":                "replay.result.json",
			"causalTimeline":        "causal.timeline.json",
			"eventsTimeline":        "events.timeline.json",
			"logs":                  "logs.redacted.json",
			"managedFieldsOwners":   "managed-fields.owners.json",
			"runtimeSecretBoundary": "secret.boundary.json",
			"rootCause":             "root-cause.json",
			"fixPatch":              "fix/incident-fix.patch",
			"fixPR":                 "fix/pr.md",
		},
	}
	files := map[string]any{
		"manifest.json":              manifest,
		"capture.bundle.json":        bundle,
		"replay.result.json":         proof,
		"causal.timeline.json":       bundle.CausalTimeline,
		"events.timeline.json":       bundle.EventsTimeline,
		"logs.redacted.json":         bundle.Logs,
		"managed-fields.owners.json": bundle.ManagedFields,
		"secret.boundary.json":       bundle.RuntimeSecretBoundary,
		"root-cause.json":            proof.RootCause,
	}
	written := make([]string, 0, len(files)+2)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeJSONFile(filepath.Join(dir, name), files[name]); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		written = append(written, name)
	}
	paths, err := writeIncidentPRArtifacts(proof.RootCause, incidentPROptions{From: dir, OutDir: filepath.Join(dir, "fix")})
	if err != nil {
		return nil, err
	}
	written = append(written, filepath.ToSlash(strings.TrimPrefix(paths["patch"], dir+string(os.PathSeparator))))
	written = append(written, filepath.ToSlash(strings.TrimPrefix(paths["pr"], dir+string(os.PathSeparator))))
	return written, nil
}

func loadIncidentBundle(path string) (incidentBundle, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return incidentBundle{}, fmt.Errorf("incident bundle path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return incidentBundle{}, fmt.Errorf("inspect incident bundle: %w", err)
	}
	if info.IsDir() {
		path = filepath.Join(path, "capture.bundle.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return incidentBundle{}, fmt.Errorf("read incident bundle: %w", err)
	}
	var bundle incidentBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return incidentBundle{}, fmt.Errorf("parse incident bundle: %w", err)
	}
	if bundle.Tool != incidentTool {
		return incidentBundle{}, fmt.Errorf("%s does not look like a Torque incident bundle", path)
	}
	return bundle, nil
}

func loadIncidentRootCause(path string) (incidentRootCause, error) {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return incidentRootCause{}, fmt.Errorf("inspect incident proof: %w", err)
	}
	if info.IsDir() {
		if raw, err := os.ReadFile(filepath.Join(path, "root-cause.json")); err == nil {
			var root incidentRootCause
			if err := json.Unmarshal(raw, &root); err != nil {
				return incidentRootCause{}, fmt.Errorf("parse root cause: %w", err)
			}
			return root, nil
		}
		return incidentRootCause{}, fmt.Errorf("root-cause.json not found in %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return incidentRootCause{}, fmt.Errorf("read incident proof: %w", err)
	}
	var root incidentRootCause
	if err := json.Unmarshal(raw, &root); err == nil && root.Version != "" && root.Status != "" {
		return root, nil
	}
	var bundle incidentBundle
	if err := json.Unmarshal(raw, &bundle); err == nil && bundle.Tool == incidentTool {
		if bundle.RootCause.Version != "" {
			return bundle.RootCause, nil
		}
		return buildIncidentRootCause(bundle), nil
	}
	return incidentRootCause{}, fmt.Errorf("%s does not look like incident root cause or bundle JSON", path)
}

func writeIncidentPRArtifacts(root incidentRootCause, opts incidentPROptions) (map[string]string, error) {
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
	patchPath := filepath.Join(outDir, "incident-fix.patch")
	prPath := filepath.Join(outDir, "pr.md")
	body := renderIncidentPRMarkdown(root, opts.Branch)
	if err := os.WriteFile(patchPath, []byte(renderIncidentPatch(root, body)), 0o644); err != nil {
		return nil, fmt.Errorf("write incident patch: %w", err)
	}
	if err := os.WriteFile(prPath, []byte(body+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write incident PR body: %w", err)
	}
	return map[string]string{"patch": patchPath, "pr": prPath}, nil
}

func renderIncidentPatch(root incidentRootCause, body string) string {
	filename := ".torque/incidents/" + sanitizeFilename(firstNonEmpty(root.Release, "incident")) + "-root-cause.md"
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

func renderIncidentPRMarkdown(root incidentRootCause, branch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Torque Incident Repair\n\n")
	fmt.Fprintf(&b, "- Release: `%s`\n", firstNonEmpty(root.Release, "-"))
	fmt.Fprintf(&b, "- Namespace: `%s`\n", firstNonEmpty(root.Namespace, "-"))
	fmt.Fprintf(&b, "- Status: `%s`\n", firstNonEmpty(root.Status, "-"))
	fmt.Fprintf(&b, "- Primary cause: `%s`\n", firstNonEmpty(root.PrimaryCause, "none"))
	fmt.Fprintf(&b, "- Confidence: `%s`\n", firstNonEmpty(root.Confidence, "-"))
	if branch != "" {
		fmt.Fprintf(&b, "- Branch: `%s`\n", branch)
	}
	if root.Summary != "" {
		fmt.Fprintf(&b, "\n## Summary\n\n%s\n", root.Summary)
	}
	if len(root.Evidence) > 0 {
		fmt.Fprintf(&b, "\n## Evidence\n\n")
		for _, item := range root.Evidence {
			fmt.Fprintf(&b, "- `%s` `%s/%s`: %s - %s\n", item.Source, firstNonEmpty(item.Resource.Namespace, "cluster"), item.Resource.Name, firstNonEmpty(item.Reason, "-"), item.Message)
		}
	}
	if len(root.Recommendations) > 0 {
		fmt.Fprintf(&b, "\n## Repair\n\n")
		for _, item := range root.Recommendations {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	fmt.Fprintf(&b, "\n## Validation\n\n```bash\n")
	fmt.Fprintf(&b, "torque incident capture --release %s -n %s --since 1h --out incident.torque\n", shellQuoteToken(root.Release), shellQuoteToken(firstNonEmpty(root.Namespace, "default")))
	fmt.Fprintf(&b, "torque incident replay incident.torque --lab k3s --out incident-replay-proof\n")
	fmt.Fprintf(&b, "torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json\n")
	fmt.Fprintf(&b, "```\n")
	return strings.TrimRight(b.String(), "\n")
}

func renderIncidentReplayText(out interface{ Write([]byte) (int, error) }, proof incidentReplayProof, path string) {
	fmt.Fprintf(out, "Incident replay: %s\n", strings.ToUpper(passFail(proof.Passed)))
	fmt.Fprintf(out, "Lab: %s\n", proof.Lab)
	fmt.Fprintf(out, "Observe-only: true\n")
	fmt.Fprintf(out, "Release: %s\n", proof.Release)
	fmt.Fprintf(out, "Namespace: %s\n", proof.Namespace)
	fmt.Fprintf(out, "Blocked: %t\n", proof.Blocked)
	if proof.RootCause.PrimaryCause != "" {
		fmt.Fprintf(out, "Primary cause: %s\n", proof.RootCause.PrimaryCause)
	}
	if strings.TrimSpace(path) != "" {
		fmt.Fprintf(out, "Proof written to %s\n", path)
	}
}

func renderIncidentJSON(out interface{ Write([]byte) (int, error) }, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\n", raw)
	return nil
}

func validateIncidentFormat(format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text", "json":
		return nil
	default:
		return fmt.Errorf("unsupported --format %q (expected text or json)", format)
	}
}

func incidentResourceKey(ref guardianResourceRef) string {
	return strings.ToLower(strings.Join([]string{ref.Group, ref.Version, ref.Kind, ref.Namespace, ref.Name}, "/"))
}

func incidentUnhealthyStatus(status deploy.ResourceStatus) bool {
	s := strings.ToLower(strings.TrimSpace(status.Status))
	return s == "failed" || s == "pending" || s == "progressing" || s == "unknown" || strings.Contains(s, "error")
}

func incidentLineLooksFailure(line string) bool {
	lower := strings.ToLower(line)
	for _, token := range []string{"error", "fatal", "panic", "exception", "denied", "failed", "timeout"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

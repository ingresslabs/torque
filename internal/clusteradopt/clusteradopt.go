// File: internal/clusteradopt/clusteradopt.go
// Brief: Discover live cluster state and render a starter torque stack.

package clusteradopt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ingresslabs/torque/internal/kube"
	"github.com/ingresslabs/torque/internal/stack"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	helmrelease "helm.sh/helm/v3/pkg/release"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type DiscoverOptions struct {
	Kubeconfig      string
	Context         string
	Namespaces      []string
	AllNamespaces   bool
	IncludeSystem   bool
	IncludeHelm     bool
	IncludeGitOps   bool
	IncludeWorkload bool
}

type RenderOptions struct {
	StackName   string
	ClusterName string
	Kubeconfig  string
	Context     string
	ChartsDir   string
	WriteCharts bool
	ValuesDir   string
	WriteValues bool
}

type Snapshot struct {
	Cluster      ClusterInfo     `json:"cluster,omitempty"`
	Namespaces   []NamespaceInfo `json:"namespaces,omitempty"`
	HelmReleases []HelmRelease   `json:"helmReleases,omitempty"`
	GitOps       []GitOpsObject  `json:"gitOps,omitempty"`
	Workloads    []Workload      `json:"workloads,omitempty"`
	Helmfiles    []string        `json:"helmfiles,omitempty"`
	Warnings     []string        `json:"warnings,omitempty"`
}

type ClusterInfo struct {
	Name       string `json:"name,omitempty"`
	Context    string `json:"context,omitempty"`
	Kubeconfig string `json:"kubeconfig,omitempty"`
}

type NamespaceInfo struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type HelmRelease struct {
	Name         string         `json:"name"`
	Namespace    string         `json:"namespace"`
	Chart        string         `json:"chart,omitempty"`
	ChartVersion string         `json:"chartVersion,omitempty"`
	AppVersion   string         `json:"appVersion,omitempty"`
	Status       string         `json:"status,omitempty"`
	Values       map[string]any `json:"values,omitempty"`
	ChartObject  *chart.Chart   `json:"-"`
}

type GitOpsObject struct {
	System      string `json:"system"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
}

type Workload struct {
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	ManagedBy   string `json:"managedBy,omitempty"`
	HelmRelease string `json:"helmRelease,omitempty"`
	GitOps      string `json:"gitOps,omitempty"`
}

type ValuesFile struct {
	Path   string
	Values map[string]any
}

type ChartFile struct {
	Path  string
	Chart *chart.Chart
}

type ValuesWriteResult struct {
	Created []string
	Skipped []string
}

func Discover(ctx context.Context, kc *kube.Client, opts DiscoverOptions) (*Snapshot, error) {
	if kc == nil {
		return nil, fmt.Errorf("kubernetes client is nil")
	}
	if !opts.IncludeHelm && !opts.IncludeGitOps && !opts.IncludeWorkload {
		opts.IncludeHelm = true
		opts.IncludeGitOps = true
		opts.IncludeWorkload = true
	}
	s := &Snapshot{
		Cluster: ClusterInfo{
			Context:    strings.TrimSpace(opts.Context),
			Kubeconfig: strings.TrimSpace(opts.Kubeconfig),
		},
	}

	namespaces, nsInfo, warnings, err := resolveNamespaces(ctx, kc, opts)
	if err != nil {
		return nil, err
	}
	s.Namespaces = nsInfo
	s.Warnings = append(s.Warnings, warnings...)

	if opts.IncludeHelm {
		releases, helmWarnings := DiscoverHelmReleases(ctx, opts, namespaces)
		s.HelmReleases = releases
		s.Warnings = append(s.Warnings, helmWarnings...)
	}
	if opts.IncludeGitOps {
		gitOps, gitOpsWarnings := discoverGitOps(ctx, kc, namespaces)
		s.GitOps = gitOps
		s.Warnings = append(s.Warnings, gitOpsWarnings...)
	}
	if opts.IncludeWorkload {
		workloads, workloadWarnings := discoverWorkloads(ctx, kc, namespaces)
		s.Workloads = workloads
		s.Warnings = append(s.Warnings, workloadWarnings...)
	}
	s.Warnings = append(s.Warnings, duplicateReleaseNameWarnings(s.HelmReleases)...)

	sortSnapshot(s)
	return s, nil
}

func DiscoverHelmReleases(ctx context.Context, opts DiscoverOptions, namespaces []string) ([]HelmRelease, []string) {
	settings := cli.New()
	if strings.TrimSpace(opts.Kubeconfig) != "" {
		settings.KubeConfig = strings.TrimSpace(opts.Kubeconfig)
	}
	if strings.TrimSpace(opts.Context) != "" {
		settings.KubeContext = strings.TrimSpace(opts.Context)
	}

	var out []HelmRelease
	var warnings []string
	run := func(namespace string, allNamespaces bool) {
		actionCfg := new(action.Configuration)
		initNamespace := namespace
		if allNamespaces {
			initNamespace = ""
		}
		if err := actionCfg.Init(settings.RESTClientGetter(), initNamespace, os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
			warnings = append(warnings, fmt.Sprintf("helm discovery skipped for namespace %q: %v", namespace, err))
			return
		}
		client := action.NewList(actionCfg)
		client.Deployed = true
		client.Failed = true
		client.Pending = true
		client.AllNamespaces = allNamespaces
		client.SetStateMask()
		releases, err := client.Run()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("helm discovery failed for namespace %q: %v", namespace, err))
			return
		}
		for _, rel := range releases {
			if rel == nil {
				continue
			}
			if !opts.IncludeSystem && IsSystemNamespace(rel.Namespace) {
				continue
			}
			out = append(out, releaseFromHelm(rel))
		}
	}

	if opts.AllNamespaces {
		run("", true)
	} else {
		for _, ns := range namespaces {
			run(ns, false)
		}
	}

	select {
	case <-ctx.Done():
		warnings = append(warnings, fmt.Sprintf("helm discovery context ended: %v", ctx.Err()))
	default:
	}
	return out, dedupeStrings(warnings)
}

func RenderStackYAML(snapshot *Snapshot, opts RenderOptions) (string, []ValuesFile, []ChartFile, error) {
	if snapshot == nil {
		return "", nil, nil, fmt.Errorf("snapshot is nil")
	}
	clusterName := strings.TrimSpace(opts.ClusterName)
	if clusterName == "" {
		clusterName = firstNonEmpty(snapshot.Cluster.Name, snapshot.Cluster.Context, "default")
	}
	stackName := strings.TrimSpace(opts.StackName)
	if stackName == "" {
		stackName = "adopted-" + sanitizeName(clusterName)
	}
	if stackName == "adopted-" {
		stackName = "adopted-cluster"
	}

	inferDeps := true
	createNamespace := true
	sf := stack.StackFile{
		APIVersionKind: stack.APIVersionKind{APIVersion: "torque.dev/v1", Kind: "Stack"},
		Name:           stackName,
		Defaults: stack.ReleaseDefaults{
			Cluster: stack.ClusterTarget{
				Name:       clusterName,
				Kubeconfig: strings.TrimSpace(opts.Kubeconfig),
				Context:    strings.TrimSpace(opts.Context),
			},
			Apply: stack.ApplyOptions{CreateNamespace: &createNamespace},
		},
		CLI: stack.StackCLIConfig{
			Output:    "table",
			InferDeps: &inferDeps,
		},
	}

	var values []ValuesFile
	var charts []ChartFile
	for _, rel := range snapshot.HelmReleases {
		if strings.TrimSpace(rel.Name) == "" || strings.TrimSpace(rel.Namespace) == "" {
			continue
		}
		chartRef := firstNonEmpty(rel.Chart, rel.Name)
		if opts.WriteCharts && rel.ChartObject != nil {
			path := chartArchivePath(opts.ChartsDir, rel.Namespace, rel)
			chartRef = "./" + path
			charts = append(charts, ChartFile{Path: path, Chart: rel.ChartObject})
		}
		spec := stack.ReleaseSpec{
			Name:         rel.Name,
			Namespace:    rel.Namespace,
			Chart:        chartRef,
			ChartVersion: rel.ChartVersion,
			Tags:         releaseTags(rel),
		}
		if opts.WriteValues && len(rel.Values) > 0 {
			path := valuesPath(opts.ValuesDir, rel.Namespace, rel.Name)
			spec.Values = []string{path}
			values = append(values, ValuesFile{Path: strings.TrimPrefix(path, "./"), Values: rel.Values})
		}
		sf.Releases = append(sf.Releases, spec)
	}

	raw, err := yaml.Marshal(sf)
	if err != nil {
		return "", nil, nil, fmt.Errorf("render stack yaml: %w", err)
	}
	header := renderDiscoveryHeader(snapshot, opts)
	return header + string(raw), values, charts, nil
}

func WriteValuesFiles(root string, files []ValuesFile, force bool, dryRun bool) (ValuesWriteResult, error) {
	res := ValuesWriteResult{}
	for _, f := range files {
		rel := filepath.Clean(strings.TrimSpace(f.Path))
		if rel == "." || rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return res, fmt.Errorf("invalid values path %q", f.Path)
		}
		target := filepath.Join(root, rel)
		if _, err := os.Stat(target); err == nil {
			if !force {
				res.Skipped = append(res.Skipped, filepath.ToSlash(rel))
				continue
			}
		} else if err != nil && !os.IsNotExist(err) {
			return res, err
		}
		res.Created = append(res.Created, filepath.ToSlash(rel))
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return res, err
		}
		raw, err := yaml.Marshal(f.Values)
		if err != nil {
			return res, fmt.Errorf("render %s: %w", f.Path, err)
		}
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			return res, fmt.Errorf("write %s: %w", target, err)
		}
	}
	return res, nil
}

func WriteChartFiles(root string, files []ChartFile, force bool, dryRun bool) (ValuesWriteResult, error) {
	res := ValuesWriteResult{}
	for _, f := range files {
		rel := filepath.Clean(strings.TrimSpace(f.Path))
		if rel == "." || rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return res, fmt.Errorf("invalid chart path %q", f.Path)
		}
		if f.Chart == nil {
			continue
		}
		target := filepath.Join(root, rel)
		if _, err := os.Stat(target); err == nil {
			if !force {
				res.Skipped = append(res.Skipped, filepath.ToSlash(rel))
				continue
			}
		} else if err != nil && !os.IsNotExist(err) {
			return res, err
		}
		res.Created = append(res.Created, filepath.ToSlash(rel))
		if dryRun {
			continue
		}
		if f.Chart.Metadata != nil {
			if strings.TrimSpace(f.Chart.Metadata.APIVersion) == "" {
				f.Chart.Metadata.APIVersion = "v2"
			}
			if strings.TrimSpace(f.Chart.Metadata.Version) == "" {
				f.Chart.Metadata.Version = "0.0.0"
			}
		}
		outDir := filepath.Dir(target)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return res, err
		}
		saved, err := chartutil.Save(f.Chart, outDir)
		if err != nil {
			return res, fmt.Errorf("save chart %s: %w", f.Path, err)
		}
		if filepath.Clean(saved) != filepath.Clean(target) {
			if err := os.Rename(saved, target); err != nil {
				return res, fmt.Errorf("rename saved chart %s to %s: %w", saved, target, err)
			}
		}
	}
	return res, nil
}

func DiscoverHelmfiles(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "dist", "node_modules", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		name := strings.ToLower(d.Name())
		if name == "helmfile.yaml" || name == "helmfile.yml" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			out = append(out, "./"+filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func resolveNamespaces(ctx context.Context, kc *kube.Client, opts DiscoverOptions) ([]string, []NamespaceInfo, []string, error) {
	explicit := normalizeStrings(opts.Namespaces)
	if len(explicit) > 0 {
		info := make([]NamespaceInfo, 0, len(explicit))
		for _, ns := range explicit {
			info = append(info, NamespaceInfo{Name: ns})
		}
		return explicit, info, nil, nil
	}
	if opts.AllNamespaces {
		list, err := kc.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("list namespaces: %w", err)
		}
		var namespaces []string
		var info []NamespaceInfo
		for _, ns := range list.Items {
			name := ns.Name
			if !opts.IncludeSystem && IsSystemNamespace(name) {
				continue
			}
			namespaces = append(namespaces, name)
			info = append(info, NamespaceInfo{Name: name, Status: string(ns.Status.Phase)})
		}
		sort.Strings(namespaces)
		sort.Slice(info, func(i, j int) bool { return info[i].Name < info[j].Name })
		return namespaces, info, nil, nil
	}
	ns := strings.TrimSpace(kc.Namespace)
	if ns == "" {
		ns = "default"
	}
	return []string{ns}, []NamespaceInfo{{Name: ns}}, nil, nil
}

func releaseFromHelm(rel *helmrelease.Release) HelmRelease {
	out := HelmRelease{
		Name:      rel.Name,
		Namespace: rel.Namespace,
	}
	if rel.Info != nil {
		out.Status = rel.Info.Status.String()
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		out.Chart = rel.Chart.Metadata.Name
		out.ChartVersion = rel.Chart.Metadata.Version
		out.AppVersion = rel.Chart.Metadata.AppVersion
		out.ChartObject = rel.Chart
	}
	if len(rel.Config) > 0 {
		out.Values = map[string]any{}
		for k, v := range rel.Config {
			out.Values[k] = v
		}
	}
	return out
}

func discoverWorkloads(ctx context.Context, kc *kube.Client, namespaces []string) ([]Workload, []string) {
	var out []Workload
	var warnings []string
	for _, ns := range namespaces {
		if list, err := kc.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{}); err != nil {
			warnings = append(warnings, fmt.Sprintf("deployments in %s skipped: %v", ns, err))
		} else {
			for _, obj := range list.Items {
				out = append(out, workloadFromMeta("Deployment", obj.Namespace, obj.Name, obj.Labels, obj.Annotations))
			}
		}
		if list, err := kc.Clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{}); err != nil {
			warnings = append(warnings, fmt.Sprintf("statefulsets in %s skipped: %v", ns, err))
		} else {
			for _, obj := range list.Items {
				out = append(out, workloadFromMeta("StatefulSet", obj.Namespace, obj.Name, obj.Labels, obj.Annotations))
			}
		}
		if list, err := kc.Clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{}); err != nil {
			warnings = append(warnings, fmt.Sprintf("daemonsets in %s skipped: %v", ns, err))
		} else {
			for _, obj := range list.Items {
				out = append(out, workloadFromMeta("DaemonSet", obj.Namespace, obj.Name, obj.Labels, obj.Annotations))
			}
		}
		if list, err := kc.Clientset.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{}); err != nil {
			warnings = append(warnings, fmt.Sprintf("jobs in %s skipped: %v", ns, err))
		} else {
			for _, obj := range list.Items {
				out = append(out, workloadFromMeta("Job", obj.Namespace, obj.Name, obj.Labels, obj.Annotations))
			}
		}
		if list, err := kc.Clientset.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{}); err != nil {
			warnings = append(warnings, fmt.Sprintf("cronjobs in %s skipped: %v", ns, err))
		} else {
			for _, obj := range list.Items {
				out = append(out, workloadFromMeta("CronJob", obj.Namespace, obj.Name, obj.Labels, obj.Annotations))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return workloadKey(out[i]) < workloadKey(out[j])
	})
	return out, dedupeStrings(warnings)
}

type gitOpsKnownResource struct {
	System  string
	Kind    string
	GVR     schema.GroupVersionResource
	Extract func(*unstructured.Unstructured) GitOpsObject
}

func discoverGitOps(ctx context.Context, kc *kube.Client, namespaces []string) ([]GitOpsObject, []string) {
	known := []gitOpsKnownResource{
		{
			System: "argocd",
			Kind:   "Application",
			GVR:    schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"},
			Extract: func(u *unstructured.Unstructured) GitOpsObject {
				repo, _, _ := unstructured.NestedString(u.Object, "spec", "source", "repoURL")
				path, _, _ := unstructured.NestedString(u.Object, "spec", "source", "path")
				chart, _, _ := unstructured.NestedString(u.Object, "spec", "source", "chart")
				destNS, _, _ := unstructured.NestedString(u.Object, "spec", "destination", "namespace")
				destName, _, _ := unstructured.NestedString(u.Object, "spec", "destination", "name")
				return GitOpsObject{System: "argocd", Kind: "Application", Namespace: u.GetNamespace(), Name: u.GetName(), Source: compactParts(repo, path, chart), Destination: compactParts(destName, destNS)}
			},
		},
		{
			System: "flux",
			Kind:   "HelmRelease",
			GVR:    schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"},
			Extract: func(u *unstructured.Unstructured) GitOpsObject {
				chart, _, _ := unstructured.NestedString(u.Object, "spec", "chart", "spec", "chart")
				sourceKind, _, _ := unstructured.NestedString(u.Object, "spec", "chart", "spec", "sourceRef", "kind")
				sourceName, _, _ := unstructured.NestedString(u.Object, "spec", "chart", "spec", "sourceRef", "name")
				targetNS, _, _ := unstructured.NestedString(u.Object, "spec", "targetNamespace")
				return GitOpsObject{System: "flux", Kind: "HelmRelease", Namespace: u.GetNamespace(), Name: u.GetName(), Source: compactParts(sourceKind, sourceName, chart), Destination: targetNS}
			},
		},
		{
			System: "flux",
			Kind:   "Kustomization",
			GVR:    schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"},
			Extract: func(u *unstructured.Unstructured) GitOpsObject {
				path, _, _ := unstructured.NestedString(u.Object, "spec", "path")
				sourceKind, _, _ := unstructured.NestedString(u.Object, "spec", "sourceRef", "kind")
				sourceName, _, _ := unstructured.NestedString(u.Object, "spec", "sourceRef", "name")
				targetNS, _, _ := unstructured.NestedString(u.Object, "spec", "targetNamespace")
				return GitOpsObject{System: "flux", Kind: "Kustomization", Namespace: u.GetNamespace(), Name: u.GetName(), Source: compactParts(sourceKind, sourceName, path), Destination: targetNS}
			},
		},
	}

	var out []GitOpsObject
	var warnings []string
	for _, res := range known {
		for _, ns := range namespaces {
			list, err := kc.Dynamic.Resource(res.GVR).Namespace(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				if apierrors.IsForbidden(err) {
					warnings = append(warnings, fmt.Sprintf("%s %s in %s skipped: forbidden", res.System, res.Kind, ns))
					continue
				}
				continue
			}
			for i := range list.Items {
				item := list.Items[i]
				obj := res.Extract(&item)
				if obj.Namespace == "" {
					obj.Namespace = ns
				}
				out = append(out, obj)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return gitOpsKey(out[i]) < gitOpsKey(out[j])
	})
	return out, dedupeStrings(warnings)
}

func releaseTags(rel HelmRelease) []string {
	tags := []string{"adopted", "helm"}
	if strings.TrimSpace(rel.Status) != "" && !strings.EqualFold(rel.Status, "deployed") {
		tags = append(tags, "status:"+sanitizeName(rel.Status))
	}
	return tags
}

func renderDiscoveryHeader(snapshot *Snapshot, opts RenderOptions) string {
	var b strings.Builder
	b.WriteString("# Generated by `torque init from-cluster`.\n")
	b.WriteString("# Helm releases below are executable stack entries; GitOps and workload discoveries are kept as adoption notes.\n")
	if len(snapshot.Namespaces) > 0 {
		fmt.Fprintf(&b, "# Namespaces scanned: %s\n", strings.Join(namespaceNames(snapshot.Namespaces), ", "))
	}
	if len(snapshot.Helmfiles) > 0 {
		b.WriteString("# Helmfile files found locally:\n")
		for _, p := range limitStrings(snapshot.Helmfiles, 8) {
			fmt.Fprintf(&b, "# - %s\n", p)
		}
	}
	if len(snapshot.GitOps) > 0 {
		b.WriteString("# GitOps resources found:\n")
		for _, obj := range limitGitOps(snapshot.GitOps, 12) {
			fmt.Fprintf(&b, "# - %s %s/%s", obj.System, obj.Namespace, obj.Name)
			if obj.Source != "" {
				fmt.Fprintf(&b, " source=%s", obj.Source)
			}
			if obj.Destination != "" {
				fmt.Fprintf(&b, " destination=%s", obj.Destination)
			}
			b.WriteByte('\n')
		}
	}
	if len(snapshot.Workloads) > 0 {
		unmanaged := nonHelmWorkloads(snapshot.Workloads)
		if len(unmanaged) > 0 {
			b.WriteString("# Non-Helm workloads found; add charts/manifests before making them executable stack entries:\n")
			for _, wl := range limitWorkloads(unmanaged, 12) {
				fmt.Fprintf(&b, "# - %s %s/%s", wl.Kind, wl.Namespace, wl.Name)
				if wl.ManagedBy != "" {
					fmt.Fprintf(&b, " managed-by=%s", wl.ManagedBy)
				}
				if wl.GitOps != "" {
					fmt.Fprintf(&b, " gitops=%s", wl.GitOps)
				}
				b.WriteByte('\n')
			}
		}
	}
	if opts.WriteValues {
		b.WriteString("# Captured Helm values were written under the values paths referenced below. Review them for secrets before committing.\n")
	}
	if opts.WriteCharts {
		b.WriteString("# Installed Helm charts were exported under the chart paths referenced below so the starter stack can run without chart repo setup.\n")
	}
	for _, warning := range limitStrings(snapshot.Warnings, 12) {
		fmt.Fprintf(&b, "# Warning: %s\n", warning)
	}
	b.WriteByte('\n')
	return b.String()
}

func valuesPath(valuesDir, namespace, name string) string {
	dir := strings.Trim(strings.TrimSpace(valuesDir), "/")
	if dir == "" {
		dir = "values/adopted"
	}
	path := filepath.ToSlash(filepath.Join(dir, sanitizeSegment(namespace), sanitizeSegment(name)+".yaml"))
	if strings.HasPrefix(path, "./") {
		return path
	}
	return "./" + path
}

func chartArchivePath(chartsDir, namespace string, rel HelmRelease) string {
	dir := strings.Trim(strings.TrimSpace(chartsDir), "/")
	if dir == "" {
		dir = "charts/adopted"
	}
	chartName := firstNonEmpty(rel.Chart, rel.Name, "chart")
	version := firstNonEmpty(rel.ChartVersion, "0.0.0")
	fileName := chartName + "-" + version + ".tgz"
	return filepath.ToSlash(filepath.Join(dir, sanitizeSegment(namespace), fileName))
}

func workloadFromMeta(kind, namespace, name string, labels map[string]string, annotations map[string]string) Workload {
	w := Workload{
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		ManagedBy: labels["app.kubernetes.io/managed-by"],
	}
	if rel := annotations["meta.helm.sh/release-name"]; rel != "" {
		w.HelmRelease = rel
	}
	if inst := labels["app.kubernetes.io/instance"]; w.HelmRelease == "" && strings.EqualFold(w.ManagedBy, "Helm") {
		w.HelmRelease = inst
	}
	if app := labels["argocd.argoproj.io/instance"]; app != "" {
		w.GitOps = "argocd:" + app
	}
	if name := labels["kustomize.toolkit.fluxcd.io/name"]; name != "" {
		w.GitOps = "flux-kustomization:" + name
	}
	if name := labels["helm.toolkit.fluxcd.io/name"]; name != "" {
		w.GitOps = "flux-helmrelease:" + name
	}
	return w
}

func IsSystemNamespace(ns string) bool {
	switch strings.TrimSpace(ns) {
	case "kube-system", "kube-public", "kube-node-lease":
		return true
	case "default":
		return false
	case "":
		return false
	}
	return strings.HasPrefix(ns, "kube-")
}

func sortSnapshot(s *Snapshot) {
	sort.Slice(s.Namespaces, func(i, j int) bool { return s.Namespaces[i].Name < s.Namespaces[j].Name })
	sort.Slice(s.HelmReleases, func(i, j int) bool {
		return s.HelmReleases[i].Namespace+"/"+s.HelmReleases[i].Name < s.HelmReleases[j].Namespace+"/"+s.HelmReleases[j].Name
	})
	sort.Slice(s.GitOps, func(i, j int) bool { return gitOpsKey(s.GitOps[i]) < gitOpsKey(s.GitOps[j]) })
	sort.Slice(s.Workloads, func(i, j int) bool { return workloadKey(s.Workloads[i]) < workloadKey(s.Workloads[j]) })
	s.Warnings = dedupeStrings(s.Warnings)
}

func duplicateReleaseNameWarnings(releases []HelmRelease) []string {
	byName := map[string][]string{}
	for _, rel := range releases {
		byName[rel.Name] = append(byName[rel.Name], rel.Namespace)
	}
	var warnings []string
	for name, namespaces := range byName {
		if len(namespaces) <= 1 {
			continue
		}
		sort.Strings(namespaces)
		warnings = append(warnings, fmt.Sprintf("release name %q exists in multiple namespaces (%s); stack selection by bare release name may be ambiguous", name, strings.Join(namespaces, ", ")))
	}
	sort.Strings(warnings)
	return warnings
}

func normalizeStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	sort.Strings(out)
	return out
}

func namespaceNames(values []NamespaceInfo) []string {
	out := make([]string, 0, len(values))
	for _, ns := range values {
		out = append(out, ns.Name)
	}
	sort.Strings(out)
	return out
}

func nonHelmWorkloads(values []Workload) []Workload {
	var out []Workload
	for _, wl := range values {
		if wl.HelmRelease != "" || strings.EqualFold(wl.ManagedBy, "Helm") {
			continue
		}
		out = append(out, wl)
	}
	return out
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitGitOps(values []GitOpsObject, limit int) []GitOpsObject {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitWorkloads(values []Workload, limit int) []Workload {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactParts(parts ...string) string {
	var out []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, " ")
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func sanitizeSegment(value string) string {
	out := sanitizeName(value)
	if out == "" {
		return "unknown"
	}
	return out
}

func workloadKey(w Workload) string {
	return w.Namespace + "/" + w.Kind + "/" + w.Name
}

func gitOpsKey(g GitOpsObject) string {
	return g.System + "/" + g.Namespace + "/" + g.Kind + "/" + g.Name
}

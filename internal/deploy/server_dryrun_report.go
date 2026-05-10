package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ingresslabs/torque/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	ServerDryRunStatusPassed  = "passed"
	ServerDryRunStatusFailed  = "failed"
	ServerDryRunStatusSkipped = "skipped"

	ServerDryRunClassAdmissionDenied        = "admission_denied"
	ServerDryRunClassAPIMappingMissing      = "api_mapping_missing"
	ServerDryRunClassAPIError               = "api_error"
	ServerDryRunClassFieldOwnershipConflict = "field_ownership_conflict"
	ServerDryRunClassImmutableField         = "immutable_field"
	ServerDryRunClassNamespaceMissing       = "namespace_missing"
	ServerDryRunClassNone                   = ""
)

// ServerDryRunReport records Kubernetes server-side dry-run behavior for a proposed manifest.
type ServerDryRunReport struct {
	Version      string               `json:"version"`
	FieldManager string               `json:"fieldManager"`
	Force        bool                 `json:"force"`
	Summary      ServerDryRunSummary  `json:"summary"`
	Results      []ServerDryRunResult `json:"results,omitempty"`
}

type ServerDryRunSummary struct {
	Total                   int `json:"total"`
	Passed                  int `json:"passed"`
	Failed                  int `json:"failed"`
	Skipped                 int `json:"skipped"`
	AdmissionDenied         int `json:"admissionDenied"`
	FieldOwnershipConflicts int `json:"fieldOwnershipConflicts"`
	ImmutableFields         int `json:"immutableFields"`
	NamespaceMissing        int `json:"namespaceMissing"`
	APIMapperMissing        int `json:"apiMapperMissing"`
}

type ServerDryRunResult struct {
	Resource   ServerDryRunResource `json:"resource"`
	Operation  string               `json:"operation"`
	Status     string               `json:"status"`
	ErrorClass string               `json:"errorClass,omitempty"`
	Reason     string               `json:"reason,omitempty"`
	Message    string               `json:"message,omitempty"`
	Hook       string               `json:"hook,omitempty"`
	DryRun     bool                 `json:"dryRun"`
}

type ServerDryRunResource struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// RunServerDryRunReport attempts a server-side apply dry-run for each object in the manifest.
func RunServerDryRunReport(ctx context.Context, client *kube.Client, proposedManifest string, opts ServerPlanOptions) (*ServerDryRunReport, error) {
	if client == nil || client.Dynamic == nil || client.RESTMapper == nil {
		return nil, fmt.Errorf("kube client missing dynamic/mapper")
	}
	if strings.TrimSpace(opts.FieldManager) == "" {
		opts.FieldManager = "torque-simulate"
	}
	report := &ServerDryRunReport{
		Version:      "v1",
		FieldManager: opts.FieldManager,
		Force:        opts.Force,
	}
	if strings.TrimSpace(proposedManifest) == "" {
		return report, nil
	}
	objs, err := parseManifestObjects(proposedManifest)
	if err != nil {
		return nil, err
	}
	for _, obj := range objs {
		result := runServerDryRunObject(ctx, client, obj, opts)
		report.Results = append(report.Results, result)
	}
	sort.SliceStable(report.Results, func(i, j int) bool {
		a, b := report.Results[i].Resource, report.Results[j].Resource
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
	report.Summary = summarizeServerDryRunResults(report.Results)
	return report, nil
}

func runServerDryRunObject(ctx context.Context, client *kube.Client, obj manifestObject, opts ServerPlanOptions) ServerDryRunResult {
	result := ServerDryRunResult{
		Resource: ServerDryRunResource{
			Group:     obj.Group,
			Version:   obj.Version,
			Kind:      obj.Kind,
			Namespace: obj.Namespace,
			Name:      obj.Name,
		},
		Operation: "server-side-apply",
		DryRun:    true,
	}
	if obj.IsHook {
		result.Status = ServerDryRunStatusSkipped
		result.Hook = obj.Hook
		result.Reason = "helm hook"
		result.Message = "Helm hook objects are not included in the apply dry-run."
		return result
	}
	if obj.Normalized == nil {
		result.Status = ServerDryRunStatusSkipped
		result.ErrorClass = ServerDryRunClassAPIError
		result.Reason = "empty object"
		result.Message = "Manifest object could not be normalized."
		return result
	}
	gvk := schema.GroupVersionKind{Group: obj.Group, Version: obj.Version, Kind: obj.Kind}
	if gvk.Kind == "" || gvk.Version == "" {
		result.Status = ServerDryRunStatusSkipped
		result.ErrorClass = ServerDryRunClassAPIMappingMissing
		result.Reason = "missing apiVersion or kind"
		result.Message = "Object has no complete Kubernetes type."
		return result
	}
	mapping, mapErr := client.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if mapErr != nil || mapping == nil {
		result.Status = ServerDryRunStatusSkipped
		result.ErrorClass = ServerDryRunClassAPIMappingMissing
		result.Reason = "mapping failed"
		if mapErr != nil {
			result.Message = mapErr.Error()
			if meta.IsNoMatchError(mapErr) {
				result.Reason = "no api match"
			}
		}
		return result
	}
	body, encErr := json.Marshal(obj.Normalized.Object)
	if encErr != nil {
		result.Status = ServerDryRunStatusFailed
		result.ErrorClass = ServerDryRunClassAPIError
		result.Reason = "encode object"
		result.Message = encErr.Error()
		return result
	}
	resource := client.Dynamic.Resource(mapping.Resource)
	var target dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := strings.TrimSpace(obj.Namespace)
		if ns == "" {
			ns = strings.TrimSpace(opts.DefaultNamespace)
		}
		if ns == "" {
			result.Status = ServerDryRunStatusFailed
			result.ErrorClass = ServerDryRunClassNamespaceMissing
			result.Reason = "namespace required"
			result.Message = "Namespaced resource has no namespace after rendering."
			return result
		}
		target = resource.Namespace(ns)
	} else {
		target = resource
	}
	force := opts.Force
	patchOpts := metav1.PatchOptions{
		FieldManager: opts.FieldManager,
		DryRun:       []string{metav1.DryRunAll},
	}
	if force {
		patchOpts.Force = &force
	}
	_, err := target.Patch(ctx, obj.Name, types.ApplyPatchType, body, patchOpts)
	if err == nil {
		result.Status = ServerDryRunStatusPassed
		return result
	}
	class, reason, message := ClassifyServerDryRunError(err, obj.Kind)
	result.Status = ServerDryRunStatusFailed
	result.ErrorClass = class
	result.Reason = reason
	result.Message = message
	return result
}

// ClassifyServerDryRunError converts Kubernetes API errors into simulation-proof classes.
func ClassifyServerDryRunError(err error, kind string) (class, reason, message string) {
	if err == nil {
		return ServerDryRunClassNone, "", ""
	}
	message = strings.TrimSpace(err.Error())
	reason = "api error"
	if isImmutableFieldError(err, kind) {
		return ServerDryRunClassImmutableField, "immutable field", message
	}
	if apierrors.IsConflict(err) {
		return ServerDryRunClassFieldOwnershipConflict, "field ownership conflict", message
	}
	if apierrors.IsNotFound(err) {
		return ServerDryRunClassNamespaceMissing, "object or namespace missing", message
	}
	if statusErr, ok := err.(*apierrors.StatusError); ok && statusErr != nil {
		if statusErr.ErrStatus.Reason != "" {
			reason = string(statusErr.ErrStatus.Reason)
		}
	}
	msg := strings.ToLower(message)
	if apierrors.IsForbidden(err) ||
		strings.Contains(msg, "admission webhook") ||
		strings.Contains(msg, "denied the request") ||
		strings.Contains(msg, "violates") {
		return ServerDryRunClassAdmissionDenied, strings.TrimSpace(reason), message
	}
	return ServerDryRunClassAPIError, strings.TrimSpace(reason), message
}

func summarizeServerDryRunResults(results []ServerDryRunResult) ServerDryRunSummary {
	var summary ServerDryRunSummary
	for _, result := range results {
		summary.Total++
		switch result.Status {
		case ServerDryRunStatusPassed:
			summary.Passed++
		case ServerDryRunStatusSkipped:
			summary.Skipped++
		default:
			summary.Failed++
		}
		switch result.ErrorClass {
		case ServerDryRunClassAdmissionDenied:
			summary.AdmissionDenied++
		case ServerDryRunClassFieldOwnershipConflict:
			summary.FieldOwnershipConflicts++
		case ServerDryRunClassImmutableField:
			summary.ImmutableFields++
		case ServerDryRunClassNamespaceMissing:
			summary.NamespaceMissing++
		case ServerDryRunClassAPIMappingMissing:
			summary.APIMapperMissing++
		}
	}
	return summary
}

package deploy

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestClassifyServerDryRunError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind string
		want string
	}{
		{
			name: "immutable service field",
			err: apierrors.NewInvalid(schema.GroupKind{Group: "", Kind: "Service"}, "api", field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("clusterIP"), "10.0.0.1", "field is immutable"),
			}),
			kind: "Service",
			want: ServerDryRunClassImmutableField,
		},
		{
			name: "field ownership conflict",
			err:  apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api", fmt.Errorf("conflict")),
			kind: "Deployment",
			want: ServerDryRunClassFieldOwnershipConflict,
		},
		{
			name: "admission webhook denial",
			err:  apierrors.NewForbidden(corev1.Resource("pods"), "api", fmt.Errorf("admission webhook denied the request")),
			kind: "Pod",
			want: ServerDryRunClassAdmissionDenied,
		},
		{
			name: "missing namespace",
			err:  apierrors.NewNotFound(corev1.Resource("namespaces"), "missing"),
			kind: "Deployment",
			want: ServerDryRunClassNamespaceMissing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason, message := ClassifyServerDryRunError(tt.err, tt.kind)
			if got != tt.want {
				t.Fatalf("class=%q reason=%q message=%q, want %q", got, reason, message, tt.want)
			}
			if reason == "" || message == "" {
				t.Fatalf("expected reason and message, got reason=%q message=%q", reason, message)
			}
		})
	}
}

func TestSummarizeServerDryRunResults(t *testing.T) {
	summary := summarizeServerDryRunResults([]ServerDryRunResult{
		{Status: ServerDryRunStatusPassed},
		{Status: ServerDryRunStatusSkipped, ErrorClass: ServerDryRunClassAPIMappingMissing},
		{Status: ServerDryRunStatusFailed, ErrorClass: ServerDryRunClassAdmissionDenied},
		{Status: ServerDryRunStatusFailed, ErrorClass: ServerDryRunClassFieldOwnershipConflict},
		{Status: ServerDryRunStatusFailed, ErrorClass: ServerDryRunClassImmutableField},
		{Status: ServerDryRunStatusFailed, ErrorClass: ServerDryRunClassNamespaceMissing},
	})
	if summary.Total != 6 || summary.Passed != 1 || summary.Skipped != 1 || summary.Failed != 4 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if summary.AdmissionDenied != 1 || summary.FieldOwnershipConflicts != 1 || summary.ImmutableFields != 1 || summary.NamespaceMissing != 1 || summary.APIMapperMissing != 1 {
		t.Fatalf("unexpected class counts: %#v", summary)
	}
}

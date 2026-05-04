// Package deployplan contains shared apply-plan manifest rendering helpers.
package deployplan

import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ResourceKey identifies a rendered Kubernetes object in plan artifacts.
type ResourceKey struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

func (k ResourceKey) String() string {
	scope := k.Namespace
	if scope == "" {
		scope = "cluster"
	}
	group := k.Group
	if group == "" {
		group = "core"
	}
	return fmt.Sprintf("%s/%s %s (%s)", scope, k.Name, k.Kind, group)
}

// ManifestDoc is a parsed Helm manifest document with source metadata.
type ManifestDoc struct {
	Key            ResourceKey
	Body           string
	Obj            *unstructured.Unstructured
	TemplateSource string
}

// ParseManifestDocs converts a Helm manifest blob into structured entries.
func ParseManifestDocs(manifest string) []ManifestDoc {
	files := releaseutil.SplitManifests(manifest)
	docs := make([]ManifestDoc, 0, len(files))
	for name, doc := range files {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" {
			continue
		}
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(trimmed), &obj); err != nil {
			continue
		}
		u := &unstructured.Unstructured{Object: obj}
		docs = append(docs, ManifestDoc{
			Key:            ToResourceKey(u),
			Body:           trimmed,
			Obj:            u,
			TemplateSource: PickTemplateSource(trimmed, name),
		})
	}
	return docs
}

// PickTemplateSource returns Helm's "# Source:" path when present.
func PickTemplateSource(manifestBody, fallback string) string {
	lines := strings.Split(manifestBody, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# Source:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# Source:"))
		}
		break
	}
	return fallback
}

// ToResourceKey converts an unstructured object into a stable plan key.
func ToResourceKey(obj *unstructured.Unstructured) ResourceKey {
	group := ""
	version := ""
	parts := strings.SplitN(obj.GetAPIVersion(), "/", 2)
	if len(parts) == 2 {
		group = parts[0]
		version = parts[1]
	} else if len(parts) == 1 {
		version = parts[0]
	}
	return ResourceKey{
		Group:     group,
		Version:   version,
		Kind:      obj.GetKind(),
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

// DocsToMap indexes parsed manifest docs by resource key.
func DocsToMap(docs []ManifestDoc) map[ResourceKey]ManifestDoc {
	result := make(map[ResourceKey]ManifestDoc, len(docs))
	for _, doc := range docs {
		if doc.Key.Name == "" || doc.Key.Kind == "" {
			continue
		}
		result[doc.Key] = doc
	}
	return result
}

// BuildManifestBlobs returns rendered manifest YAML keyed by graph node id.
func BuildManifestBlobs(desired map[ResourceKey]ManifestDoc) map[string]string {
	if len(desired) == 0 {
		return nil
	}
	out := make(map[string]string, len(desired))
	for key, doc := range desired {
		if doc.Body == "" {
			doc.Body = ObjectYAML(doc.Obj)
		}
		out[GraphNodeID(key)] = doc.Body
	}
	return out
}

// BuildManifestTemplateIndex returns Helm template source paths keyed by graph node id.
func BuildManifestTemplateIndex(desired map[ResourceKey]ManifestDoc) map[string]string {
	if len(desired) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, doc := range desired {
		if doc.TemplateSource == "" {
			continue
		}
		out[GraphNodeID(key)] = doc.TemplateSource
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildLiveManifestBlobs returns live object YAML keyed by graph node id.
func BuildLiveManifestBlobs(live map[ResourceKey]*unstructured.Unstructured) map[string]string {
	if len(live) == 0 {
		return nil
	}
	out := make(map[string]string, len(live))
	for key, obj := range live {
		if obj == nil {
			continue
		}
		out[GraphNodeID(key)] = ObjectYAML(obj.DeepCopy())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildManifestDiffs returns unified diffs between live and rendered manifest blobs.
func BuildManifestDiffs(live, rendered map[string]string) map[string]string {
	if len(live) == 0 || len(rendered) == 0 {
		return nil
	}
	diffs := make(map[string]string)
	for id, desired := range rendered {
		liveBody, ok := live[id]
		if !ok || strings.TrimSpace(liveBody) == "" {
			continue
		}
		if diff := DiffStrings(liveBody, desired); strings.TrimSpace(diff) != "" {
			diffs[id] = diff
		}
	}
	if len(diffs) == 0 {
		return nil
	}
	return diffs
}

// GraphNodeID returns the stable UI/data id for a resource key.
func GraphNodeID(key ResourceKey) string {
	ns := key.Namespace
	if ns == "" {
		ns = "cluster"
	}
	return fmt.Sprintf("%s|%s|%s", strings.ToLower(ns), strings.ToLower(key.Kind), strings.ToLower(key.Name))
}

// ObjectYAML serializes an object after stripping runtime-only fields.
func ObjectYAML(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	trimmed := TrimUnstructured(obj.DeepCopy())
	if trimmed == nil {
		return ""
	}
	data, err := yaml.Marshal(trimmed.Object)
	if err != nil {
		return ""
	}
	return string(data)
}

// TrimUnstructured strips runtime-only metadata before diffing.
func TrimUnstructured(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(obj.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	unstructured.RemoveNestedField(obj.Object, "metadata", "selfLink")
	unstructured.RemoveNestedField(obj.Object, "metadata", "generation")
	unstructured.RemoveNestedField(obj.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(obj.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	unstructured.RemoveNestedField(obj.Object, "status")
	return obj
}

// DiffStrings renders a unified diff from live/current YAML to desired YAML.
func DiffStrings(current, desired string) string {
	if current == desired {
		return ""
	}
	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(current),
		B:        difflib.SplitLines(desired),
		FromFile: "live",
		ToFile:   "desired",
		Context:  3,
	}
	diff, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return fmt.Sprintf("failed to render diff: %v", err)
	}
	return diff
}

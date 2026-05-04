package deployplan

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestManifestHelpersBuildSourceBlobsAndDiffs(t *testing.T) {
	manifest := `---
# Source: demo/templates/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-env
  namespace: demo
data:
  FOO: desired
`

	docs := ParseManifestDocs(manifest)
	if len(docs) != 1 {
		t.Fatalf("docs=%d want 1", len(docs))
	}
	if docs[0].TemplateSource != "demo/templates/configmap.yaml" {
		t.Fatalf("template source=%q", docs[0].TemplateSource)
	}

	desired := DocsToMap(docs)
	key := ResourceKey{Version: "v1", Kind: "ConfigMap", Namespace: "demo", Name: "web-env"}
	live := map[ResourceKey]*unstructured.Unstructured{
		key: {
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":            "web-env",
					"namespace":       "demo",
					"resourceVersion": "123",
				},
				"data": map[string]interface{}{"FOO": "live"},
			},
		},
	}

	templates := BuildManifestTemplateIndex(desired)
	id := GraphNodeID(key)
	if templates[id] != "demo/templates/configmap.yaml" {
		t.Fatalf("template index=%v", templates)
	}

	rendered := BuildManifestBlobs(desired)
	liveBlobs := BuildLiveManifestBlobs(live)
	if strings.Contains(liveBlobs[id], "resourceVersion") {
		t.Fatalf("live blob should strip resourceVersion: %s", liveBlobs[id])
	}
	diffs := BuildManifestDiffs(liveBlobs, rendered)
	if !strings.Contains(diffs[id], "FOO") {
		t.Fatalf("expected diff to mention data change, got %q", diffs[id])
	}
}

// File: cmd/torque/manifestutil.go
// Brief: Compatibility aliases for shared deploy plan manifest helpers.

package main

import (
	"github.com/ingresslabs/torque/internal/deployplan"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type resourceKey = deployplan.ResourceKey
type manifestDoc = deployplan.ManifestDoc

func parseManifestDocs(manifest string) []manifestDoc {
	return deployplan.ParseManifestDocs(manifest)
}

func docsToMap(docs []manifestDoc) map[resourceKey]manifestDoc {
	return deployplan.DocsToMap(docs)
}

func buildManifestBlobs(desired map[resourceKey]manifestDoc) map[string]string {
	return deployplan.BuildManifestBlobs(desired)
}

func buildManifestTemplateIndex(desired map[resourceKey]manifestDoc) map[string]string {
	return deployplan.BuildManifestTemplateIndex(desired)
}

func buildLiveManifestBlobs(live map[resourceKey]*unstructured.Unstructured) map[string]string {
	return deployplan.BuildLiveManifestBlobs(live)
}

func buildManifestDiffs(live, rendered map[string]string) map[string]string {
	return deployplan.BuildManifestDiffs(live, rendered)
}

func graphNodeID(key resourceKey) string {
	return deployplan.GraphNodeID(key)
}

func objectYAML(obj *unstructured.Unstructured) string {
	return deployplan.ObjectYAML(obj)
}

func diffStrings(current, desired string) string {
	return deployplan.DiffStrings(current, desired)
}

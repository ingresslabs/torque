package config

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestToMapInfersKindAndAPIVersionForTypedObjects(t *testing.T) {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blocked-config",
			Namespace: "proof-ns",
		},
		Data: map[string]string{
			"apiKey": "redacted",
		},
	}

	m, err := toMap(obj)
	if err != nil {
		t.Fatalf("toMap: %v", err)
	}
	if got := m["kind"]; got != "ConfigMap" {
		t.Fatalf("kind=%v, want ConfigMap", got)
	}
	if got := m["apiVersion"]; got != "v1" {
		t.Fatalf("apiVersion=%v, want v1", got)
	}
	meta, ok := m["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type=%T, want map[string]any", m["metadata"])
	}
	if got := meta["name"]; got != "blocked-config" {
		t.Fatalf("metadata.name=%v, want blocked-config", got)
	}
}

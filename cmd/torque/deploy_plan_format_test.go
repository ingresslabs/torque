package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/capture"
	"github.com/ingresslabs/torque/internal/verify"
)

func TestResolveDeployPlanFormat(t *testing.T) {
	cases := []struct {
		name      string
		format    string
		visualize bool
		want      string
	}{
		{name: "default text", format: "", visualize: false, want: "text"},
		{name: "explicit text", format: "text", visualize: false, want: "text"},
		{name: "markdown", format: "markdown", visualize: false, want: "markdown"},
		{name: "markdown shorthand", format: "md", visualize: false, want: "markdown"},
		{name: "visualize defaults to html", format: "html", visualize: true, want: "visualize-html"},
		{name: "visualize with empty format stays html", format: "", visualize: true, want: "text"},
		{name: "visualize with text stays text", format: "text", visualize: true, want: "text"},
		{name: "visualize with json", format: "json", visualize: true, want: "visualize-json"},
		{name: "visualize with yaml", format: "yaml", visualize: true, want: "visualize-yaml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDeployPlanFormat(tc.format, tc.visualize)
			if got != tc.want {
				t.Fatalf("resolveDeployPlanFormat(%q, %v)=%q, want %q", tc.format, tc.visualize, got, tc.want)
			}
		})
	}
}

func TestRenderDeployPlanMarkdown(t *testing.T) {
	result := &deployPlanResult{
		ReleaseName: "web",
		Namespace:   "prod",
		ChartRef:    "./chart",
		Changes: []planResourceChange{
			{
				Key:  resourceKey{Kind: "Deployment", Namespace: "prod", Name: "web"},
				Kind: changeUpdate,
				Diff: "--- old\n+++ new\n@@\n-image: app:old\n+image: app@sha256:abc\n",
			},
		},
		Summary: planSummary{Updates: 1},
		Images: []planImageRef{
			{
				Resource:  "Deployment/prod/web",
				Container: "app",
				Image:     "ghcr.io/acme/app@sha256:abc",
				Digest:    "sha256:abc",
				Pinned:    true,
			},
		},
		ManifestDiffs: map[string]string{
			"Deployment/prod/web": "--- old\n+++ new\n",
		},
	}

	got := renderDeployPlanMarkdown(result, true)
	for _, want := range []string{
		"<!-- torque apply plan: release=web namespace=prod -->",
		"## torque apply plan: `web`",
		"`helm rollback web -n prod`",
		"### Images",
		"`ghcr.io/acme/app@sha256:abc`",
		"### Manifest Diffs",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected markdown to contain %q, got:\n%s", want, got)
		}
	}
}

func TestCollectPlanImages(t *testing.T) {
	docs := parseManifestDocs(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
spec:
  template:
    spec:
      initContainers:
      - name: migrate
        image: ghcr.io/acme/migrate:latest
      containers:
      - name: app
        image: ghcr.io/acme/app@sha256:abc
`)
	images := collectPlanImages(docsToMap(docs))
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d: %#v", len(images), images)
	}
	if images[0].Image != "ghcr.io/acme/app@sha256:abc" || !images[0].Pinned {
		t.Fatalf("expected pinned app image first, got %#v", images[0])
	}
	if images[1].Container != "init/migrate" || images[1].Pinned {
		t.Fatalf("expected unpinned init image second, got %#v", images[1])
	}
}

func TestLoadPlanVerifyReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verify.json")
	rep := verify.Report{
		Tool:    "verifier",
		Mode:    verify.ModeBlock,
		Passed:  false,
		Blocked: true,
		Inputs: []verify.Input{{
			Kind:           "chart",
			RenderedSHA256: "sha256:rendered",
		}},
		Summary: verify.Summary{
			Total:   1,
			BySev:   map[verify.Severity]int{verify.SeverityHigh: 1},
			Blocked: true,
		},
		Findings: []verify.Finding{{
			RuleID:   "k8s/no-latest",
			Severity: verify.SeverityHigh,
			Message:  "image tag is mutable",
		}},
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadPlanVerifyReports([]string{path}, "sha256:rendered")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Blocked || !got[0].RenderedSHA256Matches {
		t.Fatalf("unexpected report: %#v", got)
	}
}

func TestLoadPlanBuildProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build.sqlite")
	rec, err := capture.Open(path, capture.SessionMeta{
		Command:   "torque build",
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordArtifact(context.Background(), "build.digest", "sha256:abc"); err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordArtifact(context.Background(), "build.tags_json", `["ghcr.io/acme/app:dev"]`); err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordArtifact(context.Background(), "build.policy_post_report_json", `{"passed":true,"denyCount":0,"warnCount":1}`); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := loadPlanBuildProvenance(context.Background(), []string{path}, []planImageRef{{Digest: "sha256:abc", Pinned: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Verdict != "safe" || !got[0].Referenced {
		t.Fatalf("unexpected provenance: %#v", got)
	}
}

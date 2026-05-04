package capture

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummarizeDetectsFailedDeployCapture(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "apply.sqlite")
	rec, err := Open(dbPath, SessionMeta{
		Command:   "ktl apply",
		Args:      []string{"apply", "--chart", "./chart", "--release", "api", "-n", "prod"},
		StartedAt: time.Now().UTC(),
		Entities: Entities{
			KubeContext: "prod-context",
			Namespace:   "prod",
			Release:     "api",
			Chart:       "./chart",
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := rec.RecordEvent(context.Background(), EventMeta{
		Kind:      "deploy",
		Level:     "error",
		Source:    "summary",
		Namespace: "prod",
		Message:   "upgrade failed: deployment api timed out",
	}, map[string]any{
		"summary": map[string]any{
			"status": "failed",
			"error":  "deployment api timed out",
		},
	}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := rec.RecordArtifact(context.Background(), "apply.status", "failed"); err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	summary, err := Summarize(context.Background(), dbPath, SummaryOptions{})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(summary.Sessions) != 1 {
		t.Fatalf("sessions=%d", len(summary.Sessions))
	}
	sess := summary.Sessions[0]
	if sess.Outcome != "failed" {
		t.Fatalf("Outcome=%q", sess.Outcome)
	}
	if len(sess.FailureHints) == 0 || !strings.Contains(sess.FailureHints[0].Message, "timed out") {
		t.Fatalf("expected failure hint with timeout, got %#v", sess.FailureHints)
	}
	if sess.PrimaryCause == nil || sess.PrimaryCause.Category != "rollout_timeout" {
		t.Fatalf("expected rollout timeout primary cause, got %#v", sess.PrimaryCause)
	}
	if !containsString(sess.Suggestions, "ktl revert --release api -n prod") {
		t.Fatalf("expected revert suggestion, got %#v", sess.Suggestions)
	}
	if sess.RollbackCommand != "ktl revert --release api -n prod" {
		t.Fatalf("RollbackCommand=%q", sess.RollbackCommand)
	}
	if sess.ApplyStatus != "failed" {
		t.Fatalf("ApplyStatus=%q", sess.ApplyStatus)
	}
}

func TestSummarizeReadsBuildEvidence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "build.sqlite")
	rec, err := Open(dbPath, SessionMeta{
		Command:   "ktl build",
		Args:      []string{"build", "--context", ".", "--tag", "ghcr.io/acme/api:dev"},
		StartedAt: time.Now().UTC(),
		Entities:  Entities{BuildContext: "."},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for name, text := range map[string]string{
		"build.digest":                  "sha256:abc",
		"build.tags_json":               `["ghcr.io/acme/api:dev"]`,
		"build.policy_post_report_json": `{"passed":true,"denyCount":0,"warnCount":1}`,
	} {
		if err := rec.RecordArtifact(context.Background(), name, text); err != nil {
			t.Fatalf("RecordArtifact(%s): %v", name, err)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	summary, err := Summarize(context.Background(), dbPath, SummaryOptions{})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	sess := summary.Sessions[0]
	if sess.BuildDigest != "sha256:abc" {
		t.Fatalf("BuildDigest=%q", sess.BuildDigest)
	}
	if len(sess.BuildTags) != 1 || sess.BuildTags[0] != "ghcr.io/acme/api:dev" {
		t.Fatalf("BuildTags=%#v", sess.BuildTags)
	}
	if sess.BuildPolicy == nil || sess.BuildPolicy.WarnCount != 1 {
		t.Fatalf("BuildPolicy=%#v", sess.BuildPolicy)
	}
	if sess.PrimaryCause != nil {
		t.Fatalf("did not expect primary cause for warn-only policy, got %#v", sess.PrimaryCause)
	}
	if sess.Outcome != "completed_with_warnings" {
		t.Fatalf("Outcome=%q", sess.Outcome)
	}
}

func TestSummarizeClassifiesResourceStatus(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "apply.sqlite")
	rec, err := Open(dbPath, SessionMeta{
		Command:   "ktl apply",
		Args:      []string{"apply", "--chart", "./chart", "--release", "api", "-n", "prod"},
		StartedAt: time.Now().UTC(),
		Entities:  Entities{Namespace: "prod", Release: "api", Chart: "./chart"},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := rec.RecordEvent(context.Background(), EventMeta{
		Kind:    "deploy",
		Source:  "resources",
		Message: "resource update",
	}, map[string]any{
		"resources": []map[string]any{
			{
				"kind":      "Pod",
				"namespace": "prod",
				"name":      "api-123",
				"status":    "Pending",
				"reason":    "ImagePullBackOff",
				"message":   "failed to pull image ghcr.io/acme/api:missing",
			},
		},
	}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	summary, err := Summarize(context.Background(), dbPath, SummaryOptions{})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	sess := summary.Sessions[0]
	if sess.PrimaryCause == nil || sess.PrimaryCause.Category != "image_pull" {
		t.Fatalf("PrimaryCause=%#v", sess.PrimaryCause)
	}
	if len(sess.ResourceHints) == 0 || sess.ResourceHints[0].Resource != "Pod/prod/api-123" {
		t.Fatalf("ResourceHints=%#v", sess.ResourceHints)
	}
	if sess.LogsCommand != "ktl logs deploy/api -n prod --events" {
		t.Fatalf("LogsCommand=%q", sess.LogsCommand)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

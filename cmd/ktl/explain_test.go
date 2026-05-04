package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/ktl/internal/capture"
)

func TestExplainCommandPrintsCaptureSummary(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	dbPath := filepath.Join(t.TempDir(), "apply.sqlite")
	rec, err := capture.Open(dbPath, capture.SessionMeta{
		Command:   "ktl apply",
		Args:      []string{"apply", "--chart", "./chart", "--release", "api", "-n", "prod"},
		StartedAt: time.Now().UTC(),
		Entities: capture.Entities{
			Namespace: "prod",
			Release:   "api",
			Chart:     "./chart",
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := rec.RecordEvent(context.Background(), capture.EventMeta{
		Kind:      "deploy",
		Level:     "error",
		Source:    "summary",
		Namespace: "prod",
		Message:   "upgrade failed",
	}, map[string]any{"summary": map[string]any{"status": "failed", "error": "upgrade failed"}}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"explain", dbPath})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Capture:",
		"Outcome: failed",
		"upgrade failed",
		"ktl revert --release api -n prod",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestExplainCommandJSONFormat(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	dbPath := filepath.Join(t.TempDir(), "logs.sqlite")
	rec, err := capture.Open(dbPath, capture.SessionMeta{
		Command:   "ktl logs",
		Args:      []string{"logs", "deploy/api"},
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"explain", dbPath, "--format", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := out.String(); !strings.Contains(got, `"sessions"`) || !strings.Contains(got, `"command": "ktl logs"`) {
		t.Fatalf("expected JSON summary, got:\n%s", got)
	}
}

func TestExplainCommandMarkdownFormat(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	dbPath := filepath.Join(t.TempDir(), "apply.sqlite")
	rec, err := capture.Open(dbPath, capture.SessionMeta{
		Command:   "ktl apply",
		Args:      []string{"apply", "--chart", "./chart", "--release", "api", "-n", "prod"},
		StartedAt: time.Now().UTC(),
		Entities:  capture.Entities{Namespace: "prod", Release: "api", Chart: "./chart"},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := rec.RecordEvent(context.Background(), capture.EventMeta{
		Kind:    "deploy",
		Level:   "error",
		Source:  "summary",
		Message: "deployment api timed out",
	}, map[string]any{"summary": map[string]any{"status": "failed", "error": "deployment api timed out"}}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"explain", dbPath, "--format", "markdown"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"# ktl explain", "Primary cause", "rollout_timeout", "ktl revert --release api -n prod"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected markdown to contain %q, got:\n%s", want, got)
		}
	}
}

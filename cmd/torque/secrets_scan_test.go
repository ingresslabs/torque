package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretsScanRepoWritesRedactedReport(t *testing.T) {
	dir := t.TempDir()
	rawSecret := "AKIA1234567890ABCDEF"
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("awsAccessKey: "+rawSecret+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	report := filepath.Join(dir, "secrets.json")
	root := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"secrets", "scan",
		"--scope", "repo",
		"--root", dir,
		"--report", report,
		"--format", "json",
		"--mode", "block",
		"--fail-on", "high",
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "secret scan blocked") {
		t.Fatalf("expected secret scan block, err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, rawSecret) {
		t.Fatalf("report leaked raw secret:\n%s", text)
	}
	if !strings.Contains(text, "secret/value_aws_access_key") {
		t.Fatalf("report missing AWS finding:\n%s", text)
	}
}

func TestSecretsScanRenderManifestAllowsSecretRefs(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "rendered.yaml")
	if err := os.WriteFile(manifest, []byte(`
apiVersion: v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:1.27
          env:
            - name: API_TOKEN
              value: secret://vault/prod/api#token
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	root := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"secrets", "scan",
		"--scope", "render",
		"--manifest", manifest,
		"--format", "json",
		"--mode", "block",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("scan render: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"secretReferences": 1`) {
		t.Fatalf("expected secret reference count, got:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "vault/prod/api") {
		t.Fatalf("report should not echo full secret reference path:\n%s", stdout.String())
	}
}

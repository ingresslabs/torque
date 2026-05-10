package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifierSecurityProfileWritesSecretsReportAndEvidence(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(cwd, "..", ".."))
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	dir := t.TempDir()
	rawSecret := "AKIA1234567890ABCDEF"
	manifest := filepath.Join(dir, "rendered.yaml")
	if err := os.WriteFile(manifest, []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: prod
data:
  awsAccessKey: AKIA1234567890ABCDEF
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	verifyReport := filepath.Join(dir, "verify.json")
	secretsReport := filepath.Join(dir, "secrets.json")
	evidenceDir := filepath.Join(dir, "evidence")

	cmd := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--manifest", manifest,
		"--security-profile", "enterprise",
		"--security-boundary-matrix",
		"--secrets-report", secretsReport,
		"--security-evidence", evidenceDir,
		"--mode", "warn",
		"--format", "json",
		"--report", verifyReport,
	})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "verify blocked") {
		t.Fatalf("expected secret gate to block, err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	for _, path := range []string{verifyReport, secretsReport, filepath.Join(evidenceDir, "manifest.json"), filepath.Join(evidenceDir, "boundary.matrix.json"), filepath.Join(evidenceDir, "redaction.proof.json")} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		if strings.Contains(text, rawSecret) {
			t.Fatalf("%s leaked raw secret:\n%s", path, text)
		}
		if path == secretsReport && !strings.Contains(text, "secret/value_aws_access_key") {
			t.Fatalf("secrets report missing secret rule:\n%s", text)
		}
	}
}

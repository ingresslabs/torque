package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanDockerfileForSecretsScansComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM busybox:1.36\n# -----BEGIN PRIVATE KEY-----\nRUN true\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	rules, err := CompileConfig(DefaultConfig())
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	findings, err := ScanDockerfileForSecretsWithRules(path, rules)
	if err != nil {
		t.Fatalf("ScanDockerfileForSecrets: %v", err)
	}
	if !containsRule(findings, "arg_value_private_key") {
		t.Fatalf("expected private-key finding, got %#v", findings)
	}
}

func containsRule(findings []Finding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}

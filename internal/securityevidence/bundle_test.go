package securityevidence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/verify"
)

func TestWriteBundleDoesNotStoreRawSecrets(t *testing.T) {
	dir := t.TempDir()
	rawSecret := "AKIA1234567890ABCDEF"
	secretReport := &verify.SecretScanReport{
		Version:     "v1",
		Tool:        "torque-secrets",
		Mode:        verify.ModeBlock,
		FailOn:      verify.SeverityHigh,
		Passed:      false,
		Blocked:     true,
		EvaluatedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		Summary: verify.SecretScanSummary{
			Total:           1,
			RawSecretStored: false,
		},
		Findings: []verify.Finding{{
			RuleID:   "secret/value_aws_access_key",
			Severity: verify.SeverityCritical,
			Message:  "AWS access key-like value detected",
			Observed: "AKI...DEF",
		}},
		BoundaryMatrix: &verify.SecurityBoundaryMatrix{
			Version: "v1",
			Passed:  true,
			Rows: []verify.SecurityBoundaryMatrixRow{{
				Surface:      "ConfigMap.data",
				Boundary:     "blocked",
				Status:       "blocked",
				Passed:       true,
				Present:      true,
				FindingCount: 1,
			}},
		},
		RedactionProof: verify.RedactionProof{
			Surfaces: []verify.RedactionSurfaceProof{{
				Surface:         "verifier.report",
				RawSecretStored: false,
			}},
		},
	}
	verifyReport := &verify.Report{
		Tool:    "torque-verify",
		Engine:  verify.EngineMeta{Name: "builtin", Ruleset: "builtin@test"},
		Mode:    verify.ModeBlock,
		Blocked: true,
		Findings: []verify.Finding{{
			RuleID:   "secret/value_aws_access_key",
			Severity: verify.SeverityCritical,
			Message:  "AWS access key-like value detected",
			Observed: "AKI...DEF",
		}},
	}
	if err := WriteBundle(BundleOptions{
		Dir:                  dir,
		GeneratedAt:          time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		RenderedManifestHash: "sha256:test",
	}, verifyReport, secretReport); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	for _, name := range []string{"manifest.json", "secrets.report.json", "boundary.matrix.json", "verifier.report.json", "redaction.proof.json", filepath.Join("reports", "security.md")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected bundle file %s: %v", name, err)
		}
	}
	if err := AssertNoRawSecrets(dir, []string{rawSecret}); err != nil {
		t.Fatalf("raw secret leaked: %v", err)
	}
}

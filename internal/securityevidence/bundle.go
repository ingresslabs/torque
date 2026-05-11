package securityevidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/verify"
)

type BundleManifest struct {
	Version              string    `json:"version"`
	GeneratedAt          time.Time `json:"generatedAt"`
	Tool                 string    `json:"tool"`
	Ruleset              string    `json:"ruleset,omitempty"`
	DetectorVersion      string    `json:"detectorVersion"`
	RenderedManifestHash string    `json:"renderedManifestSha256,omitempty"`
	VerifierFindings     int       `json:"verifierFindings"`
	SecretFindings       int       `json:"secretFindings"`
	RedactionEnabled     bool      `json:"redactionEnabled"`
	RawSecretStored      bool      `json:"rawSecretStored"`
	Blocked              bool      `json:"blocked"`
}

type BundleOptions struct {
	Dir                  string
	GeneratedAt          time.Time
	RenderedManifestHash string
}

func WriteBundle(opts BundleOptions, verifierReport *verify.Report, secretsReport *verify.SecretScanReport) error {
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(dir, "reports"), 0o755); err != nil {
		return err
	}
	generatedAt := opts.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	manifest := BundleManifest{
		Version:              "v1",
		GeneratedAt:          generatedAt,
		Tool:                 "torque-security-evidence",
		DetectorVersion:      "builtin-secrets@v1",
		RenderedManifestHash: strings.TrimSpace(opts.RenderedManifestHash),
		RedactionEnabled:     true,
		RawSecretStored:      false,
	}
	if verifierReport != nil {
		manifest.Ruleset = verifierReport.Engine.Ruleset
		manifest.VerifierFindings = len(verifierReport.Findings)
		manifest.Blocked = verifierReport.Blocked
	}
	if secretsReport != nil {
		manifest.SecretFindings = len(secretsReport.Findings)
		manifest.RawSecretStored = secretsReport.Summary.RawSecretStored
		if secretsReport.Blocked {
			manifest.Blocked = true
		}
	}
	if err := writeJSON(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		return err
	}
	if secretsReport != nil {
		if err := writeJSON(filepath.Join(dir, "secrets.report.json"), secretsReport); err != nil {
			return err
		}
		if secretsReport.BoundaryMatrix != nil {
			if err := writeJSON(filepath.Join(dir, "boundary.matrix.json"), secretsReport.BoundaryMatrix); err != nil {
				return err
			}
		}
		if secretsReport.FlowGraph != nil {
			if err := writeJSON(filepath.Join(dir, "secret.flow.graph.json"), secretsReport.FlowGraph); err != nil {
				return err
			}
		}
		if err := writeJSON(filepath.Join(dir, "redaction.proof.json"), secretsReport.RedactionProof); err != nil {
			return err
		}
	}
	if verifierReport != nil {
		if err := writeJSON(filepath.Join(dir, "verifier.report.json"), verifierReport); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "reports", "security.md"), []byte(renderMarkdown(manifest, verifierReport, secretsReport)), 0o644)
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func renderMarkdown(manifest BundleManifest, verifierReport *verify.Report, secretsReport *verify.SecretScanReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Torque Security Evidence\n\n")
	fmt.Fprintf(&b, "- Generated: `%s`\n", manifest.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Blocked: `%t`\n", manifest.Blocked)
	fmt.Fprintf(&b, "- Verifier findings: `%d`\n", manifest.VerifierFindings)
	fmt.Fprintf(&b, "- Secret findings: `%d`\n", manifest.SecretFindings)
	fmt.Fprintf(&b, "- Raw secret stored: `%t`\n", manifest.RawSecretStored)
	if manifest.RenderedManifestHash != "" {
		fmt.Fprintf(&b, "- Rendered manifest SHA256: `%s`\n", manifest.RenderedManifestHash)
	}
	if secretsReport != nil && len(secretsReport.Findings) > 0 {
		fmt.Fprintf(&b, "\n## Secret Findings\n\n")
		for _, f := range secretsReport.Findings {
			fmt.Fprintf(&b, "- `%s` `%s` %s (%s)\n", f.Severity, f.RuleID, f.Message, firstNonEmpty(f.ResourceKey, f.Location, f.Path))
		}
	}
	if secretsReport != nil && secretsReport.BoundaryMatrix != nil {
		fmt.Fprintf(&b, "\n## Security Boundary Matrix\n\n")
		fmt.Fprintf(&b, "| Surface | Boundary | Status | Findings |\n")
		fmt.Fprintf(&b, "| --- | --- | --- | ---: |\n")
		for _, row := range secretsReport.BoundaryMatrix.Rows {
			fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%d` |\n", row.Surface, row.Boundary, row.Status, row.FindingCount)
		}
	}
	if secretsReport != nil && secretsReport.FlowGraph != nil {
		fmt.Fprintf(&b, "\n## Secret Flow Graph\n\n")
		fmt.Fprintf(&b, "- Nodes: `%d`\n", secretsReport.FlowGraph.Summary.Nodes)
		fmt.Fprintf(&b, "- Edges: `%d`\n", secretsReport.FlowGraph.Summary.Edges)
		fmt.Fprintf(&b, "- Values sources: `%d`\n", secretsReport.FlowGraph.Summary.ValuesSources)
		fmt.Fprintf(&b, "- Template sources: `%d`\n", secretsReport.FlowGraph.Summary.TemplateSources)
		fmt.Fprintf(&b, "- Rendered objects: `%d`\n", secretsReport.FlowGraph.Summary.RenderedObjects)
		fmt.Fprintf(&b, "- Live objects: `%d`\n", secretsReport.FlowGraph.Summary.LiveObjects)
		fmt.Fprintf(&b, "- Forbidden flows: `%d`\n", secretsReport.FlowGraph.Summary.ForbiddenFlows)
		fmt.Fprintf(&b, "- Secret references: `%d`\n", secretsReport.FlowGraph.Summary.SecretReferences)
		fmt.Fprintf(&b, "- Raw secret stored: `%t`\n", secretsReport.FlowGraph.Summary.RawSecretStored)
	}
	if verifierReport != nil && len(verifierReport.Findings) > 0 {
		fmt.Fprintf(&b, "\n## Verifier Findings\n\n")
		for _, f := range verifierReport.Findings {
			fmt.Fprintf(&b, "- `%s` `%s` %s (%s)\n", f.Severity, f.RuleID, f.Message, firstNonEmpty(f.ResourceKey, f.Location, f.Path))
		}
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "-"
}

package verify

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ingresslabs/torque/internal/secrets"
)

type SecretScanOptions struct {
	Mode           Mode
	FailOn         Severity
	Profile        string
	Source         string
	Stage          string
	Surface        string
	BoundaryMatrix bool
	FlowGraph      bool
	TargetKind     string
	ValuesFiles    []string
	RenderedPath   string
	RenderedSource string
	EvaluatedAt    time.Time
}

type SecretTextInput struct {
	Path    string
	Content string
	Stage   string
}

type SecretScanReport struct {
	Version        string                  `json:"version"`
	Tool           string                  `json:"tool"`
	Mode           Mode                    `json:"mode"`
	FailOn         Severity                `json:"failOn,omitempty"`
	Profile        string                  `json:"profile,omitempty"`
	Passed         bool                    `json:"passed"`
	Blocked        bool                    `json:"blocked"`
	EvaluatedAt    time.Time               `json:"evaluatedAt"`
	Source         string                  `json:"source,omitempty"`
	Summary        SecretScanSummary       `json:"summary"`
	Findings       []Finding               `json:"findings,omitempty"`
	BoundaryMatrix *SecurityBoundaryMatrix `json:"boundaryMatrix,omitempty"`
	FlowGraph      *SecretFlowGraph        `json:"flowGraph,omitempty"`
	RedactionProof RedactionProof          `json:"redactionProof"`
}

type SecretScanSummary struct {
	Total                   int              `json:"total"`
	BySeverity              map[Severity]int `json:"bySeverity,omitempty"`
	SecretReferences        int              `json:"secretReferences"`
	AllowedMaterializations int              `json:"allowedMaterializations"`
	RedactedMatches         int              `json:"redactedMatches"`
	RawSecretStored         bool             `json:"rawSecretStored"`
}

type RedactionProof struct {
	Surfaces     []RedactionSurfaceProof `json:"surfaces,omitempty"`
	FailedClosed bool                    `json:"failedClosed"`
}

type RedactionSurfaceProof struct {
	Surface         string               `json:"surface"`
	Matches         []RedactionRuleMatch `json:"matches,omitempty"`
	RawSecretStored bool                 `json:"rawSecretStored"`
}

type RedactionRuleMatch struct {
	RuleID string `json:"ruleId"`
	Count  int    `json:"count"`
}

type secretScanState struct {
	opts          SecretScanOptions
	rules         secrets.CompiledRules
	findings      []Finding
	refs          int
	allowed       int
	matchCounts   map[string]int
	seenFindings  map[string]struct{}
	flowEvents    []secretFlowEvent
	evaluatedAt   time.Time
	sourceDefault string
}

type secretCandidate struct {
	value      string
	key        string
	fieldPath  string
	location   string
	stage      string
	source     string
	subject    Subject
	resource   string
	kind       string
	template   string
	allowed    bool
	reference  bool
	decoded    bool
	sourceLine int
}

type secretMatch struct {
	ruleID     string
	message    string
	match      string
	severity   Severity
	confidence float64
	detector   string
	kind       string
}

type secretFlowEvent struct {
	Kind         string
	Stage        string
	Source       string
	Line         int
	Resource     string
	ResourceKind string
	Subject      Subject
	FieldPath    string
	Location     string
	Template     string
	RuleID       string
	Detector     string
	Preview      string
	Fingerprint  string
	Boundary     string
	Surface      string
	TargetKind   string
	RawStored    bool
}

func ScanRenderedSecrets(objects []map[string]any, opts SecretScanOptions) (*SecretScanReport, error) {
	state, err := newSecretScanState(opts)
	if err != nil {
		return nil, err
	}
	if state.opts.Mode == ModeOff {
		report := state.report()
		if opts.BoundaryMatrix {
			report.BoundaryMatrix = BuildSecurityBoundaryMatrix(objects, report, opts)
		}
		return report, nil
	}
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		subj := subjectFromObject(obj)
		kind := strings.TrimSpace(subj.Kind)
		template := strings.TrimSpace(toString(obj["__torque_source"]))
		stage := firstNonEmptyString(opts.Stage, "render")
		if strings.EqualFold(strings.TrimSpace(opts.TargetKind), "namespace") {
			stage = "live"
		}
		state.walkObject(obj, nil, secretCandidate{
			stage:    stage,
			source:   firstNonEmptyString(template, opts.RenderedPath, opts.Source, "rendered manifest"),
			subject:  subj,
			resource: resourceKey(subj),
			kind:     kind,
			template: template,
		})
	}
	report := state.report()
	if opts.BoundaryMatrix {
		report.BoundaryMatrix = BuildSecurityBoundaryMatrix(objects, report, opts)
	}
	return report, nil
}

func ScanTextSecrets(inputs []SecretTextInput, opts SecretScanOptions) (*SecretScanReport, error) {
	state, err := newSecretScanState(opts)
	if err != nil {
		return nil, err
	}
	if state.opts.Mode == ModeOff {
		return state.report(), nil
	}
	for _, input := range inputs {
		stage := firstNonEmptyString(input.Stage, opts.Stage, "source")
		source := firstNonEmptyString(input.Path, opts.Source, "source")
		lines := strings.Split(strings.ReplaceAll(input.Content, "\r\n", "\n"), "\n")
		for i, line := range lines {
			key, value := splitPotentialKeyValue(line)
			if strings.TrimSpace(value) == "" {
				value = line
			}
			state.scanCandidate(secretCandidate{
				value:      value,
				key:        key,
				fieldPath:  key,
				location:   fmt.Sprintf("%s:%d", source, i+1),
				stage:      stage,
				source:     source,
				sourceLine: i + 1,
			})
		}
	}
	return state.report(), nil
}

func WriteSecretScanReport(w io.Writer, report *SecretScanReport) error {
	if w == nil || report == nil {
		return nil
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, _ = w.Write(raw)
	return nil
}

func RenderSecretScanText(w io.Writer, report *SecretScanReport) {
	if w == nil || report == nil {
		return
	}
	fmt.Fprintf(w, "Secret findings: %d (blocked=%v)\n", report.Summary.Total, report.Blocked)
	if report.Profile != "" {
		fmt.Fprintf(w, "Profile: %s\n", report.Profile)
	}
	fmt.Fprintf(w, "Secret references: %d\n", report.Summary.SecretReferences)
	if len(report.Findings) == 0 {
		return
	}
	for _, f := range report.Findings {
		target := firstNonEmptyString(f.ResourceKey, f.Location, f.Path, "-")
		fmt.Fprintf(w, "- [%s] %s: %s (%s)\n", strings.ToUpper(string(f.Severity)), f.RuleID, f.Message, target)
	}
}

func newSecretScanState(opts SecretScanOptions) (*secretScanState, error) {
	if strings.TrimSpace(string(opts.Mode)) == "" {
		opts.Mode = ModeWarn
	}
	if strings.TrimSpace(string(opts.FailOn)) == "" {
		opts.FailOn = SeverityHigh
	}
	if strings.TrimSpace(opts.Profile) == "enterprise" {
		opts.Mode = ModeBlock
		if strings.TrimSpace(string(opts.FailOn)) == "" {
			opts.FailOn = SeverityHigh
		}
	}
	if strings.TrimSpace(opts.Surface) == "" {
		opts.Surface = "verifier.report"
	}
	now := opts.EvaluatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rules, err := secrets.CompileConfig(secrets.DefaultConfig())
	if err != nil {
		return nil, err
	}
	return &secretScanState{
		opts:          opts,
		rules:         rules,
		matchCounts:   map[string]int{},
		seenFindings:  map[string]struct{}{},
		evaluatedAt:   now.UTC(),
		sourceDefault: firstNonEmptyString(opts.Source, "input"),
	}, nil
}

func (s *secretScanState) walkObject(value any, path []string, base secretCandidate) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			next := append(append([]string{}, path...), k)
			s.walkObject(typed[k], next, base)
		}
	case []any:
		for i, item := range typed {
			next := append(append([]string{}, path...), fmt.Sprintf("[%d]", i))
			s.walkObject(item, next, base)
		}
	case string:
		fieldPath := renderFieldPath(path)
		c := base
		c.value = typed
		c.key = lastPathKey(path)
		c.fieldPath = fieldPath
		c.location = fieldPath
		c.reference = isAllowedSecretReferencePath(path)
		c.allowed = c.reference || isAllowedSecretMaterial(base.kind, path)
		s.scanCandidate(c)
	default:
		// Scalars other than strings are not secret material.
	}
}

func (s *secretScanState) scanCandidate(c secretCandidate) {
	value := strings.TrimSpace(c.value)
	if value == "" {
		return
	}
	if strings.HasPrefix(value, "secret://") {
		s.refs++
		s.addFlowEvent("secret_reference", c, secretMatch{
			ruleID:   "secret/reference",
			match:    value,
			detector: "secret_reference",
			kind:     "reference",
		}, "allowed", "secret://<redacted>", "")
		return
	}
	if c.allowed {
		if c.reference {
			s.refs++
			s.addFlowEvent("secret_reference", c, secretMatch{
				ruleID:   "secret/kubernetes_reference",
				match:    value,
				detector: "kubernetes_secret_reference",
				kind:     "reference",
			}, "allowed", "<kubernetes-secret-reference>", "")
			return
		}
		s.allowed++
		s.countAllowedMaterial(value, c)
		s.addFlowEvent("allowed_materialization", c, secretMatch{
			ruleID:   "secret/allowed_materialization",
			match:    value,
			detector: "kubernetes_secret_boundary",
			kind:     "allowed_materialization",
		}, "allowed", "<redacted>", "")
		return
	}
	candidates := []struct {
		value   string
		decoded bool
	}{
		{value: value},
	}
	if decoded, ok := decodeMaybeBase64(value); ok {
		candidates = append(candidates, struct {
			value   string
			decoded bool
		}{value: decoded, decoded: true})
	}
	for _, item := range candidates {
		cc := c
		cc.value = item.value
		cc.decoded = item.decoded
		for _, match := range s.detect(cc) {
			s.addFinding(cc, match)
		}
	}
}

func (s *secretScanState) countAllowedMaterial(value string, c secretCandidate) {
	values := []string{value}
	if decoded, ok := decodeMaybeBase64(value); ok {
		values = append(values, decoded)
	}
	for _, v := range values {
		for _, match := range s.detect(secretCandidate{value: v, key: c.key, fieldPath: c.fieldPath, stage: c.stage, source: c.source}) {
			s.matchCounts[match.ruleID]++
		}
	}
}

func (s *secretScanState) detect(c secretCandidate) []secretMatch {
	value := strings.TrimSpace(c.value)
	if value == "" || isLikelyPlaceholder(value) {
		return nil
	}
	var out []secretMatch
	apply := secrets.ApplyRenderScalar
	if c.stage == "source" || c.stage == "repo" || c.stage == "artifact" || c.stage == "build" {
		apply = secrets.ApplySourceScalar
	}
	for _, rule := range s.rules.Rules {
		if !rule.Applies(apply) {
			continue
		}
		for _, m := range rule.FindAllString(value) {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			out = append(out, secretMatch{
				ruleID:     "secret/" + strings.TrimSpace(rule.ID),
				message:    firstNonEmptyString(rule.Message, "secret-like value detected"),
				match:      m,
				severity:   secretSeverity(rule.Severity),
				confidence: 0.97,
				detector:   strings.TrimSpace(rule.ID),
				kind:       "provider_shape",
			})
		}
	}
	if len(out) == 0 && credentialKey(c.key, c.fieldPath) && suspiciousCredentialValue(value) {
		out = append(out, secretMatch{
			ruleID:     "secret/contextual_credential_value",
			message:    "credential-like field contains a literal value",
			match:      value,
			severity:   SeverityHigh,
			confidence: 0.82,
			detector:   "keyword_context",
			kind:       "key_context+entropy",
		})
	}
	return dedupeSecretMatches(out)
}

func (s *secretScanState) addFinding(c secretCandidate, match secretMatch) {
	key := strings.Join([]string{match.ruleID, c.resource, c.location, c.fieldPath}, "|")
	if _, ok := s.seenFindings[key]; ok {
		return
	}
	s.seenFindings[key] = struct{}{}
	s.matchCounts[match.ruleID]++
	subj := c.subject
	msg := match.message
	if c.kind != "" && c.kind != "Secret" {
		msg = fmt.Sprintf("%s in %s %s", strings.TrimSuffix(msg, "."), c.kind, firstNonEmptyString(subj.Name, "resource"))
	}
	fieldPath := c.fieldPath
	location := c.location
	if fieldPath == "" {
		fieldPath = c.key
	}
	fingerprint := "sha256:" + SHA256Hex(strings.Join([]string{
		match.ruleID,
		c.resource,
		fieldPath,
		strings.ToLower(c.key),
	}, "|"))
	evidenceKind := match.kind
	if c.decoded {
		evidenceKind += "+decoded"
	}
	f := Finding{
		RuleID:      match.ruleID,
		Severity:    match.severity,
		Category:    "secret_flow",
		Confidence:  match.confidence,
		Message:     msg,
		FieldPath:   fieldPath,
		Location:    location,
		Path:        c.source,
		Line:        c.sourceLine,
		ResourceKey: c.resource,
		Expected:    "secret reference or Kubernetes Secret mount",
		Observed:    secrets.Redact(match.match),
		Subject:     subj,
		Fingerprint: fingerprint,
		Tags:        []string{"secret", "redacted", c.stage},
		Evidence: map[string]any{
			"detector":     match.detector,
			"evidenceKind": evidenceKind,
			"redaction":    "value_preview_only",
			"sourceStage":  firstNonEmptyString(c.stage, "source"),
			"sinkStage":    firstNonEmptyString(c.stage, "render"),
			"sink":         fieldPath,
			"rawStored":    false,
		},
		Fix: &FindingFix{
			Summary:   "Move the literal value to a secret provider reference or Kubernetes Secret mount.",
			PatchHint: patchHintForSecret(c),
		},
	}
	s.findings = append(s.findings, f)
	s.addFlowEvent("forbidden_boundary", c, match, "blocked", f.Observed, f.Fingerprint)
}

func (s *secretScanState) report() *SecretScanReport {
	sortFindings(s.findings)
	blocked := s.opts.Mode == ModeBlock && hasAtLeast(s.findings, s.opts.FailOn)
	summary := SecretScanSummary{
		Total:                   len(s.findings),
		BySeverity:              map[Severity]int{},
		SecretReferences:        s.refs,
		AllowedMaterializations: s.allowed,
		RawSecretStored:         false,
	}
	for _, f := range s.findings {
		summary.BySeverity[f.Severity]++
	}
	for _, count := range s.matchCounts {
		summary.RedactedMatches += count
	}
	proofMatches := make([]RedactionRuleMatch, 0, len(s.matchCounts))
	for rule, count := range s.matchCounts {
		if count > 0 {
			proofMatches = append(proofMatches, RedactionRuleMatch{RuleID: rule, Count: count})
		}
	}
	sort.Slice(proofMatches, func(i, j int) bool { return proofMatches[i].RuleID < proofMatches[j].RuleID })
	report := &SecretScanReport{
		Version:     "v1",
		Tool:        "torque-secrets",
		Mode:        s.opts.Mode,
		FailOn:      s.opts.FailOn,
		Profile:     strings.TrimSpace(s.opts.Profile),
		Passed:      !blocked,
		Blocked:     blocked,
		EvaluatedAt: s.evaluatedAt,
		Source:      s.sourceDefault,
		Summary:     summary,
		Findings:    append([]Finding(nil), s.findings...),
		RedactionProof: RedactionProof{
			Surfaces: []RedactionSurfaceProof{{
				Surface:         firstNonEmptyString(s.opts.Surface, "verifier.report"),
				Matches:         proofMatches,
				RawSecretStored: false,
			}},
			FailedClosed: false,
		},
	}
	if s.opts.FlowGraph {
		report.FlowGraph = s.buildFlowGraph()
	}
	return report
}

func secretSeverity(sev secrets.Severity) Severity {
	switch sev {
	case secrets.SeverityBlock:
		return SeverityCritical
	default:
		return SeverityHigh
	}
}

func isAllowedSecretMaterial(kind string, path []string) bool {
	if strings.EqualFold(strings.TrimSpace(kind), "Secret") && len(path) >= 2 {
		first := strings.TrimSpace(path[0])
		return first == "data" || first == "stringData"
	}
	return false
}

func isAllowedSecretReferencePath(path []string) bool {
	joined := strings.ToLower(strings.Join(path, "."))
	switch {
	case strings.Contains(joined, "secretkeyref"):
		return true
	case strings.Contains(joined, "secretref"):
		return true
	case strings.Contains(joined, "volumes.") && strings.Contains(joined, ".secret."):
		return true
	case strings.Contains(joined, "projected.sources") && strings.Contains(joined, ".secret."):
		return true
	default:
		return false
	}
}

func decodeMaybeBase64(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 12 || len(value)%4 != 0 {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", false
	}
	if len(raw) == 0 || !mostlyPrintable(raw) {
		return "", false
	}
	return string(raw), true
}

func mostlyPrintable(raw []byte) bool {
	printable := 0
	for _, b := range raw {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 32 && b <= 126) {
			printable++
		}
	}
	return float64(printable)/float64(len(raw)) >= 0.9
}

func renderFieldPath(path []string) string {
	if len(path) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range path {
		if strings.HasPrefix(p, "[") {
			b.WriteString(p)
			continue
		}
		if b.Len() > 0 {
			b.WriteString(".")
		}
		b.WriteString(p)
	}
	return b.String()
}

func lastPathKey(path []string) string {
	for i := len(path) - 1; i >= 0; i-- {
		p := strings.TrimSpace(path[i])
		if p != "" && !strings.HasPrefix(p, "[") {
			return p
		}
	}
	return ""
}

func splitPotentialKeyValue(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}
	for _, sep := range []string{":", "="} {
		if k, v, ok := strings.Cut(line, sep); ok {
			k = strings.Trim(strings.TrimSpace(k), `"'`)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if k != "" && len(k) <= 96 {
				return k, v
			}
		}
	}
	return "", line
}

func credentialKey(key string, fieldPath string) bool {
	needle := strings.ToLower(strings.TrimSpace(key + " " + fieldPath))
	for _, word := range []string{"password", "passwd", "secret", "token", "apikey", "api_key", "api-key", "accesskey", "access_key", "privatekey", "private_key", "clientsecret", "client_secret", "connectionstring", "connection_string", "dsn"} {
		if strings.Contains(needle, word) {
			return true
		}
	}
	return false
}

func suspiciousCredentialValue(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if len(value) < 8 {
		return false
	}
	if strings.HasPrefix(value, "secret://") {
		return false
	}
	if isLikelyPlaceholder(value) {
		return false
	}
	return entropy(value) >= 2.8 || len(value) >= 16
}

func isLikelyPlaceholder(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return true
	}
	trimmed := strings.Trim(v, `"'<>`)
	switch trimmed {
	case "changeme", "change-me", "replace-me", "redacted", "password", "secret", "token", "example", "dummy", "test", "placeholder", "your-token", "your-secret":
		return true
	}
	return strings.Contains(trimmed, "example") || strings.Contains(trimmed, "placeholder") || strings.Contains(trimmed, "replace")
}

func entropy(value string) float64 {
	counts := map[rune]float64{}
	var total float64
	for _, r := range value {
		if unicode.IsSpace(r) {
			continue
		}
		counts[r]++
		total++
	}
	if total == 0 {
		return 0
	}
	var e float64
	for _, c := range counts {
		p := c / total
		e -= p * math.Log2(p)
	}
	return e
}

func dedupeSecretMatches(matches []secretMatch) []secretMatch {
	if len(matches) < 2 {
		return matches
	}
	seen := map[string]struct{}{}
	out := make([]secretMatch, 0, len(matches))
	for _, m := range matches {
		key := m.ruleID + "|" + m.match
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	return out
}

func patchHintForSecret(c secretCandidate) string {
	key := strings.TrimSpace(c.key)
	if key == "" {
		key = "value"
	}
	switch strings.ToLower(strings.TrimSpace(c.kind)) {
	case "configmap":
		return fmt.Sprintf("Move %s to a Secret and reference it with valueFrom.secretKeyRef.", key)
	default:
		return fmt.Sprintf("%s: secret://vault/path#%s", key, key)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

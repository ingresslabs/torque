package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/verify"
	cfgpkg "github.com/ingresslabs/torque/internal/verify/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type securityBenchmarkOptions struct {
	CorpusDir             string
	ReportPath            string
	Format                string
	EvaluatedAt           time.Time
	LiveK3SBoundaryMatrix bool
	LiveNamespace         string
	LiveConfirm           bool
}

type securityBenchmarkCorpus struct {
	Version string                      `yaml:"version"`
	Cases   []securityBenchmarkCaseSpec `yaml:"cases"`
}

type securityBenchmarkCaseSpec struct {
	ID               string                         `yaml:"id"`
	Scope            string                         `yaml:"scope"`
	Path             string                         `yaml:"path"`
	Manifest         string                         `yaml:"manifest"`
	FileType         string                         `yaml:"fileType"`
	ExpectedFindings []securityBenchmarkExpectation `yaml:"expectedFindings"`
	RawSecrets       []string                       `yaml:"rawSecrets"`
}

type securityBenchmarkExpectation struct {
	RuleID   string `yaml:"ruleId" json:"ruleId"`
	Family   string `yaml:"family" json:"family"`
	FileType string `yaml:"fileType" json:"fileType"`
	MinCount int    `yaml:"minCount" json:"minCount"`
}

type securityBenchmarkReport struct {
	Version               string                              `json:"version"`
	Tool                  string                              `json:"tool"`
	Corpus                string                              `json:"corpus"`
	GeneratedAt           time.Time                           `json:"generatedAt"`
	Summary               securityBenchmarkSummary            `json:"summary"`
	RecallBySecretFamily  map[string]securityBenchmarkMetric  `json:"recallBySecretFamily,omitempty"`
	PrecisionByFileType   map[string]securityBenchmarkMetric  `json:"precisionByFileType,omitempty"`
	Cases                 []securityBenchmarkCaseResult       `json:"cases,omitempty"`
	LiveK3SBoundaryMatrix securityBenchmarkLiveBoundaryMatrix `json:"liveK3SBoundaryMatrix"`
}

type securityBenchmarkSummary struct {
	Cases                   int     `json:"cases"`
	Passed                  int     `json:"passed"`
	Failed                  int     `json:"failed"`
	TruePositives           int     `json:"truePositives"`
	FalsePositives          int     `json:"falsePositives"`
	FalseNegatives          int     `json:"falseNegatives"`
	TrueNegativeCases       int     `json:"trueNegativeCases"`
	NegativeCases           int     `json:"negativeCases"`
	Recall                  float64 `json:"recall"`
	Precision               float64 `json:"precision"`
	FalsePositiveRate       float64 `json:"falsePositiveRate"`
	RuntimeMillis           int64   `json:"runtimeMillis"`
	RedactionEscapeCount    int     `json:"redactionEscapeCount"`
	FlowGraphNodes          int     `json:"flowGraphNodes"`
	FlowGraphEdges          int     `json:"flowGraphEdges"`
	ProvenanceChains        int     `json:"provenanceChains"`
	LiveObjects             int     `json:"liveObjects"`
	BoundaryMatrixPassCount int     `json:"boundaryMatrixPassCount"`
	BoundaryMatrixFailCount int     `json:"boundaryMatrixFailCount"`
}

type securityBenchmarkMetric struct {
	Cases             int     `json:"cases"`
	TruePositives     int     `json:"truePositives"`
	FalsePositives    int     `json:"falsePositives"`
	FalseNegatives    int     `json:"falseNegatives"`
	TrueNegativeCases int     `json:"trueNegativeCases"`
	Recall            float64 `json:"recall"`
	Precision         float64 `json:"precision"`
	FalsePositiveRate float64 `json:"falsePositiveRate"`
}

type securityBenchmarkCaseResult struct {
	ID                   string         `json:"id"`
	Scope                string         `json:"scope"`
	Path                 string         `json:"path,omitempty"`
	Passed               bool           `json:"passed"`
	ExpectedFindingCount int            `json:"expectedFindingCount"`
	ActualFindingCount   int            `json:"actualFindingCount"`
	TruePositives        int            `json:"truePositives"`
	FalsePositives       int            `json:"falsePositives"`
	FalseNegatives       int            `json:"falseNegatives"`
	Recall               float64        `json:"recall"`
	Precision            float64        `json:"precision"`
	RuntimeMillis        int64          `json:"runtimeMillis"`
	RedactionEscapes     int            `json:"redactionEscapes"`
	FlowGraphNodes       int            `json:"flowGraphNodes"`
	FlowGraphEdges       int            `json:"flowGraphEdges"`
	ProvenanceChains     int            `json:"provenanceChains"`
	LiveObjects          int            `json:"liveObjects"`
	BoundaryMatrixPassed *bool          `json:"boundaryMatrixPassed,omitempty"`
	FindingRules         map[string]int `json:"findingRules,omitempty"`
	Error                string         `json:"error,omitempty"`
}

type securityBenchmarkLiveBoundaryMatrix struct {
	Status               string `json:"status"`
	Passed               bool   `json:"passed"`
	Namespace            string `json:"namespace,omitempty"`
	Findings             int    `json:"findings,omitempty"`
	BoundaryMatrixPassed bool   `json:"boundaryMatrixPassed,omitempty"`
	FlowGraphNodes       int    `json:"flowGraphNodes,omitempty"`
	FlowGraphEdges       int    `json:"flowGraphEdges,omitempty"`
	ProvenanceChains     int    `json:"provenanceChains,omitempty"`
	LiveObjects          int    `json:"liveObjects,omitempty"`
	Error                string `json:"error,omitempty"`
}

func newSecurityBenchmarkCommand(kubeconfig, kubeContext *string) *cobra.Command {
	opts := securityBenchmarkOptions{CorpusDir: "./testdata/security", Format: "table"}
	var evaluatedAt string
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Benchmark secret detection, redaction, and boundary evidence",
		Long:  "Benchmark secret detection against a corpus and publish recall, precision, false-positive, runtime, redaction, flow-graph, and Kubernetes boundary metrics.",
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			if strings.TrimSpace(evaluatedAt) != "" {
				parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(evaluatedAt))
				if err != nil {
					return fmt.Errorf("--evaluated-at must be RFC3339: %w", err)
				}
				now = parsed.UTC()
			}
			opts.EvaluatedAt = now
			report, err := runSecurityBenchmark(cmd.Context(), kubeconfig, kubeContext, opts)
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.ReportPath) != "" && strings.TrimSpace(opts.ReportPath) != "-" {
				out, closer, err := cfgpkg.OpenOutput(cmd.OutOrStdout(), opts.ReportPath)
				if err != nil {
					return err
				}
				if err := writeSecurityBenchmarkJSON(out, report); err != nil {
					if closer != nil {
						_ = closer.Close()
					}
					return err
				}
				if closer != nil {
					_ = closer.Close()
				}
			}
			switch strings.ToLower(strings.TrimSpace(opts.Format)) {
			case "", "table", "text":
				renderSecurityBenchmarkText(cmd.OutOrStdout(), report)
			case "json":
				if strings.TrimSpace(opts.ReportPath) == "" || strings.TrimSpace(opts.ReportPath) == "-" {
					return writeSecurityBenchmarkJSON(cmd.OutOrStdout(), report)
				}
			default:
				return fmt.Errorf("unsupported --format %q (expected table or json)", opts.Format)
			}
			if report.Summary.Failed > 0 || report.Summary.RedactionEscapeCount > 0 || (report.LiveK3SBoundaryMatrix.Status == "failed") {
				return fmt.Errorf("security benchmark failed (failed=%d redactionEscapes=%d live=%s)", report.Summary.Failed, report.Summary.RedactionEscapeCount, report.LiveK3SBoundaryMatrix.Status)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.CorpusDir, "corpus", opts.CorpusDir, "Security benchmark corpus directory")
	cmd.Flags().StringVar(&opts.ReportPath, "report", "", "Write benchmark JSON report to this path")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: table or json")
	cmd.Flags().StringVar(&evaluatedAt, "evaluated-at", "", "Override evaluation time (RFC3339) for deterministic reports/tests")
	cmd.Flags().BoolVar(&opts.LiveK3SBoundaryMatrix, "live-k3s-boundary-matrix", false, "Run the live k3s boundary matrix probe against a temporary namespace")
	cmd.Flags().StringVar(&opts.LiveNamespace, "live-namespace", "", "Namespace for --live-k3s-boundary-matrix (default: generated)")
	cmd.Flags().BoolVar(&opts.LiveConfirm, "live-confirm", false, "Confirm that --live-k3s-boundary-matrix may create and delete its namespace")
	decorateCommandHelp(cmd, "Benchmark Flags")
	return cmd
}

func runSecurityBenchmark(ctx context.Context, kubeconfig, kubeContext *string, opts securityBenchmarkOptions) (*securityBenchmarkReport, error) {
	corpusDir := strings.TrimSpace(opts.CorpusDir)
	if corpusDir == "" {
		corpusDir = "./testdata/security"
	}
	absCorpus, err := filepath.Abs(corpusDir)
	if err != nil {
		return nil, err
	}
	corpus, err := loadSecurityBenchmarkCorpus(absCorpus)
	if err != nil {
		return nil, err
	}
	generatedAt := opts.EvaluatedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	report := &securityBenchmarkReport{
		Version:              "v1",
		Tool:                 "torque-security-benchmark",
		Corpus:               absCorpus,
		GeneratedAt:          generatedAt,
		RecallBySecretFamily: map[string]securityBenchmarkMetric{},
		PrecisionByFileType:  map[string]securityBenchmarkMetric{},
		LiveK3SBoundaryMatrix: securityBenchmarkLiveBoundaryMatrix{
			Status: "skipped",
		},
	}
	for _, spec := range corpus.Cases {
		result := runSecurityBenchmarkCase(ctx, kubeconfig, kubeContext, absCorpus, spec, generatedAt)
		report.Cases = append(report.Cases, result)
		mergeSecurityBenchmarkResult(report, spec, result)
	}
	if opts.LiveK3SBoundaryMatrix {
		live, err := runSecurityBenchmarkLiveBoundaryMatrix(ctx, kubeconfig, kubeContext, opts, generatedAt)
		if err != nil {
			live.Status = "failed"
			live.Error = err.Error()
		}
		report.LiveK3SBoundaryMatrix = live
	}
	finalizeSecurityBenchmarkReport(report)
	return report, nil
}

func loadSecurityBenchmarkCorpus(corpusDir string) (*securityBenchmarkCorpus, error) {
	raw, err := os.ReadFile(filepath.Join(corpusDir, "corpus.yaml"))
	if err != nil {
		return nil, err
	}
	var corpus securityBenchmarkCorpus
	if err := yaml.Unmarshal(raw, &corpus); err != nil {
		return nil, err
	}
	if strings.TrimSpace(corpus.Version) == "" {
		return nil, fmt.Errorf("security corpus version is required")
	}
	if len(corpus.Cases) == 0 {
		return nil, fmt.Errorf("security corpus has no cases")
	}
	return &corpus, nil
}

func runSecurityBenchmarkCase(ctx context.Context, kubeconfig, kubeContext *string, corpusDir string, spec securityBenchmarkCaseSpec, generatedAt time.Time) securityBenchmarkCaseResult {
	result := securityBenchmarkCaseResult{
		ID:           strings.TrimSpace(spec.ID),
		Scope:        strings.ToLower(strings.TrimSpace(spec.Scope)),
		Path:         strings.TrimSpace(spec.Path),
		FindingRules: map[string]int{},
	}
	if result.Scope == "" {
		result.Scope = "repo"
	}
	if result.ID == "" {
		result.ID = result.Path
	}
	start := time.Now()
	scanOpts := verify.SecretScanOptions{
		Mode:           verify.ModeWarn,
		FailOn:         verify.SeverityHigh,
		Profile:        "",
		Surface:        "torque.security.benchmark",
		BoundaryMatrix: result.Scope == "render",
		FlowGraph:      true,
		EvaluatedAt:    generatedAt,
	}
	var (
		scan *verify.SecretScanReport
		err  error
	)
	targetPath := resolveSecurityBenchmarkCasePath(corpusDir, spec.Path)
	switch result.Scope {
	case "render":
		manifest := resolveSecurityBenchmarkCasePath(corpusDir, firstScanValue(spec.Manifest, spec.Path))
		scan, err = runRenderSecretScan(ctx, kubeconfig, kubeContext, "", "", "", manifest, nil, nil, scanOpts)
	case "build":
		scan, err = runBuildSecretScan(targetPath, scanOpts)
	case "artifact":
		scan, err = runTextSecretScan("artifact", targetPath, scanOpts)
	case "repo":
		scan, err = runTextSecretScan("repo", targetPath, scanOpts)
	default:
		err = fmt.Errorf("unsupported benchmark scope %q", result.Scope)
	}
	result.RuntimeMillis = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		result.Passed = false
		return result
	}
	result.ExpectedFindingCount = expectedSecurityBenchmarkCount(spec.ExpectedFindings)
	result.ActualFindingCount = len(scan.Findings)
	for _, finding := range scan.Findings {
		result.FindingRules[finding.RuleID]++
	}
	result.TruePositives, result.FalsePositives, result.FalseNegatives = scoreSecurityBenchmarkFindings(spec.ExpectedFindings, result.FindingRules)
	result.Recall = ratio(result.TruePositives, result.TruePositives+result.FalseNegatives)
	result.Precision = ratio(result.TruePositives, result.TruePositives+result.FalsePositives)
	if result.ExpectedFindingCount == 0 && result.ActualFindingCount == 0 {
		result.Recall = 1
		result.Precision = 1
	}
	if scan.FlowGraph != nil {
		result.FlowGraphNodes = scan.FlowGraph.Summary.Nodes
		result.FlowGraphEdges = scan.FlowGraph.Summary.Edges
		result.ProvenanceChains = scan.FlowGraph.Summary.ProvenanceChains
		result.LiveObjects = scan.FlowGraph.Summary.LiveObjects
	}
	if scan.BoundaryMatrix != nil {
		passed := scan.BoundaryMatrix.Passed
		result.BoundaryMatrixPassed = &passed
	}
	raw, _ := json.Marshal(scan)
	result.RedactionEscapes = countRedactionEscapes(raw, spec.RawSecrets)
	result.Passed = result.FalseNegatives == 0 && result.FalsePositives == 0 && result.RedactionEscapes == 0
	return result
}

func resolveSecurityBenchmarkCasePath(corpusDir string, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return corpusDir
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(corpusDir, p)
}

func expectedSecurityBenchmarkCount(expectations []securityBenchmarkExpectation) int {
	total := 0
	for _, exp := range expectations {
		total += maxSecurityBenchmarkInt(1, exp.MinCount)
	}
	return total
}

func scoreSecurityBenchmarkFindings(expectations []securityBenchmarkExpectation, actual map[string]int) (int, int, int) {
	expectedByRule := map[string]int{}
	for _, exp := range expectations {
		ruleID := strings.TrimSpace(exp.RuleID)
		if ruleID == "" {
			continue
		}
		expectedByRule[ruleID] += maxSecurityBenchmarkInt(1, exp.MinCount)
	}
	tp, fn := 0, 0
	for ruleID, expected := range expectedByRule {
		matched := minSecurityBenchmarkInt(actual[ruleID], expected)
		tp += matched
		fn += expected - matched
	}
	fp := 0
	for ruleID, count := range actual {
		fp += maxSecurityBenchmarkInt(0, count-expectedByRule[ruleID])
	}
	return tp, fp, fn
}

func mergeSecurityBenchmarkResult(report *securityBenchmarkReport, spec securityBenchmarkCaseSpec, result securityBenchmarkCaseResult) {
	report.Summary.Cases++
	if result.Passed {
		report.Summary.Passed++
	} else {
		report.Summary.Failed++
	}
	report.Summary.TruePositives += result.TruePositives
	report.Summary.FalsePositives += result.FalsePositives
	report.Summary.FalseNegatives += result.FalseNegatives
	report.Summary.RuntimeMillis += result.RuntimeMillis
	report.Summary.RedactionEscapeCount += result.RedactionEscapes
	report.Summary.FlowGraphNodes += result.FlowGraphNodes
	report.Summary.FlowGraphEdges += result.FlowGraphEdges
	report.Summary.ProvenanceChains += result.ProvenanceChains
	report.Summary.LiveObjects += result.LiveObjects
	if result.BoundaryMatrixPassed != nil {
		if *result.BoundaryMatrixPassed {
			report.Summary.BoundaryMatrixPassCount++
		} else {
			report.Summary.BoundaryMatrixFailCount++
		}
	}
	if len(spec.ExpectedFindings) == 0 {
		report.Summary.NegativeCases++
		if result.FalsePositives == 0 {
			report.Summary.TrueNegativeCases++
		}
	}
	for _, exp := range spec.ExpectedFindings {
		family := strings.TrimSpace(exp.Family)
		if family == "" {
			family = strings.TrimPrefix(strings.TrimSpace(exp.RuleID), "secret/")
		}
		metric := report.RecallBySecretFamily[family]
		metric.Cases++
		expected := maxSecurityBenchmarkInt(1, exp.MinCount)
		actual := result.FindingRules[strings.TrimSpace(exp.RuleID)]
		matched := minSecurityBenchmarkInt(actual, expected)
		metric.TruePositives += matched
		metric.FalseNegatives += expected - matched
		report.RecallBySecretFamily[family] = metric
	}
	fileType := securityBenchmarkFileType(spec)
	metric := report.PrecisionByFileType[fileType]
	metric.Cases++
	metric.TruePositives += result.TruePositives
	metric.FalsePositives += result.FalsePositives
	metric.FalseNegatives += result.FalseNegatives
	if len(spec.ExpectedFindings) == 0 && result.FalsePositives == 0 {
		metric.TrueNegativeCases++
	}
	report.PrecisionByFileType[fileType] = metric
}

func finalizeSecurityBenchmarkReport(report *securityBenchmarkReport) {
	report.Summary.Recall = ratio(report.Summary.TruePositives, report.Summary.TruePositives+report.Summary.FalseNegatives)
	report.Summary.Precision = ratio(report.Summary.TruePositives, report.Summary.TruePositives+report.Summary.FalsePositives)
	report.Summary.FalsePositiveRate = ratio(report.Summary.NegativeCases-report.Summary.TrueNegativeCases, report.Summary.NegativeCases)
	for family, metric := range report.RecallBySecretFamily {
		finalizeSecurityBenchmarkMetric(&metric)
		report.RecallBySecretFamily[family] = metric
	}
	for fileType, metric := range report.PrecisionByFileType {
		finalizeSecurityBenchmarkMetric(&metric)
		report.PrecisionByFileType[fileType] = metric
	}
}

func finalizeSecurityBenchmarkMetric(metric *securityBenchmarkMetric) {
	metric.Recall = ratio(metric.TruePositives, metric.TruePositives+metric.FalseNegatives)
	metric.Precision = ratio(metric.TruePositives, metric.TruePositives+metric.FalsePositives)
	if metric.TruePositives+metric.FalseNegatives == 0 && metric.TrueNegativeCases > 0 {
		metric.Recall = 1
	}
	if metric.TruePositives+metric.FalsePositives == 0 && metric.TrueNegativeCases > 0 {
		metric.Precision = 1
	}
	metric.FalsePositiveRate = ratio(metric.FalsePositives, metric.FalsePositives+metric.TrueNegativeCases)
}

func securityBenchmarkFileType(spec securityBenchmarkCaseSpec) string {
	if ft := strings.TrimSpace(spec.FileType); ft != "" {
		return ft
	}
	for _, exp := range spec.ExpectedFindings {
		if ft := strings.TrimSpace(exp.FileType); ft != "" {
			return ft
		}
	}
	path := firstScanValue(spec.Manifest, spec.Path)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext != "" {
		return ext
	}
	return "unknown"
}

func countRedactionEscapes(raw []byte, secrets []string) int {
	count := 0
	text := string(raw)
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		if strings.Contains(text, secret) {
			count++
		}
	}
	return count
}

func runSecurityBenchmarkLiveBoundaryMatrix(ctx context.Context, kubeconfig, kubeContext *string, opts securityBenchmarkOptions, generatedAt time.Time) (securityBenchmarkLiveBoundaryMatrix, error) {
	if !opts.LiveConfirm && strings.TrimSpace(os.Getenv("TORQUE_SECURITY_E2E_CONFIRM")) != "1" {
		return securityBenchmarkLiveBoundaryMatrix{Status: "failed", Error: "set --live-confirm or TORQUE_SECURITY_E2E_CONFIRM=1"}, nil
	}
	kcfg := ""
	if kubeconfig != nil {
		kcfg = strings.TrimSpace(*kubeconfig)
	}
	if kcfg == "" {
		kcfg = strings.TrimSpace(os.Getenv("TORQUE_SECURITY_E2E_KUBECONFIG"))
	}
	if kcfg == "" {
		kcfg = strings.TrimSpace(os.Getenv("KUBECONFIG"))
	}
	if kcfg == "" {
		return securityBenchmarkLiveBoundaryMatrix{Status: "failed", Error: "kubeconfig is required for live k3s benchmark"}, nil
	}
	kctx := ""
	if kubeContext != nil {
		kctx = strings.TrimSpace(*kubeContext)
	}
	namespace := strings.TrimSpace(opts.LiveNamespace)
	if namespace == "" {
		namespace = fmt.Sprintf("torque-security-benchmark-%d", time.Now().UTC().UnixNano())
	}
	live := securityBenchmarkLiveBoundaryMatrix{Status: "running", Namespace: namespace}
	kubeconfigValue := kcfg
	contextValue := kctx
	if err := kubectlApplySecurityBenchmarkFixture(ctx, kubeconfigValue, contextValue, namespace); err != nil {
		_ = kubectlDeleteSecurityBenchmarkNamespace(context.Background(), kubeconfigValue, contextValue, namespace)
		return live, err
	}
	defer func() {
		_ = kubectlDeleteSecurityBenchmarkNamespace(context.Background(), kubeconfigValue, contextValue, namespace)
	}()
	kptr := &kubeconfigValue
	cptr := &contextValue
	scan, err := runRenderSecretScan(ctx, kptr, cptr, "", "", namespace, "", nil, nil, verify.SecretScanOptions{
		Mode:           verify.ModeWarn,
		FailOn:         verify.SeverityHigh,
		Profile:        "enterprise",
		Surface:        "torque.security.benchmark",
		BoundaryMatrix: true,
		FlowGraph:      true,
		EvaluatedAt:    generatedAt,
	})
	if err != nil {
		return live, err
	}
	live.Status = "passed"
	live.Findings = len(scan.Findings)
	if scan.BoundaryMatrix != nil {
		live.BoundaryMatrixPassed = scan.BoundaryMatrix.Passed
	}
	if scan.FlowGraph != nil {
		live.FlowGraphNodes = scan.FlowGraph.Summary.Nodes
		live.FlowGraphEdges = scan.FlowGraph.Summary.Edges
		live.ProvenanceChains = scan.FlowGraph.Summary.ProvenanceChains
		live.LiveObjects = scan.FlowGraph.Summary.LiveObjects
	}
	live.Passed = scan.BoundaryMatrix != nil && scan.BoundaryMatrix.Passed && scan.FlowGraph != nil && scan.FlowGraph.Summary.ForbiddenFlows > 0
	if !live.Passed {
		live.Status = "failed"
	}
	return live, nil
}

func kubectlApplySecurityBenchmarkFixture(ctx context.Context, kubeconfig, kubeContext, namespace string) error {
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    torque.dev/benchmark: security
---
apiVersion: v1
kind: Secret
metadata:
  name: allowed-secret
  namespace: %s
data:
  awsAccessKey: QUtJQTEyMzQ1Njc4OTBBQkNERUY=
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: blocked-config
  namespace: %s
data:
  awsAccessKey: AKIA1234567890ABCDEF
---
apiVersion: v1
kind: Pod
metadata:
  name: env-leak
  namespace: %s
spec:
  containers:
    - name: app
      image: busybox:1.36
      command: ["sh", "-c", "sleep 3600"]
      env:
        - name: API_TOKEN
          value: AKIA1234567890ABCDEF
        - name: SAFE_TOKEN
          valueFrom:
            secretKeyRef:
              name: allowed-secret
              key: awsAccessKey
`, namespace, namespace, namespace, namespace)
	args := kubectlSecurityBenchmarkArgs(kubeconfig, kubeContext, "apply", "-f", "-")
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply security benchmark fixture: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func kubectlDeleteSecurityBenchmarkNamespace(ctx context.Context, kubeconfig, kubeContext, namespace string) error {
	if strings.TrimSpace(namespace) == "" {
		return nil
	}
	args := kubectlSecurityBenchmarkArgs(kubeconfig, kubeContext, "delete", "namespace", namespace, "--ignore-not-found=true")
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl delete security benchmark namespace: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func kubectlSecurityBenchmarkArgs(kubeconfig, kubeContext string, args ...string) []string {
	out := make([]string, 0, len(args)+4)
	if strings.TrimSpace(kubeconfig) != "" {
		out = append(out, "--kubeconfig", strings.TrimSpace(kubeconfig))
	}
	if strings.TrimSpace(kubeContext) != "" {
		out = append(out, "--context", strings.TrimSpace(kubeContext))
	}
	out = append(out, args...)
	return out
}

func writeSecurityBenchmarkJSON(w interface{ Write([]byte) (int, error) }, report *securityBenchmarkReport) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = w.Write(raw)
	return err
}

func renderSecurityBenchmarkText(w interface{ Write([]byte) (int, error) }, report *securityBenchmarkReport) {
	if w == nil || report == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "Security benchmark: %d cases (%d passed, %d failed)\n", report.Summary.Cases, report.Summary.Passed, report.Summary.Failed)
	_, _ = fmt.Fprintf(w, "Recall: %.3f  Precision: %.3f  False positive rate: %.3f\n", report.Summary.Recall, report.Summary.Precision, report.Summary.FalsePositiveRate)
	_, _ = fmt.Fprintf(w, "Runtime: %dms  Redaction escapes: %d  Flow graph: %d nodes / %d edges / %d provenance chains\n", report.Summary.RuntimeMillis, report.Summary.RedactionEscapeCount, report.Summary.FlowGraphNodes, report.Summary.FlowGraphEdges, report.Summary.ProvenanceChains)
	if report.LiveK3SBoundaryMatrix.Status != "" && report.LiveK3SBoundaryMatrix.Status != "skipped" {
		_, _ = fmt.Fprintf(w, "Live k3s boundary matrix: %s (namespace=%s)\n", report.LiveK3SBoundaryMatrix.Status, report.LiveK3SBoundaryMatrix.Namespace)
	}
	var ids []string
	for _, result := range report.Cases {
		if result.Passed {
			continue
		}
		ids = append(ids, result.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		_, _ = fmt.Fprintf(w, "- failed: %s\n", id)
	}
}

func ratio(num, denom int) float64 {
	if denom <= 0 {
		return 0
	}
	return float64(num) / float64(denom)
}

func minSecurityBenchmarkInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxSecurityBenchmarkInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

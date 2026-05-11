package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
)

const (
	applyPredictionArtifactName = "apply.prediction.json"
	proofBundleArtifactName     = "apply.proof_bundle.json"
)

type applyPrediction struct {
	Version             int                         `json:"version"`
	GeneratedAt         string                      `json:"generatedAt"`
	Release             string                      `json:"release"`
	Namespace           string                      `json:"namespace"`
	Chart               string                      `json:"chart,omitempty"`
	ChartVersion        string                      `json:"chartVersion,omitempty"`
	Risk                string                      `json:"risk"`
	RiskReasons         []string                    `json:"riskReasons,omitempty"`
	Summary             applyPredictionSummary      `json:"summary"`
	Changes             []applyPredictionChange     `json:"changes,omitempty"`
	WillRestart         []applyPredictionChange     `json:"willRestart,omitempty"`
	Images              []planImageRef              `json:"images,omitempty"`
	MissingDependencies []applyPredictionDependency `json:"missingDependencies,omitempty"`
	Warnings            []string                    `json:"warnings,omitempty"`
	Rollback            applyPredictionRollback     `json:"rollback"`
	RenderedSHA256      string                      `json:"renderedSha256,omitempty"`
	PlanGeneratedAt     string                      `json:"planGeneratedAt,omitempty"`
	ClusterHost         string                      `json:"clusterHost,omitempty"`
}

type applyPredictionSummary struct {
	Creates             int `json:"creates"`
	Updates             int `json:"updates"`
	Deletes             int `json:"deletes"`
	Unchanged           int `json:"unchanged"`
	Images              int `json:"images"`
	UnpinnedImages      int `json:"unpinnedImages"`
	MissingDependencies int `json:"missingDependencies"`
	QuotaFails          int `json:"quotaFails"`
	QuotaWarnings       int `json:"quotaWarnings"`
	RestartingWorkloads int `json:"restartingWorkloads"`
}

type applyPredictionChange struct {
	Action    string `json:"action"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

type applyPredictionDependency struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

type applyPredictionRollback struct {
	Available  bool   `json:"available"`
	Revision   int    `json:"revision,omitempty"`
	Status     string `json:"status,omitempty"`
	Deployed   string `json:"deployedAt,omitempty"`
	Confidence string `json:"confidence"`
	Note       string `json:"note,omitempty"`
}

type applyProofBundle struct {
	Version              int                        `json:"version"`
	GeneratedAt          string                     `json:"generatedAt"`
	StartedAt            string                     `json:"startedAt,omitempty"`
	FinishedAt           string                     `json:"finishedAt,omitempty"`
	Command              []string                   `json:"command,omitempty"`
	Release              string                     `json:"release"`
	Namespace            string                     `json:"namespace"`
	Chart                string                     `json:"chart,omitempty"`
	ChartVersion         string                     `json:"chartVersion,omitempty"`
	Status               string                     `json:"status"`
	Error                string                     `json:"error,omitempty"`
	DryRun               bool                       `json:"dryRun,omitempty"`
	Prediction           *applyPrediction           `json:"prediction,omitempty"`
	Plan                 *deployPlanResult          `json:"plan,omitempty"`
	HistoryBefore        []deploy.HistoryBreadcrumb `json:"historyBefore,omitempty"`
	HistoryAfter         []deploy.HistoryBreadcrumb `json:"historyAfter,omitempty"`
	LastSuccessfulBefore *deploy.HistoryBreadcrumb  `json:"lastSuccessfulBefore,omitempty"`
	LastSuccessfulAfter  *deploy.HistoryBreadcrumb  `json:"lastSuccessfulAfter,omitempty"`
	ResourceSnapshot     []deploy.ResourceStatus    `json:"resourceSnapshot,omitempty"`
	RollbackProof        *applyRollbackProof        `json:"rollbackProof,omitempty"`
	CapturePath          string                     `json:"capturePath,omitempty"`
	PhaseDurations       map[string]string          `json:"phaseDurations,omitempty"`
	Evidence             []string                   `json:"evidence,omitempty"`
}

type applyProofBundleInput struct {
	StartedAt            time.Time
	Command              []string
	Release              string
	Namespace            string
	Chart                string
	ChartVersion         string
	DryRun               bool
	Err                  error
	Prediction           *applyPrediction
	Plan                 *deployPlanResult
	HistoryBefore        []deploy.HistoryBreadcrumb
	LastSuccessfulBefore *deploy.HistoryBreadcrumb
	HistoryAfter         []deploy.HistoryBreadcrumb
	LastSuccessfulAfter  *deploy.HistoryBreadcrumb
	ResourceSnapshot     []deploy.ResourceStatus
	RollbackProof        *applyRollbackProof
	CapturePath          string
	PhaseDurations       map[string]string
}

func buildApplyPrediction(plan *deployPlanResult, history []deploy.HistoryBreadcrumb, lastSuccessful *deploy.HistoryBreadcrumb) *applyPrediction {
	if plan == nil {
		return nil
	}
	baseRisk, reasons := planRiskSummary(plan)
	risk := normalizeRisk(baseRisk)
	missing := collectPredictionMissingDependencies(plan)
	if len(missing) > 0 {
		risk = maxPredictionRisk(risk, "High")
		reasons = append(reasons, fmt.Sprintf("%d missing external dependenc%s detected.", len(missing), pluralY(len(missing))))
	}
	willRestart := collectPredictionRestartChanges(plan.Changes)
	if len(willRestart) > 0 && risk == "Low" {
		risk = "Medium"
		reasons = append(reasons, fmt.Sprintf("%d workload%s will restart.", len(willRestart), pluralS(len(willRestart))))
	}
	if len(history) > 0 && lastSuccessful == nil {
		risk = maxPredictionRisk(risk, "High")
		reasons = append(reasons, "No deployed Helm revision is available for rollback.")
	}
	quotaFails, quotaWarns := quotaStatusCounts(plan)
	out := &applyPrediction{
		Version:             1,
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		Release:             plan.ReleaseName,
		Namespace:           plan.Namespace,
		Chart:               firstNonEmpty(plan.ChartRef, plan.RequestedChart),
		ChartVersion:        firstNonEmpty(plan.ChartVersion, plan.RequestedVersion),
		Risk:                risk,
		RiskReasons:         uniquePredictionStrings(reasons),
		Summary:             applyPredictionSummary{Creates: plan.Summary.Creates, Updates: plan.Summary.Updates, Deletes: plan.Summary.Deletes, Unchanged: plan.Summary.Unchanged, Images: len(plan.Images), UnpinnedImages: countUnpinnedImages(plan.Images), MissingDependencies: len(missing), QuotaFails: quotaFails, QuotaWarnings: quotaWarns, RestartingWorkloads: len(willRestart)},
		Changes:             collectPredictionChanges(plan.Changes),
		WillRestart:         willRestart,
		Images:              append([]planImageRef(nil), plan.Images...),
		MissingDependencies: missing,
		Warnings:            append([]string(nil), plan.Warnings...),
		Rollback:            buildPredictionRollback(history, lastSuccessful),
		RenderedSHA256:      plan.RenderedSHA256,
		PlanGeneratedAt:     plan.GeneratedAt.UTC().Format(time.RFC3339Nano),
		ClusterHost:         plan.ClusterHost,
	}
	return out
}

func renderApplyPrediction(out io.Writer, prediction *applyPrediction) {
	if out == nil || prediction == nil {
		return
	}
	fmt.Fprintf(out, "Prediction: %s risk\n", strings.ToUpper(prediction.Risk))
	fmt.Fprintf(out, "Plan: %d create, %d update, %d delete, %d unchanged\n", prediction.Summary.Creates, prediction.Summary.Updates, prediction.Summary.Deletes, prediction.Summary.Unchanged)
	if len(prediction.WillRestart) > 0 {
		fmt.Fprintln(out, "Will restart:")
		for _, change := range limitPredictionChanges(prediction.WillRestart, 8) {
			fmt.Fprintf(out, "  %s/%s (ns: %s)\n", change.Kind, change.Name, emptyAsDash(change.Namespace))
		}
		if extra := len(prediction.WillRestart) - minInt(len(prediction.WillRestart), 8); extra > 0 {
			fmt.Fprintf(out, "  (and %d more)\n", extra)
		}
	}
	if len(prediction.MissingDependencies) > 0 {
		fmt.Fprintln(out, "Missing dependencies:")
		for _, dep := range limitPredictionDependencies(prediction.MissingDependencies, 8) {
			fmt.Fprintf(out, "  %s/%s (ns: %s)\n", dep.Kind, dep.Name, emptyAsDash(dep.Namespace))
		}
		if extra := len(prediction.MissingDependencies) - minInt(len(prediction.MissingDependencies), 8); extra > 0 {
			fmt.Fprintf(out, "  (and %d more)\n", extra)
		}
	}
	if len(prediction.RiskReasons) > 0 {
		fmt.Fprintln(out, "Risk reasons:")
		for _, reason := range limitStrings(prediction.RiskReasons, 8) {
			fmt.Fprintf(out, "  - %s\n", reason)
		}
		if extra := len(prediction.RiskReasons) - minInt(len(prediction.RiskReasons), 8); extra > 0 {
			fmt.Fprintf(out, "  (and %d more)\n", extra)
		}
	}
	rb := prediction.Rollback
	if rb.Available {
		fmt.Fprintf(out, "Rollback: revision %d available (%s confidence)\n", rb.Revision, rb.Confidence)
	} else {
		fmt.Fprintf(out, "Rollback: unavailable (%s confidence)\n", rb.Confidence)
	}
}

func buildApplyProofBundle(in applyProofBundleInput) applyProofBundle {
	now := time.Now().UTC()
	status := "succeeded"
	errText := ""
	if in.Err != nil {
		status = "failed"
		errText = in.Err.Error()
	}
	evidence := []string{
		"predictive plan",
		"Helm history before apply",
		"Helm history after apply",
		"phase durations",
	}
	if len(in.ResourceSnapshot) > 0 {
		evidence = append(evidence, "resource readiness snapshot")
	}
	if in.RollbackProof != nil {
		evidence = append(evidence, "rollback proof")
	}
	if strings.TrimSpace(in.CapturePath) != "" {
		evidence = append(evidence, "SQLite capture reference")
	}
	return applyProofBundle{
		Version:              1,
		GeneratedAt:          now.Format(time.RFC3339Nano),
		StartedAt:            in.StartedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt:           now.Format(time.RFC3339Nano),
		Command:              append([]string(nil), in.Command...),
		Release:              strings.TrimSpace(in.Release),
		Namespace:            strings.TrimSpace(in.Namespace),
		Chart:                strings.TrimSpace(in.Chart),
		ChartVersion:         strings.TrimSpace(in.ChartVersion),
		Status:               status,
		Error:                errText,
		DryRun:               in.DryRun,
		Prediction:           in.Prediction,
		Plan:                 sanitizeDeployPlanForProof(in.Plan),
		HistoryBefore:        deploy.CloneBreadcrumbs(in.HistoryBefore),
		HistoryAfter:         deploy.CloneBreadcrumbs(in.HistoryAfter),
		LastSuccessfulBefore: deploy.CloneBreadcrumbPointer(in.LastSuccessfulBefore),
		LastSuccessfulAfter:  deploy.CloneBreadcrumbPointer(in.LastSuccessfulAfter),
		ResourceSnapshot:     append([]deploy.ResourceStatus(nil), in.ResourceSnapshot...),
		RollbackProof:        in.RollbackProof,
		CapturePath:          strings.TrimSpace(in.CapturePath),
		PhaseDurations:       cloneStringMap(in.PhaseDurations),
		Evidence:             evidence,
	}
}

func sanitizeDeployPlanForProof(plan *deployPlanResult) *deployPlanResult {
	if plan == nil {
		return nil
	}
	cp := *plan
	cp.ManifestBlobs = nil
	cp.LiveManifests = nil
	cp.ManifestDiffs = nil
	cp.ManifestTemplates = nil
	cp.TemplateSources = nil
	if len(cp.Changes) > 0 {
		cp.Changes = append([]planResourceChange(nil), cp.Changes...)
		for i := range cp.Changes {
			cp.Changes[i].Diff = ""
		}
	}
	return &cp
}

func writeApplyProofBundle(ctx context.Context, rec artifactRecorder, path string, bundle applyProofBundle) (string, error) {
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", err
	}
	text := string(raw)
	if rec != nil {
		if err := rec.RecordArtifact(ctx, proofBundleArtifactName, text); err != nil {
			return "", err
		}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if strings.TrimSpace(filepath.Dir(path)) != "." {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func resolveApplyProofBundlePath(path, releaseName string, now time.Time) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path != "__auto__" {
		return path
	}
	slug := sanitizeFilename(releaseName)
	if slug == "" {
		slug = "release"
	}
	return fmt.Sprintf("torque-proof-bundle-%s-%s.json", slug, now.UTC().Format("20060102-150405"))
}

func collectPredictionChanges(changes []planResourceChange) []applyPredictionChange {
	out := make([]applyPredictionChange, 0, len(changes))
	for _, change := range changes {
		out = append(out, applyPredictionChange{
			Action:    string(change.Kind),
			Kind:      change.Key.Kind,
			Namespace: change.Key.Namespace,
			Name:      change.Key.Name,
		})
	}
	return out
}

func collectPredictionRestartChanges(changes []planResourceChange) []applyPredictionChange {
	var out []applyPredictionChange
	for _, change := range changes {
		if change.Kind != changeUpdate || !isWorkloadKind(change.Key.Kind) {
			continue
		}
		out = append(out, applyPredictionChange{
			Action:    string(change.Kind),
			Kind:      change.Key.Kind,
			Namespace: change.Key.Namespace,
			Name:      change.Key.Name,
		})
	}
	return out
}

func collectPredictionMissingDependencies(plan *deployPlanResult) []applyPredictionDependency {
	if plan == nil {
		return nil
	}
	var out []applyPredictionDependency
	for _, node := range plan.GraphNodes {
		if !strings.EqualFold(node.Source, "external") || node.Live {
			continue
		}
		if !predictionDependencyKind(node.Kind) {
			continue
		}
		out = append(out, applyPredictionDependency{
			Kind:      node.Kind,
			Namespace: node.Namespace,
			Name:      node.Name,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func predictionDependencyKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "configmap", "secret", "serviceaccount", "persistentvolumeclaim":
		return true
	default:
		return false
	}
}

func buildPredictionRollback(history []deploy.HistoryBreadcrumb, lastSuccessful *deploy.HistoryBreadcrumb) applyPredictionRollback {
	if lastSuccessful != nil && lastSuccessful.Revision > 0 {
		return applyPredictionRollback{
			Available:  true,
			Revision:   lastSuccessful.Revision,
			Status:     lastSuccessful.Status,
			Deployed:   lastSuccessful.DeployedAt,
			Confidence: "high",
			Note:       "Helm has a deployed revision Torque can target with rollback.",
		}
	}
	if len(history) == 0 {
		return applyPredictionRollback{
			Available:  false,
			Confidence: "medium",
			Note:       "No previous release history found; a failed new install can be cleaned up by Helm atomic/uninstall behavior.",
		}
	}
	return applyPredictionRollback{
		Available:  false,
		Confidence: "low",
		Note:       "Release history exists but no deployed revision is available.",
	}
}

func normalizeRisk(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return "High"
	case "medium":
		return "Medium"
	case "low":
		return "Low"
	default:
		return "Unknown"
	}
}

func maxPredictionRisk(current, next string) string {
	if riskRank(next) > riskRank(current) {
		return normalizeRisk(next)
	}
	return normalizeRisk(current)
}

func riskRank(value string) int {
	switch normalizeRisk(value) {
	case "High":
		return 3
	case "Medium":
		return 2
	case "Low":
		return 1
	default:
		return 0
	}
}

func uniquePredictionStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitPredictionChanges(values []applyPredictionChange, limit int) []applyPredictionChange {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitPredictionDependencies(values []applyPredictionDependency, limit int) []applyPredictionDependency {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func emptyAsDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

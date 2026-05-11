package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ingresslabs/torque/internal/deploy"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/storage/driver"
)

const rollbackProofArtifactName = "apply.rollback_proof.json"

type applyRollbackSLO struct {
	Path                string   `json:"path,omitempty"`
	SHA256              string   `json:"sha256,omitempty"`
	Size                int64    `json:"size,omitempty"`
	MinReadyPercent     *int     `json:"minReadyPercent,omitempty"`
	MaxFailedResources  *int     `json:"maxFailedResources,omitempty"`
	MaxPendingResources *int     `json:"maxPendingResources,omitempty"`
	Keys                []string `json:"keys,omitempty"`
}

type applyRollbackProof struct {
	Version              int                        `json:"version"`
	GeneratedAt          string                     `json:"generatedAt"`
	Release              string                     `json:"release"`
	Namespace            string                     `json:"namespace"`
	Chart                string                     `json:"chart,omitempty"`
	ChartVersion         string                     `json:"chartVersion,omitempty"`
	Mode                 string                     `json:"mode"`
	Outcome              string                     `json:"outcome"`
	Trigger              applyRollbackTrigger       `json:"trigger"`
	SLO                  *applyRollbackSLO          `json:"slo,omitempty"`
	HistoryBefore        []deploy.HistoryBreadcrumb `json:"historyBefore,omitempty"`
	HistoryAfter         []deploy.HistoryBreadcrumb `json:"historyAfter,omitempty"`
	LastSuccessfulBefore *deploy.HistoryBreadcrumb  `json:"lastSuccessfulBefore,omitempty"`
	LastSuccessfulAfter  *deploy.HistoryBreadcrumb  `json:"lastSuccessfulAfter,omitempty"`
	RolledBackToRevision int                        `json:"rolledBackToRevision,omitempty"`
	ResourceSnapshot     []deploy.ResourceStatus    `json:"resourceSnapshot,omitempty"`
	PhaseDurations       map[string]string          `json:"phaseDurations,omitempty"`
	RollbackCommand      string                     `json:"rollbackCommand,omitempty"`
	Evidence             []string                   `json:"evidence,omitempty"`
}

type applyRollbackTrigger struct {
	Source    string `json:"source"`
	Reason    string `json:"reason"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	FailedAt  string `json:"failedAt,omitempty"`
}

type applyRollbackProofInput struct {
	Release              string
	Namespace            string
	Chart                string
	ChartVersion         string
	Mode                 string
	Outcome              string
	TriggerSource        string
	TriggerReason        string
	Err                  error
	StartedAt            time.Time
	SLO                  *applyRollbackSLO
	HistoryBefore        []deploy.HistoryBreadcrumb
	LastSuccessfulBefore *deploy.HistoryBreadcrumb
	HistoryAfter         []deploy.HistoryBreadcrumb
	LastSuccessfulAfter  *deploy.HistoryBreadcrumb
	Resources            []deploy.ResourceStatus
	PhaseDurations       map[string]string
}

type statusSnapshotRecorder struct {
	mu   sync.Mutex
	rows []deploy.ResourceStatus
}

func (r *statusSnapshotRecorder) Update(rows []deploy.ResourceStatus) {
	if r == nil || rows == nil {
		return
	}
	cp := make([]deploy.ResourceStatus, len(rows))
	copy(cp, rows)
	r.mu.Lock()
	r.rows = cp
	r.mu.Unlock()
}

func (r *statusSnapshotRecorder) Snapshot() []deploy.ResourceStatus {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]deploy.ResourceStatus, len(r.rows))
	copy(cp, r.rows)
	return cp
}

func loadApplyRollbackSLO(path string) (*applyRollbackSLO, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --slo: %w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse --slo yaml: %w", err)
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("--slo %s is empty", path)
	}
	info, _ := os.Stat(path)
	sum := sha256.Sum256(raw)
	spec := mapLookupMap(doc, "spec")
	if spec == nil {
		spec = doc
	}
	keys := make([]string, 0, len(spec))
	for key := range spec {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := &applyRollbackSLO{
		Path:   path,
		SHA256: hex.EncodeToString(sum[:]),
		Keys:   keys,
	}
	if info != nil {
		out.Size = info.Size()
	}
	out.MinReadyPercent = mapLookupIntPtr(spec, "minReadyPercent", "min_ready_percent")
	out.MaxFailedResources = mapLookupIntPtr(spec, "maxFailedResources", "max_failed_resources")
	out.MaxPendingResources = mapLookupIntPtr(spec, "maxPendingResources", "max_pending_resources")
	return out, nil
}

func (s *applyRollbackSLO) Evaluate(rows []deploy.ResourceStatus) error {
	if s == nil {
		return nil
	}
	total := len(rows)
	ready := 0
	failed := 0
	pending := 0
	for _, row := range rows {
		switch strings.ToLower(strings.TrimSpace(row.Status)) {
		case "ready":
			ready++
		case "failed":
			failed++
		case "pending", "progressing", "unknown", "":
			pending++
		default:
			pending++
		}
	}
	if s.MaxFailedResources != nil && failed > *s.MaxFailedResources {
		return fmt.Errorf("SLO failed: %d failed resource(s), max %d", failed, *s.MaxFailedResources)
	}
	if s.MaxPendingResources != nil && pending > *s.MaxPendingResources {
		return fmt.Errorf("SLO failed: %d pending/progressing resource(s), max %d", pending, *s.MaxPendingResources)
	}
	if s.MinReadyPercent != nil && total > 0 {
		pct := (ready * 100) / total
		if pct < *s.MinReadyPercent {
			return fmt.Errorf("SLO failed: %d%% ready, min %d%%", pct, *s.MinReadyPercent)
		}
	}
	return nil
}

func runApplyRollback(ctx context.Context, actionCfg *action.Configuration, releaseName string, target *deploy.HistoryBreadcrumb, wait bool, timeout time.Duration) (string, error) {
	if actionCfg == nil {
		return "", errors.New("helm action config is unavailable")
	}
	releaseName = strings.TrimSpace(releaseName)
	if releaseName == "" {
		return "", errors.New("release name is required")
	}
	if target != nil && target.Revision > 0 {
		rollback := action.NewRollback(actionCfg)
		rollback.Version = target.Revision
		rollback.Wait = wait
		rollback.Timeout = timeout
		return "helm-rollback", rollback.Run(releaseName)
	}
	uninstall := action.NewUninstall(actionCfg)
	uninstall.Wait = wait
	uninstall.Timeout = timeout
	_, err := uninstall.Run(releaseName)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return "helm-uninstall", nil
	}
	return "helm-uninstall", err
}

func buildApplyRollbackProof(in applyRollbackProofInput) applyRollbackProof {
	now := time.Now().UTC()
	out := applyRollbackProof{
		Version:              1,
		GeneratedAt:          now.Format(time.RFC3339Nano),
		Release:              strings.TrimSpace(in.Release),
		Namespace:            strings.TrimSpace(in.Namespace),
		Chart:                strings.TrimSpace(in.Chart),
		ChartVersion:         strings.TrimSpace(in.ChartVersion),
		Mode:                 firstNonEmpty(in.Mode, "helm-atomic"),
		Outcome:              firstNonEmpty(in.Outcome, "rollback-requested"),
		SLO:                  in.SLO,
		HistoryBefore:        deploy.CloneBreadcrumbs(in.HistoryBefore),
		HistoryAfter:         deploy.CloneBreadcrumbs(in.HistoryAfter),
		LastSuccessfulBefore: deploy.CloneBreadcrumbPointer(in.LastSuccessfulBefore),
		LastSuccessfulAfter:  deploy.CloneBreadcrumbPointer(in.LastSuccessfulAfter),
		ResourceSnapshot:     append([]deploy.ResourceStatus(nil), in.Resources...),
		PhaseDurations:       cloneStringMap(in.PhaseDurations),
		RollbackCommand:      rollbackSuggestion(in.Release, in.Namespace),
		Evidence: []string{
			"Helm history before apply",
			"Helm history after rollback action",
			"resource readiness snapshot",
			"phase durations",
		},
	}
	if in.SLO != nil {
		out.Evidence = append(out.Evidence, "SLO file hash")
	}
	out.Trigger = applyRollbackTrigger{
		Source:    firstNonEmpty(in.TriggerSource, "helm"),
		Reason:    firstNonEmpty(in.TriggerReason, "apply failed"),
		StartedAt: in.StartedAt.UTC().Format(time.RFC3339Nano),
		FailedAt:  now.Format(time.RFC3339Nano),
	}
	if in.Err != nil {
		out.Trigger.Error = in.Err.Error()
	}
	if out.LastSuccessfulAfter != nil {
		out.RolledBackToRevision = out.LastSuccessfulAfter.Revision
	}
	return out
}

func writeApplyRollbackProof(ctx context.Context, rec artifactRecorder, path string, proof applyRollbackProof) (string, error) {
	raw, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return "", err
	}
	text := string(raw) + "\n"
	if rec != nil {
		if err := rec.RecordArtifact(ctx, rollbackProofArtifactName, text); err != nil {
			return "", err
		}
	}
	path = strings.TrimSpace(path)
	if path == "-" {
		fmt.Print(text)
		return "-", nil
	}
	if path == "" {
		path = defaultRollbackProofPath(proof.Release, time.Now())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return "", err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func defaultRollbackProofPath(release string, ts time.Time) string {
	slug := sanitizeFilename(release)
	if slug == "" {
		slug = "release"
	}
	return fmt.Sprintf("torque-rollback-proof-%s-%s.json", slug, ts.Format("20060102-150405"))
}

func rollbackSuggestion(release, namespace string) string {
	release = strings.TrimSpace(release)
	if release == "" {
		return ""
	}
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		return fmt.Sprintf("torque revert --release %s", shellQuoteToken(release))
	}
	return fmt.Sprintf("torque revert --release %s -n %s", shellQuoteToken(release), shellQuoteToken(ns))
}

func shellQuoteToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func mapLookupMap(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			if out, ok := raw.(map[string]any); ok {
				return out
			}
		}
	}
	return nil
}

func mapLookupIntPtr(m map[string]any, keys ...string) *int {
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case int:
			return &v
		case int64:
			i := int(v)
			return &i
		case float64:
			i := int(v)
			return &i
		}
	}
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

package capture

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type SummaryOptions struct {
	SessionID       string
	MaxFailureHints int
	MaxResources    int
}

type CaptureSummary struct {
	Path        string           `json:"path"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Sessions    []SessionSummary `json:"sessions"`
}

type SessionSummary struct {
	SessionID       string              `json:"sessionId"`
	RunID           string              `json:"runId,omitempty"`
	ParentRunID     string              `json:"parentRunId,omitempty"`
	Command         string              `json:"command"`
	Args            []string            `json:"args,omitempty"`
	StartedAt       time.Time           `json:"startedAt"`
	EndedAt         *time.Time          `json:"endedAt,omitempty"`
	Duration        string              `json:"duration,omitempty"`
	Outcome         string              `json:"outcome"`
	Host            string              `json:"host,omitempty"`
	User            string              `json:"user,omitempty"`
	Tags            map[string]string   `json:"tags,omitempty"`
	Extra           map[string]string   `json:"extra,omitempty"`
	Entities        Entities            `json:"entities,omitempty"`
	DroppedEvents   int64               `json:"droppedEvents,omitempty"`
	EventCount      int                 `json:"eventCount"`
	ArtifactCount   int                 `json:"artifactCount"`
	EventKinds      map[string]int      `json:"eventKinds,omitempty"`
	EventSources    map[string]int      `json:"eventSources,omitempty"`
	ChangeSummary   ChangeSummary       `json:"changeSummary,omitempty"`
	PrimaryCause    *Cause              `json:"primaryCause,omitempty"`
	Causes          []Cause             `json:"causes,omitempty"`
	FailureHints    []FailureHint       `json:"failureHints,omitempty"`
	ResourceHints   []ResourceHint      `json:"resourceHints,omitempty"`
	ArtifactNames   []string            `json:"artifactNames,omitempty"`
	BuildDigest     string              `json:"buildDigest,omitempty"`
	BuildTags       []string            `json:"buildTags,omitempty"`
	BuildPolicy     *PolicySummary      `json:"buildPolicy,omitempty"`
	ApplyStatus     string              `json:"applyStatus,omitempty"`
	StackNodeCount  int                 `json:"stackNodeCount,omitempty"`
	ReviewCommand   string              `json:"reviewCommand,omitempty"`
	RollbackCommand string              `json:"rollbackCommand,omitempty"`
	LogsCommand     string              `json:"logsCommand,omitempty"`
	FixCommands     []CommandSuggestion `json:"fixCommands,omitempty"`
	Suggestions     []string            `json:"suggestions,omitempty"`
}

type ChangeSummary struct {
	HasDiff           bool `json:"hasDiff,omitempty"`
	DiffLines         int  `json:"diffLines,omitempty"`
	ManifestBytes     int  `json:"manifestBytes,omitempty"`
	ManifestResources int  `json:"manifestResources,omitempty"`
}

type Cause struct {
	Category string   `json:"category"`
	Severity string   `json:"severity"`
	Resource string   `json:"resource,omitempty"`
	Message  string   `json:"message"`
	Evidence string   `json:"evidence,omitempty"`
	Fixes    []string `json:"fixes,omitempty"`
}

type CommandSuggestion struct {
	Purpose string `json:"purpose"`
	Command string `json:"command"`
}

type FailureHint struct {
	Seq       int64     `json:"seq,omitempty"`
	TS        time.Time `json:"ts,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Level     string    `json:"level,omitempty"`
	Source    string    `json:"source,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	Pod       string    `json:"pod,omitempty"`
	Container string    `json:"container,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type ResourceHint struct {
	Resource string `json:"resource"`
	Count    int    `json:"count"`
}

type PolicySummary struct {
	Passed    bool `json:"passed"`
	DenyCount int  `json:"denyCount"`
	WarnCount int  `json:"warnCount"`
}

type summarySessionRow struct {
	SessionID     string
	RunID         string
	ParentRunID   string
	Command       string
	MetaJSON      string
	StartedAtRaw  string
	EndedAtRaw    string
	Entities      Entities
	DroppedEvents int64
}

type summaryEventRow struct {
	Seq         int64
	TS          time.Time
	Kind        string
	Level       string
	Source      string
	Namespace   string
	Pod         string
	Container   string
	Message     string
	PayloadType string
	PayloadBlob []byte
	PayloadJSON string
}

// Summarize reads a capture SQLite database and returns a compact explanation-oriented summary.
func Summarize(ctx context.Context, path string, opts SummaryOptions) (*CaptureSummary, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("capture path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat capture: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.MaxFailureHints <= 0 {
		opts.MaxFailureHints = 8
	}
	if opts.MaxResources <= 0 {
		opts.MaxResources = 6
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open capture: %w", err)
	}
	defer db.Close()

	sessionRows, err := readSummarySessions(ctx, db, strings.TrimSpace(opts.SessionID))
	if err != nil {
		return nil, err
	}
	summary := &CaptureSummary{
		Path:        path,
		GeneratedAt: time.Now().UTC(),
		Sessions:    make([]SessionSummary, 0, len(sessionRows)),
	}
	for _, row := range sessionRows {
		sess, err := summarizeSession(ctx, db, row, opts, path)
		if err != nil {
			return nil, err
		}
		summary.Sessions = append(summary.Sessions, sess)
	}
	if len(summary.Sessions) == 0 {
		if opts.SessionID != "" {
			return nil, fmt.Errorf("capture session %q not found", opts.SessionID)
		}
		return nil, fmt.Errorf("capture has no sessions")
	}
	return summary, nil
}

func readSummarySessions(ctx context.Context, db *sql.DB, filter string) ([]summarySessionRow, error) {
	cols, err := sqliteTableColumns(ctx, db, "torque_capture_sessions")
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("capture is missing torque_capture_sessions")
	}
	query := fmt.Sprintf(`
SELECT
  %s, %s, %s, %s, %s, %s, %s,
  %s, %s, %s, %s, %s, %s, %s, %s
FROM torque_capture_sessions
ORDER BY %s ASC, session_id ASC
`,
		sqliteColumnExpr(cols, "session_id", "''"),
		sqliteColumnExpr(cols, "run_id", "''"),
		sqliteColumnExpr(cols, "parent_run_id", "''"),
		sqliteColumnExpr(cols, "command", "''"),
		sqliteColumnExpr(cols, "meta_json", "''"),
		sqliteColumnExpr(cols, "started_at", "''"),
		sqliteColumnExpr(cols, "ended_at", "''"),
		sqliteColumnExpr(cols, "cluster", "''"),
		sqliteColumnExpr(cols, "kube_context", "''"),
		sqliteColumnExpr(cols, "namespace", "''"),
		sqliteColumnExpr(cols, "release", "''"),
		sqliteColumnExpr(cols, "chart", "''"),
		sqliteColumnExpr(cols, "image_ref", "''"),
		sqliteColumnExpr(cols, "image_digest", "''"),
		sqliteColumnExpr(cols, "build_context", "''"),
		sqliteOrderExpr(cols, "started_at_ns", "started_at"),
	)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query capture sessions: %w", err)
	}
	defer rows.Close()

	var out []summarySessionRow
	for rows.Next() {
		var (
			row summarySessionRow
			ns  = nullableStrings(15)
		)
		if err := rows.Scan(
			&ns[0], &ns[1], &ns[2], &ns[3], &ns[4],
			&ns[5], &ns[6], &ns[7], &ns[8], &ns[9],
			&ns[10], &ns[11], &ns[12], &ns[13], &ns[14],
		); err != nil {
			return nil, fmt.Errorf("scan capture session: %w", err)
		}
		row.SessionID = nullString(ns[0])
		row.RunID = nullString(ns[1])
		row.ParentRunID = nullString(ns[2])
		row.Command = nullString(ns[3])
		row.MetaJSON = nullString(ns[4])
		row.StartedAtRaw = nullString(ns[5])
		row.EndedAtRaw = nullString(ns[6])
		row.Entities = Entities{
			Cluster:      nullString(ns[7]),
			KubeContext:  nullString(ns[8]),
			Namespace:    nullString(ns[9]),
			Release:      nullString(ns[10]),
			Chart:        nullString(ns[11]),
			ImageRef:     nullString(ns[12]),
			ImageDigest:  nullString(ns[13]),
			BuildContext: nullString(ns[14]),
		}
		if filter != "" && row.SessionID != filter && row.RunID != filter {
			continue
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read capture sessions: %w", err)
	}
	if hasSQLiteColumn(cols, "dropped_events") {
		for i := range out {
			var dropped sql.NullInt64
			if err := db.QueryRowContext(ctx, `SELECT dropped_events FROM torque_capture_sessions WHERE session_id = ?`, out[i].SessionID).Scan(&dropped); err != nil && err != sql.ErrNoRows {
				return nil, fmt.Errorf("read dropped event count: %w", err)
			}
			if dropped.Valid {
				out[i].DroppedEvents = dropped.Int64
			}
		}
	}
	return out, nil
}

func summarizeSession(ctx context.Context, db *sql.DB, row summarySessionRow, opts SummaryOptions, path string) (SessionSummary, error) {
	var meta SessionMeta
	if strings.TrimSpace(row.MetaJSON) != "" {
		_ = json.Unmarshal([]byte(row.MetaJSON), &meta)
	}
	started := parseCaptureTime(firstNonEmpty(row.StartedAtRaw, meta.StartedAt.Format(time.RFC3339Nano)))
	var ended *time.Time
	if t := parseCaptureTime(row.EndedAtRaw); !t.IsZero() {
		ended = &t
	}

	tags := map[string]string{}
	for k, v := range meta.Tags {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			tags[k] = v
		}
	}
	if dbTags, err := readSessionTags(ctx, db, row.SessionID); err == nil {
		for k, v := range dbTags {
			tags[k] = v
		}
	}
	if len(tags) == 0 {
		tags = nil
	}

	entities := row.Entities
	mergeEntities(&entities, meta.Entities)
	sess := SessionSummary{
		SessionID:     row.SessionID,
		RunID:         firstNonEmpty(row.RunID, meta.RunID),
		ParentRunID:   firstNonEmpty(row.ParentRunID, meta.ParentRunID),
		Command:       firstNonEmpty(row.Command, meta.Command),
		Args:          append([]string(nil), meta.Args...),
		StartedAt:     started,
		EndedAt:       ended,
		Host:          meta.Host,
		User:          meta.User,
		Tags:          tags,
		Extra:         meta.Extra,
		Entities:      entities,
		DroppedEvents: row.DroppedEvents,
		EventKinds:    map[string]int{},
		EventSources:  map[string]int{},
	}
	if ended != nil && !started.IsZero() {
		sess.Duration = ended.Sub(started).Round(time.Millisecond).String()
	}

	failed, warned, succeeded, err := summarizeEvents(ctx, db, &sess, opts)
	if err != nil {
		return sess, err
	}
	if err := summarizeArtifacts(ctx, db, &sess); err != nil {
		return sess, err
	}
	if sess.BuildPolicy != nil {
		if sess.BuildPolicy.DenyCount > 0 || !sess.BuildPolicy.Passed {
			failed = true
			sess.addCause(classifyCause("build policy denied the build", "", "build.policy_post_report_json"))
		} else if sess.BuildPolicy.WarnCount > 0 {
			warned = true
		}
	}
	sess.finalizeCauses()
	sess.Outcome = inferOutcome(ended, failed, warned, succeeded)
	sess.FixCommands = buildCommandSuggestions(sess, path)
	for _, suggestion := range sess.FixCommands {
		switch suggestion.Purpose {
		case "review":
			sess.ReviewCommand = suggestion.Command
		case "rollback":
			sess.RollbackCommand = suggestion.Command
		case "logs":
			sess.LogsCommand = suggestion.Command
		}
		sess.Suggestions = append(sess.Suggestions, suggestion.Command)
	}
	sess.Suggestions = dedupeStrings(sess.Suggestions)
	if len(sess.EventKinds) == 0 {
		sess.EventKinds = nil
	}
	if len(sess.EventSources) == 0 {
		sess.EventSources = nil
	}
	return sess, nil
}

func summarizeEvents(ctx context.Context, db *sql.DB, sess *SessionSummary, opts SummaryOptions) (bool, bool, bool, error) {
	eventRows, err := readSummaryEvents(ctx, db, sess.SessionID)
	if err != nil {
		return false, false, false, err
	}
	sess.EventCount = len(eventRows)

	var failed, warned, succeeded bool
	resourceCounts := map[string]int{}
	seenHints := map[string]struct{}{}
	for _, ev := range eventRows {
		if ev.Kind != "" {
			sess.EventKinds[ev.Kind]++
		}
		if ev.Source != "" {
			sess.EventSources[ev.Source]++
		}
		if resource := eventResource(ev); resource != "" {
			if eventLooksProblematic(ev) {
				resourceCounts[resource]++
			}
		}

		payload := decodeEventPayload(ev)
		if diff := payloadPathString(payload, "diff", "text"); diff != "" {
			sess.ChangeSummary.HasDiff = true
			sess.ChangeSummary.DiffLines += countNonEmptyLines(diff)
		}
		for _, resource := range payloadSlice(payload, "resources") {
			rs := resourceStatusFromMap(resource)
			if rs.Resource == "" {
				continue
			}
			if resourceLooksProblematic(rs.Status, rs.Reason, rs.Message) {
				resourceCounts[rs.Resource]++
				if looksLikeFailure(rs.Status) || looksLikeFailure(rs.Reason) || looksLikeFailure(rs.Message) {
					failed = true
				} else {
					warned = true
				}
				sess.addCause(classifyCause(firstNonEmpty(rs.Message, rs.Reason, rs.Status), rs.Resource, "resource status"))
			}
		}
		if nodeID := payloadPathString(payload, "nodeId"); nodeID != "" && eventLooksProblematic(ev) {
			sess.addCause(classifyCause(firstNonEmpty(payloadPathString(payload, "error", "message"), ev.Message), "stack/"+nodeID, ev.Source))
		}
		if status := payloadPathString(payload, "summary", "status"); status != "" {
			if looksLikeSuccess(status) {
				succeeded = true
			}
			if looksLikeFailure(status) {
				failed = true
			}
		}
		payloadMessage := firstNonEmpty(
			payloadPathString(payload, "summary", "error"),
			payloadPathString(payload, "error", "message"),
			payloadPathString(payload, "log", "message"),
			payloadPathString(payload, "phase", "message"),
		)
		if payloadMessage != "" && strings.TrimSpace(ev.Message) == "" {
			ev.Message = payloadMessage
		}

		switch {
		case eventLooksFailed(ev):
			failed = true
			sess.addCause(classifyCause(ev.Message, eventResource(ev), firstNonEmpty(ev.Source, ev.Kind)))
		case eventLooksWarning(ev):
			warned = true
			sess.addCause(classifyCause(ev.Message, eventResource(ev), firstNonEmpty(ev.Source, ev.Kind)))
		}
		if looksLikeSuccess(ev.Source) || looksLikeSuccess(ev.Message) {
			succeeded = true
		}
		if eventLooksProblematic(ev) && len(sess.FailureHints) < opts.MaxFailureHints {
			key := strings.Join([]string{ev.Kind, ev.Source, ev.Level, ev.Namespace, ev.Pod, ev.Container, ev.Message}, "\x00")
			if _, ok := seenHints[key]; !ok {
				seenHints[key] = struct{}{}
				sess.FailureHints = append(sess.FailureHints, FailureHint{
					Seq:       ev.Seq,
					TS:        ev.TS,
					Kind:      ev.Kind,
					Level:     ev.Level,
					Source:    ev.Source,
					Namespace: ev.Namespace,
					Pod:       ev.Pod,
					Container: ev.Container,
					Message:   ev.Message,
				})
			}
		}
	}
	sess.ResourceHints = topResourceHints(resourceCounts, opts.MaxResources)
	return failed, warned, succeeded, nil
}

func readSummaryEvents(ctx context.Context, db *sql.DB, sessionID string) ([]summaryEventRow, error) {
	cols, err := sqliteTableColumns(ctx, db, "torque_capture_events")
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, nil
	}
	seqExpr := "id"
	if hasSQLiteColumn(cols, "seq") {
		seqExpr = "COALESCE(seq, id)"
	}
	query := fmt.Sprintf(`
SELECT
  %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s
FROM torque_capture_events
WHERE session_id = ?
ORDER BY %s ASC, id ASC
`,
		seqExpr,
		sqliteColumnExpr(cols, "ts", "''"),
		sqliteColumnExpr(cols, "kind", "''"),
		sqliteColumnExpr(cols, "level", "''"),
		sqliteColumnExpr(cols, "source", "''"),
		sqliteColumnExpr(cols, "namespace", "''"),
		sqliteColumnExpr(cols, "pod", "''"),
		sqliteColumnExpr(cols, "container", "''"),
		sqliteColumnExpr(cols, "message", "''"),
		sqliteColumnExpr(cols, "payload_type", "''"),
		sqliteColumnExpr(cols, "payload_blob", "NULL"),
		sqliteColumnExpr(cols, "payload_json", "''"),
		seqExpr,
	)
	rows, err := db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query capture events: %w", err)
	}
	defer rows.Close()

	var out []summaryEventRow
	for rows.Next() {
		var (
			ev    summaryEventRow
			seq   sql.NullInt64
			tsRaw sql.NullString
			ns    = nullableStrings(9)
			blob  []byte
		)
		if err := rows.Scan(&seq, &tsRaw, &ns[0], &ns[1], &ns[2], &ns[3], &ns[4], &ns[5], &ns[6], &ns[7], &blob, &ns[8]); err != nil {
			return nil, fmt.Errorf("scan capture event: %w", err)
		}
		if seq.Valid {
			ev.Seq = seq.Int64
		}
		ev.TS = parseCaptureTime(nullString(tsRaw))
		ev.Kind = nullString(ns[0])
		ev.Level = nullString(ns[1])
		ev.Source = nullString(ns[2])
		ev.Namespace = nullString(ns[3])
		ev.Pod = nullString(ns[4])
		ev.Container = nullString(ns[5])
		ev.Message = nullString(ns[6])
		ev.PayloadType = nullString(ns[7])
		ev.PayloadBlob = blob
		ev.PayloadJSON = nullString(ns[8])
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read capture events: %w", err)
	}
	return out, nil
}

func summarizeArtifacts(ctx context.Context, db *sql.DB, sess *SessionSummary) error {
	artifacts, err := readSessionArtifacts(ctx, db, sess.SessionID)
	if err != nil {
		return err
	}
	sess.ArtifactCount = len(artifacts)
	nameSet := map[string]struct{}{}
	for _, art := range artifacts {
		name := strings.TrimSpace(art.Name)
		if name == "" {
			continue
		}
		nameSet[name] = struct{}{}
		switch name {
		case "build.digest":
			sess.BuildDigest = strings.TrimSpace(art.Text)
		case "build.tags_json":
			_ = json.Unmarshal([]byte(art.Text), &sess.BuildTags)
		case "build.policy_post_report_json", "build.policy_pre_report_json":
			if ps := parsePolicySummary(art.Text); ps != nil {
				sess.BuildPolicy = ps
			}
		case "apply.status":
			sess.ApplyStatus = strings.TrimSpace(art.Text)
		case "stack.plan.json":
			sess.StackNodeCount = countStackPlanNodes(art.Text)
		case "rendered_manifest", "apply.release.manifest":
			sess.ChangeSummary.ManifestBytes += len(art.Text)
			sess.ChangeSummary.ManifestResources += countManifestResources(art.Text)
		}
	}
	sess.ArtifactNames = make([]string, 0, len(nameSet))
	for name := range nameSet {
		sess.ArtifactNames = append(sess.ArtifactNames, name)
	}
	sort.Strings(sess.ArtifactNames)
	return nil
}

func readSessionArtifacts(ctx context.Context, db *sql.DB, sessionID string) ([]Artifact, error) {
	cols, err := sqliteTableColumns(ctx, db, "torque_capture_artifacts")
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, nil
	}
	seqExpr := "id"
	if hasSQLiteColumn(cols, "seq") {
		seqExpr = "COALESCE(seq, id)"
	}
	query := fmt.Sprintf(`
SELECT %s, %s, %s, %s
FROM torque_capture_artifacts
WHERE session_id = ?
ORDER BY %s ASC, id ASC
`,
		seqExpr,
		sqliteColumnExpr(cols, "ts", "''"),
		sqliteColumnExpr(cols, "name", "''"),
		sqliteColumnExpr(cols, "text", "''"),
		seqExpr,
	)
	rows, err := db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query capture artifacts: %w", err)
	}
	defer rows.Close()

	var out []Artifact
	for rows.Next() {
		var (
			art   Artifact
			seq   sql.NullInt64
			tsRaw sql.NullString
			name  sql.NullString
			text  sql.NullString
		)
		if err := rows.Scan(&seq, &tsRaw, &name, &text); err != nil {
			return nil, fmt.Errorf("scan capture artifact: %w", err)
		}
		if seq.Valid {
			art.Seq = seq.Int64
		}
		art.TS = parseCaptureTime(nullString(tsRaw))
		art.Name = nullString(name)
		art.Text = nullString(text)
		out = append(out, art)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read capture artifacts: %w", err)
	}
	return out, nil
}

func readSessionTags(ctx context.Context, db *sql.DB, sessionID string) (map[string]string, error) {
	cols, err := sqliteTableColumns(ctx, db, "torque_capture_tags")
	if err != nil || len(cols) == 0 {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM torque_capture_tags WHERE session_id = ? ORDER BY key`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out, rows.Err()
}

func sqliteTableColumns(ctx context.Context, db *sql.DB, table string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()
	cols := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		cols[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return cols, rows.Err()
}

func hasSQLiteColumn(cols map[string]struct{}, name string) bool {
	_, ok := cols[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func sqliteColumnExpr(cols map[string]struct{}, name, fallback string) string {
	if hasSQLiteColumn(cols, name) {
		return name
	}
	return fallback
}

func sqliteOrderExpr(cols map[string]struct{}, preferred, fallback string) string {
	if hasSQLiteColumn(cols, preferred) {
		return preferred
	}
	return fallback
}

func nullableStrings(n int) []sql.NullString {
	return make([]sql.NullString, n)
}

func nullString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return strings.TrimSpace(v.String)
}

func parseCaptureTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func mergeEntities(dst *Entities, src Entities) {
	if dst.Cluster == "" {
		dst.Cluster = src.Cluster
	}
	if dst.KubeContext == "" {
		dst.KubeContext = src.KubeContext
	}
	if dst.Namespace == "" {
		dst.Namespace = src.Namespace
	}
	if dst.Release == "" {
		dst.Release = src.Release
	}
	if dst.Chart == "" {
		dst.Chart = src.Chart
	}
	if dst.ImageRef == "" {
		dst.ImageRef = src.ImageRef
	}
	if dst.ImageDigest == "" {
		dst.ImageDigest = src.ImageDigest
	}
	if dst.BuildContext == "" {
		dst.BuildContext = src.BuildContext
	}
}

func decodeEventPayload(ev summaryEventRow) map[string]any {
	raw, err := DecodePayload(ev.PayloadType, ev.PayloadBlob, ev.PayloadJSON)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func payloadPathString(m map[string]any, keys ...string) string {
	if len(m) == 0 || len(keys) == 0 {
		return ""
	}
	var cur any = m
	for _, key := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = obj[key]
		if !ok {
			return ""
		}
	}
	switch v := cur.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func payloadSlice(m map[string]any, key string) []map[string]any {
	if len(m) == 0 || strings.TrimSpace(key) == "" {
		return nil
	}
	raw, ok := m[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

type resourceStatusSummary struct {
	Resource string
	Status   string
	Reason   string
	Message  string
}

func resourceStatusFromMap(m map[string]any) resourceStatusSummary {
	kind := mapString(m, "kind")
	namespace := mapString(m, "namespace")
	name := mapString(m, "name")
	resource := strings.Trim(strings.Join([]string{kind, namespace, name}, "/"), "/")
	if kind != "" && name != "" {
		if namespace != "" {
			resource = fmt.Sprintf("%s/%s/%s", kind, namespace, name)
		} else {
			resource = fmt.Sprintf("%s/%s", kind, name)
		}
	}
	return resourceStatusSummary{
		Resource: resource,
		Status:   mapString(m, "status"),
		Reason:   mapString(m, "reason"),
		Message:  mapString(m, "message"),
	}
}

func mapString(m map[string]any, key string) string {
	if len(m) == 0 {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func eventLooksProblematic(ev summaryEventRow) bool {
	return eventLooksFailed(ev) || eventLooksWarning(ev)
}

func resourceLooksProblematic(values ...string) bool {
	joined := strings.ToLower(strings.Join(values, " "))
	if strings.TrimSpace(joined) == "" {
		return false
	}
	if strings.Contains(joined, "ready") && !strings.Contains(joined, "not ready") {
		return false
	}
	for _, token := range []string{"failed", "pending", "progressing", "unknown", "backoff", "error", "warning", "denied", "forbidden", "timeout", "unschedulable"} {
		if strings.Contains(joined, token) {
			return true
		}
	}
	return false
}

func eventLooksFailed(ev summaryEventRow) bool {
	if looksLikeFailure(ev.Level) || looksLikeFailure(ev.Source) {
		return true
	}
	return looksLikeFailure(ev.Message)
}

func eventLooksWarning(ev summaryEventRow) bool {
	needle := strings.ToLower(strings.Join([]string{ev.Level, ev.Source, ev.Message}, " "))
	return strings.Contains(needle, "warn") || strings.Contains(needle, "warning")
}

func looksLikeFailure(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	for _, token := range []string{
		"failed", "failure", "error", "denied", "forbidden",
		"timeout", "timed out", "backoff", "imagepull",
		"errimagepull", "crashloop", "unhealthy", "unschedulable",
	} {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

func looksLikeSuccess(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	for _, token := range []string{"succeeded", "success", "deployed", "completed", "done"} {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

func eventResource(ev summaryEventRow) string {
	switch {
	case ev.Pod != "" && ev.Container != "":
		return fmt.Sprintf("pod/%s container/%s", ev.Pod, ev.Container)
	case ev.Pod != "":
		return "pod/" + ev.Pod
	case ev.Namespace != "":
		return "namespace/" + ev.Namespace
	default:
		return ""
	}
}

func topResourceHints(counts map[string]int, limit int) []ResourceHint {
	if len(counts) == 0 || limit <= 0 {
		return nil
	}
	out := make([]ResourceHint, 0, len(counts))
	for resource, count := range counts {
		out = append(out, ResourceHint{Resource: resource, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Resource < out[j].Resource
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func parsePolicySummary(raw string) *PolicySummary {
	var v struct {
		Passed    bool `json:"passed"`
		DenyCount int  `json:"denyCount"`
		WarnCount int  `json:"warnCount"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	return &PolicySummary{Passed: v.Passed, DenyCount: v.DenyCount, WarnCount: v.WarnCount}
}

func countStackPlanNodes(raw string) int {
	var v struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return 0
	}
	return len(v.Nodes)
}

func countManifestResources(raw string) int {
	count := 0
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "kind:") {
			count++
		}
	}
	return count
}

func countNonEmptyLines(raw string) int {
	count := 0
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func (s *SessionSummary) addCause(c Cause) {
	if s == nil || strings.TrimSpace(c.Message) == "" {
		return
	}
	if c.Category == "" {
		c.Category = "unknown_failure"
	}
	if c.Severity == "" {
		c.Severity = "high"
	}
	for _, existing := range s.Causes {
		if existing.Category == c.Category &&
			existing.Resource == c.Resource &&
			existing.Message == c.Message {
			return
		}
	}
	s.Causes = append(s.Causes, c)
}

func (s *SessionSummary) finalizeCauses() {
	if s == nil || len(s.Causes) == 0 {
		return
	}
	sort.SliceStable(s.Causes, func(i, j int) bool {
		if causeSeverityRank(s.Causes[i].Severity) == causeSeverityRank(s.Causes[j].Severity) {
			return causeCategoryRank(s.Causes[i].Category) < causeCategoryRank(s.Causes[j].Category)
		}
		return causeSeverityRank(s.Causes[i].Severity) < causeSeverityRank(s.Causes[j].Severity)
	})
	s.PrimaryCause = &s.Causes[0]
}

func classifyCause(message, resource, evidence string) Cause {
	message = strings.TrimSpace(message)
	resource = strings.TrimSpace(resource)
	evidence = strings.TrimSpace(evidence)
	needle := strings.ToLower(strings.Join([]string{message, evidence}, " "))
	c := Cause{
		Category: "unknown_failure",
		Severity: "high",
		Resource: resource,
		Message:  firstNonEmpty(message, "captured failure signal"),
		Evidence: evidence,
		Fixes: []string{
			"Inspect the captured failure hints and the affected Kubernetes resource before rerunning.",
		},
	}
	switch {
	case strings.Contains(needle, "imagepullbackoff") || strings.Contains(needle, "errimagepull") ||
		strings.Contains(needle, "pull access denied") || strings.Contains(needle, "manifest unknown") ||
		strings.Contains(needle, "failed to pull image"):
		c.Category = "image_pull"
		c.Fixes = []string{
			"Verify the image tag or digest exists in the registry.",
			"Verify imagePullSecrets or registry credentials for the namespace.",
			"Rebuild and push the image, then attach the build capture to the next apply plan.",
		}
	case strings.Contains(needle, "crashloopbackoff") || strings.Contains(needle, "back-off restarting") ||
		strings.Contains(needle, "container exited") || strings.Contains(needle, "exit code"):
		c.Category = "crash_loop"
		c.Fixes = []string{
			"Read the failing container logs and Kubernetes events.",
			"Fix the application startup error, env/config mismatch, or failing probe before rerunning apply.",
		}
	case strings.Contains(needle, "failedscheduling") || strings.Contains(needle, "unschedulable") ||
		strings.Contains(needle, "insufficient") || strings.Contains(needle, "taint") ||
		strings.Contains(needle, "node affinity"):
		c.Category = "scheduling"
		c.Fixes = []string{
			"Check node capacity, taints, selectors, affinity, and requested CPU/memory.",
			"Lower resource requests or add compatible capacity before rerunning.",
		}
	case strings.Contains(needle, "failedmount") || strings.Contains(needle, "mountvolume") ||
		strings.Contains(needle, "secret") && strings.Contains(needle, "not found") ||
		strings.Contains(needle, "configmap") && strings.Contains(needle, "not found") ||
		strings.Contains(needle, "persistentvolumeclaim") && strings.Contains(needle, "not found"):
		c.Category = "mount_or_dependency"
		c.Fixes = []string{
			"Verify referenced Secrets, ConfigMaps, PVCs, and service accounts exist in the target namespace.",
			"Run the deploy plan again after repairing missing dependencies.",
		}
	case strings.Contains(needle, "forbidden") || strings.Contains(needle, "denied") ||
		strings.Contains(needle, "policy") || strings.Contains(needle, "unauthorized"):
		c.Category = "policy_or_rbac"
		c.Fixes = []string{
			"Check verifier/policy findings and Kubernetes RBAC for the service account or kube context.",
			"Update the chart or approval policy before applying again.",
		}
	case strings.Contains(needle, "timed out") || strings.Contains(needle, "timeout") ||
		strings.Contains(needle, "deadline exceeded"):
		c.Category = "rollout_timeout"
		c.Fixes = []string{
			"Inspect the non-ready resource statuses and logs; avoid only increasing timeout until the blocker is understood.",
			"After fixing readiness, rerun apply with capture enabled.",
		}
	case strings.Contains(needle, "template") || strings.Contains(needle, "render") ||
		strings.Contains(needle, "yaml parse") || strings.Contains(needle, "helm"):
		c.Category = "helm_render_or_upgrade"
		c.Fixes = []string{
			"Run torque apply plan locally and fix the Helm template, values, or immutable field issue.",
		}
	}
	return c
}

func causeSeverityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

func causeCategoryRank(category string) int {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "policy_or_rbac":
		return 0
	case "image_pull":
		return 1
	case "crash_loop":
		return 2
	case "scheduling":
		return 3
	case "mount_or_dependency":
		return 4
	case "rollout_timeout":
		return 5
	case "helm_render_or_upgrade":
		return 6
	default:
		return 9
	}
}

func inferOutcome(ended *time.Time, failed, warned, succeeded bool) string {
	switch {
	case failed:
		return "failed"
	case ended == nil:
		return "running"
	case warned:
		return "completed_with_warnings"
	case succeeded:
		return "succeeded"
	default:
		return "completed"
	}
}

func buildCommandSuggestions(sess SessionSummary, path string) []CommandSuggestion {
	var out []CommandSuggestion
	nsFlag := ""
	if sess.Entities.Namespace != "" {
		nsFlag = " -n " + shellQuoteIfNeeded(sess.Entities.Namespace)
	}
	if sess.Entities.Chart != "" && sess.Entities.Release != "" {
		out = append(out, CommandSuggestion{
			Purpose: "review",
			Command: fmt.Sprintf("torque apply plan --chart %s --release %s%s", shellQuoteIfNeeded(sess.Entities.Chart), shellQuoteIfNeeded(sess.Entities.Release), nsFlag),
		})
	}
	if sess.Entities.Release != "" && strings.Contains(strings.ToLower(sess.Command), "apply") {
		out = append(out, CommandSuggestion{
			Purpose: "rollback",
			Command: fmt.Sprintf("torque revert --release %s%s", shellQuoteIfNeeded(sess.Entities.Release), nsFlag),
		})
	}
	if sess.Entities.Release != "" && sess.Entities.Namespace != "" {
		out = append(out, CommandSuggestion{
			Purpose: "logs",
			Command: fmt.Sprintf("torque logs deploy/%s%s --events", shellQuoteIfNeeded(sess.Entities.Release), nsFlag),
		})
	}
	if strings.Contains(strings.ToLower(sess.Command), "build") && sess.BuildDigest != "" {
		out = append(out, CommandSuggestion{
			Purpose: "attach-build-evidence",
			Command: fmt.Sprintf("torque apply plan ... --build-capture %s", shellQuoteIfNeeded(path)),
		})
	}
	if strings.TrimSpace(path) != "" {
		out = append(out, CommandSuggestion{
			Purpose: "share-explanation",
			Command: fmt.Sprintf("torque explain %s --format markdown", shellQuoteIfNeeded(path)),
		})
	}
	return dedupeCommandSuggestions(out)
}

func dedupeCommandSuggestions(values []CommandSuggestion) []CommandSuggestion {
	seen := map[string]struct{}{}
	out := make([]CommandSuggestion, 0, len(values))
	for _, value := range values {
		value.Purpose = strings.TrimSpace(value.Purpose)
		value.Command = strings.TrimSpace(value.Command)
		if value.Command == "" {
			continue
		}
		if _, ok := seen[value.Command]; ok {
			continue
		}
		seen[value.Command] = struct{}{}
		out = append(out, value)
	}
	return out
}

func shellQuoteIfNeeded(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@' || r == '=' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func dedupeStrings(values []string) []string {
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

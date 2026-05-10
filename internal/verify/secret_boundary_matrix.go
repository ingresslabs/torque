package verify

import (
	"sort"
	"strings"
	"time"
)

type SecurityBoundaryMatrix struct {
	Version     string                        `json:"version"`
	Profile     string                        `json:"profile,omitempty"`
	Source      string                        `json:"source,omitempty"`
	GeneratedAt time.Time                     `json:"generatedAt"`
	Passed      bool                          `json:"passed"`
	Summary     SecurityBoundaryMatrixSummary `json:"summary"`
	Rows        []SecurityBoundaryMatrixRow   `json:"rows"`
}

type SecurityBoundaryMatrixSummary struct {
	Total                   int `json:"total"`
	Present                 int `json:"present"`
	Passed                  int `json:"passed"`
	Failed                  int `json:"failed"`
	BlockedFindings         int `json:"blockedFindings"`
	AllowedMaterializations int `json:"allowedMaterializations"`
	References              int `json:"references"`
}

type SecurityBoundaryMatrixRow struct {
	Surface                 string         `json:"surface"`
	Boundary                string         `json:"boundary"`
	Status                  string         `json:"status"`
	Passed                  bool           `json:"passed"`
	Present                 bool           `json:"present"`
	ExpectedResourceKinds   []string       `json:"expectedResourceKinds,omitempty"`
	ResourceKinds           []string       `json:"resourceKinds,omitempty"`
	Resources               []string       `json:"resources,omitempty"`
	FieldPaths              []string       `json:"fieldPaths,omitempty"`
	FindingCount            int            `json:"findingCount"`
	AllowedMaterializations int            `json:"allowedMaterializations,omitempty"`
	ReferenceCount          int            `json:"referenceCount,omitempty"`
	Evidence                map[string]any `json:"evidence,omitempty"`
}

type securityBoundaryRowDef struct {
	surface  string
	boundary string
	kinds    []string
}

var securityBoundaryRows = []securityBoundaryRowDef{
	{surface: "Secret.data", boundary: "allowed", kinds: []string{"Secret"}},
	{surface: "Secret.stringData", boundary: "allowed", kinds: []string{"Secret"}},
	{surface: "ConfigMap.data", boundary: "blocked", kinds: []string{"ConfigMap"}},
	{surface: "ConfigMap.binaryData", boundary: "blocked", kinds: []string{"ConfigMap"}},
	{surface: "metadata.annotations", boundary: "blocked"},
	{surface: "metadata.labels", boundary: "blocked"},
	{surface: "env.value", boundary: "blocked", kinds: []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"}},
	{surface: "command", boundary: "blocked", kinds: []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"}},
	{surface: "args", boundary: "blocked", kinds: []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"}},
	{surface: "probes", boundary: "blocked", kinds: []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"}},
	{surface: "secretKeyRef", boundary: "allowed", kinds: []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"}},
	{surface: "secret volume", boundary: "allowed", kinds: []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob"}},
}

func BuildSecurityBoundaryMatrix(objects []map[string]any, report *SecretScanReport, opts SecretScanOptions) *SecurityBoundaryMatrix {
	generatedAt := opts.EvaluatedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	builder := newSecurityBoundaryMatrixBuilder()
	for _, obj := range objects {
		builder.recordObject(obj)
	}
	if report != nil {
		for _, finding := range report.Findings {
			builder.recordFinding(finding)
		}
	}
	return builder.matrix(strings.TrimSpace(opts.Profile), strings.TrimSpace(opts.Source), generatedAt)
}

type securityBoundaryMatrixBuilder struct {
	rows map[string]*SecurityBoundaryMatrixRow
}

func newSecurityBoundaryMatrixBuilder() *securityBoundaryMatrixBuilder {
	rows := make(map[string]*SecurityBoundaryMatrixRow, len(securityBoundaryRows))
	for _, def := range securityBoundaryRows {
		rows[def.surface] = &SecurityBoundaryMatrixRow{
			Surface:               def.surface,
			Boundary:              def.boundary,
			Status:                "not_present",
			Passed:                true,
			ExpectedResourceKinds: append([]string(nil), def.kinds...),
		}
	}
	return &securityBoundaryMatrixBuilder{rows: rows}
}

func (b *securityBoundaryMatrixBuilder) recordObject(obj map[string]any) {
	if obj == nil {
		return
	}
	sub := subjectFromObject(obj)
	kind := strings.TrimSpace(sub.Kind)
	meta := toMap(obj["metadata"])
	if len(toMap(meta["annotations"])) > 0 {
		b.recordSurface("metadata.annotations", sub, "metadata.annotations", len(toMap(meta["annotations"])))
	}
	if len(toMap(meta["labels"])) > 0 {
		b.recordSurface("metadata.labels", sub, "metadata.labels", len(toMap(meta["labels"])))
	}
	switch kind {
	case "Secret":
		if n := len(toMap(obj["data"])); n > 0 {
			b.recordSurface("Secret.data", sub, "data", n)
		}
		if n := len(toMap(obj["stringData"])); n > 0 {
			b.recordSurface("Secret.stringData", sub, "stringData", n)
		}
	case "ConfigMap":
		if n := len(toMap(obj["data"])); n > 0 {
			b.recordSurface("ConfigMap.data", sub, "data", n)
		}
		if n := len(toMap(obj["binaryData"])); n > 0 {
			b.recordSurface("ConfigMap.binaryData", sub, "binaryData", n)
		}
	}
	for _, spec := range podSpecsForBoundaryMatrix(obj) {
		if len(toMap(spec.meta["annotations"])) > 0 {
			b.recordSurface("metadata.annotations", sub, spec.metaPath+".annotations", len(toMap(spec.meta["annotations"])))
		}
		if len(toMap(spec.meta["labels"])) > 0 {
			b.recordSurface("metadata.labels", sub, spec.metaPath+".labels", len(toMap(spec.meta["labels"])))
		}
		b.recordPodSpec(sub, spec)
	}
}

func (b *securityBoundaryMatrixBuilder) recordPodSpec(sub Subject, spec boundaryPodSpec) {
	if spec.spec == nil {
		return
	}
	for _, group := range []string{"initContainers", "containers", "ephemeralContainers"} {
		for i, raw := range toSlice(spec.spec[group]) {
			container := toMap(raw)
			if container == nil {
				continue
			}
			base := spec.specPath + "." + group + "[" + intString(i) + "]"
			if len(toSlice(container["command"])) > 0 {
				b.recordSurface("command", sub, base+".command", len(toSlice(container["command"])))
			}
			if len(toSlice(container["args"])) > 0 {
				b.recordSurface("args", sub, base+".args", len(toSlice(container["args"])))
			}
			for _, probe := range []string{"livenessProbe", "readinessProbe", "startupProbe"} {
				if toMap(container[probe]) != nil {
					b.recordSurface("probes", sub, base+"."+probe, 1)
				}
			}
			for j, rawEnv := range toSlice(container["env"]) {
				env := toMap(rawEnv)
				if env == nil {
					continue
				}
				envPath := base + ".env[" + intString(j) + "]"
				if strings.TrimSpace(toString(env["value"])) != "" {
					b.recordSurface("env.value", sub, envPath+".value", 1)
				}
				valueFrom := toMap(env["valueFrom"])
				if valueFrom != nil && toMap(valueFrom["secretKeyRef"]) != nil {
					b.recordSurface("secretKeyRef", sub, envPath+".valueFrom.secretKeyRef", 1)
				}
			}
			for j, rawEnvFrom := range toSlice(container["envFrom"]) {
				envFrom := toMap(rawEnvFrom)
				if envFrom != nil && toMap(envFrom["secretRef"]) != nil {
					b.recordSurface("secretKeyRef", sub, base+".envFrom["+intString(j)+"].secretRef", 1)
				}
			}
		}
	}
	for i, raw := range toSlice(spec.spec["volumes"]) {
		volume := toMap(raw)
		if volume == nil {
			continue
		}
		volumePath := spec.specPath + ".volumes[" + intString(i) + "]"
		if toMap(volume["secret"]) != nil {
			b.recordSurface("secret volume", sub, volumePath+".secret", 1)
		}
		projected := toMap(volume["projected"])
		for j, rawSource := range toSlice(projected["sources"]) {
			source := toMap(rawSource)
			if source != nil && toMap(source["secret"]) != nil {
				b.recordSurface("secret volume", sub, volumePath+".projected.sources["+intString(j)+"].secret", 1)
			}
		}
	}
}

func (b *securityBoundaryMatrixBuilder) recordSurface(surface string, sub Subject, fieldPath string, count int) {
	row := b.rows[surface]
	if row == nil {
		return
	}
	row.Present = true
	row.Resources = appendUniqueString(row.Resources, resourceKey(sub))
	row.FieldPaths = appendUniqueString(row.FieldPaths, fieldPath)
	if strings.TrimSpace(sub.Kind) != "" {
		row.ResourceKinds = appendUniqueString(row.ResourceKinds, strings.TrimSpace(sub.Kind))
	}
	if surface == "Secret.data" || surface == "Secret.stringData" {
		row.AllowedMaterializations += count
	}
	if surface == "secretKeyRef" || surface == "secret volume" {
		row.ReferenceCount += count
	}
}

func (b *securityBoundaryMatrixBuilder) recordFinding(finding Finding) {
	if finding.Category != "secret_flow" {
		return
	}
	surface := securityBoundarySurfaceForFinding(finding)
	if surface == "" {
		return
	}
	row := b.rows[surface]
	if row == nil {
		return
	}
	row.Present = true
	row.FindingCount++
	row.Resources = appendUniqueString(row.Resources, firstNonEmptyString(finding.ResourceKey, resourceKey(finding.Subject)))
	row.FieldPaths = appendUniqueString(row.FieldPaths, firstNonEmptyString(finding.FieldPath, finding.Location))
	if strings.TrimSpace(finding.Subject.Kind) != "" {
		row.ResourceKinds = appendUniqueString(row.ResourceKinds, strings.TrimSpace(finding.Subject.Kind))
	}
}

func (b *securityBoundaryMatrixBuilder) matrix(profile, source string, generatedAt time.Time) *SecurityBoundaryMatrix {
	out := &SecurityBoundaryMatrix{
		Version:     "v1",
		Profile:     profile,
		Source:      source,
		GeneratedAt: generatedAt,
		Passed:      true,
	}
	for _, def := range securityBoundaryRows {
		row := b.rows[def.surface]
		if row == nil {
			continue
		}
		sort.Strings(row.ExpectedResourceKinds)
		sort.Strings(row.ResourceKinds)
		sort.Strings(row.Resources)
		sort.Strings(row.FieldPaths)
		row.Status, row.Passed = securityBoundaryStatus(row)
		row.Evidence = map[string]any{
			"expected": expectedBoundaryNarrative(row.Boundary),
		}
		out.Rows = append(out.Rows, *row)
		out.Summary.Total++
		if row.Present {
			out.Summary.Present++
		}
		if row.Passed {
			out.Summary.Passed++
		} else {
			out.Summary.Failed++
			out.Passed = false
		}
		out.Summary.BlockedFindings += row.FindingCount
		out.Summary.AllowedMaterializations += row.AllowedMaterializations
		out.Summary.References += row.ReferenceCount
	}
	return out
}

func securityBoundaryStatus(row *SecurityBoundaryMatrixRow) (string, bool) {
	if row == nil || !row.Present {
		return "not_present", true
	}
	switch strings.TrimSpace(row.Boundary) {
	case "allowed":
		if row.FindingCount > 0 {
			return "violation", false
		}
		return "allowed", true
	case "blocked":
		if row.FindingCount > 0 {
			return "blocked", true
		}
		return "clean", true
	default:
		return "unknown", true
	}
}

func expectedBoundaryNarrative(boundary string) string {
	switch strings.TrimSpace(boundary) {
	case "allowed":
		return "references or Kubernetes Secret material may appear here without raw-value findings"
	case "blocked":
		return "raw secret-like values on this surface must produce redacted secret_flow findings"
	default:
		return ""
	}
}

func securityBoundarySurfaceForFinding(finding Finding) string {
	kind := strings.TrimSpace(finding.Subject.Kind)
	path := strings.ToLower(strings.TrimSpace(firstNonEmptyString(finding.FieldPath, finding.Location)))
	switch {
	case strings.EqualFold(kind, "Secret") && strings.HasPrefix(path, "data"):
		return "Secret.data"
	case strings.EqualFold(kind, "Secret") && strings.HasPrefix(path, "stringdata"):
		return "Secret.stringData"
	case strings.EqualFold(kind, "ConfigMap") && strings.HasPrefix(path, "data"):
		return "ConfigMap.data"
	case strings.EqualFold(kind, "ConfigMap") && strings.HasPrefix(path, "binarydata"):
		return "ConfigMap.binaryData"
	case strings.Contains(path, "metadata.annotations"):
		return "metadata.annotations"
	case strings.Contains(path, "metadata.labels"):
		return "metadata.labels"
	case strings.Contains(path, "secretkeyref") || strings.Contains(path, "secretref"):
		return "secretKeyRef"
	case strings.Contains(path, "volumes") && strings.Contains(path, "secret"):
		return "secret volume"
	case strings.Contains(path, "probe"):
		return "probes"
	case strings.Contains(path, "env[") && strings.HasSuffix(path, ".value"):
		return "env.value"
	case strings.Contains(path, "command[") || strings.HasSuffix(path, ".command"):
		return "command"
	case strings.Contains(path, "args[") || strings.HasSuffix(path, ".args"):
		return "args"
	default:
		return ""
	}
}

type boundaryPodSpec struct {
	spec     map[string]any
	specPath string
	meta     map[string]any
	metaPath string
}

func podSpecsForBoundaryMatrix(obj map[string]any) []boundaryPodSpec {
	kind := strings.TrimSpace(toString(obj["kind"]))
	spec := toMap(obj["spec"])
	switch kind {
	case "Pod":
		return []boundaryPodSpec{{
			spec:     spec,
			specPath: "spec",
			meta:     toMap(obj["metadata"]),
			metaPath: "metadata",
		}}
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		template := toMap(spec["template"])
		return []boundaryPodSpec{{
			spec:     toMap(template["spec"]),
			specPath: "spec.template.spec",
			meta:     toMap(template["metadata"]),
			metaPath: "spec.template.metadata",
		}}
	case "Job":
		template := toMap(spec["template"])
		return []boundaryPodSpec{{
			spec:     toMap(template["spec"]),
			specPath: "spec.template.spec",
			meta:     toMap(template["metadata"]),
			metaPath: "spec.template.metadata",
		}}
	case "CronJob":
		template := toMap(toMap(toMap(spec["jobTemplate"])["spec"])["template"])
		return []boundaryPodSpec{{
			spec:     toMap(template["spec"]),
			specPath: "spec.jobTemplate.spec.template.spec",
			meta:     toMap(template["metadata"]),
			metaPath: "spec.jobTemplate.spec.template.metadata",
		}}
	default:
		return nil
	}
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func intString(v int) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var out [20]byte
	i := len(out)
	for v > 0 {
		i--
		out[i] = digits[v%10]
		v /= 10
	}
	return string(out[i:])
}

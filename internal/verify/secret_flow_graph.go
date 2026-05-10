package verify

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type SecretFlowGraph struct {
	Version     string                 `json:"version"`
	GeneratedAt time.Time              `json:"generatedAt"`
	Source      string                 `json:"source,omitempty"`
	Profile     string                 `json:"profile,omitempty"`
	Summary     SecretFlowGraphSummary `json:"summary"`
	Nodes       []SecretFlowNode       `json:"nodes,omitempty"`
	Edges       []SecretFlowEdge       `json:"edges,omitempty"`
}

type SecretFlowGraphSummary struct {
	Nodes                   int  `json:"nodes"`
	Edges                   int  `json:"edges"`
	ForbiddenFlows          int  `json:"forbiddenFlows"`
	AllowedMaterializations int  `json:"allowedMaterializations"`
	SecretReferences        int  `json:"secretReferences"`
	RedactedOutputs         int  `json:"redactedOutputs"`
	RawSecretStored         bool `json:"rawSecretStored"`
}

type SecretFlowNode struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Stage        string  `json:"stage,omitempty"`
	Surface      string  `json:"surface,omitempty"`
	Boundary     string  `json:"boundary,omitempty"`
	ResourceKey  string  `json:"resourceKey,omitempty"`
	ResourceKind string  `json:"resourceKind,omitempty"`
	FieldPath    string  `json:"fieldPath,omitempty"`
	Path         string  `json:"path,omitempty"`
	Line         int     `json:"line,omitempty"`
	Subject      Subject `json:"subject,omitempty"`
	RuleID       string  `json:"ruleId,omitempty"`
	ValuePreview string  `json:"valuePreview,omitempty"`
	RawStored    bool    `json:"rawStored"`
}

type SecretFlowEdge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Kind     string `json:"kind"`
	Boundary string `json:"boundary,omitempty"`
	RuleID   string `json:"ruleId,omitempty"`
	Redacted bool   `json:"redacted"`
}

func (s *secretScanState) addFlowEvent(kind string, c secretCandidate, match secretMatch, boundary string, preview string, fingerprint string) {
	if !s.opts.FlowGraph {
		return
	}
	ruleID := strings.TrimSpace(match.ruleID)
	if ruleID == "" {
		ruleID = "secret/flow"
	}
	s.flowEvents = append(s.flowEvents, secretFlowEvent{
		Kind:         strings.TrimSpace(kind),
		Stage:        firstNonEmptyString(c.stage, s.opts.Stage, "source"),
		Source:       firstNonEmptyString(c.source, s.opts.Source, s.sourceDefault),
		Line:         c.sourceLine,
		Resource:     strings.TrimSpace(c.resource),
		ResourceKind: strings.TrimSpace(c.kind),
		Subject:      c.subject,
		FieldPath:    strings.TrimSpace(c.fieldPath),
		Location:     strings.TrimSpace(c.location),
		RuleID:       ruleID,
		Detector:     strings.TrimSpace(match.detector),
		Preview:      strings.TrimSpace(preview),
		Fingerprint:  strings.TrimSpace(fingerprint),
		Boundary:     strings.TrimSpace(boundary),
		Surface:      firstNonEmptyString(s.opts.Surface, "verifier.report"),
		RawStored:    false,
	})
}

func (s *secretScanState) buildFlowGraph() *SecretFlowGraph {
	graph := &SecretFlowGraph{
		Version:     "v1",
		GeneratedAt: s.evaluatedAt,
		Source:      s.sourceDefault,
		Profile:     strings.TrimSpace(s.opts.Profile),
	}
	if len(s.flowEvents) == 0 {
		return graph
	}

	nodes := make(map[string]SecretFlowNode, len(s.flowEvents)*3)
	edges := make(map[string]SecretFlowEdge, len(s.flowEvents)*2)
	upsertNode := func(node SecretFlowNode) {
		if strings.TrimSpace(node.ID) == "" {
			return
		}
		if existing, ok := nodes[node.ID]; ok {
			if existing.ValuePreview == "" {
				existing.ValuePreview = node.ValuePreview
			}
			if existing.RuleID == "" {
				existing.RuleID = node.RuleID
			}
			if existing.Subject.Kind == "" {
				existing.Subject = node.Subject
			}
			nodes[node.ID] = existing
			return
		}
		nodes[node.ID] = node
	}
	upsertEdge := func(edge SecretFlowEdge) {
		if strings.TrimSpace(edge.ID) == "" || strings.TrimSpace(edge.From) == "" || strings.TrimSpace(edge.To) == "" {
			return
		}
		edges[edge.ID] = edge
	}

	for _, event := range s.flowEvents {
		sourceID := flowNodeID("source", event.Stage, event.Source, fmt.Sprint(event.Line), event.FieldPath, event.RuleID)
		boundaryID := flowNodeID("boundary", event.Kind, event.Resource, event.Location, event.FieldPath, event.RuleID)
		reportID := flowNodeID("redaction", event.Surface, event.RuleID)

		upsertNode(SecretFlowNode{
			ID:        sourceID,
			Kind:      "source",
			Stage:     event.Stage,
			Surface:   "input",
			Path:      event.Source,
			Line:      event.Line,
			FieldPath: event.FieldPath,
			RuleID:    event.RuleID,
			RawStored: false,
		})
		upsertNode(SecretFlowNode{
			ID:           boundaryID,
			Kind:         "boundary",
			Stage:        firstNonEmptyString(event.Stage, "render"),
			Surface:      firstNonEmptyString(event.Location, event.FieldPath),
			Boundary:     event.Boundary,
			ResourceKey:  event.Resource,
			ResourceKind: event.ResourceKind,
			FieldPath:    event.FieldPath,
			Path:         event.Source,
			Line:         event.Line,
			Subject:      event.Subject,
			RuleID:       event.RuleID,
			ValuePreview: event.Preview,
			RawStored:    false,
		})
		upsertEdge(SecretFlowEdge{
			ID:       flowEdgeID(sourceID, boundaryID, event.Kind, event.RuleID),
			From:     sourceID,
			To:       boundaryID,
			Kind:     event.Kind,
			Boundary: event.Boundary,
			RuleID:   event.RuleID,
			Redacted: true,
		})

		switch event.Kind {
		case "forbidden_boundary":
			graph.Summary.ForbiddenFlows++
			graph.Summary.RedactedOutputs++
			upsertNode(SecretFlowNode{
				ID:           reportID,
				Kind:         "redaction",
				Stage:        "report",
				Surface:      event.Surface,
				Boundary:     "redacted",
				RuleID:       event.RuleID,
				ValuePreview: event.Preview,
				RawStored:    false,
			})
			upsertEdge(SecretFlowEdge{
				ID:       flowEdgeID(boundaryID, reportID, "redacted_output", event.RuleID),
				From:     boundaryID,
				To:       reportID,
				Kind:     "redacted_output",
				Boundary: "redacted",
				RuleID:   event.RuleID,
				Redacted: true,
			})
		case "allowed_materialization":
			graph.Summary.AllowedMaterializations++
		case "secret_reference":
			graph.Summary.SecretReferences++
		}
	}

	graph.Nodes = make([]SecretFlowNode, 0, len(nodes))
	for _, node := range nodes {
		graph.Nodes = append(graph.Nodes, node)
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	graph.Edges = make([]SecretFlowEdge, 0, len(edges))
	for _, edge := range edges {
		graph.Edges = append(graph.Edges, edge)
	}
	sort.Slice(graph.Edges, func(i, j int) bool { return graph.Edges[i].ID < graph.Edges[j].ID })
	graph.Summary.Nodes = len(graph.Nodes)
	graph.Summary.Edges = len(graph.Edges)
	graph.Summary.RawSecretStored = false
	return graph
}

func flowNodeID(parts ...string) string {
	return "secret-flow-node:sha256:" + SHA256Hex(strings.Join(cleanFlowIDParts(parts...), "|"))
}

func flowEdgeID(parts ...string) string {
	return "secret-flow-edge:sha256:" + SHA256Hex(strings.Join(cleanFlowIDParts(parts...), "|"))
}

func cleanFlowIDParts(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}

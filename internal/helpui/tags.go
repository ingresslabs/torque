package helpui

import "strings"

var commandTags = map[string][]string{
	"torque contract":            {"contract", "runtime", "guardian", "incident", "proof"},
	"torque contract synthesize": {"contract", "runtime", "recurrence", "proof"},
	"torque contract test":       {"contract", "runtime", "gate", "proof"},
	"torque proof":               {"proof", "graph", "signature", "evidence"},
	"torque proof graph":         {"proof", "graph", "release", "html", "signature"},
	"torque proof verify":        {"proof", "verify", "signature", "hash"},
	"torque proof diff":          {"proof", "diff", "evidence"},
	"torque proof gate":          {"proof", "gate", "policy", "release"},
	"torque proof attest":        {"proof", "attest", "signature", "release"},
	"torque agent":               {"agent", "policy", "proof", "gate", "authorization"},
	"torque agent policy":        {"agent", "policy", "proof", "gate"},
	"torque agent policy check":  {"agent", "policy", "proof", "gate", "authorization"},
	"torque agent run":           {"agent", "run", "proof", "authorization"},
	"torque release":             {"release", "autopilot", "score", "proof", "gate"},
	"torque release autopilot":   {"release", "autopilot", "agent", "flight", "attest", "proof", "gate"},
	"torque release score":       {"release", "score", "readiness", "proof", "gate"},
	"torque flight":              {"flight", "recorder", "timeline", "proof", "release"},
	"torque flight record":       {"flight", "record", "timeline", "proof"},
	"torque flight replay":       {"flight", "replay", "timeline", "proof"},
	"torque flight explain":      {"flight", "explain", "timeline", "proof"},
	"torque init":                {"onboarding", "setup"},
	"torque help":                {"onboarding"},
	"torque security":            {"security", "benchmark", "evidence"},
	"torque security benchmark":  {"security", "benchmark", "secrets", "redaction", "evidence"},
}

func tagsForCommand(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if tags, ok := commandTags[path]; ok && len(tags) > 0 {
		out := make([]string, 0, len(tags))
		out = append(out, tags...)
		return out
	}
	return nil
}

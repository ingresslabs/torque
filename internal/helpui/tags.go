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

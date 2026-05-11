package helpui

import "strings"

var commandOwners = map[string][]string{
	"torque apply":          {"internal/deploy", "internal/ui"},
	"torque apply plan":     {"internal/deploy", "internal/ui"},
	"torque apply simulate": {"internal/deploy", "internal/deployplan"},
	"torque build":          {"internal/workflows/buildsvc"},
	"torque contract":       {"cmd/torque", "internal/deploy"},
	"torque delete":         {"internal/deploy", "internal/ui"},
	"torque init":           {"internal/appconfig"},
	"torque guardian":       {"cmd/torque", "internal/deployplan"},
	"torque help":           {"internal/helpui"},
	"torque incident":       {"cmd/torque", "internal/deploy"},
	"torque logs":           {"internal/tailer"},
	"torque replay":         {"cmd/torque"},
	"torque secrets":        {"internal/secretstore"},
	"torque stack":          {"internal/stack"},
}

func ownersForCommand(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	// Allow subcommands to inherit ownership from parent commands.
	for candidate := path; candidate != ""; {
		if owners, ok := commandOwners[candidate]; ok && len(owners) > 0 {
			out := make([]string, 0, len(owners))
			out = append(out, owners...)
			return out
		}
		idx := strings.LastIndex(candidate, " ")
		if idx < 0 {
			break
		}
		candidate = strings.TrimSpace(candidate[:idx])
	}
	return nil
}

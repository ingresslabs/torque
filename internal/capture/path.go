package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ResolvePath expands capture paths. If requested is empty or "__auto__",
// it creates a default filename in the current working directory.
func ResolvePath(commandPath, requested string, now time.Time) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == "__auto__" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base := strings.ReplaceAll(strings.TrimSpace(commandPath), " ", "-")
		base = strings.ReplaceAll(base, string(os.PathSeparator), "-")
		base = strings.ReplaceAll(base, ":", "-")
		base = strings.Trim(base, "-")
		if base == "" {
			base = "torque"
		}
		name := fmt.Sprintf("torque-capture-%s-%s.sqlite", base, now.UTC().Format("20060102-150405"))
		return filepath.Join(wd, name), nil
	}
	return requested, nil
}

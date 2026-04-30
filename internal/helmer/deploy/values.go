// File: internal/deploy/values.go
// Brief: Shared values/helpers for template rendering.

package deploy

import (
	"fmt"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
	cliValues "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
)

func buildValues(settings *cli.EnvSettings, files, setVals, setStringVals, setFileVals []string) (map[string]interface{}, error) {
	valOpts := &cliValues.Options{
		ValueFiles:   files,
		Values:       setVals,
		StringValues: setStringVals,
		FileValues:   setFileVals,
	}
	providers := getter.All(settings)
	vals, err := valOpts.MergeValues(providers)
	if err != nil {
		return nil, fmt.Errorf("merge values: %w", err)
	}
	return vals, nil
}

func ensureInstallable(ch *chart.Chart) error {
	if ch.Metadata == nil {
		return fmt.Errorf("chart metadata missing")
	}
	chartType := ch.Metadata.Type
	if chartType == "" || chartType == "application" {
		return nil
	}
	return fmt.Errorf("%s charts are not installable", chartType)
}

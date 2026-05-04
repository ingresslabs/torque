package capture

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type Artifact struct {
	Seq  int64
	TS   time.Time
	Name string
	Text string
}

func ReadArtifacts(ctx context.Context, path string, names ...string) ([]Artifact, error) {
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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open capture: %w", err)
	}
	defer db.Close()

	var (
		rows *sql.Rows
		args []any
	)
	cleanNames := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			cleanNames = append(cleanNames, name)
		}
	}
	if len(cleanNames) == 0 {
		rows, err = db.QueryContext(ctx, `
SELECT seq, ts, name, text
FROM ktl_capture_artifacts
ORDER BY seq ASC
`)
	} else {
		placeholders := make([]string, len(cleanNames))
		for i, name := range cleanNames {
			placeholders[i] = "?"
			args = append(args, name)
		}
		query := `
SELECT seq, ts, name, text
FROM ktl_capture_artifacts
WHERE name IN (` + strings.Join(placeholders, ",") + `)
ORDER BY seq ASC
`
		rows, err = db.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("query capture artifacts: %w", err)
	}
	defer rows.Close()

	var out []Artifact
	for rows.Next() {
		var (
			art   Artifact
			tsRaw string
		)
		if err := rows.Scan(&art.Seq, &tsRaw, &art.Name, &art.Text); err != nil {
			return nil, fmt.Errorf("scan capture artifact: %w", err)
		}
		if ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(tsRaw)); err == nil {
			art.TS = ts
		}
		out = append(out, art)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read capture artifacts: %w", err)
	}
	return out, nil
}

func LatestArtifactText(artifacts []Artifact, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for i := len(artifacts) - 1; i >= 0; i-- {
		if strings.TrimSpace(artifacts[i].Name) == name {
			return artifacts[i].Text
		}
	}
	return ""
}

func ArtifactNames(artifacts []Artifact) []string {
	seen := map[string]struct{}{}
	for _, art := range artifacts {
		name := strings.TrimSpace(art.Name)
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

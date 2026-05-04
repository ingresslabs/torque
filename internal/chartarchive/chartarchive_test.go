// File: internal/chartarchive/chartarchive_test.go
// Brief: Tests for packaging chart archives into sqlite.

package chartarchive

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestPackageDirWritesSQLiteArchive(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	chartYAML := `apiVersion: v2
name: demo
version: 0.1.0
appVersion: "1.0.0"
`
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte("replicaCount: 1\n"), 0o644); err != nil {
		t.Fatalf("write values.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "templates", "deployment.yaml"), []byte("kind: Deployment\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, ".helmignore"), []byte("secret.txt\n"), 0o644); err != nil {
		t.Fatalf("write .helmignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "secret.txt"), []byte("do-not-ship\n"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	outDir := t.TempDir()
	res, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: outDir})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	if res.ArchivePath == "" {
		t.Fatalf("expected archive path to be populated")
	}
	if _, err := os.Stat(res.ArchivePath); err != nil {
		t.Fatalf("stat archive: %v", err)
	}

	db, err := sql.Open("sqlite", res.ArchivePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var archiveType string
	if err := db.QueryRow(`SELECT value FROM torque_archive_meta WHERE key = 'torque_archive_type'`).Scan(&archiveType); err != nil {
		t.Fatalf("read archive type: %v", err)
	}
	if archiveType != "chart" {
		t.Fatalf("expected archive type chart, got %q", archiveType)
	}

	var chartName string
	if err := db.QueryRow(`SELECT value FROM torque_archive_meta WHERE key = 'chart_name'`).Scan(&chartName); err != nil {
		t.Fatalf("read chart name: %v", err)
	}
	if chartName != "demo" {
		t.Fatalf("expected chart name demo, got %q", chartName)
	}

	var chartDirMeta string
	if err := db.QueryRow(`SELECT value FROM torque_archive_meta WHERE key = 'chart_dir'`).Scan(&chartDirMeta); err != nil {
		t.Fatalf("read chart_dir: %v", err)
	}
	if chartDirMeta == "" {
		t.Fatalf("expected chart_dir to be populated")
	}

	var contentSHA string
	if err := db.QueryRow(`SELECT value FROM torque_archive_meta WHERE key = 'content_sha256'`).Scan(&contentSHA); err != nil {
		t.Fatalf("read content_sha256: %v", err)
	}
	if contentSHA == "" {
		t.Fatalf("expected content_sha256 to be populated")
	}
	var manifestJSON string
	if err := db.QueryRow(`SELECT value FROM torque_archive_meta WHERE key = 'file_manifest_json'`).Scan(&manifestJSON); err != nil {
		t.Fatalf("read file_manifest_json: %v", err)
	}
	var manifest []FileManifestEntry
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		t.Fatalf("parse file manifest: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM torque_chart_files`).Scan(&count); err != nil {
		t.Fatalf("count files: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected files to be packaged")
	}
	if len(manifest) != count {
		t.Fatalf("manifest entries=%d sqlite rows=%d", len(manifest), count)
	}

	var secretCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM torque_chart_files WHERE path = 'secret.txt'`).Scan(&secretCount); err != nil {
		t.Fatalf("query secret: %v", err)
	}
	if secretCount != 0 {
		t.Fatalf("expected secret.txt to be ignored by .helmignore")
	}
}

func TestPackageDirIncludesSubcharts(t *testing.T) {
	chartDir := t.TempDir()
	writeChartFile(t, chartDir, "Chart.yaml", "apiVersion: v2\nname: parent\nversion: 0.1.0\n")
	writeChartFile(t, chartDir, "values.yaml", "replicaCount: 1\n")
	writeChartFile(t, chartDir, "charts/worker/Chart.yaml", "apiVersion: v2\nname: worker\nversion: 0.1.0\n")
	writeChartFile(t, chartDir, "charts/worker/templates/job.yaml", "kind: Job\n")

	res, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: t.TempDir()})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	db, err := sql.Open("sqlite", res.ArchivePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, path := range []string{
		"Chart.yaml",
		"values.yaml",
		"charts/worker/Chart.yaml",
		"charts/worker/templates/job.yaml",
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(1) FROM torque_chart_files WHERE path = ?`, path).Scan(&count); err != nil {
			t.Fatalf("query %s: %v", path, err)
		}
		if count != 1 {
			t.Fatalf("expected %s to be packaged, count=%d", path, count)
		}
	}
}

func writeChartFile(t *testing.T, root string, rel string, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestPackageDirRequiresForceToOverwrite(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}

	outDir := t.TempDir()
	res, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: outDir})
	if err != nil {
		t.Fatalf("package: %v", err)
	}

	if _, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: res.ArchivePath}); err == nil {
		t.Fatalf("expected overwrite to fail without force")
	}

	if _, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: res.ArchivePath, Force: true}); err != nil {
		t.Fatalf("expected overwrite with force to succeed: %v", err)
	}
}

func TestPackageDirErrorsOnMissingChartYAML(t *testing.T) {
	chartDir := t.TempDir()
	_, err := PackageDir(context.Background(), chartDir, PackageOptions{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "chart.yaml not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

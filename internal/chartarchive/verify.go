package chartarchive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type VerifyResult struct {
	ArchivePath    string `json:"archivePath"`
	ChartName      string `json:"chartName,omitempty"`
	ChartVersion   string `json:"chartVersion,omitempty"`
	ChartDir       string `json:"chartDir,omitempty"`
	ManifestSHA256 string `json:"manifestSha256,omitempty"`
	FileCount      int    `json:"fileCount"`
	TotalBytes     int64  `json:"totalBytes"`
	ContentSHA256  string `json:"contentSha256,omitempty"`
}

func VerifyArchive(ctx context.Context, path string) (*VerifyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("archive path is required")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()

	meta, err := readMeta(ctx, db)
	if err != nil {
		return nil, err
	}
	if meta["torque_archive_type"] != archiveType {
		return nil, fmt.Errorf("unexpected archive type %q (want %q)", meta["torque_archive_type"], archiveType)
	}
	if meta["torque_archive_version"] != archiveVersion {
		return nil, fmt.Errorf("unexpected archive version %q (want %q)", meta["torque_archive_version"], archiveVersion)
	}

	rows, err := db.QueryContext(ctx, `SELECT path, mode, sha256, size, data FROM torque_chart_files ORDER BY path ASC`)
	if err != nil {
		return nil, fmt.Errorf("read chart files: %w", err)
	}
	defer rows.Close()

	digest := sha256.New()
	var (
		fileCount  int
		totalBytes int64
		manifest   []FileManifestEntry
	)
	for rows.Next() {
		var (
			p    string
			mode int64
			sha  string
			size int64
			data []byte
		)
		if err := rows.Scan(&p, &mode, &sha, &size, &data); err != nil {
			return nil, fmt.Errorf("scan chart file: %w", err)
		}
		if err := validateArchivePath(p); err != nil {
			return nil, err
		}
		actual := sha256.Sum256(data)
		actualHex := fmt.Sprintf("%x", actual[:])
		if strings.TrimSpace(sha) != actualHex {
			return nil, fmt.Errorf("sha256 mismatch for %s", p)
		}
		if size != int64(len(data)) {
			return nil, fmt.Errorf("size mismatch for %s (expected %d got %d)", p, size, len(data))
		}
		recordDigest(digest, p, actualHex)
		manifest = append(manifest, FileManifestEntry{
			Path:   p,
			Mode:   mode,
			Size:   size,
			SHA256: actualHex,
		})
		fileCount++
		totalBytes += int64(len(data))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chart files: %w", err)
	}

	contentSHA := fmt.Sprintf("%x", digest.Sum(nil))
	if expected := strings.TrimSpace(meta["content_sha256"]); expected != "" && expected != contentSHA {
		return nil, fmt.Errorf("content_sha256 mismatch (expected %s got %s)", expected, contentSHA)
	}
	if err := verifyMetaCounts(meta, fileCount, totalBytes); err != nil {
		return nil, err
	}
	manifestSHA, err := verifyManifestMeta(meta, manifest)
	if err != nil {
		return nil, err
	}

	return &VerifyResult{
		ArchivePath:    path,
		ChartName:      strings.TrimSpace(meta["chart_name"]),
		ChartVersion:   strings.TrimSpace(meta["chart_version"]),
		ChartDir:       strings.TrimSpace(meta["chart_dir"]),
		ManifestSHA256: manifestSHA,
		FileCount:      fileCount,
		TotalBytes:     totalBytes,
		ContentSHA256:  firstNonEmpty(strings.TrimSpace(meta["content_sha256"]), contentSHA),
	}, nil
}

func readMeta(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM torque_archive_meta`)
	if err != nil {
		return nil, fmt.Errorf("read archive meta: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan archive meta: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate archive meta: %w", err)
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func verifyMetaCounts(meta map[string]string, fileCount int, totalBytes int64) error {
	if raw := strings.TrimSpace(meta["file_count"]); raw != "" {
		expected, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("invalid file_count metadata %q", raw)
		}
		if expected != fileCount {
			return fmt.Errorf("file_count mismatch (expected %d got %d)", expected, fileCount)
		}
	}
	if raw := strings.TrimSpace(meta["total_bytes"]); raw != "" {
		expected, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid total_bytes metadata %q", raw)
		}
		if expected != totalBytes {
			return fmt.Errorf("total_bytes mismatch (expected %d got %d)", expected, totalBytes)
		}
	}
	return nil
}

func verifyManifestMeta(meta map[string]string, actual []FileManifestEntry) (string, error) {
	raw := strings.TrimSpace(meta["file_manifest_json"])
	if raw == "" {
		return "", nil
	}
	var expected []FileManifestEntry
	if err := json.Unmarshal([]byte(raw), &expected); err != nil {
		return "", fmt.Errorf("parse file_manifest_json: %w", err)
	}
	if len(expected) != len(actual) {
		return "", fmt.Errorf("file manifest count mismatch (expected %d got %d)", len(expected), len(actual))
	}
	for i := range expected {
		if err := validateArchivePath(expected[i].Path); err != nil {
			return "", err
		}
	}
	if !reflect.DeepEqual(expected, actual) {
		return "", fmt.Errorf("file manifest mismatch")
	}
	_, manifestSHA, err := encodeFileManifest(expected)
	if err != nil {
		return "", err
	}
	if expectedSHA := strings.TrimSpace(meta["file_manifest_sha256"]); expectedSHA != "" && expectedSHA != manifestSHA {
		return "", fmt.Errorf("file_manifest_sha256 mismatch (expected %s got %s)", expectedSHA, manifestSHA)
	}
	return firstNonEmpty(strings.TrimSpace(meta["file_manifest_sha256"]), manifestSHA), nil
}

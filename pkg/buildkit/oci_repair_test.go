package buildkit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestRepairOCILayoutIndexRepointsMissingDescriptor(t *testing.T) {
	dir := t.TempDir()
	blobDir := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("write oci-layout: %v", err)
	}

	configDigest := writeOCIRepairBlob(t, blobDir, []byte(`{"architecture":"amd64","os":"linux"}`))
	layerDigest := writeOCIRepairBlob(t, blobDir, []byte("layer"))
	manifest := fmt.Sprintf(`{
		"schemaVersion": 2,
		"mediaType": %q,
		"config": {"mediaType": %q, "digest": %q, "size": 37},
		"layers": [{"mediaType": %q, "digest": %q, "size": 5}]
	}`,
		string(types.OCIManifestSchema1),
		string(types.OCIConfigJSON),
		configDigest,
		string(types.OCILayer),
		layerDigest,
	)
	manifestDigest := writeOCIRepairBlob(t, blobDir, []byte(manifest))
	missingSum := sha256.Sum256([]byte("missing"))
	missingDigest := "sha256:" + hex.EncodeToString(missingSum[:])

	index := fmt.Sprintf(`{
		"schemaVersion": 2,
		"mediaType": %q,
		"manifests": [{
			"mediaType": %q,
			"digest": %q,
			"size": 502,
			"annotations": {"org.opencontainers.image.ref.name": "dev"},
			"platform": {"architecture": "amd64", "os": "linux"}
		}]
	}`, string(types.OCIImageIndex), string(types.DockerManifestSchema2), missingDigest)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if err := repairOCILayoutIndex(dir); err != nil {
		t.Fatalf("repairOCILayoutIndex returned error: %v", err)
	}

	var repaired ociIndex
	if err := readJSON(filepath.Join(dir, "index.json"), &repaired); err != nil {
		t.Fatalf("read repaired index: %v", err)
	}
	if len(repaired.Manifests) != 1 {
		t.Fatalf("expected one manifest, got %d", len(repaired.Manifests))
	}
	got := repaired.Manifests[0]
	if got.Digest != manifestDigest {
		t.Fatalf("digest = %q, want %q", got.Digest, manifestDigest)
	}
	if got.MediaType != string(types.OCIManifestSchema1) {
		t.Fatalf("mediaType = %q, want %q", got.MediaType, types.OCIManifestSchema1)
	}
	if got.Size == 0 {
		t.Fatalf("expected repaired descriptor size")
	}
	if got.Annotations["org.opencontainers.image.ref.name"] != "dev" {
		t.Fatalf("expected annotations to be preserved: %#v", got.Annotations)
	}
}

func writeOCIRepairBlob(t *testing.T, blobDir string, content []byte) string {
	t.Helper()
	sum := sha256.Sum256(content)
	hexDigest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(blobDir, hexDigest), content, 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	return "sha256:" + hexDigest
}

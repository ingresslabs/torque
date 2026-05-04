package buildkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/types"
)

type ociRepairCandidate struct {
	digest   string
	media    string
	size     int64
	platform ociImagePlatform
}

type ociImagePlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}

func repairOCILayoutIndex(layoutDir string) error {
	layoutDir = strings.TrimSpace(layoutDir)
	if layoutDir == "" {
		return nil
	}

	indexPath := filepath.Join(layoutDir, "index.json")
	var idx ociIndex
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}

	var candidates []ociRepairCandidate
	var changed bool
	for i := range idx.Manifests {
		desc := &idx.Manifests[i]
		if descriptorBlobExists(layoutDir, desc.Digest) {
			continue
		}
		if candidates == nil {
			var err error
			candidates, err = collectOCIManifestCandidates(layoutDir)
			if err != nil {
				return err
			}
		}
		candidate, ok := selectOCIRepairCandidate(candidates, desc.Platform)
		if !ok {
			return fmt.Errorf("oci layout index references missing blob %s and no replacement manifest was found", desc.Digest)
		}
		desc.Digest = candidate.digest
		desc.MediaType = candidate.media
		desc.Size = candidate.size
		changed = true
	}
	if !changed {
		return nil
	}
	if idx.SchemaVersion == 0 {
		idx.SchemaVersion = 2
	}
	if strings.TrimSpace(idx.MediaType) == "" {
		idx.MediaType = string(types.OCIImageIndex)
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, raw, 0o644)
}

func collectOCIManifestCandidates(layoutDir string) ([]ociRepairCandidate, error) {
	blobDir := filepath.Join(layoutDir, "blobs", "sha256")
	entries, err := os.ReadDir(blobDir)
	if err != nil {
		return nil, err
	}
	candidates := make([]ociRepairCandidate, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if _, err := parseSHA256Digest("sha256:" + name); err != nil {
			continue
		}
		path := filepath.Join(blobDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var manifest ociManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			continue
		}
		if !isImageManifestMediaType(manifest.MediaType) || manifest.Config == nil {
			continue
		}
		if !manifestReferencesAvailable(layoutDir, manifest) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, ociRepairCandidate{
			digest:   "sha256:" + strings.ToLower(name),
			media:    manifest.MediaType,
			size:     info.Size(),
			platform: readManifestPlatform(layoutDir, manifest),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].digest < candidates[j].digest
	})
	return candidates, nil
}

func isImageManifestMediaType(mediaType string) bool {
	switch strings.TrimSpace(mediaType) {
	case string(types.OCIManifestSchema1), string(types.DockerManifestSchema2):
		return true
	default:
		return false
	}
}

func manifestReferencesAvailable(layoutDir string, manifest ociManifest) bool {
	if manifest.Config != nil && strings.TrimSpace(manifest.Config.Digest) != "" {
		if !descriptorBlobExists(layoutDir, manifest.Config.Digest) {
			return false
		}
	}
	for _, layer := range manifest.Layers {
		if strings.TrimSpace(layer.Digest) == "" {
			continue
		}
		if !descriptorBlobExists(layoutDir, layer.Digest) {
			return false
		}
	}
	return true
}

func descriptorBlobExists(layoutDir, digest string) bool {
	parsed, err := parseSHA256Digest(digest)
	if err != nil {
		return false
	}
	_, err = os.Stat(blobPath(layoutDir, parsed))
	return err == nil
}

func readManifestPlatform(layoutDir string, manifest ociManifest) ociImagePlatform {
	if manifest.Config == nil {
		return ociImagePlatform{}
	}
	parsed, err := parseSHA256Digest(manifest.Config.Digest)
	if err != nil {
		return ociImagePlatform{}
	}
	var platform ociImagePlatform
	if err := readJSON(blobPath(layoutDir, parsed), &platform); err != nil {
		return ociImagePlatform{}
	}
	return platform
}

func selectOCIRepairCandidate(candidates []ociRepairCandidate, platform any) (ociRepairCandidate, bool) {
	for _, candidate := range candidates {
		if platformMatches(platform, candidate.platform) {
			return candidate, true
		}
	}
	if len(candidates) == 1 && !platformHasFields(platform) {
		return candidates[0], true
	}
	return ociRepairCandidate{}, false
}

func platformMatches(want any, got ociImagePlatform) bool {
	wantOS := platformField(want, "os")
	wantArch := platformField(want, "architecture")
	wantVariant := platformField(want, "variant")
	if wantOS != "" && !strings.EqualFold(wantOS, got.OS) {
		return false
	}
	if wantArch != "" && !strings.EqualFold(wantArch, got.Architecture) {
		return false
	}
	if wantVariant != "" && !strings.EqualFold(wantVariant, got.Variant) {
		return false
	}
	return wantOS != "" || wantArch != "" || wantVariant != ""
}

func platformHasFields(platform any) bool {
	return platformField(platform, "os") != "" || platformField(platform, "architecture") != "" || platformField(platform, "variant") != ""
}

func platformField(platform any, key string) string {
	m, ok := platform.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := m[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

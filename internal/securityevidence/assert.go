package securityevidence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func AssertNoRawSecrets(root string, rawSecrets []string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	var firstErr error
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		for _, secret := range rawSecrets {
			secret = strings.TrimSpace(secret)
			if secret == "" {
				continue
			}
			if strings.Contains(text, secret) {
				firstErr = fmt.Errorf("raw secret found in %s", path)
				return firstErr
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return firstErr
}

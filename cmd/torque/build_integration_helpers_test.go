//go:build integration && linux

// File: cmd/torque/build_integration_helpers_test.go
// Brief: CLI command wiring and implementation for 'build integration helpers'.

// Package main provides the torque CLI entrypoints.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var (
	integrationBinaryPath string
	buildBinaryOnce       sync.Once
)

func buildIntegrationBinary(t *testing.T) string {
	t.Helper()
	buildBinaryOnce.Do(func() {
		path := filepath.Join(os.TempDir(), fmt.Sprintf("torque.integration.%d", time.Now().UnixNano()))
		cmd := exec.Command("go", "build", "-o", path, "./cmd/torque")
		cmd.Dir = intTestRepoRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("go build torque: %v", err)
		}
		integrationBinaryPath = path
	})
	if integrationBinaryPath == "" {
		t.Fatalf("integration binary path missing")
	}
	return integrationBinaryPath
}

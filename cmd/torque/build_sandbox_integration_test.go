//go:build integration && linux

// File: cmd/torque/build_sandbox_integration_test.go
// Brief: CLI command wiring and implementation for 'build sandbox integration'.

// Package main provides the torque CLI entrypoints.

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildRunsInsideSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	requireCommand(t, "nsjail")
	requireCommand(t, "docker")

	tmp := t.TempDir()
	contextDir := filepath.Join(tmp, "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatalf("mkdir context: %v", err)
	}
	dockerfile := `FROM alpine:3.20
RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache curl
RUN printf "sandbox-ok" > /proof.txt
CMD ["cat", "/proof.txt"]
`
	if err := os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	torqueBin := buildIntegrationBinary(t)

	tag := fmt.Sprintf("torque.local/sandbox:%d", time.Now().UnixNano())
	cmd := exec.Command(torqueBin, "build", contextDir, "--tag", tag, "--no-cache")
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(&buf, os.Stdout)
	cmd.Stderr = io.MultiWriter(&buf, os.Stderr)
	cmd.Env = append(os.Environ(), "TORQUE_SANDBOX_DISABLE=0")

	if err := cmd.Run(); err != nil {
		t.Fatalf("torque build failed: %v\n%s", err, buf.String())
	}
	output := buf.String()
	if !strings.Contains(output, "Running torque build inside the default sandbox") {
		t.Fatalf("expected sandbox banner in output:\n%s", output)
	}
	if !strings.Contains(output, "Built "+tag) {
		t.Fatalf("expected successful build output for %s:\n%s", tag, output)
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
}

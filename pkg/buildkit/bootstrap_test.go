package buildkit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestIsDialError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "grpc unix missing",
			err:  errors.New("transport: Error while dialing: dial unix /run/user/0/buildkit/buildkitd.sock: connect: no such file or directory"),
			want: true,
		},
		{
			name: "wrapped econ refused",
			err:  &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("some other failure"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isDialError(tc.err); got != tc.want {
				t.Fatalf("isDialError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestEnsureDockerBackedBuilder_CachesResult(t *testing.T) {
	dockerFallback.mu.Lock()
	dockerFallback.resolved = false
	dockerFallback.addr = ""
	dockerFallback.context = ""
	dockerFallback.err = nil
	dockerFallback.mu.Unlock()

	origLookPath := dockerLookPath
	t.Cleanup(func() { dockerLookPath = origLookPath })
	dockerLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }

	origRunner := dockerBuildxRunner
	t.Cleanup(func() { dockerBuildxRunner = origRunner })

	origContainerRunning := dockerContainerRunning
	t.Cleanup(func() { dockerContainerRunning = origContainerRunning })
	dockerContainerRunning = func(_ context.Context, _ string, _ string) (bool, bool, error) {
		return false, false, nil
	}

	origVersionRunner := dockerVersionRunner
	t.Cleanup(func() { dockerVersionRunner = origVersionRunner })
	dockerVersionRunner = func(_ context.Context, _ string) error { return nil }

	var calls []string
	dockerBuildxRunner = func(_ context.Context, _ io.Writer, dockerContext string, args ...string) error {
		calls = append(calls, dockerContext+"|"+strings.Join(args, " "))
		if len(args) == 2 && args[0] == "inspect" && args[1] == dockerFallbackBuilderName {
			return errors.New("missing builder")
		}
		return nil
	}

	var buf bytes.Buffer
	addr1, _, err := ensureDockerBackedBuilder(context.Background(), &buf, "")
	if err != nil {
		t.Fatalf("ensureDockerBackedBuilder() err = %v", err)
	}
	addr2, _, err := ensureDockerBackedBuilder(context.Background(), &buf, "")
	if err != nil {
		t.Fatalf("ensureDockerBackedBuilder() (cached) err = %v", err)
	}
	if addr1 != addr2 {
		t.Fatalf("addresses differ: %q != %q", addr1, addr2)
	}
	if want := 3; len(calls) != want {
		t.Fatalf("docker buildx calls = %d, want %d (%v)", len(calls), want, calls)
	}
	if got := buf.String(); strings.Count(got, "checking Docker Buildx builder") != 1 || strings.Count(got, "Provisioning Docker Buildx builder") != 1 || strings.Count(got, "Using Docker Buildx builder") != 1 {
		t.Fatalf("unexpected log output:\n%s", got)
	}
}

func TestEnsureDockerBackedBuilder_ReusesExistingContainerWithoutBuildxInstance(t *testing.T) {
	dockerFallback.mu.Lock()
	dockerFallback.resolved = false
	dockerFallback.addr = ""
	dockerFallback.context = ""
	dockerFallback.err = nil
	dockerFallback.mu.Unlock()

	origLookPath := dockerLookPath
	t.Cleanup(func() { dockerLookPath = origLookPath })
	dockerLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }

	origBuildxRunner := dockerBuildxRunner
	t.Cleanup(func() { dockerBuildxRunner = origBuildxRunner })

	origContainerRunning := dockerContainerRunning
	t.Cleanup(func() { dockerContainerRunning = origContainerRunning })

	origContainerStarter := dockerContainerStarter
	t.Cleanup(func() { dockerContainerStarter = origContainerStarter })

	origVersionRunner := dockerVersionRunner
	t.Cleanup(func() { dockerVersionRunner = origVersionRunner })
	dockerVersionRunner = func(_ context.Context, _ string) error { return nil }

	var buildxCalls []string
	dockerBuildxRunner = func(_ context.Context, _ io.Writer, _ string, args ...string) error {
		buildxCalls = append(buildxCalls, strings.Join(args, " "))
		if len(args) == 2 && args[0] == "inspect" && args[1] == dockerFallbackBuilderName {
			return errors.New("missing builder metadata")
		}
		return nil
	}
	dockerContainerRunning = func(_ context.Context, _ string, name string) (bool, bool, error) {
		want := "buildx_buildkit_" + dockerFallbackBuilderName + "0"
		if name != want {
			t.Fatalf("container name = %q, want %q", name, want)
		}
		return true, true, nil
	}
	dockerContainerStarter = func(_ context.Context, _ io.Writer, _ string, name string) error {
		t.Fatalf("did not expect docker start for running container %s", name)
		return nil
	}

	var buf bytes.Buffer
	addr, _, err := ensureDockerBackedBuilder(context.Background(), &buf, "")
	if err != nil {
		t.Fatalf("ensureDockerBackedBuilder() err = %v", err)
	}
	if want := "docker-container://buildx_buildkit_" + dockerFallbackBuilderName + "0"; addr != want {
		t.Fatalf("addr = %q, want %q", addr, want)
	}
	if len(buildxCalls) != 1 {
		t.Fatalf("buildx calls = %v, want only metadata inspect", buildxCalls)
	}
	if got := buf.String(); !strings.Contains(got, "Using Docker Buildx builder container") {
		t.Fatalf("expected container reuse log, got:\n%s", got)
	}
	if got := buf.String(); strings.Contains(got, "Provisioning Docker Buildx builder") {
		t.Fatalf("did not expect provisioning log for container reuse:\n%s", got)
	}
}

func TestEnsureDockerBackedBuilder_PicksWorkingDockerContext(t *testing.T) {
	dockerFallback.mu.Lock()
	dockerFallback.resolved = false
	dockerFallback.addr = ""
	dockerFallback.context = ""
	dockerFallback.err = nil
	dockerFallback.mu.Unlock()

	origLookPath := dockerLookPath
	t.Cleanup(func() { dockerLookPath = origLookPath })
	dockerLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }

	origBuildxRunner := dockerBuildxRunner
	t.Cleanup(func() { dockerBuildxRunner = origBuildxRunner })

	origContainerRunning := dockerContainerRunning
	t.Cleanup(func() { dockerContainerRunning = origContainerRunning })
	dockerContainerRunning = func(_ context.Context, _ string, _ string) (bool, bool, error) {
		return false, false, nil
	}

	origVersionRunner := dockerVersionRunner
	t.Cleanup(func() { dockerVersionRunner = origVersionRunner })

	origContextLister := dockerContextLister
	t.Cleanup(func() { dockerContextLister = origContextLister })

	t.Cleanup(func() { _ = os.Unsetenv("DOCKER_CONTEXT") })
	_ = os.Unsetenv("DOCKER_CONTEXT")

	dockerVersionRunner = func(_ context.Context, dockerContext string) error {
		if dockerContext == "colima" {
			return nil
		}
		return errors.New("cannot connect")
	}
	dockerContextLister = func(_ context.Context) ([]string, error) {
		return []string{"desktop-linux", "colima"}, nil
	}
	dockerBuildxRunner = func(_ context.Context, _ io.Writer, _ string, _ ...string) error { return nil }

	var buf bytes.Buffer
	_, selected, err := ensureDockerBackedBuilder(context.Background(), &buf, "")
	if err != nil {
		t.Fatalf("ensureDockerBackedBuilder() err = %v", err)
	}
	if selected != "colima" {
		t.Fatalf("selected context = %q, want %q", selected, "colima")
	}
	if got := buf.String(); !strings.Contains(got, "using docker context colima") {
		t.Fatalf("expected context selection log, got:\n%s", got)
	}
}

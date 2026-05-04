// File: internal/workflows/buildsvc/sandbox_common.go
// Brief: Internal buildsvc package implementation for 'sandbox common'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"context"
	"os"
)

const (
	sandboxActiveEnvKey        = "TORQUE_SANDBOX_ACTIVE"
	legacySandboxActiveEnvKey  = "TORQUE_NSJAIL_ACTIVE"
	sandboxLogPathEnvKey       = "TORQUE_SANDBOX_LOG_PATH"
	legacySandboxLogPathEnv    = "TORQUE_NSJAIL_LOG_PATH"
	sandboxDisableEnvKey       = "TORQUE_SANDBOX_DISABLE"
	legacySandboxDisableEnv    = "TORQUE_NSJAIL_DISABLE"
	sandboxContextEnvKey       = "TORQUE_SANDBOX_CONTEXT"
	legacySandboxContextEnvKey = "TORQUE_NSJAIL_CONTEXT"
	sandboxCacheEnvKey         = "TORQUE_SANDBOX_CACHE"
	legacySandboxCacheEnvKey   = "TORQUE_NSJAIL_CACHE"
	sandboxBuilderEnvKey       = "TORQUE_SANDBOX_BUILDER"
	legacySandboxBuilderEnvKey = "TORQUE_NSJAIL_BUILDER"
)

type sandboxInjector func(ctx context.Context, opts *Options, streams Streams, contextAbs string) (bool, error)

func sandboxActive() bool {
	return os.Getenv(sandboxActiveEnvKey) == "1" || os.Getenv(legacySandboxActiveEnvKey) == "1"
}

func sandboxLogPathFromEnv() string {
	if v := os.Getenv(sandboxLogPathEnvKey); v != "" {
		return v
	}
	return os.Getenv(legacySandboxLogPathEnv)
}

func sandboxContextFromEnv() string {
	if v := os.Getenv(sandboxContextEnvKey); v != "" {
		return v
	}
	return os.Getenv(legacySandboxContextEnvKey)
}

func sandboxCacheFromEnv() string {
	if v := os.Getenv(sandboxCacheEnvKey); v != "" {
		return v
	}
	return os.Getenv(legacySandboxCacheEnvKey)
}

// File: internal/featureflags/featureflags_test.go
// Brief: Internal featureflags package implementation for 'featureflags'.

// Package featureflags provides featureflags helpers.

package featureflags

import (
	"context"
	"errors"
	"testing"
)

func TestResolveWithNoRegisteredFlags(t *testing.T) {
	flags, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got := flags.EnabledNames(); len(got) != 0 {
		t.Fatalf("expected no enabled flags, got %v", got)
	}
}

func TestResolveUnknown(t *testing.T) {
	_, err := Resolve([]string{"not-a-real-flag"})
	if !errors.Is(err, ErrUnknownFeature) {
		t.Fatalf("expected ErrUnknownFeature, got %v", err)
	}
}

func TestEnabledFromEnv(t *testing.T) {
	env := []string{
		"KTL_FEATURE_DEAD_FLAG=1",
		"SOME_OTHER=value",
		"KTL_FEATURE_BOGUS=0",
	}
	list := EnabledFromEnv(env)
	if len(list) != 1 || list[0] != "dead-flag" {
		t.Fatalf("expected one normalized env flag, got %v", list)
	}
	_, err := Resolve(list)
	if !errors.Is(err, ErrUnknownFeature) {
		t.Fatalf("expected stale env flag to be rejected, got %v", err)
	}
}

func TestContextHelpers(t *testing.T) {
	flags, err := Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := ContextWithFlags(context.Background(), flags)
	actual := FromContext(ctx)
	if got := actual.EnabledNames(); len(got) != 0 {
		t.Fatalf("expected context to preserve empty flag set, got %v", got)
	}
}

func TestEnabledFromEnvUsesProcessEnv(t *testing.T) {
	t.Setenv("KTL_FEATURE_DEAD_FLAG", "true")
	list := EnabledFromEnv(nil)
	if len(list) != 1 {
		t.Fatalf("expected 1 env flag, got %d", len(list))
	}
	if list[0] != "dead-flag" {
		t.Fatalf("expected normalized env flag, got %v", list)
	}
}

// File: cmd/torque/logs_test.go
// Brief: CLI command wiring and implementation for 'logs'.

// Package main provides the torque CLI entrypoints.

package main

import "testing"

func TestRequestedHelpRecognizesDash(t *testing.T) {
	if !requestedHelp("-") {
		t.Fatalf("expected single dash to trigger help detection")
	}
}

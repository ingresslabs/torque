// File: cmd/torque/term_helpers.go
// Brief: CLI command wiring and implementation for 'term helpers'.

// Package main provides the torque CLI entrypoints.

package main

import (
	"io"

	"github.com/ingresslabs/torque/internal/ui"
)

func isTerminalReader(r io.Reader) bool {
	return ui.IsTerminalReader(r)
}

func isTerminalWriter(w io.Writer) bool {
	return ui.IsTerminalWriter(w)
}

package main

import (
	"io"
	"os"

	"golang.org/x/term"
)

func isTerminalWriter(w io.Writer) bool {
	switch v := w.(type) {
	case *os.File:
		return term.IsTerminal(int(v.Fd()))
	default:
		return false
	}
}

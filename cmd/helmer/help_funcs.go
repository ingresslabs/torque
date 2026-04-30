// File: cmd/helmer/help_funcs.go
// Brief: Cobra help template helpers.

package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() {
	cobra.AddTemplateFunc("hasNonHelpSubcommands", func(cmd *cobra.Command) bool {
		if cmd == nil {
			return false
		}
		for _, sub := range cmd.Commands() {
			if sub == nil || sub.Name() == "help" || sub.Hidden {
				continue
			}
			return true
		}
		return false
	})
	cobra.AddTemplateFunc("flagUsages", func(fs *pflag.FlagSet) string {
		if fs == nil {
			return ""
		}
		return strings.TrimRight(fs.FlagUsagesWrapped(100), "\n")
	})
}

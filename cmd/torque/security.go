package main

import "github.com/spf13/cobra"

func newSecurityCommand(kubeconfig, kubeContext *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Run security evidence workflows",
		Long:  "Run security evidence workflows such as benchmark corpus evaluation for secret detection, redaction, and Kubernetes boundary proofs.",
	}
	cmd.AddCommand(newSecurityBenchmarkCommand(kubeconfig, kubeContext))
	decorateCommandHelp(cmd, "Security Flags")
	return cmd
}

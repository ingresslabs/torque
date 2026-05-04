// File: cmd/torque/delete.go
// Brief: CLI command wiring and implementation for 'delete'.

// Package main provides the torque CLI entrypoints.

package main

import "github.com/spf13/cobra"

// delete.go exposes the top-level 'torque delete' command while reusing the deploy destroy implementation.

func newDeleteCommand(kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	cmd := newDeployRemovalCommand(deployRemovalConfig{
		Use:       "delete",
		Short:     "Delete a Helm release and its resources",
		HelpLabel: "Delete Flags",
	}, nil, kubeconfig, kubeContext, logLevel, remoteAgent)
	cmd.Aliases = append(cmd.Aliases, "destroy")
	cmd.Example = `  # Delete a release but keep its history
  torque delete --release web-prod --namespace prod --keep-history`
	return cmd
}

// File: cmd/torque/apply.go
// Brief: CLI command wiring and implementation for 'apply'.

// Package main provides the torque CLI entrypoints.

package main

import "github.com/spf13/cobra"

// apply.go exposes the top-level 'torque apply' command while reusing the deploy apply implementation.

func newApplyCommand(kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	cmd := newDeployApplyCommand(nil, kubeconfig, kubeContext, logLevel, remoteAgent, "Apply Flags")
	cmd.AddCommand(newDeployPlanCommand(nil, kubeconfig, kubeContext, "Apply Plan Flags"))
	cmd.AddCommand(newApplySimulateCommand(nil, kubeconfig, kubeContext, "Apply Simulation Flags"))
	cmd.Example = `  # Apply a chart with prod values
  torque apply --chart ./charts/web --release web-prod --namespace prod -f values/prod.yaml`
	return cmd
}

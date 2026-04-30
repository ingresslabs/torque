// File: cmd/helmer/main.go
// Brief: Helmer CLI entrypoint.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kubekattle/ktl/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := newRootCommand()
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var kubeconfigPath string
	var kubeContext string
	var showVersion bool
	cmd := &cobra.Command{
		Use:           "helmer <command>",
		Short:         "Helm plan preview tool",
		Long:          "helmer provides the ktl apply plan experience as a standalone CLI.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				info := version.Get()
				fmt.Fprintf(cmd.OutOrStdout(), "helmer %s\n", info.Version)
				return nil
			}
			if len(args) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "unknown command %q for %q\n\n", args[0], cmd.Name())
			}
			return pflag.ErrHelp
		},
	}
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n\n", err)
		}
		return pflag.ErrHelp
	})
	cmd.PersistentFlags().StringVarP(&kubeconfigPath, "kubeconfig", "k", "", "Path to the kubeconfig file to use for CLI requests")
	cmd.PersistentFlags().StringVarP(&kubeContext, "context", "K", "", "Name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVar(&showVersion, "version", false, "Print version and exit")
	decorateCommandHelp(cmd, "Global Flags")
	cmd.AddCommand(newDeployPlanCommand(nil, &kubeconfigPath, &kubeContext, "Plan Flags"))
	return cmd
}

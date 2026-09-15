package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
)

func newStartCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	return &cobra.Command{
		Use:     "start TYPE NAME [NAME...]",
		Short:   "Start resources",
		Example: `  pvectl start vm web-server`,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := reg.Lookup(args[0])
			if err != nil {
				return err
			}
			starter, ok := h.(resource.Starter)
			if !ok {
				return fmt.Errorf("resource type %s does not support start", h.Kind())
			}
			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}
			for _, name := range args[1:] {
				if err := starter.Start(cmd.Context(), client, name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s started\n", resourceID(h, name))
			}
			return nil
		},
	}
}

func newStopCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	return &cobra.Command{
		Use:     "stop TYPE NAME [NAME...]",
		Short:   "Stop resources",
		Example: `  pvectl stop vm web-server`,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := reg.Lookup(args[0])
			if err != nil {
				return err
			}
			stopper, ok := h.(resource.Stopper)
			if !ok {
				return fmt.Errorf("resource type %s does not support stop", h.Kind())
			}
			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}
			for _, name := range args[1:] {
				if err := stopper.Stop(cmd.Context(), client, name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s stopped\n", resourceID(h, name))
			}
			return nil
		},
	}
}

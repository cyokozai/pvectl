package cmd

import (
	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
)

func newDescribeCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "describe TYPE NAME [NAME...]",
		Short: "Show detailed information about resources",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := reg.Lookup(args[0])
			if err != nil {
				return err
			}
			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}
			for _, name := range args[1:] {
				if err := h.Describe(cmd.Context(), client, name, cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

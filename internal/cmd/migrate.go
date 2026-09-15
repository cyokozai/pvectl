package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
)

func newMigrateCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	var target string
	var online bool

	cmd := &cobra.Command{
		Use:   "migrate TYPE NAME --to NODE",
		Short: "Move a guest to another node",
		Long: `migrate moves a guest to another Proxmox VE node and waits for the task
to finish.

This is the only pvectl verb that migrates. apply never does: when a
manifest's spec.targetNode disagrees with the node the guest actually
runs on, apply reports an error instead of moving it. Migration is a
one-off operation, so it leaves no trace in any manifest.`,
		Example: `  pvectl migrate vm web-server --to pve2
  pvectl migrate vm web-server --to pve2 --online`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := reg.Lookup(args[0])
			if err != nil {
				return err
			}
			name := args[1]

			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}
			ref, err := client.FindGuest(cmd.Context(), name)
			if err != nil {
				return err
			}
			if ref.Node == target {
				fmt.Fprintf(cmd.OutOrStdout(), "%s already on node %s\n", resourceID(h, name), target)
				return nil
			}

			if err := client.MigrateGuest(cmd.Context(), ref, target, online); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s migrated from %s to %s\n", resourceID(h, name), ref.Node, target)
			return nil
		},
	}

	cmd.Flags().StringVar(&target, "to", "", "node to move the guest to (required)")
	cmd.Flags().BoolVar(&online, "online", false,
		"live-migrate a running guest instead of requiring it to be stopped first")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/resource"
)

func newGetCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "get TYPE [NAME...]",
		Short: "Display one or many resources",
		Example: `  pvectl get vm
  pvectl get vm web-server -o yaml
  pvectl get vms -o wide`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := reg.Lookup(args[0])
			if err != nil {
				return err
			}
			format, err := printer.ParseFormat(f.Output)
			if err != nil {
				return err
			}
			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}

			var objs []printer.Object
			if len(args) == 1 {
				objs, err = h.List(cmd.Context(), client)
				if err != nil {
					return err
				}
			} else {
				for _, name := range args[1:] {
					obj, err := h.Get(cmd.Context(), client, name)
					if err != nil {
						return err
					}
					objs = append(objs, obj)
				}
			}
			return printer.Print(cmd.OutOrStdout(), format, objs, h.Columns(format == printer.FormatWide))
		},
	}
}

package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
)

func newDeleteCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	var filenames []string
	cmd := &cobra.Command{
		Use:   "delete (TYPE NAME... | -f FILENAME)",
		Short: "Delete resources by name or by manifest",
		Example: `  pvectl delete vm web-server
  pvectl delete -f vm.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := f.Client(cmd.Context())
			if err != nil {
				return err
			}

			if len(filenames) > 0 {
				if len(args) > 0 {
					return errors.New("use either -f or TYPE NAME, not both")
				}
				objs, err := runtime.DecodeFiles(filenames)
				if err != nil {
					return err
				}
				for _, obj := range objs {
					h, err := reg.ForObject(obj)
					if err != nil {
						return err
					}
					if err := h.Delete(cmd.Context(), client, obj.Metadata.Name); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s deleted\n", resourceID(h, obj.Metadata.Name))
				}
				return nil
			}

			if len(args) < 2 {
				return errors.New("specify TYPE NAME... or -f FILENAME")
			}
			h, err := reg.Lookup(args[0])
			if err != nil {
				return err
			}
			for _, name := range args[1:] {
				if err := h.Delete(cmd.Context(), client, name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s deleted\n", resourceID(h, name))
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&filenames, "filename", "f", nil,
		"manifest file, directory, or - for stdin (repeatable)")
	return cmd
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
)

func newDiffCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	var filenames []string
	cmd := &cobra.Command{
		Use:   "diff -f FILENAME",
		Short: "Diff live resources against manifests",
		Long: `Diff compares each manifest against the live configuration and prints
the managed keys that would change. Exit status: 0 no differences,
1 differences found, >1 error.`,
		Example: `  pvectl diff -f vm.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			objs, err := runtime.DecodeFiles(filenames)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			client, err := f.Client(cmd.Context())
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}

			found := false
			for _, obj := range objs {
				h, err := reg.ForObject(obj)
				if err != nil {
					return &ExitError{Code: 2, Err: err}
				}
				res, err := h.Diff(cmd.Context(), client, obj)
				if err != nil {
					return &ExitError{Code: 2, Err: err}
				}
				if res.Empty() {
					continue
				}
				found = true
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", resourceID(h, obj.Metadata.Name), obj.Source)
				if err := res.Render(cmd.OutOrStdout()); err != nil {
					return &ExitError{Code: 2, Err: err}
				}
			}
			if found {
				return &ExitError{Code: 1} // differences found; not an error message
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&filenames, "filename", "f", nil,
		"manifest file, directory, or - for stdin (repeatable)")
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}

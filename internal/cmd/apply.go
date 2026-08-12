package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
)

func newApplyCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	var (
		filenames []string
		dryRun    string
	)
	cmd := &cobra.Command{
		Use:   "apply -f FILENAME",
		Short: "Apply a configuration to resources by file (idempotent)",
		Long: `Apply creates resources that do not exist and updates the ones that do,
comparing only the fields the manifest declares. Re-running apply on an
unchanged manifest is a no-op. Every Proxmox task is awaited.`,
		Example: `  pvectl apply -f vm.yaml
  pvectl apply -f vm1.yaml -f vm2.yaml
  pvectl apply -f manifests/
  cat vm.yaml | pvectl apply -f -
  pvectl apply -f vm.yaml --dry-run=server`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := parseDryRun(dryRun)
			if err != nil {
				return err
			}
			objs, err := runtime.DecodeFiles(filenames)
			if err != nil {
				return err
			}
			if len(objs) == 0 {
				return errors.New("no objects found in the given files")
			}

			var client api.Client
			if opts.DryRun != resource.DryRunClient {
				if client, err = f.Client(cmd.Context()); err != nil {
					return err
				}
			}

			// kubectl behavior: keep going per document, aggregate failures.
			failed := 0
			for _, obj := range objs {
				if err := applyOne(cmd, reg, client, obj, opts); err != nil {
					failed++
					fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				}
			}
			if failed > 0 {
				return &ExitError{Code: 1, Err: fmt.Errorf("%d of %d objects failed to apply", failed, len(objs))}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&filenames, "filename", "f", nil,
		"manifest file, directory, or - for stdin (repeatable)")
	cmd.Flags().StringVar(&dryRun, "dry-run", "",
		`"client" validates locally without contacting the server; "server" reads live state and reports the would-be action without writing`)
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}

func applyOne(cmd *cobra.Command, reg *resource.Registry, client api.Client, obj *runtime.Unstructured, opts resource.ApplyOptions) error {
	h, err := reg.ForObject(obj)
	if err != nil {
		return err
	}
	res, err := h.Apply(cmd.Context(), client, obj, opts)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %s\n", resourceID(h, res.Name), w)
	}
	suffix := ""
	switch opts.DryRun {
	case resource.DryRunClient:
		suffix = " (client dry run)"
	case resource.DryRunServer:
		suffix = " (server dry run)"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s%s\n", resourceID(h, res.Name), res.Action, suffix)
	return nil
}

func parseDryRun(s string) (resource.ApplyOptions, error) {
	switch s {
	case "":
		return resource.ApplyOptions{}, nil
	case "client":
		return resource.ApplyOptions{DryRun: resource.DryRunClient}, nil
	case "server":
		return resource.ApplyOptions{DryRun: resource.DryRunServer}, nil
	default:
		return resource.ApplyOptions{}, fmt.Errorf("invalid --dry-run value %q (use client or server)", s)
	}
}

// resourceID renders the kubectl-style "virtualmachine/web-server" id.
func resourceID(h resource.Handler, name string) string {
	return fmt.Sprintf("%s/%s", strings.ToLower(h.Kind()), name)
}

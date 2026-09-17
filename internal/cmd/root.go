// Package cmd wires the cobra command tree. Commands are thin: every
// generic verb resolves its resource handler through the registry and
// its client through the cliopt.Factory, so new resource kinds plug in
// with a single Register call and global flags work everywhere.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/resource/vm"
	"github.com/cyokozai/pvectl/internal/verbose"
)

// ExitError carries a specific process exit code (e.g. diff's exit 1
// for "differences found").
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// DefaultRegistry returns the registry with all built-in kinds.
// M2+: register new handlers here — nothing else changes.
func DefaultRegistry() *resource.Registry {
	reg := resource.NewRegistry()
	reg.Register(vm.NewHandler())
	return reg
}

// NewRootCmd builds the command tree around the given factory and
// registry (both swappable in tests).
func NewRootCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	root := &cobra.Command{
		Use:   "pvectl",
		Short: "A kubectl-like CLI for Proxmox VE",
		Long: `pvectl manages Proxmox VE resources with a kubectl-like workflow:
declare resources in YAML/JSON manifests and apply them idempotently,
or operate on them directly with get/describe/delete/start/stop.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Debug output goes to cobra's error stream, never to stdout,
		// so -v can be combined with -o yaml / -o json and with pipes.
		// Set here rather than in Execute so tests that swap the
		// streams capture it too.
		//
		// cobra runs only the nearest PersistentPreRun in the chain: a
		// subcommand that declares one of its own silences this. None
		// do today; one that starts to must call this first.
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			f.ErrOut = cmd.ErrOrStderr()
			cmd.SetContext(verbose.NewContext(cmd.Context(), f.Logger()))
		},
	}

	root.PersistentFlags().StringVar(&f.ConfigPath, "config", "",
		"path to the pvectl config file (default ~/.pvectl/config, env PVECTL_CONFIG)")
	root.PersistentFlags().StringVar(&f.ContextName, "context", "",
		"config context to use (default current-context, env PVECTL_CONTEXT)")
	root.PersistentFlags().StringVarP(&f.Output, "output", "o", "",
		"output format: table|wide|yaml|json|name")
	root.PersistentFlags().DurationVar(&f.Timeout, "timeout", 5*time.Minute,
		"how long to wait for Proxmox tasks to complete")
	root.PersistentFlags().IntVarP(&f.Verbosity, "v", "v", 0,
		"debug output level on stderr, on kubectl's scale: 0 silent, 1-5 pvectl's own decisions "+
			"(config/context, name resolution, diff, tasks, parameters), 6 HTTP request summaries, "+
			"7 +headers, 8 +bodies, 9 +untruncated bodies. Credentials are always redacted")

	root.AddCommand(
		newGetCmd(f, reg),
		newDescribeCmd(f, reg),
		newApplyCmd(f, reg),
		newDiffCmd(f, reg),
		newDeleteCmd(f, reg),
		newStartCmd(f, reg),
		newStopCmd(f, reg),
		newExecCmd(f, reg),
		newMigrateCmd(f, reg),
		newConfigCmd(f),
		newVersionCmd(),
	)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := NewRootCmd(&cliopt.Factory{}, DefaultRegistry())

	err := root.Execute()
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", exitErr.Err)
		}
		return exitErr.Code
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return 1
}

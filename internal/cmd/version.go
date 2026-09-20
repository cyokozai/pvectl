package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata, injected via -ldflags "-X ..." (see Makefile).
var (
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "pvectl version %s (commit %s, built %s)\n", Version, GitCommit, BuildDate)
		},
	}
}

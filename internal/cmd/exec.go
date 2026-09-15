package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/resource"
)

func newExecCmd(f *cliopt.Factory, reg *resource.Registry) *cobra.Command {
	var execTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "exec TYPE NAME -- COMMAND [ARGS...]",
		Short: "Run a command inside a guest through the QEMU guest agent",
		Long: `exec runs a command inside a running guest without SSH, using the QEMU
guest agent, and reports the guest-side stdout, stderr, and exit code as
its own. The guest needs "agent: 1" in its config and a running
qemu-guest-agent.

Everything after "--" is handed to the guest untouched, so the command's
own flags are never parsed by pvectl.

exec is a one-off operation and leaves no trace in any manifest: nothing
about "I ran this once" is a desired state to converge on.`,
		Example: `  pvectl exec vm web-server -- systemctl is-active nginx
  pvectl exec vm web-server -- /bin/sh -c "df -h / | tail -1"
  pvectl exec vm web-server --exec-timeout 5m -- apt-get -y dist-upgrade`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 {
				return errors.New(`separate the guest from the command with "--", e.g. pvectl exec vm web-server -- uname -a`)
			}
			if dash != 2 {
				return errors.New(`specify exactly TYPE NAME before "--"`)
			}
			command := args[dash:]
			if len(command) == 0 {
				return errors.New(`no command given after "--"`)
			}

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
			if ref.Type != "qemu" {
				return fmt.Errorf("%s is a %s guest; exec needs the QEMU guest agent",
					resourceID(h, name), ref.Type)
			}

			status, err := api.ExecWait(cmd.Context(), client, ref, command,
				api.ExecOptions{Timeout: execTimeout})
			if err != nil {
				return err
			}

			if status.Stdout != "" {
				fmt.Fprint(cmd.OutOrStdout(), status.Stdout)
			}
			if status.Stderr != "" {
				fmt.Fprint(cmd.ErrOrStderr(), status.Stderr)
			}
			if status.OutTruncated {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: the guest agent truncated stdout\n")
			}
			if status.ErrTruncated {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: the guest agent truncated stderr\n")
			}

			if status.Signaled && status.Signal != 0 {
				return &ExitError{
					Code: exitCode(128 + status.Signal),
					Err:  fmt.Errorf("command killed by signal %d in %s", status.Signal, resourceID(h, name)),
				}
			}
			if status.ExitCode != 0 {
				// The guest's exit code becomes pvectl's, so shell
				// pipelines can branch on it like a local command.
				return &ExitError{Code: exitCode(status.ExitCode)}
			}
			return nil
		},
	}

	cmd.Flags().DurationVar(&execTimeout, "exec-timeout", api.DefaultExecTimeout,
		"how long to wait for the guest command to finish (separate from --timeout, which bounds Proxmox task waits)")
	return cmd
}

// exitCode keeps a guest-reported status inside the range a process can
// actually exit with, falling back to 1 for anything out of bounds.
func exitCode(code int) int {
	if code <= 0 || code > 255 {
		return 1
	}
	return code
}

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

TYPE picks the kind of guest, and NAME is looked up among guests of that
kind only: a VM and an LXC container may share a name, and a guest of
another kind is refused with the TYPE that reaches it.

LXC containers are not supported: Proxmox VE offers no exec in its REST
API for containers, and pct exec only runs locally on the node.

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
			guest, err := resolveGuest(cmd.Context(), client, reg, h, name)
			if err != nil {
				return err
			}
			ref := summaryRef(guest)
			if ref.Type == "lxc" {
				return fmt.Errorf("%q is an LXC container, and Proxmox VE has no exec in the REST API for containers: "+
					"pct exec only runs locally on the node (%s). Use ssh to the container, or pct exec %d on that node",
					name, ref.Node, ref.VMID)
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

// guestKind ties a Proxmox guest type to the resource kind that manages
// it and the short TYPE pvectl's own hints use for it.
type guestKind struct {
	apiType string // "qemu" | "lxc", as /cluster/resources reports it
	kind    string // resource kind, as Handler.GVK().Kind reports it
	typeArg string // short TYPE to suggest on the command line
}

var guestKinds = []guestKind{
	{apiType: "qemu", kind: "VirtualMachine", typeArg: "vm"},
	{apiType: "lxc", kind: "LinuxContainer", typeArg: "ct"},
}

func guestKindByKind(kind string) (guestKind, bool) {
	for _, k := range guestKinds {
		if k.kind == kind {
			return k, true
		}
	}
	return guestKind{}, false
}

func guestKindByType(apiType string) (guestKind, bool) {
	for _, k := range guestKinds {
		if k.apiType == apiType {
			return k, true
		}
	}
	return guestKind{}, false
}

// resolveGuest finds the guest named name among guests of h's kind only,
// as the resource handlers do (ADR-010 §1): a VM and a container may
// share a name, and TYPE says which one is meant. A guest of another kind
// with that name is reported as not found, with the TYPE that would
// reach it. Returns the cluster row, which also carries the power state.
func resolveGuest(ctx context.Context, client api.Client, reg *resource.Registry, h resource.Handler, name string) (*api.GuestSummary, error) {
	want, ok := guestKindByKind(h.GVK().Kind)
	if !ok {
		return nil, fmt.Errorf("resource type %s is not a guest kind; this command takes a guest TYPE such as %q",
			h.GVK().Kind, guestKinds[0].typeArg)
	}
	guests, err := client.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	var matches, others []api.GuestSummary
	for _, g := range guests {
		if g.Name != name {
			continue
		}
		if g.Type == want.apiType {
			matches = append(matches, g)
		} else {
			others = append(others, g)
		}
	}
	switch {
	case len(matches) == 1:
		return &matches[0], nil
	case len(matches) > 1:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = fmt.Sprintf("%d (node %s)", m.VMID, m.Node)
		}
		return nil, fmt.Errorf("%s matches vmids %s: %w", resourceID(h, name), strings.Join(ids, ", "), api.ErrAmbiguousName)
	case len(others) > 0:
		return nil, kindMismatchError(reg, h, name, others[0])
	default:
		return nil, fmt.Errorf("%s: %w", resourceID(h, name), api.ErrNotFound)
	}
}

// kindMismatchError explains that name belongs to a guest of another
// kind. It names that guest's TYPE only when this registry can resolve
// it, so the hint never points at a kind pvectl does not manage.
func kindMismatchError(reg *resource.Registry, h resource.Handler, name string, other api.GuestSummary) error {
	got, ok := guestKindByType(other.Type)
	if !ok {
		return fmt.Errorf("%s: %w (%q is a %s guest, vmid %d)",
			resourceID(h, name), api.ErrNotFound, name, other.Type, other.VMID)
	}
	if _, err := reg.Lookup(got.typeArg); err != nil {
		return fmt.Errorf("%s: %w (%q is a %s, vmid %d, which this pvectl does not manage)",
			resourceID(h, name), api.ErrNotFound, name, got.kind, other.VMID)
	}
	return fmt.Errorf("%s: %w (%q is a %s, vmid %d: address it as TYPE %q)",
		resourceID(h, name), api.ErrNotFound, name, got.kind, other.VMID, got.typeArg)
}

func summaryRef(g *api.GuestSummary) *api.GuestRef {
	return &api.GuestRef{VMID: g.VMID, Node: g.Node, Type: g.Type}
}

// exitCode keeps a guest-reported status inside the range a process can
// actually exit with, falling back to 1 for anything out of bounds.
func exitCode(code int) int {
	if code <= 0 || code > 255 {
		return 1
	}
	return code
}

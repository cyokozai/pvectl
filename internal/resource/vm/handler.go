package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/resource"
)

// Handler implements resource.Handler for VirtualMachine.
type Handler struct{}

// Interface guards.
var (
	_ resource.Handler = (*Handler)(nil)
	_ resource.Starter = (*Handler)(nil)
	_ resource.Stopper = (*Handler)(nil)
)

// NewHandler returns the VirtualMachine handler.
func NewHandler() *Handler { return &Handler{} }

// Kind implements resource.Handler.
func (h *Handler) Kind() string { return KindName }

// APIVersion implements resource.Handler.
func (h *Handler) APIVersion() string { return APIVersion }

// Aliases implements resource.Handler.
func (h *Handler) Aliases() []string {
	return []string{"vm", "vms", "virtualmachine", "virtualmachines"}
}

// Columns implements resource.Handler.
func (h *Handler) Columns(bool) []printer.Column {
	return []printer.Column{
		{Name: "NAME", Extract: func(o printer.Object) string { return o.(*VM).Metadata.Name }},
		{Name: "VMID", Extract: func(o printer.Object) string { return fmt.Sprintf("%d", o.(*VM).Status.VMID) }},
		{Name: "NODE", Extract: func(o printer.Object) string { return o.(*VM).Status.Node }},
		{Name: "STATUS", Extract: func(o printer.Object) string { return o.(*VM).Status.State }},
		{Name: "CORES", Extract: func(o printer.Object) string { return fmt.Sprintf("%d", o.(*VM).Spec.Resources.CPU.Cores) }},
		{Name: "MEMORY", Extract: func(o printer.Object) string { return fmt.Sprintf("%dMi", o.(*VM).Spec.Resources.Memory) }},
		{Name: "UPTIME", WideOnly: true, Extract: func(o printer.Object) string { return formatUptime(o.(*VM).Status.Uptime) }},
		{Name: "TAGS", WideOnly: true, Extract: func(o printer.Object) string { return strings.Join(o.(*VM).Spec.Tags, ",") }},
	}
}

// List implements resource.Handler. It builds objects from the cluster
// resource list alone — one API call, no per-VM config fetches.
func (h *Handler) List(ctx context.Context, c api.Client) ([]printer.Object, error) {
	guests, err := c.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	var vms []*VM
	for _, g := range guests {
		if g.Type == "qemu" {
			vms = append(vms, newVMFromSummary(g))
		}
	}
	sort.Slice(vms, func(i, j int) bool { return vms[i].Status.VMID < vms[j].Status.VMID })

	objs := make([]printer.Object, len(vms))
	for i, vm := range vms {
		objs[i] = vm
	}
	return objs, nil
}

// Get implements resource.Handler. The returned object round-trips:
// `get vm x -o yaml | pvectl apply -f -` applies as unchanged.
func (h *Handler) Get(ctx context.Context, c api.Client, name string) (printer.Object, error) {
	summary, err := h.findQemu(ctx, c, name)
	if err != nil {
		return nil, err
	}
	cfg, err := c.QemuConfig(ctx, &api.GuestRef{VMID: summary.VMID, Node: summary.Node, Type: summary.Type})
	if err != nil {
		return nil, err
	}
	return newVMFromConfig(*summary, cfg), nil
}

// Delete implements resource.Handler.
func (h *Handler) Delete(ctx context.Context, c api.Client, name string) error {
	summary, err := h.findQemu(ctx, c, name)
	if err != nil {
		return err
	}
	return c.DeleteGuest(ctx, &api.GuestRef{VMID: summary.VMID, Node: summary.Node, Type: summary.Type})
}

// Start implements resource.Starter.
func (h *Handler) Start(ctx context.Context, c api.Client, name string) error {
	summary, err := h.findQemu(ctx, c, name)
	if err != nil {
		return err
	}
	return c.StartGuest(ctx, &api.GuestRef{VMID: summary.VMID, Node: summary.Node, Type: summary.Type})
}

// Stop implements resource.Stopper.
func (h *Handler) Stop(ctx context.Context, c api.Client, name string) error {
	summary, err := h.findQemu(ctx, c, name)
	if err != nil {
		return err
	}
	return c.StopGuest(ctx, &api.GuestRef{VMID: summary.VMID, Node: summary.Node, Type: summary.Type})
}

// Describe implements resource.Handler.
func (h *Handler) Describe(ctx context.Context, c api.Client, name string, w io.Writer) error {
	obj, err := h.Get(ctx, c, name)
	if err != nil {
		return err
	}
	vm := obj.(*VM)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	p := func(format string, args ...any) { fmt.Fprintf(tw, format+"\n", args...) } //nolint:errcheck // buffered writer, flushed below

	p("Name:\t%s", vm.Metadata.Name)
	p("Kind:\t%s", vm.Kind)
	p("VMID:\t%d", vm.Status.VMID)
	p("Node:\t%s", vm.Status.Node)
	p("Status:\t%s", vm.Status.State)
	if vm.Status.Uptime > 0 {
		p("Uptime:\t%s", formatUptime(vm.Status.Uptime))
	}
	cpu := vm.Spec.Resources.CPU
	cpuLine := fmt.Sprintf("%d cores", cpu.Cores)
	if cpu.Sockets > 0 {
		cpuLine += fmt.Sprintf(", %d sockets", cpu.Sockets)
	}
	if cpu.Type != "" {
		cpuLine += fmt.Sprintf(" (%s)", cpu.Type)
	}
	p("CPU:\t%s", cpuLine)
	p("Memory:\t%d MB", vm.Spec.Resources.Memory)
	if vm.Spec.Description != "" {
		p("Description:\t%s", vm.Spec.Description)
	}
	if len(vm.Spec.Disks) > 0 {
		p("Disks:\t")
		for _, d := range vm.Spec.Disks {
			line := fmt.Sprintf("  %s:\t%s %s", d.Name, d.Storage, d.Size)
			if d.Cache != "" {
				line += " cache=" + d.Cache
			}
			if d.Format != "" {
				line += " format=" + d.Format
			}
			p("%s", line)
		}
	}
	if len(vm.Spec.Networks) > 0 {
		p("Networks:\t")
		for _, n := range vm.Spec.Networks {
			line := fmt.Sprintf("  %s:\t%s bridge=%s", n.Name, n.Model, n.Bridge)
			if n.Tag > 0 {
				line += fmt.Sprintf(" vlan=%d", n.Tag)
			}
			p("%s", line)
		}
	}
	if ci := vm.Spec.CloudInit; ci != nil {
		p("CloudInit:\t")
		if ci.User != "" {
			p("  User:\t%s", ci.User)
		}
		if len(ci.SSHKeys) > 0 {
			p("  SSHKeys:\t%d key(s)", len(ci.SSHKeys))
		}
		if ci.IPConfig != "" {
			p("  IPConfig:\t%s", ci.IPConfig)
		}
		if ci.Nameserver != "" {
			p("  Nameserver:\t%s", ci.Nameserver)
		}
	}
	p("StartOnBoot:\t%v", vm.Spec.StartOnBoot)
	if len(vm.Spec.Tags) > 0 {
		p("Tags:\t%s", strings.Join(vm.Spec.Tags, ", "))
	}
	return tw.Flush()
}

// findQemu resolves a qemu guest by name, returning its cluster summary.
func (h *Handler) findQemu(ctx context.Context, c api.Client, name string) (*api.GuestSummary, error) {
	guests, err := c.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	var matches, qemuMatches []api.GuestSummary
	for _, g := range guests {
		if g.Name != name {
			continue
		}
		matches = append(matches, g)
		if g.Type == "qemu" {
			qemuMatches = append(qemuMatches, g)
		}
	}
	switch {
	case len(qemuMatches) == 1:
		return &qemuMatches[0], nil
	case len(qemuMatches) > 1:
		ids := make([]string, len(qemuMatches))
		for i, m := range qemuMatches {
			ids[i] = fmt.Sprintf("%d (node %s)", m.VMID, m.Node)
		}
		return nil, fmt.Errorf("virtualmachine %q matches vmids %s; identify it by spec.vmid: %w",
			name, strings.Join(ids, ", "), api.ErrAmbiguousName)
	case len(matches) > 0:
		return nil, fmt.Errorf("guest %q is a %s guest, not a VirtualMachine", name, matches[0].Type)
	default:
		return nil, fmt.Errorf("virtualmachine %q: %w", name, api.ErrNotFound)
	}
}

// resolveRef finds the guest a manifest refers to: by vmid when pinned,
// else by name. Returns nil when the VM does not exist yet.
func (h *Handler) resolveRef(ctx context.Context, c api.Client, name string, spec *Spec) (*api.GuestRef, error) {
	if spec.VMID != nil {
		ref, err := c.GuestByID(ctx, *spec.VMID)
		switch {
		case err == nil:
			if ref.Type != "qemu" {
				return nil, fmt.Errorf("vmid %d is a %s guest, not a VirtualMachine", *spec.VMID, ref.Type)
			}
			return ref, nil
		case errors.Is(err, api.ErrNotFound):
			// Creating under a pinned vmid: the name must not already be
			// taken by another guest, or apply would duplicate it.
			existing, findErr := c.FindGuest(ctx, name)
			if findErr == nil {
				return nil, fmt.Errorf("virtualmachine %q already exists as vmid %d, but the manifest declares vmid %d",
					name, existing.VMID, *spec.VMID)
			}
			if !errors.Is(findErr, api.ErrNotFound) {
				return nil, findErr
			}
			return nil, nil
		default:
			return nil, err
		}
	}

	ref, err := c.FindGuest(ctx, name)
	switch {
	case err == nil:
		if ref.Type != "qemu" {
			return nil, fmt.Errorf("guest %q is a %s guest, not a VirtualMachine", name, ref.Type)
		}
		return ref, nil
	case errors.Is(err, api.ErrNotFound):
		return nil, nil
	default:
		return nil, err
	}
}

// formatUptime renders seconds as "3d4h", "2h5m", or "42s".
func formatUptime(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	d := seconds / 86400
	hr := (seconds % 86400) / 3600
	m := (seconds % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd%dh", d, hr)
	case hr > 0:
		return fmt.Sprintf("%dh%dm", hr, m)
	case m > 0:
		return fmt.Sprintf("%dm%ds", m, seconds%60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

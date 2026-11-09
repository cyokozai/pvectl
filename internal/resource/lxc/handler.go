package lxc

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
	"github.com/cyokozai/pvectl/internal/verbose"
)

// Handler implements resource.Handler for LinuxContainer.
type Handler struct{}

// Interface guards: every verb ADR-010 §6 lists. exec is not among
// them (there is no REST exec for containers), and migrate is wired in
// the command layer.
var (
	_ resource.Handler = (*Handler)(nil)
	_ resource.Applier = (*Handler)(nil)
	_ resource.Deleter = (*Handler)(nil)
	_ resource.Starter = (*Handler)(nil)
	_ resource.Stopper = (*Handler)(nil)
)

// NewHandler returns the LinuxContainer handler.
func NewHandler() *Handler { return &Handler{} }

// GVK implements resource.Handler.
func (h *Handler) GVK() runtime.GVK { return GroupVersionKind }

// Aliases implements resource.Handler.
func (h *Handler) Aliases() []string {
	return []string{"lxc", "ct", "linuxcontainer", "linuxcontainers"}
}

// Columns implements resource.Handler. Every column reads what the
// cluster resource list already carries, so the table never needs a
// per-container config read.
func (h *Handler) Columns(bool) []printer.Column {
	ct := func(o printer.Object) *LinuxContainer { return o.(*LinuxContainer) }
	return []printer.Column{
		{Name: "NAME", Extract: func(o printer.Object) string { return ct(o).Metadata.Name }},
		{Name: "VMID", Extract: func(o printer.Object) string { return fmt.Sprintf("%d", ct(o).Status.VMID) }},
		{Name: "NODE", Extract: func(o printer.Object) string { return ct(o).Status.Node }},
		{Name: "STATUS", Extract: func(o printer.Object) string { return ct(o).Status.State }},
		{Name: "CORES", Extract: func(o printer.Object) string { return fmt.Sprintf("%d", ct(o).Spec.Resources.Cores) }},
		{Name: "MEMORY", Extract: func(o printer.Object) string { return fmt.Sprintf("%dMi", ct(o).Spec.Resources.Memory) }},
		{Name: "UPTIME", WideOnly: true, Extract: func(o printer.Object) string { return formatUptime(ct(o).Status.Uptime) }},
		{Name: "TAGS", WideOnly: true, Extract: func(o printer.Object) string { return strings.Join(ct(o).Spec.Tags, ",") }},
	}
}

// List implements resource.Handler. The objects are built from the
// cluster resource list alone — one API call — which is all a table
// needs. The list row lacks what only the config has (unprivileged,
// volumes, NICs), so each object reads its config when, and only when,
// it is serialized: `get ct -o yaml` without a name round-trips into
// apply like the named form, while the table output stays one call.
func (h *Handler) List(ctx context.Context, c api.Client) ([]printer.Object, error) {
	guests, err := c.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	var cts []*LinuxContainer
	for _, g := range guests {
		if g.Type != "lxc" {
			continue
		}
		ct := newLinuxContainerFromSummary(g)
		ct.full = func() (*LinuxContainer, error) {
			cfg, err := c.LxcConfig(ctx, &api.GuestRef{VMID: g.VMID, Node: g.Node, Type: g.Type})
			if err != nil {
				return nil, err
			}
			return newLinuxContainerFromConfig(g, cfg), nil
		}
		cts = append(cts, ct)
	}
	sort.Slice(cts, func(i, j int) bool { return cts[i].Status.VMID < cts[j].Status.VMID })

	objs := make([]printer.Object, len(cts))
	for i, ct := range cts {
		objs[i] = ct
	}
	return objs, nil
}

// Get implements resource.Handler. The returned object round-trips:
// `get ct x -o yaml | pvectl apply -f -` applies as unchanged.
func (h *Handler) Get(ctx context.Context, c api.Client, name string) (printer.Object, error) {
	summary, err := h.findLxc(ctx, c, name)
	if err != nil {
		return nil, err
	}
	cfg, err := c.LxcConfig(ctx, refOf(summary))
	if err != nil {
		return nil, err
	}
	return newLinuxContainerFromConfig(*summary, cfg), nil
}

// Delete implements resource.Deleter.
func (h *Handler) Delete(ctx context.Context, c api.Client, name string) error {
	summary, err := h.findLxc(ctx, c, name)
	if err != nil {
		return err
	}
	return c.DeleteGuest(ctx, refOf(summary))
}

// Start implements resource.Starter.
func (h *Handler) Start(ctx context.Context, c api.Client, name string) error {
	summary, err := h.findLxc(ctx, c, name)
	if err != nil {
		return err
	}
	return c.StartGuest(ctx, refOf(summary))
}

// Stop implements resource.Stopper.
func (h *Handler) Stop(ctx context.Context, c api.Client, name string) error {
	summary, err := h.findLxc(ctx, c, name)
	if err != nil {
		return err
	}
	return c.StopGuest(ctx, refOf(summary))
}

// Describe implements resource.Handler.
func (h *Handler) Describe(ctx context.Context, c api.Client, name string, w io.Writer) error {
	obj, err := h.Get(ctx, c, name)
	if err != nil {
		return err
	}
	ct := obj.(*LinuxContainer)
	s := ct.Spec

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	p := func(format string, args ...any) { fmt.Fprintf(tw, format+"\n", args...) } //nolint:errcheck // buffered writer, flushed below

	p("Name:\t%s", ct.Metadata.Name)
	p("Kind:\t%s", ct.Kind)
	p("VMID:\t%d", ct.Status.VMID)
	p("Node:\t%s", ct.Status.Node)
	p("Status:\t%s", ct.Status.State)
	if ct.Status.Uptime > 0 {
		p("Uptime:\t%s", formatUptime(ct.Status.Uptime))
	}
	p("Unprivileged:\t%t", s.Unprivileged != nil && *s.Unprivileged)
	if s.Resources.Cores > 0 {
		p("Cores:\t%d", s.Resources.Cores)
	}
	p("Memory:\t%d MB", s.Resources.Memory)
	if s.Resources.Swap != nil {
		p("Swap:\t%d MB", *s.Resources.Swap)
	}
	if s.Description != "" {
		p("Description:\t%s", s.Description)
	}
	if s.RootFS != nil {
		p("RootFS:\t%s %s", s.RootFS.Storage, s.RootFS.Size)
	}
	if len(s.MountPoints) > 0 {
		p("MountPoints:\t")
		for _, mp := range s.MountPoints {
			p("  %s:\t%s %s at %s", mp.Name, mp.Storage, mp.Size, mp.Path)
		}
	}
	if len(s.Networks) > 0 {
		p("Networks:\t")
		for _, n := range s.Networks {
			line := fmt.Sprintf("  %s:\t%s bridge=%s", n.Name, n.Interface, n.Bridge)
			if n.IP != "" {
				line += " ip=" + n.IP
			}
			if n.GW != "" {
				line += " gw=" + n.GW
			}
			if n.Tag != nil && *n.Tag > 0 {
				line += fmt.Sprintf(" vlan=%d", *n.Tag)
			}
			p("%s", line)
		}
	}
	if s.Nameserver != "" {
		p("Nameserver:\t%s", s.Nameserver)
	}
	if opts := featureOpts(s.Features); len(opts) > 0 {
		p("Features:\t%s", joinOpts(opts))
	}
	p("RunStrategy:\t%s", s.RunStrategy)
	if len(s.Tags) > 0 {
		p("Tags:\t%s", strings.Join(s.Tags, ", "))
	}
	return tw.Flush()
}

// findLxc resolves a container by name among the lxc guests only, so a
// VM with the same name never interferes (ADR-010 §1). When no
// container matches but another kind of guest does, the not-found error
// says so.
func (h *Handler) findLxc(ctx context.Context, c api.Client, name string) (*api.GuestSummary, error) {
	guests, err := c.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	var matches []api.GuestSummary
	otherType := ""
	for _, g := range guests {
		if g.Name != name {
			continue
		}
		if g.Type == "lxc" {
			matches = append(matches, g)
		} else if otherType == "" {
			otherType = g.Type
		}
	}
	log := verbose.FromContext(ctx)
	switch {
	case len(matches) == 1:
		log.Logf(verbose.LevelResolve, "resolved linuxcontainer %q to vmid %d on node %s",
			name, matches[0].VMID, matches[0].Node)
		return &matches[0], nil
	case len(matches) > 1:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = fmt.Sprintf("%d (node %s)", m.VMID, m.Node)
		}
		return nil, fmt.Errorf("linuxcontainer %q matches vmids %s; identify it by spec.vmid: %w",
			name, strings.Join(ids, ", "), api.ErrAmbiguousName)
	case otherType == "qemu":
		return nil, fmt.Errorf("linuxcontainer %q: %w (a VirtualMachine has that name: use `vm` instead of `ct`)",
			name, api.ErrNotFound)
	case otherType != "":
		return nil, fmt.Errorf("linuxcontainer %q: %w (a %s guest has that name)", name, api.ErrNotFound, otherType)
	default:
		log.Logf(verbose.LevelResolve, "no linuxcontainer named %q among %d cluster guests", name, len(guests))
		return nil, fmt.Errorf("linuxcontainer %q: %w", name, api.ErrNotFound)
	}
}

func refOf(g *api.GuestSummary) *api.GuestRef {
	return &api.GuestRef{VMID: g.VMID, Node: g.Node, Type: g.Type}
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

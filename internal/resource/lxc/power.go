package lxc

import (
	"context"
	"fmt"

	"github.com/cyokozai/pvectl/internal/api"
)

// Power-state convergence, duplicated from internal/resource/vm on
// purpose (ADR-010 §1): it is extracted once a third kind exists.

// powerAction is the transition runStrategy asks for, if any.
type powerAction string

const (
	powerNone  powerAction = ""
	powerStart powerAction = "start"
	powerStop  powerAction = "stop"
)

// wantsPowerConvergence reports whether a strategy has an opinion about
// the power state. Manual (the default) does not, and so costs no API
// call either.
func wantsPowerConvergence(s RunStrategy) bool {
	return s == RunStrategyAlways || s == RunStrategyHalted
}

// plannedPowerFor reports the transition runStrategy wants given the
// live cluster row. A nil summary means nothing was read, which only
// happens when no strategy asked for convergence.
func plannedPowerFor(s RunStrategy, summary *api.GuestSummary) powerAction {
	if summary == nil || !wantsPowerConvergence(s) {
		return powerNone
	}
	running := summary.Status == "running"
	switch {
	case s == RunStrategyAlways && !running:
		return powerStart
	case s == RunStrategyHalted && running:
		return powerStop
	default:
		return powerNone
	}
}

// applyPower performs the planned transition.
func applyPower(ctx context.Context, c api.Client, action powerAction, ref *api.GuestRef) error {
	switch action {
	case powerStart:
		return c.StartGuest(ctx, ref)
	case powerStop:
		return c.StopGuest(ctx, ref)
	default:
		return nil
	}
}

// reconcilePower reads the live power state and performs the transition
// spec.runStrategy wants. A container is created stopped, and Proxmox's
// own start=1 is not used, so the power state is handled in one place.
func reconcilePower(ctx context.Context, c api.Client, spec *Spec, ref *api.GuestRef) error {
	if !wantsPowerConvergence(spec.RunStrategy) {
		return nil
	}
	summary, err := summaryByID(ctx, c, ref.VMID)
	if err != nil {
		return err
	}
	return applyPower(ctx, c, plannedPowerFor(spec.RunStrategy, summary), ref)
}

// summaryByID returns the cluster-level row for a vmid. GuestRef alone
// carries no power state or pool, and api.Client has no per-guest
// status call.
func summaryByID(ctx context.Context, c api.Client, vmid int) (*api.GuestSummary, error) {
	guests, err := c.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range guests {
		if g.VMID == vmid {
			return &g, nil
		}
	}
	return nil, fmt.Errorf("vmid %d: %w", vmid, api.ErrNotFound)
}

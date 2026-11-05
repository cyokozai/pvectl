package lxc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/diff"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
	"github.com/cyokozai/pvectl/internal/secretref"
)

// Apply implements resource.Applier: idempotent create-or-update.
//
//	decode → validate → resolve identity among lxc guests (vmid, else name)
//	  not found → osTemplate required → root secret → template on storage
//	              (download the {url, sha256} form if missing) → create
//	  found     → diff managed keys → PUT changed keys / resize grown volumes
//	  finally   → converge the power state to spec.runStrategy
func (h *Handler) Apply(ctx context.Context, c api.Client, obj *runtime.Unstructured, opts resource.ApplyOptions) (*resource.ApplyResult, error) {
	name := obj.Metadata.Name
	spec, err := decodeSpec(obj)
	if err != nil {
		return nil, err
	}
	if err := validateSpec(name, spec); err != nil {
		return nil, fmt.Errorf("%s: %w", obj.Source, err)
	}

	if opts.DryRun == resource.DryRunClient {
		// Without the server, create and update cannot be told apart,
		// so the create-only checks report but never fail.
		warnings, err := clientDryRunSecrets(spec)
		if err != nil {
			return nil, fmt.Errorf("%s: linuxcontainer %q: %w", obj.Source, name, err)
		}
		return &resource.ApplyResult{Action: resource.ActionValidated, Name: name, Warnings: warnings}, nil
	}

	ref, err := h.resolveRef(ctx, c, name, spec)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		if err := validateForCreate(name, spec); err != nil {
			return nil, fmt.Errorf("%s: %w", obj.Source, err)
		}
		if err := resolveRootPassword(spec); err != nil {
			return nil, fmt.Errorf("%s: linuxcontainer %q: %w", obj.Source, name, err)
		}
		return h.applyCreate(ctx, c, name, spec, opts)
	}
	return h.applyUpdate(ctx, c, name, spec, ref, opts)
}

// clientDryRunSecrets checks that spec.root.passwordFrom resolves on
// this machine. A client-side dry-run checks the manifest, not the
// machine it runs on, so an unresolvable reference is a warning there.
// The real apply resolves it only on create (ADR-010 §5).
func clientDryRunSecrets(spec *Spec) ([]string, error) {
	err := resolveRootPassword(spec)
	switch {
	case err == nil:
		return nil, nil
	case errors.Is(err, secretref.ErrUnresolved):
		return []string{fmt.Sprintf("spec.root.passwordFrom %q is not resolvable here: %v "+
			"(the reference itself is valid; creating the container needs it present)", spec.Root.PasswordFrom, err)}, nil
	default:
		return nil, err
	}
}

func (h *Handler) applyCreate(ctx context.Context, c api.Client, name string, spec *Spec, opts resource.ApplyOptions) (*resource.ApplyResult, error) {
	if opts.DryRun == resource.DryRunServer {
		warnings, err := templateDryRun(ctx, c, spec)
		if err != nil {
			return nil, err
		}
		return &resource.ApplyResult{Action: resource.ActionCreated, Name: name, Warnings: warnings}, nil
	}

	// Download first: a failed download creates nothing (ADR-010 §4).
	if err := ensureTemplate(ctx, c, spec); err != nil {
		return nil, fmt.Errorf("linuxcontainer %q: %w", name, err)
	}

	vmid := 0
	if spec.VMID != nil {
		vmid = *spec.VMID
	} else {
		next, err := c.NextID(ctx)
		if err != nil {
			return nil, err
		}
		vmid = next
	}
	if err := c.CreateLxc(ctx, spec.TargetNode, vmid, createParams(name, spec)); err != nil {
		return nil, err
	}

	ref := &api.GuestRef{VMID: vmid, Node: spec.TargetNode, Type: "lxc"}
	if err := reconcilePower(ctx, c, spec, ref); err != nil {
		return nil, fmt.Errorf("linuxcontainer %q was created but could not be brought to runStrategy %s: %w",
			name, spec.RunStrategy, err)
	}
	return &resource.ApplyResult{Action: resource.ActionCreated, Name: name}, nil
}

// templateOnStorage reports whether the volid the container is created
// from is already on its storage on the target node.
func templateOnStorage(ctx context.Context, c api.Client, spec *Spec) (bool, error) {
	t := *spec.OSTemplate
	volid := DerivedVolID(t)
	vols, err := c.StorageContent(ctx, spec.TargetNode, templateStorage(t), templateContent)
	if err != nil {
		return false, err
	}
	for _, v := range vols {
		if v.VolID == volid {
			return true, nil
		}
	}
	return false, nil
}

// templateDryRun reports, for --dry-run=server, whether the create
// would download the template, or would fail because a written volid is
// not there. Nothing is downloaded.
func templateDryRun(ctx context.Context, c api.Client, spec *Spec) ([]string, error) {
	t := *spec.OSTemplate
	present, err := templateOnStorage(ctx, c, spec)
	if err != nil {
		return nil, err
	}
	switch {
	case present:
		return nil, nil
	case needsDownload(t):
		return []string{fmt.Sprintf("OS template %s is not on storage %s of node %s yet; it would be downloaded from %s",
			DerivedVolID(t), templateStorage(t), spec.TargetNode, t.URL)}, nil
	default:
		return []string{fmt.Sprintf("OS template %s is not on storage %s of node %s; the create would fail",
			t.VolID, templateStorage(t), spec.TargetNode)}, nil
	}
}

// ensureTemplate downloads the {url, sha256} form of the template into
// its storage on the target node unless the derived volid is already
// there. A written volid is used as is: pvectl never downloads what it
// was not told the source of.
func ensureTemplate(ctx context.Context, c api.Client, spec *Spec) error {
	t := *spec.OSTemplate
	if !needsDownload(t) {
		return nil
	}
	present, err := templateOnStorage(ctx, c, spec)
	if err != nil || present {
		return err
	}
	storage := templateStorage(t)
	err = c.DownloadURL(ctx, spec.TargetNode, storage, downloadURLParams(t))
	switch {
	case err == nil:
		return nil
	case isForbidden(err):
		return fmt.Errorf("%w\n  hint: downloading an OS template needs Datastore.AllocateTemplate on /storage/%s, "+
			"and some Proxmox VE versions also ask for Sys.Audit and Sys.Modify on /nodes/%s; grant them to the API token, "+
			"or put the template on the storage yourself and write spec.osTemplate.volid", err, storage, spec.TargetNode)
	case isTaskTimeout(err):
		return fmt.Errorf("%w\n  hint: the download task may still be running on the Proxmox side; raise --timeout and apply again "+
			"(once the file is on the storage it is not downloaded again)", err)
	default:
		return err
	}
}

// isForbidden reports an HTTP 403 from the API. Proxmox VE names the
// missing privilege only in the status line, which the SDK drops, so
// the code is all there is to go on.
func isForbidden(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "code: 403") || strings.Contains(msg, "403 Forbidden") || strings.Contains(msg, "403 Permission")
}

// isTaskTimeout reports that waiting for a task ran out of time, either
// the SDK's task wait or the context deadline.
func isTaskTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Wait timeout")
}

func (h *Handler) applyUpdate(ctx context.Context, c api.Client, name string, spec *Spec, ref *api.GuestRef, opts resource.ApplyOptions) (*resource.ApplyResult, error) {
	if ref.Node != spec.TargetNode {
		return nil, fmt.Errorf("linuxcontainer %q exists on node %q but the manifest declares %q; apply does not migrate (use pvectl migrate ct)",
			name, ref.Node, spec.TargetNode)
	}

	// The cluster row carries the pool and the power state, neither of
	// which GuestRef knows. Fetched once, only when something needs it.
	var summary *api.GuestSummary
	if spec.Pool != "" || wantsPowerConvergence(spec.RunStrategy) {
		var err error
		if summary, err = summaryByID(ctx, c, ref.VMID); err != nil {
			return nil, err
		}
	}
	if err := checkCreateOnly(name, spec, summary); err != nil {
		return nil, err
	}
	warnings := updateWarnings(name, spec)

	desired := desiredParams(name, spec)
	current, err := c.LxcConfig(ctx, ref)
	if err != nil {
		return nil, err
	}
	d := diff.Compute(current, desired, NormalizeRules(desired))

	// The power state is part of the desired state, so an otherwise
	// identical config still counts as a change when runStrategy asks
	// for a transition.
	power := plannedPowerFor(spec.RunStrategy, summary)
	if d.Empty() && power == powerNone {
		return &resource.ApplyResult{Action: resource.ActionUnchanged, Name: name, Warnings: warnings}, nil
	}

	put, resizes, err := planUpdate(name, current, desired, d)
	if err != nil {
		return nil, err
	}

	if opts.DryRun == resource.DryRunServer {
		return &resource.ApplyResult{Action: resource.ActionConfigured, Name: name, Diff: &d, Warnings: warnings}, nil
	}

	if len(put) > 0 {
		if err := c.UpdateLxcConfig(ctx, ref, put); err != nil {
			return nil, err
		}
	}
	for _, r := range resizes {
		path := fmt.Sprintf("/nodes/%s/lxc/%d/resize", ref.Node, ref.VMID)
		if err := c.Raw().PutTask(ctx, path, map[string]any{"disk": r.disk, "size": r.size}); err != nil {
			return nil, fmt.Errorf("failed to resize %s: %w", r.disk, err)
		}
	}
	if err := applyPower(ctx, c, power, ref); err != nil {
		return nil, fmt.Errorf("linuxcontainer %q was configured but could not be brought to runStrategy %s: %w",
			name, spec.RunStrategy, err)
	}
	return &resource.ApplyResult{Action: resource.ActionConfigured, Name: name, Diff: &d, Warnings: warnings}, nil
}

// resolveRef finds the container a manifest refers to: by vmid when
// pinned, else by name — among lxc guests only. Returns nil when the
// container does not exist yet.
func (h *Handler) resolveRef(ctx context.Context, c api.Client, name string, spec *Spec) (*api.GuestRef, error) {
	if spec.VMID != nil {
		ref, err := c.GuestByID(ctx, *spec.VMID)
		switch {
		case err == nil:
			if ref.Type != "lxc" {
				return nil, fmt.Errorf("vmid %d is a %s guest, not a LinuxContainer", *spec.VMID, ref.Type)
			}
			return ref, nil
		case errors.Is(err, api.ErrNotFound):
			// Creating under a pinned vmid: the name must not already be
			// taken by another container, or apply would duplicate it.
			existing, findErr := h.findLxc(ctx, c, name)
			if findErr == nil {
				return nil, fmt.Errorf("linuxcontainer %q already exists as vmid %d, but the manifest declares vmid %d",
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

	summary, err := h.findLxc(ctx, c, name)
	switch {
	case err == nil:
		return refOf(summary), nil
	case errors.Is(err, api.ErrNotFound):
		return nil, nil
	default:
		return nil, err
	}
}

// Diff implements resource.Applier for the diff command: live state vs
// manifest, without writing anything.
func (h *Handler) Diff(ctx context.Context, c api.Client, obj *runtime.Unstructured) (*diff.Result, error) {
	name := obj.Metadata.Name
	spec, err := decodeSpec(obj)
	if err != nil {
		return nil, err
	}
	if err := validateSpec(name, spec); err != nil {
		return nil, fmt.Errorf("%s: %w", obj.Source, err)
	}

	desired := desiredParams(name, spec)
	ref, err := h.resolveRef(ctx, c, name, spec)
	if err != nil {
		return nil, err
	}
	current := map[string]any{}
	if ref != nil {
		if current, err = c.LxcConfig(ctx, ref); err != nil {
			return nil, err
		}
	}
	d := diff.Compute(current, desired, NormalizeRules(desired))
	return &d, nil
}

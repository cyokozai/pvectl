package vm

import (
	"context"
	"errors"
	"fmt"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/diff"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
)

// Apply implements resource.Handler: idempotent create-or-update.
//
//	decode → validate → resolve identity (vmid, else name)
//	  not found → create (direct or clone+reconfigure)
//	  found     → diff managed keys → PUT changed keys / resize grown disks
func (h *Handler) Apply(ctx context.Context, c api.Client, obj *runtime.Unstructured, opts resource.ApplyOptions) (*resource.ApplyResult, error) {
	name := obj.Metadata.Name
	spec := &Spec{}
	if err := obj.DecodeSpec(spec); err != nil {
		return nil, err
	}
	if err := validateSpec(name, spec); err != nil {
		return nil, fmt.Errorf("%s: %w", obj.Source, err)
	}

	if opts.DryRun == resource.DryRunClient {
		return &resource.ApplyResult{Action: resource.ActionValidated, Name: name}, nil
	}

	ref, err := h.resolveRef(ctx, c, name, spec)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return h.applyCreate(ctx, c, name, spec, opts)
	}
	return h.applyUpdate(ctx, c, name, spec, ref, opts)
}

func (h *Handler) applyCreate(ctx context.Context, c api.Client, name string, spec *Spec, opts resource.ApplyOptions) (*resource.ApplyResult, error) {
	var warnings []string
	if spec.Clone != "" && len(spec.Disks) > 0 {
		warnings = append(warnings, "spec.disks are ignored when cloning; the clone keeps the source's disks")
	}

	if opts.DryRun == resource.DryRunServer {
		return &resource.ApplyResult{Action: resource.ActionCreated, Name: name, Warnings: warnings}, nil
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

	if spec.Clone != "" {
		if err := h.createFromClone(ctx, c, name, spec, vmid); err != nil {
			return nil, err
		}
	} else {
		if err := c.CreateQemu(ctx, spec.TargetNode, vmid, createParams(name, spec)); err != nil {
			return nil, err
		}
	}
	return &resource.ApplyResult{Action: resource.ActionCreated, Name: name, Warnings: warnings}, nil
}

// createFromClone clones the source and then applies the manifest's
// config on top — resources, networks, cloud-init, tags, and onboot are
// honored instead of silently inheriting the source's values.
func (h *Handler) createFromClone(ctx context.Context, c api.Client, name string, spec *Spec, vmid int) error {
	src, err := c.FindGuest(ctx, spec.Clone)
	if err != nil {
		return fmt.Errorf("clone source %q: %w", spec.Clone, err)
	}
	if src.Type != "qemu" {
		return fmt.Errorf("clone source %q is a %s guest, not a VirtualMachine", spec.Clone, src.Type)
	}

	if err := c.CloneQemu(ctx, src, cloneParams(name, spec, vmid)); err != nil {
		return err
	}

	post := configParams(name, spec)
	if len(post) == 0 {
		return nil
	}
	newRef := &api.GuestRef{VMID: vmid, Node: spec.TargetNode, Type: "qemu"}
	if err := c.UpdateQemuConfig(ctx, newRef, post); err != nil {
		return fmt.Errorf("clone of %q succeeded but post-clone configuration failed: %w", spec.Clone, err)
	}
	return nil
}

func (h *Handler) applyUpdate(ctx context.Context, c api.Client, name string, spec *Spec, ref *api.GuestRef, opts resource.ApplyOptions) (*resource.ApplyResult, error) {
	var warnings []string
	if spec.Clone != "" {
		warnings = append(warnings, "spec.clone is create-only and ignored on update")
	}
	if spec.Pool != "" {
		warnings = append(warnings, "spec.pool is create-only and ignored on update")
	}

	if ref.Node != spec.TargetNode {
		return nil, fmt.Errorf("virtualmachine %q exists on node %q but the manifest declares %q; apply does not migrate (use the Proxmox migrate API)",
			name, ref.Node, spec.TargetNode)
	}

	desired := desiredParams(name, spec)
	current, err := c.QemuConfig(ctx, ref)
	if err != nil {
		return nil, err
	}
	d := diff.Compute(current, desired, NormalizeRules(desired))
	if d.Empty() {
		return &resource.ApplyResult{Action: resource.ActionUnchanged, Name: name, Warnings: warnings}, nil
	}

	put, resizes, err := h.planUpdate(name, current, desired, d)
	if err != nil {
		return nil, err
	}

	if opts.DryRun == resource.DryRunServer {
		return &resource.ApplyResult{Action: resource.ActionConfigured, Name: name, Diff: &d, Warnings: warnings}, nil
	}

	if len(put) > 0 {
		if err := c.UpdateQemuConfig(ctx, ref, put); err != nil {
			return nil, err
		}
	}
	for _, r := range resizes {
		path := fmt.Sprintf("/nodes/%s/qemu/%d/resize", ref.Node, ref.VMID)
		if err := c.Raw().PutTask(ctx, path, map[string]any{"disk": r.disk, "size": r.size}); err != nil {
			return nil, fmt.Errorf("failed to resize disk %s: %w", r.disk, err)
		}
	}
	return &resource.ApplyResult{Action: resource.ActionConfigured, Name: name, Diff: &d, Warnings: warnings}, nil
}

type resizeOp struct {
	disk string
	size string
}

// planUpdate turns diff entries into a config PUT payload plus resize
// operations, rejecting changes Proxmox cannot perform in place.
func (h *Handler) planUpdate(name string, current, desired map[string]any, d diff.Result) (map[string]any, []resizeOp, error) {
	put := map[string]any{}
	var resizes []resizeOp

	for _, e := range d.Entries {
		if !rxDiskKey.MatchString(e.Key) {
			put[e.Key] = desired[e.Key]
			continue
		}

		curRaw, exists := current[e.Key]
		if !exists {
			// New disk slot: allocation syntax works on config PUT.
			put[e.Key] = desired[e.Key]
			continue
		}
		cur := parseDiskValue(fmt.Sprintf("%v", curRaw))
		des := parseDiskValue(fmt.Sprintf("%v", desired[e.Key]))

		if des.Storage != cur.Storage {
			return nil, nil, fmt.Errorf("virtualmachine %q: cannot change storage of %s from %q to %q in place; move the disk manually",
				name, e.Key, cur.Storage, des.Storage)
		}
		if desFormat, ok := des.Opts["format"]; ok {
			if curFormat, ok := cur.Opts["format"]; ok && curFormat != desFormat {
				return nil, nil, fmt.Errorf("virtualmachine %q: cannot change format of %s from %q to %q; recreate the disk manually",
					name, e.Key, curFormat, desFormat)
			}
		}
		switch {
		case des.SizeBytes < cur.SizeBytes:
			return nil, nil, fmt.Errorf("virtualmachine %q: cannot shrink %s from %s to %s (Proxmox VE does not support shrinking)",
				name, e.Key, bytesToSizeString(cur.SizeBytes), bytesToSizeString(des.SizeBytes))
		case des.SizeBytes > cur.SizeBytes:
			resizes = append(resizes, resizeOp{disk: e.Key, size: bytesToSizeString(des.SizeBytes)})
		}

		if value, changed := rebuildDiskValue(cur, des); changed {
			put[e.Key] = value
		}
	}
	return put, resizes, nil
}

// rebuildDiskValue merges the manifest's disk options onto the existing
// volume value ("storage:volume,opts"). Reports changed=false when the
// options already match (size-only growth goes through resize instead).
func rebuildDiskValue(cur diskValue, des diskValue) (string, bool) {
	changed := false
	merged := map[string]string{}
	for k, v := range cur.Opts {
		merged[k] = v
	}
	for k, v := range des.Opts {
		if merged[k] != v {
			merged[k] = v
			changed = true
		}
	}
	if !changed {
		return "", false
	}
	if cur.SizeRaw != "" {
		merged["size"] = cur.SizeRaw
	}
	return cur.Storage + ":" + cur.Volume + optsSuffix(merged), true
}

// Diff implements resource.Handler for the diff command: live state vs
// manifest, without writing anything.
func (h *Handler) Diff(ctx context.Context, c api.Client, obj *runtime.Unstructured) (*diff.Result, error) {
	name := obj.Metadata.Name
	spec := &Spec{}
	if err := obj.DecodeSpec(spec); err != nil {
		return nil, err
	}
	if err := validateSpec(name, spec); err != nil {
		return nil, fmt.Errorf("%s: %w", obj.Source, err)
	}

	desired := desiredParams(name, spec)

	ref, err := h.resolveRef(ctx, c, name, spec)
	if err != nil && !errors.Is(err, api.ErrNotFound) {
		return nil, err
	}
	current := map[string]any{}
	if ref != nil {
		current, err = c.QemuConfig(ctx, ref)
		if err != nil {
			return nil, err
		}
	}
	d := diff.Compute(current, desired, NormalizeRules(desired))
	return &d, nil
}

package lxc

import (
	"fmt"
	"strings"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/diff"
)

// resizeOp is one PUT /nodes/{node}/lxc/{vmid}/resize call: disk is
// "rootfs" or "mpN", size the absolute new size ("16G").
type resizeOp struct {
	disk string
	size string
}

// planUpdate turns diff entries into a config PUT payload plus resize
// operations, rejecting changes Proxmox cannot perform in place.
//
// A config PUT replaces a key's whole value, so for netN and features
// the live sub-keys the manifest does not write (hwaddr, type, other
// feature flags) are carried over; otherwise an update would quietly
// change state the manifest never mentioned.
func planUpdate(name string, current, desired map[string]any, d diff.Result) (map[string]any, []resizeOp, error) {
	put := map[string]any{}
	var resizes []resizeOp

	for _, e := range d.Entries {
		curRaw, exists := current[e.Key]
		curStr := fmt.Sprintf("%v", curRaw)
		desStr := fmt.Sprintf("%v", desired[e.Key])

		switch {
		case e.Key == keyUnpriv:
			return nil, nil, fmt.Errorf("linuxcontainer %q: spec.unprivileged is create-only; the manifest declares %s but the container is %s — "+
				"recreate the container to change it, or match the manifest to the live value",
				name, privilegeName(desStr), privilegeName(e.Current))

		case !exists:
			// New key or slot: allocation syntax works on config PUT.
			put[e.Key] = desired[e.Key]

		case e.Key == keyRootFS || rxMountKey.MatchString(e.Key):
			value, resize, err := planVolume(name, e.Key, curStr, desStr)
			if err != nil {
				return nil, nil, err
			}
			if resize != nil {
				resizes = append(resizes, *resize)
			}
			if value != "" {
				put[e.Key] = value
			}

		case rxNetKey.MatchString(e.Key):
			put[e.Key] = mergeNetValue(curStr, desStr)

		case e.Key == keyFeatures:
			merged := parseOpts(curStr)
			for k, v := range parseOpts(desStr) {
				merged[k] = v
			}
			put[e.Key] = joinOpts(merged)

		default:
			put[e.Key] = desired[e.Key]
		}
	}
	return put, resizes, nil
}

// planVolume handles one changed rootfs / mpN: storage changes and
// shrinking are errors, growth is a resize, and option changes (mp=)
// rebuild the value on the existing volume.
func planVolume(name, key, curStr, desStr string) (string, *resizeOp, error) {
	cur, des := parseVolumeValue(curStr), parseVolumeValue(desStr)
	if des.Storage != cur.Storage {
		return "", nil, fmt.Errorf("linuxcontainer %q: cannot change storage of %s from %q to %q in place; move the volume manually",
			name, key, cur.Storage, des.Storage)
	}
	var resize *resizeOp
	switch {
	case des.SizeBytes < cur.SizeBytes:
		return "", nil, fmt.Errorf("linuxcontainer %q: cannot shrink %s from %s to %s (Proxmox VE does not support shrinking)",
			name, key, bytesToSizeString(cur.SizeBytes), bytesToSizeString(des.SizeBytes))
	case des.SizeBytes > cur.SizeBytes:
		resize = &resizeOp{disk: key, size: bytesToSizeString(des.SizeBytes)}
	}
	value, _ := rebuildVolumeValue(cur, des)
	return value, resize, nil
}

// rebuildVolumeValue merges the manifest's volume options onto the
// existing volume ("storage:volume,opts"). Reports changed=false when
// the options already match (size-only growth goes through resize).
func rebuildVolumeValue(cur, des volumeValue) (string, bool) {
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
	return cur.Storage + ":" + cur.Volume + "," + joinOpts(merged), true
}

// mergeNetValue lays the written NIC sub-keys over the live ones and
// renders name= first, the way Proxmox writes it.
func mergeNetValue(curStr, desStr string) string {
	merged := parseOpts(curStr)
	for k, v := range parseOpts(desStr) {
		merged[k] = v
	}
	nameOpt := "name=" + merged["name"]
	delete(merged, "name")
	if len(merged) == 0 {
		return nameOpt
	}
	return nameOpt + "," + joinOpts(merged)
}

func privilegeName(v string) string {
	if v == "1" {
		return "unprivileged"
	}
	return "privileged"
}

// checkCreateOnly rejects a declared pool that does not match the live
// one (ADR-006 §6). spec.osTemplate and spec.root are exempt: Proxmox
// keeps no record of either, so there is no live value to disagree
// with. spec.unprivileged is checked through the diff (planUpdate).
// A nil summary means the cluster row was not read.
func checkCreateOnly(name string, spec *Spec, summary *api.GuestSummary) error {
	if spec.Pool == "" || summary == nil || spec.Pool == summary.Pool {
		return nil
	}
	live := summary.Pool
	if live == "" {
		live = "(none)"
	}
	return fmt.Errorf("linuxcontainer %q: spec.pool is create-only; the manifest declares %q but the container is in pool %s — "+
		"move it with the Proxmox pool API, or drop spec.pool from the manifest", name, spec.Pool, live)
}

// updateWarnings lists what an update ignores on purpose. spec.root is
// only a warning so a manifest kept as it was at create time can be
// applied again and again (ADR-010 §5).
func updateWarnings(name string, spec *Spec) []string {
	if spec.Root == nil || (spec.Root.PasswordFrom == "" && len(spec.Root.SSHKeys) == 0) {
		return nil
	}
	fields := []string{}
	if spec.Root.PasswordFrom != "" {
		fields = append(fields, "passwordFrom")
	}
	if len(spec.Root.SSHKeys) > 0 {
		fields = append(fields, "sshKeys")
	}
	return []string{fmt.Sprintf("linuxcontainer %q already exists; spec.root (%s) is applied only at create and was not changed",
		name, strings.Join(fields, ", "))}
}

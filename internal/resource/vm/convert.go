package vm

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// cloudInitSlot is the drive slot the cloud-init disk is attached to.
const cloudInitSlot = "ide2"

// createParams renders the full flat parameter map for POST
// /nodes/{node}/qemu. Disk values use allocation syntax ("storage:GB").
// vmid is intentionally absent — the api layer sets it.
func createParams(name string, spec *Spec) map[string]any {
	params := typedCreateParams(name, spec)
	mergeRaw(params, spec)
	return params
}

// typedCreateParams is createParams without spec.raw merged in — the
// key set pvectl generates from typed fields alone.
func typedCreateParams(name string, spec *Spec) map[string]any {
	params := typedConfigParams(name, spec)

	for _, disk := range spec.Disks {
		params[disk.Name] = diskAllocationValue(disk)
	}
	if spec.CloudInit != nil {
		params[cloudInitSlot] = cloudInitDriveValue(spec)
	}
	return params
}

// mergeRaw copies spec.raw onto the generated params. Values are passed
// through verbatim; validateSpec has already rejected keys that collide
// with a generated one.
func mergeRaw(params map[string]any, spec *Spec) {
	for key, value := range spec.Raw {
		params[key] = value
	}
}

// rawConflicts lists the spec.raw keys pvectl already generates from a
// typed field. The comparison is made against the generated flat key
// set — not a static field list — so slot-addressed keys (scsi0, net1,
// ide2, ...) are caught too.
func rawConflicts(name string, spec *Spec) []string {
	if len(spec.Raw) == 0 {
		return nil
	}
	generated := typedCreateParams(name, spec)
	var conflicts []string
	for key := range spec.Raw {
		if _, has := generated[key]; has {
			conflicts = append(conflicts, key)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

// cloneParams renders the parameter map for POST /nodes/{node}/qemu/{src}/clone.
func cloneParams(name string, spec *Spec, newID int) map[string]any {
	params := map[string]any{
		"newid":  newID,
		"name":   name,
		"target": spec.TargetNode,
	}
	if spec.FullClone {
		params["full"] = 1
	}
	if spec.Pool != "" {
		params["pool"] = spec.Pool
	}
	if spec.Description != "" {
		params["description"] = spec.Description
	}
	return params
}

// configParams renders the keys settable via PUT
// /nodes/{node}/qemu/{vmid}/config: everything except disk allocation
// and create-only keys. Used for updates and post-clone configuration.
func configParams(name string, spec *Spec) map[string]any {
	params := typedConfigParams(name, spec)
	mergeRaw(params, spec)
	return params
}

// typedConfigParams is configParams without spec.raw merged in.
func typedConfigParams(name string, spec *Spec) map[string]any {
	params := map[string]any{
		"name":   name,
		"cores":  spec.Resources.CPU.Cores,
		"memory": spec.Resources.Memory,
	}
	if spec.Resources.CPU.Sockets > 0 {
		params["sockets"] = spec.Resources.CPU.Sockets
	}
	if spec.Resources.CPU.Type != "" {
		params["cpu"] = spec.Resources.CPU.Type
	}
	if spec.Description != "" {
		params["description"] = spec.Description
	}
	// Manual (the default) leaves onboot unmanaged; only the two
	// strategies that express an intent about boot declare the key.
	switch spec.RunStrategy {
	case RunStrategyAlways:
		params["onboot"] = 1
	case RunStrategyHalted:
		params["onboot"] = 0
	}
	if len(spec.Tags) > 0 {
		tags := append([]string(nil), spec.Tags...)
		sort.Strings(tags)
		params["tags"] = strings.Join(tags, ";")
	}
	for _, net := range spec.Networks {
		params[net.Name] = networkValue(net)
	}
	if ci := spec.CloudInit; ci != nil {
		if ci.User != "" {
			params["ciuser"] = ci.User
		}
		if ci.Password != "" {
			params["cipassword"] = ci.Password
		}
		if len(ci.SSHKeys) > 0 {
			params["sshkeys"] = url.PathEscape(strings.Join(ci.SSHKeys, "\n"))
		}
		if ci.IPConfig != "" {
			params["ipconfig0"] = ci.IPConfig
		}
		if ci.Nameserver != "" {
			params["nameserver"] = ci.Nameserver
		}
	}

	// A clone inherits sizing from its source; drop zero-value keys so
	// they neither overwrite the inherited config nor appear in diffs.
	if spec.Resources.CPU.Cores == 0 {
		delete(params, "cores")
	}
	if spec.Resources.Memory == 0 {
		delete(params, "memory")
	}
	return params
}

// desiredParams is the diff input: configParams plus desired disk
// values. The cloud-init drive slot is excluded — it is an
// implementation detail, not managed state.
func desiredParams(name string, spec *Spec) map[string]any {
	params := typedConfigParams(name, spec)
	for _, disk := range spec.Disks {
		params[disk.Name] = diskAllocationValue(disk)
	}
	mergeRaw(params, spec)
	return params
}

// diskAllocationValue renders "storage:sizeGB,opt=..." create syntax.
func diskAllocationValue(disk Disk) string {
	value := fmt.Sprintf("%s:%s", disk.Storage, sizeToGBString(disk.Size))
	opts := map[string]string{}
	if disk.Cache != "" {
		opts["cache"] = disk.Cache
	}
	if disk.Format != "" {
		opts["format"] = disk.Format
	}
	return value + optsSuffix(opts)
}

// cloudInitDriveValue picks the storage for the cloud-init drive.
func cloudInitDriveValue(spec *Spec) string {
	storage := spec.CloudInit.Storage
	if storage == "" && len(spec.Disks) > 0 {
		storage = spec.Disks[0].Storage
	}
	return storage + ":cloudinit"
}

// networkValue renders "model,bridge=...,tag=N".
func networkValue(net Network) string {
	model := net.Model
	if model == "" {
		model = "virtio"
	}
	value := model + ",bridge=" + net.Bridge
	if net.Tag > 0 {
		value += fmt.Sprintf(",tag=%d", net.Tag)
	}
	return value
}

// sizeToGBString converts a size like "32G" or "512M" to the GB number
// Proxmox allocation syntax expects ("32", "0.5").
func sizeToGBString(size string) string {
	bytes := parseSizeBytes(size)
	if bytes <= 0 {
		return size // let the API reject malformed sizes with its own error
	}
	gb := float64(bytes) / (1 << 30)
	s := fmt.Sprintf("%g", gb)
	return s
}

// optsSuffix renders ",k=v" pairs sorted by key for determinism.
func optsSuffix(opts map[string]string) string {
	if len(opts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(",")
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(opts[k])
	}
	return sb.String()
}

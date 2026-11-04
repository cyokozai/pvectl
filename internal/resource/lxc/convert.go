package lxc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cyokozai/pvectl/internal/secretref"
)

// Flat config keys pvectl generates outside the slot-addressed ones.
const (
	keyRootFS   = "rootfs"
	keyFeatures = "features"
	keyUnpriv   = "unprivileged"
)

// createParams renders the full flat parameter map for POST
// /nodes/{node}/lxc. Volume values use allocation syntax
// ("storage:GiB"). vmid is intentionally absent — the api layer sets it.
func createParams(name string, spec *Spec) map[string]any {
	params := typedCreateParams(name, spec)
	mergeRaw(params, spec)
	return params
}

// typedCreateParams is createParams without spec.raw merged in — the
// key set pvectl generates from typed fields alone.
func typedCreateParams(name string, spec *Spec) map[string]any {
	params := typedConfigParams(name, spec)
	addVolumes(params, spec)

	if spec.OSTemplate != nil {
		params["ostemplate"] = DerivedVolID(*spec.OSTemplate)
	}
	if spec.Pool != "" {
		params["pool"] = spec.Pool
	}
	if root := spec.Root; root != nil {
		if root.resolvedPassword != "" {
			params["password"] = root.resolvedPassword
		}
		if len(root.SSHKeys) > 0 {
			// Unlike qemu's sshkeys, ssh-public-keys is sent verbatim.
			params["ssh-public-keys"] = strings.Join(root.SSHKeys, "\n")
		}
	}
	return params
}

// desiredParams is the diff input: the keys apply manages after create.
// Create-only keys (ostemplate, pool, password, ssh-public-keys) are
// excluded; unprivileged stays so a mismatch can be reported, but
// planUpdate refuses to change it.
func desiredParams(name string, spec *Spec) map[string]any {
	params := typedConfigParams(name, spec)
	addVolumes(params, spec)
	mergeRaw(params, spec)
	return params
}

// typedConfigParams renders the keys settable via PUT
// /nodes/{node}/lxc/{vmid}/config, minus volumes and raw.
func typedConfigParams(name string, spec *Spec) map[string]any {
	params := map[string]any{"hostname": name}

	if spec.Unprivileged != nil {
		params[keyUnpriv] = boolInt(*spec.Unprivileged)
	}
	if spec.Resources.Cores > 0 {
		params["cores"] = spec.Resources.Cores
	}
	if spec.Resources.Memory > 0 {
		params["memory"] = spec.Resources.Memory
	}
	if spec.Resources.Swap != nil {
		params["swap"] = *spec.Resources.Swap
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
	if spec.Nameserver != "" {
		params["nameserver"] = spec.Nameserver
	}
	if value := featuresValue(spec.Features); value != "" {
		params[keyFeatures] = value
	}
	return params
}

// addVolumes adds rootfs and mpN in allocation syntax.
func addVolumes(params map[string]any, spec *Spec) {
	if spec.RootFS != nil {
		params[keyRootFS] = spec.RootFS.Storage + ":" + sizeToGBString(spec.RootFS.Size)
	}
	for _, mp := range spec.MountPoints {
		params[mp.Name] = mountPointValue(mp)
	}
}

// mountPointValue renders "storage:GiB,mp=/path".
func mountPointValue(mp MountPoint) string {
	return fmt.Sprintf("%s:%s,mp=%s", mp.Storage, sizeToGBString(mp.Size), mp.Path)
}

// networkValue renders "name=eth0,bridge=vmbr0,ip=...,gw=...,tag=N,hwaddr=...".
// Only written sub-keys appear, except name, which defaults to eth{N}
// for slot net{N} — a default inside a written NIC, not a NIC pvectl
// adds (ADR-010 §2).
func networkValue(net Network) string {
	iface := net.Interface
	if iface == "" {
		iface = "eth" + strings.TrimPrefix(net.Name, "net")
	}
	parts := []string{"name=" + iface, "bridge=" + net.Bridge}
	if net.IP != "" {
		parts = append(parts, "ip="+net.IP)
	}
	if net.GW != "" {
		parts = append(parts, "gw="+net.GW)
	}
	if net.Tag > 0 {
		parts = append(parts, fmt.Sprintf("tag=%d", net.Tag))
	}
	if net.HWAddr != "" {
		parts = append(parts, "hwaddr="+strings.ToUpper(net.HWAddr))
	}
	return strings.Join(parts, ",")
}

// featuresValue renders the written feature flags sorted by key, or ""
// when none is written.
func featuresValue(f *Features) string {
	opts := featureOpts(f)
	if len(opts) == 0 {
		return ""
	}
	return joinOpts(opts)
}

// featureOpts lists the written feature flags as "0"/"1".
func featureOpts(f *Features) map[string]string {
	opts := map[string]string{}
	if f == nil {
		return opts
	}
	for key, flag := range map[string]*bool{
		"nesting": f.Nesting,
		"keyctl":  f.Keyctl,
		"fuse":    f.Fuse,
		"mknod":   f.Mknod,
	} {
		if flag != nil {
			opts[key] = fmt.Sprint(boolInt(*flag))
		}
	}
	return opts
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
// typed field, compared against the generated flat key set so
// slot-addressed keys (mp0, net1, ...) are caught too.
func rawConflicts(name string, spec *Spec) []string {
	if len(spec.Raw) == 0 {
		return nil
	}
	generated := typedCreateParams(name, probeSpec(spec))
	var conflicts []string
	for key := range spec.Raw {
		if _, has := generated[key]; has {
			conflicts = append(conflicts, key)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

// probeSpec returns a copy of spec with deferred values filled in, so
// the generated key set is complete before apply resolves secrets.
func probeSpec(spec *Spec) *Spec {
	if spec.Root == nil || spec.Root.PasswordFrom == "" || spec.Root.resolvedPassword != "" {
		return spec
	}
	probe := *spec
	root := *spec.Root
	root.resolvedPassword = "\x00unresolved"
	probe.Root = &root
	return &probe
}

// resolveRootPassword reads spec.root.passwordFrom into the value
// createParams sends. Apply calls it before a create (and skips it for
// a client-side dry run, as vm does); errors from secretref are wrapped
// with the field name.
func resolveRootPassword(spec *Spec) error {
	if spec.Root == nil || spec.Root.PasswordFrom == "" {
		return nil
	}
	value, err := secretref.Resolve(spec.Root.PasswordFrom)
	if err != nil {
		return fmt.Errorf("spec.root.passwordFrom: %w", err)
	}
	spec.Root.resolvedPassword = value
	return nil
}

// joinOpts renders "k=v" pairs sorted by key, comma-separated.
func joinOpts(opts map[string]string) string {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + opts[k]
	}
	return strings.Join(parts, ",")
}

package vm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/runtime"
)

// newVMFromSummary builds a list-level VM object from /cluster/resources
// data only (no per-VM config fetch).
func newVMFromSummary(g api.GuestSummary) *VM {
	vmid := g.VMID
	return &VM{
		TypeMeta: runtime.TypeMeta{APIVersion: APIVersion, Kind: KindName},
		Metadata: runtime.ObjectMeta{Name: g.Name},
		Spec: Spec{
			TargetNode: g.Node,
			VMID:       &vmid,
			Resources: Resources{
				CPU:    CPU{Cores: g.MaxCPU},
				Memory: int(g.MaxMem / (1 << 20)),
			},
			Tags: g.Tags,
		},
		Status: statusFromSummary(g),
	}
}

// newVMFromConfig builds the full, round-trippable VM object from the
// guest's raw config plus its cluster summary.
func newVMFromConfig(g api.GuestSummary, cfg map[string]any) *VM {
	vmid := g.VMID
	spec := specFromConfig(g.Node, cfg)
	spec.VMID = &vmid
	name := cfgString(cfg, "name")
	if name == "" {
		name = g.Name
	}
	return &VM{
		TypeMeta: runtime.TypeMeta{APIVersion: APIVersion, Kind: KindName},
		Metadata: runtime.ObjectMeta{Name: name},
		Spec:     spec,
		Status:   statusFromSummary(g),
	}
}

func statusFromSummary(g api.GuestSummary) *Status {
	return &Status{
		VMID:   g.VMID,
		Node:   g.Node,
		State:  g.Status,
		Uptime: g.Uptime,
		CPU:    g.CPU,
		MaxMem: g.MaxMem,
	}
}

// specFromConfig reverse-converts a raw Proxmox config into the
// manifest spec shape. Write-only values (cipassword) never round-trip.
func specFromConfig(node string, cfg map[string]any) Spec {
	spec := Spec{
		TargetNode:  node,
		Description: cfgString(cfg, "description"),
		Resources: Resources{
			CPU: CPU{
				Cores:   cfgInt(cfg, "cores"),
				Sockets: cfgInt(cfg, "sockets"),
				Type:    normalizeCPUType(cfgString(cfg, "cpu")),
			},
			Memory: cfgInt(cfg, "memory"),
		},
		StartOnBoot: cfgInt(cfg, "onboot") == 1,
		Tags:        splitSortTags(cfgString(cfg, "tags")),
	}

	for key, raw := range cfg {
		value, ok := raw.(string)
		if !ok {
			continue
		}
		switch {
		case rxDiskKey.MatchString(key):
			if disk, ok := diskFromValue(key, value); ok {
				spec.Disks = append(spec.Disks, disk)
			}
		case rxNetKey.MatchString(key):
			spec.Networks = append(spec.Networks, netFromValue(key, value))
		}
	}
	sort.Slice(spec.Disks, func(i, j int) bool { return spec.Disks[i].Name < spec.Disks[j].Name })
	sort.Slice(spec.Networks, func(i, j int) bool { return spec.Networks[i].Name < spec.Networks[j].Name })

	ci := CloudInit{
		User:       cfgString(cfg, "ciuser"),
		IPConfig:   cfgString(cfg, "ipconfig0"),
		Nameserver: cfgString(cfg, "nameserver"),
	}
	if keys := cfgString(cfg, "sshkeys"); keys != "" {
		for _, key := range strings.Split(decodeSSHKeys(keys), "\n") {
			if key = strings.TrimSpace(key); key != "" {
				ci.SSHKeys = append(ci.SSHKeys, key)
			}
		}
	}
	if ci.User != "" || ci.IPConfig != "" || ci.Nameserver != "" || len(ci.SSHKeys) > 0 {
		spec.CloudInit = &ci
	}
	return spec
}

// diskFromValue parses a live disk value; cloud-init drives and CD-ROMs
// report ok=false.
func diskFromValue(key, value string) (Disk, bool) {
	if strings.Contains(value, "cloudinit") || strings.Contains(value, "media=cdrom") {
		return Disk{}, false
	}
	d := parseDiskValue(value)
	return Disk{
		Name:    key,
		Size:    bytesToSizeString(d.SizeBytes),
		Storage: d.Storage,
		Format:  d.Opts["format"],
		Cache:   d.Opts["cache"],
	}, true
}

func netFromValue(key, value string) Network {
	n := parseNetValue(value)
	tag, _ := strconv.Atoi(n.Opts["tag"])
	return Network{
		Name:   key,
		Bridge: n.Opts["bridge"],
		Model:  n.Model,
		Tag:    tag,
	}
}

// bytesToSizeString renders bytes the way manifests write sizes.
func bytesToSizeString(bytes int64) string {
	switch {
	case bytes <= 0:
		return ""
	case bytes%(1<<30) == 0:
		return fmt.Sprintf("%dG", bytes/(1<<30))
	case bytes%(1<<20) == 0:
		return fmt.Sprintf("%dM", bytes/(1<<20))
	default:
		return fmt.Sprintf("%dK", bytes/(1<<10))
	}
}

func splitSortTags(s string) []string {
	if s == "" {
		return nil
	}
	tags := strings.Split(s, ";")
	sort.Strings(tags)
	return tags
}

func cfgString(cfg map[string]any, key string) string {
	switch v := cfg[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func cfgInt(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

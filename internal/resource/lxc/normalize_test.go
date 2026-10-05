package lxc

import (
	"testing"

	"github.com/cyokozai/pvectl/internal/diff"
)

// equalUnderRules reports whether current and desired agree on key.
func equalUnderRules(t *testing.T, key string, current any, desired any) (bool, diff.Result) {
	t.Helper()
	des := map[string]any{key: desired}
	cur := map[string]any{}
	if current != nil {
		cur[key] = current
	}
	res := diff.Compute(cur, des, NormalizeRules(des))
	return res.Empty(), res
}

func TestNormalizeVolumes(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		current string // live value (volume form)
		desired string // manifest value (allocation form)
		equal   bool
	}{
		{"rootfs same", "rootfs", "local-lvm:vm-200-disk-0,size=8G", "local-lvm:8", true},
		{"rootfs unit normalized", "rootfs", "local-lvm:vm-200-disk-0,size=8192M", "local-lvm:8", true},
		{"rootfs grown", "rootfs", "local-lvm:vm-200-disk-0,size=8G", "local-lvm:16", false},
		{"rootfs storage differs", "rootfs", "other:vm-200-disk-0,size=8G", "local-lvm:8", false},
		{"rootfs server options ignored", "rootfs", "local-lvm:vm-200-disk-0,mountoptions=noatime,size=8G", "local-lvm:8", true},
		{"mp same", "mp0", "local-lvm:vm-200-disk-1,mp=/data,size=32G", "local-lvm:32,mp=/data", true},
		{"mp path differs", "mp0", "local-lvm:vm-200-disk-1,mp=/srv,size=32G", "local-lvm:32,mp=/data", false},
		{"mp backup option ignored", "mp0", "local-lvm:vm-200-disk-1,backup=1,mp=/data,size=32G", "local-lvm:32,mp=/data", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if eq, res := equalUnderRules(t, tt.key, tt.current, tt.desired); eq != tt.equal {
				t.Errorf("equal = %v, want %v (entries: %+v)", eq, tt.equal, res.Entries)
			}
		})
	}
}

func TestNormalizeNetworks(t *testing.T) {
	tests := []struct {
		name    string
		current string
		desired string
		equal   bool
	}{
		{
			"generated hwaddr and type are ignored",
			"name=eth0,bridge=vmbr0,hwaddr=BC:24:11:00:00:01,ip=dhcp,type=veth",
			"name=eth0,bridge=vmbr0,ip=dhcp",
			true,
		},
		{
			"sub-key order does not matter",
			"bridge=vmbr0,ip=10.0.0.5/24,gw=10.0.0.1,name=eth0,tag=10",
			"name=eth0,bridge=vmbr0,ip=10.0.0.5/24,gw=10.0.0.1,tag=10",
			true,
		},
		{"bridge differs", "name=eth0,bridge=vmbr1,ip=dhcp", "name=eth0,bridge=vmbr0,ip=dhcp", false},
		{"ip differs", "name=eth0,bridge=vmbr0,ip=dhcp", "name=eth0,bridge=vmbr0,ip=10.0.0.5/24", false},
		{"interface differs", "name=eth1,bridge=vmbr0", "name=eth0,bridge=vmbr0", false},
		{"written tag missing live", "name=eth0,bridge=vmbr0", "name=eth0,bridge=vmbr0,tag=10", false},
		{
			"pinned hwaddr compares case-insensitively",
			"name=eth0,bridge=vmbr0,hwaddr=bc:24:11:00:00:01",
			"name=eth0,bridge=vmbr0,hwaddr=BC:24:11:00:00:01",
			true,
		},
		{
			"pinned hwaddr differs",
			"name=eth0,bridge=vmbr0,hwaddr=BC:24:11:00:00:02",
			"name=eth0,bridge=vmbr0,hwaddr=BC:24:11:00:00:01",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if eq, res := equalUnderRules(t, "net0", tt.current, tt.desired); eq != tt.equal {
				t.Errorf("equal = %v, want %v (entries: %+v)", eq, tt.equal, res.Entries)
			}
		})
	}
}

func TestNormalizeFeatures(t *testing.T) {
	tests := []struct {
		name    string
		current any // nil: key absent live
		desired string
		equal   bool
	}{
		{"same", "nesting=1", "nesting=1", true},
		{"unwritten live sub-key is ignored", "keyctl=1,nesting=1", "nesting=1", true},
		{"absent sub-key reads as 0", "keyctl=1", "nesting=0", true},
		{"absent key reads as all 0", nil, "nesting=0", true},
		{"absent key differs from 1", nil, "nesting=1", false},
		{"value differs", "nesting=0", "nesting=1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if eq, res := equalUnderRules(t, "features", tt.current, tt.desired); eq != tt.equal {
				t.Errorf("equal = %v, want %v (entries: %+v)", eq, tt.equal, res.Entries)
			}
		})
	}
}

func TestNormalizeUnprivileged(t *testing.T) {
	tests := []struct {
		name    string
		current any
		desired int
		equal   bool
	}{
		{"1 vs true", float64(1), 1, true},
		{"absent means privileged", nil, 0, true},
		{"absent vs true", nil, 1, false},
		{"0 vs true", float64(0), 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if eq, res := equalUnderRules(t, "unprivileged", tt.current, tt.desired); eq != tt.equal {
				t.Errorf("equal = %v, want %v (entries: %+v)", eq, tt.equal, res.Entries)
			}
		})
	}
}

func TestNormalizeTagsOrderInsensitive(t *testing.T) {
	if eq, res := equalUnderRules(t, "tags", "web;prod", "prod;web"); !eq {
		t.Errorf("tag order must not produce a diff: %+v", res.Entries)
	}
}

func TestParseVolumeValue(t *testing.T) {
	t.Run("live form", func(t *testing.T) {
		v := parseVolumeValue("local-lvm:vm-200-disk-1,mp=/data,size=32G")
		if v.Storage != "local-lvm" || v.Volume != "vm-200-disk-1" || v.SizeBytes != 32<<30 || v.Opts["mp"] != "/data" {
			t.Errorf("parsed = %+v", v)
		}
	})
	t.Run("allocation form", func(t *testing.T) {
		v := parseVolumeValue("local-lvm:8")
		if v.Storage != "local-lvm" || v.Volume != "" || v.SizeBytes != 8<<30 {
			t.Errorf("parsed = %+v", v)
		}
	})
}

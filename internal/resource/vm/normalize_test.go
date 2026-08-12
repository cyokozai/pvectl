package vm

import (
	"net/url"
	"testing"

	"github.com/cyokozai/pvectl/internal/diff"
)

func TestNormalizeDiskValues(t *testing.T) {
	tests := []struct {
		name    string
		current string // live config value (volume form)
		desired string // manifest value (allocation form)
		equal   bool
	}{
		{
			"same size and options",
			"local-lvm:vm-100-disk-0,cache=writeback,format=qcow2,size=32G",
			"local-lvm:32,cache=writeback,format=qcow2",
			true,
		},
		{
			"size differs",
			"local-lvm:vm-100-disk-0,size=32G",
			"local-lvm:64",
			false,
		},
		{
			"size unit is normalized (32768M == 32G)",
			"local-lvm:vm-100-disk-0,size=32768M",
			"local-lvm:32",
			true,
		},
		{
			"server-side extra options are ignored",
			"local-lvm:vm-100-disk-0,discard=on,iothread=1,size=32G",
			"local-lvm:32",
			true,
		},
		{
			"explicitly desired option differs",
			"local-lvm:vm-100-disk-0,cache=none,size=32G",
			"local-lvm:32,cache=writeback",
			false,
		},
		{
			"storage differs",
			"other-pool:vm-100-disk-0,size=32G",
			"local-lvm:32",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := map[string]any{"scsi0": tt.desired}
			res := diff.Compute(map[string]any{"scsi0": tt.current}, desired, NormalizeRules(desired))
			if res.Empty() != tt.equal {
				t.Errorf("equal = %v, want %v (entries: %+v)", res.Empty(), tt.equal, res.Entries)
			}
		})
	}
}

func TestNormalizeNetworkValues(t *testing.T) {
	tests := []struct {
		name    string
		current string
		desired string
		equal   bool
	}{
		{
			"MAC address is ignored when manifest does not pin one",
			"virtio=DE:AD:BE:EF:00:01,bridge=vmbr0,tag=100",
			"virtio,bridge=vmbr0,tag=100",
			true,
		},
		{
			"bridge differs",
			"virtio=DE:AD:BE:EF:00:01,bridge=vmbr1",
			"virtio,bridge=vmbr0",
			false,
		},
		{
			"tag differs",
			"virtio=DE:AD:BE:EF:00:01,bridge=vmbr0,tag=200",
			"virtio,bridge=vmbr0,tag=100",
			false,
		},
		{
			"model differs",
			"e1000=DE:AD:BE:EF:00:01,bridge=vmbr0",
			"virtio,bridge=vmbr0",
			false,
		},
		{
			"server-side firewall option is ignored",
			"virtio=DE:AD:BE:EF:00:01,bridge=vmbr0,firewall=1",
			"virtio,bridge=vmbr0",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := map[string]any{"net0": tt.desired}
			res := diff.Compute(map[string]any{"net0": tt.current}, desired, NormalizeRules(desired))
			if res.Empty() != tt.equal {
				t.Errorf("equal = %v, want %v (entries: %+v)", res.Empty(), tt.equal, res.Entries)
			}
		})
	}
}

func TestNormalizeTagsOrderInsensitive(t *testing.T) {
	desired := map[string]any{"tags": "prod;web"}
	res := diff.Compute(map[string]any{"tags": "web;prod"}, desired, NormalizeRules(desired))
	if !res.Empty() {
		t.Errorf("tag order must not produce a diff: %+v", res.Entries)
	}
}

func TestNormalizeSSHKeysURLEncoding(t *testing.T) {
	key := "ssh-ed25519 AAAA test@example"
	desired := map[string]any{"sshkeys": url.PathEscape(key)}
	// The server may return a different but equivalent encoding.
	current := map[string]any{"sshkeys": url.QueryEscape(key)}
	res := diff.Compute(current, desired, NormalizeRules(desired))
	if !res.Empty() {
		t.Errorf("equivalent sshkeys encodings must not diff: %+v", res.Entries)
	}
}

func TestNormalizeCIPasswordIsWriteOnly(t *testing.T) {
	desired := map[string]any{"cipassword": "new-secret"}
	res := diff.Compute(map[string]any{"cipassword": "**********"}, desired, NormalizeRules(desired))
	if !res.Empty() {
		t.Errorf("cipassword must never appear in a diff: %+v", res.Entries)
	}
}

func TestNormalizeCPUType(t *testing.T) {
	for current, equal := range map[string]bool{
		"host":                    true,
		"cputype=host":            true,
		"cputype=host,flags=+aes": true,
		"kvm64":                   false,
	} {
		desired := map[string]any{"cpu": "host"}
		res := diff.Compute(map[string]any{"cpu": current}, desired, NormalizeRules(desired))
		if res.Empty() != equal {
			t.Errorf("cpu %q vs host: equal = %v, want %v", current, res.Empty(), equal)
		}
	}
}

func TestParseDiskValue(t *testing.T) {
	t.Run("live volume form", func(t *testing.T) {
		d := parseDiskValue("local-lvm:vm-100-disk-0,cache=writeback,size=32G")
		if d.Storage != "local-lvm" || d.Volume != "vm-100-disk-0" {
			t.Errorf("parsed = %+v", d)
		}
		if d.SizeBytes != 32*1024*1024*1024 {
			t.Errorf("SizeBytes = %d", d.SizeBytes)
		}
		if d.Opts["cache"] != "writeback" {
			t.Errorf("Opts = %+v", d.Opts)
		}
	})

	t.Run("allocation form", func(t *testing.T) {
		d := parseDiskValue("local-lvm:32,format=qcow2")
		if d.Storage != "local-lvm" || d.Volume != "" || d.SizeBytes != 32*1024*1024*1024 {
			t.Errorf("parsed = %+v", d)
		}
	})
}

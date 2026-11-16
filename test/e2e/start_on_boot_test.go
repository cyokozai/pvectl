package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/test/pvefake"
)

// TestExportStoppedOnbootGuestsApplyUnchanged is ADR-005 §5 for a
// guest stopped for maintenance with onboot=1: the export must apply
// unchanged, and apply must not send a start request.
func TestExportStoppedOnbootGuestsApplyUnchanged(t *testing.T) {
	cases := []struct {
		kind, name, prefix string
		guest              pvefake.Guest
	}{
		{"vm", "maint-vm", "virtualmachine", pvefake.Guest{
			VMID: 400, Name: "maint-vm", Node: "pve1", Type: "qemu", Status: "stopped",
			Config: map[string]any{
				"name": "maint-vm", "cores": "2", "memory": "1024", "onboot": "1",
				"scsi0": "local-lvm:vm-400-disk-0,size=16G",
				"net0":  "virtio=BC:24:11:00:04:00,bridge=vmbr0",
			},
		}},
		{"ct", "maint-ct", "linuxcontainer", pvefake.Guest{
			VMID: 401, Name: "maint-ct", Node: "pve1", Type: "lxc", Status: "stopped",
			Config: map[string]any{
				"hostname": "maint-ct", "unprivileged": "1", "cores": "1", "memory": "512", "onboot": "1",
				"rootfs": "local-lvm:vm-401-disk-0,size=8G",
				"net0":   "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:00:04:01,ip=dhcp,type=veth",
			},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			e := newEnv(t)
			e.server.AddGuest(tc.guest)

			exported, stderr, err := e.run(t, "get", tc.kind, tc.name, "-o", "yaml")
			if err != nil {
				t.Fatalf("get -o yaml: %v\nstderr: %s", err, stderr)
			}
			if !strings.Contains(exported, "startOnBoot: true") || strings.Contains(exported, "runStrategy") {
				t.Errorf("export should carry startOnBoot: true and no runStrategy:\n%s", exported)
			}

			stdout, stderr, err := e.run(t, "apply", "-f", writeFile(t, "export.yaml", exported))
			if err != nil {
				t.Fatalf("apply(export): %v\nstderr: %s\nexport:\n%s", err, stderr, exported)
			}
			if !strings.Contains(stdout, tc.prefix+"/"+tc.name+" unchanged") {
				t.Errorf("apply(export) stdout = %q, want unchanged\nexport:\n%s", stdout, exported)
			}
			for _, req := range e.server.Requests() {
				if strings.HasSuffix(req, "/status/start") {
					t.Errorf("apply(export) sent %s", req)
				}
			}
			if g := e.server.Guest(tc.guest.VMID); g.Status != "stopped" || g.Config["onboot"] != "1" {
				t.Errorf("guest after apply = %+v, want stopped with onboot=1", g)
			}
		})
	}
}

// TestRunStrategyAndStartOnBootAreIndependent: through the real stack,
// runStrategy moves only the power state and startOnBoot only onboot.
func TestRunStrategyAndStartOnBootAreIndependent(t *testing.T) {
	e := newEnv(t)
	write := func(lines string) string {
		return writeFile(t, "vm.yaml", fmt.Sprintf(manifestTemplate, 2, 1024)+lines)
	}

	if _, stderr, err := e.run(t, "apply", "-f", write("  runStrategy: Always\n")); err != nil {
		t.Fatalf("apply(Always): %v\nstderr: %s", err, stderr)
	}
	g := e.server.Guest(200)
	if g == nil || g.Status != "running" {
		t.Fatalf("guest after Always = %+v, want running", g)
	}
	if _, has := g.Config["onboot"]; has {
		t.Errorf("runStrategy alone wrote onboot: %+v", g.Config)
	}

	if _, stderr, err := e.run(t, "apply", "-f", write("  runStrategy: Always\n  startOnBoot: true\n")); err != nil {
		t.Fatalf("apply(startOnBoot): %v\nstderr: %s", err, stderr)
	}
	if g := e.server.Guest(200); g.Config["onboot"] != "1" {
		t.Errorf("onboot after startOnBoot: true = %v, want 1", g.Config["onboot"])
	}

	if _, stderr, err := e.run(t, "apply", "-f", write("  runStrategy: Halted\n")); err != nil {
		t.Fatalf("apply(Halted): %v\nstderr: %s", err, stderr)
	}
	if g := e.server.Guest(200); g.Status != "stopped" || g.Config["onboot"] != "1" {
		t.Errorf("guest after Halted = %+v, want stopped with onboot still 1", g)
	}
}

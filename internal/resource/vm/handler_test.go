package vm

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/api/apitest"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/resource"
)

// liveConfig is the raw config the API would return for fullSpec()
// applied as vmid 100 on pve1 (values echoed back as strings, plus
// server-managed keys).
func liveConfig() map[string]any {
	return map[string]any{
		"name":        "web-server",
		"cores":       "2",
		"sockets":     "1",
		"cpu":         "host",
		"memory":      "2048",
		"description": "test vm",
		"scsi0":       "local-lvm:vm-100-disk-0,cache=writeback,format=qcow2,size=32G",
		"net0":        "virtio=DE:AD:BE:EF:00:01,bridge=vmbr0,tag=100",
		"ide2":        "local-lvm:vm-100-cloudinit,media=cdrom",
		"ciuser":      "admin",
		"cipassword":  "**********",
		"sshkeys":     url.QueryEscape("ssh-ed25519 AAAA test@example"),
		"ipconfig0":   "ip=10.0.0.5/24,gw=10.0.0.1",
		"nameserver":  "1.1.1.1",
		"onboot":      "1",
		"tags":        "prod;web",
		"vmgenid":     "f9d1a2b3-0000-0000-0000-000000000000",
		"digest":      "abcdef",
		"smbios1":     "uuid=11111111-2222-3333-4444-555555555555",
	}
}

func seededFake() *apitest.Fake {
	f := &apitest.Fake{NextIDValue: 105}
	f.AddGuest(api.GuestSummary{
		VMID: 100, Name: "web-server", Node: "pve1", Status: "running",
		MaxCPU: 2, MaxMem: 2048 * 1024 * 1024, Uptime: 3600, Tags: []string{"prod", "web"},
	}, liveConfig())
	f.AddGuest(api.GuestSummary{VMID: 101, Name: "db", Node: "pve2", Status: "stopped", MaxCPU: 4, MaxMem: 4096 * 1024 * 1024}, map[string]any{
		"name": "db", "cores": "4", "memory": "4096",
	})
	f.AddGuest(api.GuestSummary{VMID: 200, Name: "ct-1", Node: "pve1", Type: "lxc", Status: "running"}, nil)
	return f
}

func TestHandlerIdentity(t *testing.T) {
	h := NewHandler()
	if h.Kind() != "VirtualMachine" || h.APIVersion() != "pve.io/v1alpha1" {
		t.Errorf("identity = %s/%s", h.APIVersion(), h.Kind())
	}
	aliases := strings.Join(h.Aliases(), ",")
	for _, want := range []string{"vm", "vms", "virtualmachine", "virtualmachines"} {
		if !strings.Contains(aliases, want) {
			t.Errorf("Aliases() = %s, missing %q", aliases, want)
		}
	}
	var _ resource.Handler = h
	var _ resource.Starter = h
	var _ resource.Stopper = h
}

func TestHandlerList(t *testing.T) {
	h := NewHandler()
	objs, err := h.List(context.Background(), seededFake())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	// Only qemu guests, sorted by vmid.
	if len(objs) != 2 {
		t.Fatalf("List() = %d objects, want 2 (lxc excluded)", len(objs))
	}
	first := objs[0].(*VM)
	if first.Metadata.Name != "web-server" || first.Status.VMID != 100 || first.Status.State != "running" {
		t.Errorf("first = %+v", first)
	}
	if first.Spec.Resources.CPU.Cores != 2 || first.Spec.Resources.Memory != 2048 {
		t.Errorf("first spec sizing = %+v", first.Spec.Resources)
	}
}

func TestHandlerColumns(t *testing.T) {
	h := NewHandler()
	objs, _ := h.List(context.Background(), seededFake())

	var sb strings.Builder
	if err := printer.Print(&sb, printer.FormatWide, objs, h.Columns(true)); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	out := sb.String()
	for _, want := range []string{"NAME", "VMID", "NODE", "STATUS", "web-server", "100", "pve1", "running", "prod,web"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestHandlerGet(t *testing.T) {
	h := NewHandler()

	t.Run("full round-trippable object", func(t *testing.T) {
		obj, err := h.Get(context.Background(), seededFake(), "web-server")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		vm := obj.(*VM)
		if vm.APIVersion != APIVersion || vm.Kind != KindName {
			t.Errorf("TypeMeta = %+v", vm.TypeMeta)
		}
		if vm.Spec.TargetNode != "pve1" || vm.Spec.Resources.CPU.Type != "host" {
			t.Errorf("spec = %+v", vm.Spec)
		}
		if len(vm.Spec.Disks) != 1 || vm.Spec.Disks[0].Name != "scsi0" || vm.Spec.Disks[0].Size != "32G" || vm.Spec.Disks[0].Cache != "writeback" {
			t.Errorf("disks = %+v", vm.Spec.Disks)
		}
		if len(vm.Spec.Networks) != 1 || vm.Spec.Networks[0].Bridge != "vmbr0" || vm.Spec.Networks[0].Tag != 100 {
			t.Errorf("networks = %+v", vm.Spec.Networks)
		}
		if vm.Spec.CloudInit == nil || vm.Spec.CloudInit.User != "admin" || len(vm.Spec.CloudInit.SSHKeys) != 1 {
			t.Errorf("cloudInit = %+v", vm.Spec.CloudInit)
		}
		if vm.Spec.CloudInit.Password != "" {
			t.Error("masked cipassword must not be read back into the spec")
		}
		if !vm.Spec.StartOnBoot || len(vm.Spec.Tags) != 2 {
			t.Errorf("onboot/tags = %v %v", vm.Spec.StartOnBoot, vm.Spec.Tags)
		}
		if vm.Status == nil || vm.Status.State != "running" || vm.Status.VMID != 100 {
			t.Errorf("status = %+v", vm.Status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		if _, err := h.Get(context.Background(), seededFake(), "ghost"); !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("Get(ghost) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("lxc guest is not a VirtualMachine", func(t *testing.T) {
		_, err := h.Get(context.Background(), seededFake(), "ct-1")
		if err == nil || !strings.Contains(err.Error(), "lxc") {
			t.Fatalf("Get(ct-1) error = %v, want kind mismatch mentioning lxc", err)
		}
	})
}

func TestHandlerDelete(t *testing.T) {
	h := NewHandler()
	f := seededFake()
	if err := h.Delete(context.Background(), f, "db"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(f.Deletes) != 1 || f.Deletes[0].VMID != 101 {
		t.Errorf("Deletes = %+v", f.Deletes)
	}
	if err := h.Delete(context.Background(), f, "ghost"); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("Delete(ghost) error = %v", err)
	}
}

func TestHandlerStartStop(t *testing.T) {
	h := NewHandler()
	f := seededFake()
	ctx := context.Background()

	if err := h.Start(ctx, f, "db"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(f.Starts) != 1 || f.Starts[0].VMID != 101 {
		t.Errorf("Starts = %+v", f.Starts)
	}
	if err := h.Stop(ctx, f, "web-server"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if len(f.Stops) != 1 || f.Stops[0].VMID != 100 {
		t.Errorf("Stops = %+v", f.Stops)
	}
}

func TestHandlerDescribe(t *testing.T) {
	h := NewHandler()
	var sb strings.Builder
	if err := h.Describe(context.Background(), seededFake(), "web-server", &sb); err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	out := sb.String()
	for _, want := range []string{"web-server", "100", "pve1", "running", "scsi0", "vmbr0", "admin", "prod"} {
		if !strings.Contains(out, want) {
			t.Errorf("Describe() missing %q:\n%s", want, out)
		}
	}
}

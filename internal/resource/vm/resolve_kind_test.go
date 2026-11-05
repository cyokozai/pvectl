package vm

import (
	"context"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/api/apitest"
	"github.com/cyokozai/pvectl/internal/resource"
)

// sameNameFake is seededFake plus LXC containers that share names with
// VirtualMachines: "web-server" exists as both a VM (100) and an LXC
// (201), and "new-vm" exists only as an LXC (202). ADR-010 §1: names
// are resolved per kind, so these containers must never get in the way
// of the VM handler.
func sameNameFake() *apitest.Fake {
	f := seededFake()
	f.AddGuest(api.GuestSummary{VMID: 201, Name: "web-server", Node: "pve2", Type: "lxc", Status: "running"},
		map[string]any{"hostname": "web-server", "cores": "1", "memory": "512"})
	f.AddGuest(api.GuestSummary{VMID: 202, Name: "new-vm", Node: "pve1", Type: "lxc", Status: "stopped"},
		map[string]any{"hostname": "new-vm", "cores": "1", "memory": "512"})
	return f
}

// assertLxcUntouched fails when any call reached one of the containers.
func assertLxcUntouched(t *testing.T, f *apitest.Fake) {
	t.Helper()
	isLxc := func(vmid int) bool { return vmid == 201 || vmid == 202 }
	for _, u := range f.Updates {
		if isLxc(u.Ref.VMID) {
			t.Errorf("config update reached lxc %d: %+v", u.Ref.VMID, u)
		}
	}
	for _, refs := range [][]api.GuestRef{f.Deletes, f.Starts, f.Stops} {
		for _, r := range refs {
			if isLxc(r.VMID) {
				t.Errorf("lifecycle call reached lxc %d", r.VMID)
			}
		}
	}
	for _, c := range f.Clones {
		if isLxc(c.Src.VMID) {
			t.Errorf("clone used lxc %d as its source", c.Src.VMID)
		}
	}
}

func TestApplyIgnoresSameNamedLinuxContainer(t *testing.T) {
	t.Run("existing VM is unchanged, not ambiguous", func(t *testing.T) {
		f := sameNameFake()
		res, err := apply(t, f, existingVMManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionUnchanged {
			t.Errorf("Action = %v, want unchanged", res.Action)
		}
		assertLxcUntouched(t, f)
	})

	t.Run("existing VM is updated in place", func(t *testing.T) {
		f := sameNameFake()
		doc := strings.Replace(existingVMManifest, "cores: 2", "cores: 4", 1)
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionConfigured {
			t.Errorf("Action = %v, want configured", res.Action)
		}
		if len(f.Updates) != 1 || f.Updates[0].Ref.VMID != 100 {
			t.Errorf("Updates = %+v, want one update of vmid 100", f.Updates)
		}
		assertLxcUntouched(t, f)
	})

	t.Run("existing VM without vmid in the manifest", func(t *testing.T) {
		f := sameNameFake()
		doc := strings.Replace(existingVMManifest, "  vmid: 100\n", "", 1)
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionUnchanged {
			t.Errorf("Action = %v, want unchanged", res.Action)
		}
		assertLxcUntouched(t, f)
	})

	t.Run("VM is created beside a container of the same name", func(t *testing.T) {
		f := sameNameFake()
		res, err := apply(t, f, newVMManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionCreated {
			t.Errorf("Action = %v, want created", res.Action)
		}
		if len(f.Creates) != 1 || f.Creates[0].VMID != 105 {
			t.Errorf("Creates = %+v, want one VM at vmid 105", f.Creates)
		}
		assertLxcUntouched(t, f)
	})

	t.Run("pinned vmid: a container holding the name is no conflict", func(t *testing.T) {
		f := sameNameFake()
		doc := strings.Replace(newVMManifest, "targetNode: pve1", "targetNode: pve1\n  vmid: 300", 1)
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Creates) != 1 || f.Creates[0].VMID != 300 {
			t.Errorf("Creates = %+v, want vmid 300", f.Creates)
		}
	})

	t.Run("pinned vmid: a VM holding the name still conflicts", func(t *testing.T) {
		f := sameNameFake()
		doc := strings.Replace(existingVMManifest, "vmid: 100", "vmid: 999", 1)
		_, err := apply(t, f, doc, resource.ApplyOptions{})
		if err == nil || !strings.Contains(err.Error(), "vmid 100") {
			t.Fatalf("Apply() error = %v, want a conflict naming vmid 100", err)
		}
		if strings.Contains(err.Error(), "201") {
			t.Errorf("error %q names the container, which is no conflict", err)
		}
		if len(f.Creates) != 0 {
			t.Errorf("Creates = %+v, want none", f.Creates)
		}
	})

	t.Run("clone source resolves to the VM, not the container", func(t *testing.T) {
		f := sameNameFake()
		doc := `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: cloned-vm
spec:
  targetNode: pve2
  clone: web-server
  resources:
    cpu:
      cores: 4
    memory: 8192
`
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Clones) != 1 || f.Clones[0].Src.VMID != 100 {
			t.Errorf("Clones = %+v, want vmid 100 as the source", f.Clones)
		}
		assertLxcUntouched(t, f)
	})

	t.Run("a container is no clone source", func(t *testing.T) {
		f := sameNameFake()
		doc := `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: cloned-vm
spec:
  targetNode: pve1
  clone: ct-1
  resources:
    cpu:
      cores: 1
    memory: 512
`
		_, err := apply(t, f, doc, resource.ApplyOptions{})
		if err == nil || !strings.Contains(err.Error(), "lxc") {
			t.Fatalf("Apply() error = %v, want a refusal naming the lxc guest", err)
		}
		if len(f.Clones) != 0 {
			t.Errorf("Clones = %+v, want none", f.Clones)
		}
	})
}

func TestDiffIgnoresSameNamedLinuxContainer(t *testing.T) {
	h := NewHandler()

	t.Run("existing VM", func(t *testing.T) {
		doc := strings.Replace(existingVMManifest, "cores: 2", "cores: 8", 1)
		res, err := h.Diff(context.Background(), sameNameFake(), manifest(t, doc))
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if len(res.Entries) != 1 || res.Entries[0].Key != "cores" || res.Entries[0].Current != "2" {
			t.Errorf("Diff = %+v, want only cores 2 -> 8 against vmid 100", res.Entries)
		}
	})

	t.Run("VM missing, container of that name present", func(t *testing.T) {
		res, err := h.Diff(context.Background(), sameNameFake(), manifest(t, newVMManifest))
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if res.Empty() {
			t.Fatal("Diff() is empty, want every desired key as an addition")
		}
		for _, e := range res.Entries {
			if e.Current != "" {
				t.Errorf("entry %+v has a Current value; it was read from the container", e)
			}
		}
	})
}

func TestLifecycleIgnoresSameNamedLinuxContainer(t *testing.T) {
	h := NewHandler()
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		obj, err := h.Get(ctx, sameNameFake(), "web-server")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got := obj.(*VM).Status.VMID; got != 100 {
			t.Errorf("Get() vmid = %d, want 100", got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		f := sameNameFake()
		if err := h.Delete(ctx, f, "web-server"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if len(f.Deletes) != 1 || f.Deletes[0].VMID != 100 {
			t.Errorf("Deletes = %+v, want vmid 100", f.Deletes)
		}
		assertLxcUntouched(t, f)
	})

	t.Run("start and stop", func(t *testing.T) {
		f := sameNameFake()
		if err := h.Start(ctx, f, "web-server"); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if err := h.Stop(ctx, f, "web-server"); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
		if len(f.Starts) != 1 || f.Starts[0].VMID != 100 || len(f.Stops) != 1 || f.Stops[0].VMID != 100 {
			t.Errorf("Starts = %+v, Stops = %+v, want vmid 100 each", f.Starts, f.Stops)
		}
		assertLxcUntouched(t, f)
	})
}

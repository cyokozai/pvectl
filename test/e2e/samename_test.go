package e2e

import (
	"os"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/test/pvefake"
)

// seedSameNamedContainer places an LXC container named like the VM the
// manifest from writeManifest declares ("e2e-vm"). ADR-010 §1: names are
// resolved per kind, so the container must not stop the VM's lifecycle.
func seedSameNamedContainer(e *env) {
	e.server.AddGuest(pvefake.Guest{
		VMID: 300, Name: "e2e-vm", Node: "pve2", Type: "lxc", Status: "running",
		Config: map[string]any{"hostname": "e2e-vm", "cores": "1", "memory": "512"},
	})
}

// assertContainerUntouched fails when any request reached the container
// or its record changed.
func assertContainerUntouched(t *testing.T, e *env) {
	t.Helper()
	for _, r := range e.server.Requests() {
		if strings.Contains(r, "/lxc/") {
			t.Errorf("request reached an lxc endpoint: %s", r)
		}
	}
	g := e.server.Guest(300)
	if g == nil {
		t.Fatal("the container is gone")
	}
	if g.Status != "running" || g.Node != "pve2" || g.Config["cores"] != "1" {
		t.Errorf("container changed: %+v", g)
	}
}

// TestVMLifecycleBesideSameNamedContainer walks apply(create) → diff →
// apply(unchanged) → apply(configured) → get → stop/start → delete for a
// VM while an LXC container of the same name exists.
func TestVMLifecycleBesideSameNamedContainer(t *testing.T) {
	e := newEnv(t)
	seedSameNamedContainer(e)
	v1 := writeManifest(t, 2, 1024)

	stdout, stderr, err := e.run(t, "apply", "-f", v1)
	if err != nil {
		t.Fatalf("apply(create): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/e2e-vm created") {
		t.Fatalf("stdout = %q", stdout)
	}
	if g := e.server.Guest(200); g == nil || g.Type != "qemu" {
		t.Fatalf("created guest = %+v, want a qemu guest at vmid 200", g)
	}

	if _, stderr, err = e.run(t, "diff", "-f", v1); err != nil {
		t.Fatalf("diff(unchanged): %v\nstderr: %s", err, stderr)
	}

	stdout, stderr, err = e.run(t, "apply", "-f", v1)
	if err != nil {
		t.Fatalf("apply(unchanged): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/e2e-vm unchanged") {
		t.Fatalf("stdout = %q", stdout)
	}

	// Without spec.vmid the VM is found by name alone, which is where a
	// kind-blind lookup sees two guests.
	unpinned := writeManifestWithout(t, writeManifest(t, 4, 2048), "  vmid: 200\n")
	stdout, stderr, err = e.run(t, "apply", "-f", unpinned)
	if err != nil {
		t.Fatalf("apply(configured, by name): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/e2e-vm configured") {
		t.Fatalf("stdout = %q", stdout)
	}
	if g := e.server.Guest(200); g.Config["cores"] != "4" {
		t.Errorf("vm cores = %v, want 4", g.Config["cores"])
	}

	stdout, stderr, err = e.run(t, "get", "vm", "e2e-vm", "-o", "yaml")
	if err != nil {
		t.Fatalf("get: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "vmid: 200") {
		t.Errorf("get -o yaml did not return vmid 200:\n%s", stdout)
	}

	for _, verb := range []string{"start", "stop"} {
		if _, stderr, err = e.run(t, verb, "vm", "e2e-vm"); err != nil {
			t.Fatalf("%s: %v\nstderr: %s", verb, err, stderr)
		}
	}
	if g := e.server.Guest(200); g.Status != "stopped" {
		t.Errorf("vm status = %q, want stopped after start/stop", g.Status)
	}

	if _, stderr, err = e.run(t, "delete", "vm", "e2e-vm"); err != nil {
		t.Fatalf("delete: %v\nstderr: %s", err, stderr)
	}
	if e.server.Guest(200) != nil {
		t.Error("the VM survived delete")
	}
	assertContainerUntouched(t, e)
}

// TestMigrateRefusesContainerUnderVMType: "migrate vm <container>" used
// to move the container. TYPE must now match the guest's kind.
func TestMigrateRefusesContainerUnderVMType(t *testing.T) {
	e := newEnv(t)
	e.server.AddGuest(pvefake.Guest{VMID: 310, Name: "ct-only", Node: "pve1", Type: "lxc", Status: "stopped"})

	_, _, err := e.run(t, "migrate", "vm", "ct-only", "--to", "pve2")
	if err == nil || !strings.Contains(err.Error(), "LinuxContainer") {
		t.Fatalf("migrate vm <container>: error = %v, want a refusal naming LinuxContainer", err)
	}
	if node := e.server.Guest(310).Node; node != "pve1" {
		t.Errorf("container moved to %s", node)
	}
	if e.server.SawRequest("POST", "/api2/json/nodes/pve1/lxc/310/migrate") {
		t.Error("the lxc migrate endpoint was called")
	}
}

// TestMigrateAndExecPickTheVMBesideSameNamedContainer: with a VM and a
// container sharing a name, TYPE vm reaches the VM instead of failing as
// ambiguous.
func TestMigrateAndExecPickTheVMBesideSameNamedContainer(t *testing.T) {
	e := newEnv(t)
	e.server.AddGuest(pvefake.Guest{VMID: 320, Name: "twin", Node: "pve1", Status: "running",
		Config: map[string]any{"name": "twin", "agent": "1"}})
	e.server.AddGuest(pvefake.Guest{VMID: 321, Name: "twin", Node: "pve1", Type: "lxc", Status: "stopped"})
	e.server.SetExec(pvefake.ExecScript{Stdout: "ok\n"})

	stdout, stderr, err := e.run(t, "exec", "vm", "twin", "--", "true")
	if err != nil {
		t.Fatalf("exec vm twin: %v\nstderr: %s", err, stderr)
	}
	if stdout != "ok\n" {
		t.Errorf("exec stdout = %q", stdout)
	}
	if !e.server.SawRequest("POST", "/api2/json/nodes/pve1/qemu/320/agent/exec") {
		t.Errorf("exec did not reach vmid 320; requests: %v", e.server.Requests())
	}

	if _, stderr, err = e.run(t, "migrate", "vm", "twin", "--to", "pve2", "--online"); err != nil {
		t.Fatalf("migrate vm twin: %v\nstderr: %s", err, stderr)
	}
	if e.server.Guest(320).Node != "pve2" || e.server.Guest(321).Node != "pve1" {
		t.Errorf("after migrate: vm on %s, container on %s; want vm on pve2 and container left on pve1",
			e.server.Guest(320).Node, e.server.Guest(321).Node)
	}
}

// writeManifestWithout rewrites path with cut removed and returns it.
func writeManifestWithout(t *testing.T, path, cut string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), cut) {
		t.Fatalf("manifest %s has no %q to remove", path, cut)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), cut, "", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

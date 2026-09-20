package e2e

import (
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/test/pvefake"
)

func TestMigrateThroughTheRealStack(t *testing.T) {
	e := newEnv(t)
	e.server.AddGuest(pvefake.Guest{VMID: 230, Name: "roamer", Node: "pve1", Status: "running"})

	stdout, stderr, err := e.run(t, "migrate", "vm", "roamer", "--to", "pve2", "--online")
	if err != nil {
		t.Fatalf("migrate: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/roamer migrated from pve1 to pve2") {
		t.Fatalf("stdout = %q", stdout)
	}
	if node := e.server.Guest(230).Node; node != "pve2" {
		t.Errorf("guest node = %q, want pve2", node)
	}
	if !e.server.SawRequest("POST", "/api2/json/nodes/pve1/qemu/230/migrate") {
		t.Errorf("the migrate endpoint was never called; requests: %v", e.server.Requests())
	}
}

// TestMigrateFailureSurfaces proves the migrate task's exit status
// becomes a command error, like every other task-backed verb.
func TestMigrateFailureSurfaces(t *testing.T) {
	e := newEnv(t)
	e.server.AddGuest(pvefake.Guest{VMID: 231, Name: "stuck", Node: "pve1", Status: "running"})
	e.server.FailNext("migrate", "migration aborted: no route to target")

	if _, _, err := e.run(t, "migrate", "vm", "stuck", "--to", "pve2"); err == nil {
		t.Fatal("migrate error = nil, want the task failure")
	}
}

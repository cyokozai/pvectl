package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
)

func TestMigrate(t *testing.T) {
	e := newEnv(t)

	if err := e.run(t, "migrate", "vm", "web-server", "--to", "pve2"); err != nil {
		t.Fatalf("migrate: %v\nstderr: %s", err, e.stderr.String())
	}
	if out := e.stdout.String(); !strings.Contains(out, "virtualmachine/web-server migrated from pve1 to pve2") {
		t.Errorf("stdout = %q", out)
	}
	if len(e.fake.Migrations) != 1 {
		t.Fatalf("MigrateGuest called %d times, want 1", len(e.fake.Migrations))
	}
	call := e.fake.Migrations[0]
	if call.Target != "pve2" || call.Online {
		t.Errorf("migrate call = %+v, want target pve2 and offline migration", call)
	}
}

func TestMigrateOnline(t *testing.T) {
	e := newEnv(t)

	if err := e.run(t, "migrate", "vm", "web-server", "--to", "pve2", "--online"); err != nil {
		t.Fatalf("migrate --online: %v", err)
	}
	if !e.fake.Migrations[0].Online {
		t.Error("migrate call did not request live migration")
	}
}

func TestMigrateToCurrentNodeIsANoOp(t *testing.T) {
	e := newEnv(t)

	if err := e.run(t, "migrate", "vm", "web-server", "--to", "pve1"); err != nil {
		t.Fatalf("migrate to the current node: %v", err)
	}
	if len(e.fake.Migrations) != 0 {
		t.Error("migrate called the API for a guest that is already there")
	}
	if out := e.stdout.String(); !strings.Contains(out, "already on node pve1") {
		t.Errorf("stdout = %q", out)
	}
}

func TestMigrateRequiresTargetNode(t *testing.T) {
	e := newEnv(t)
	if err := e.run(t, "migrate", "vm", "web-server"); err == nil {
		t.Fatal("migrate without --to: error = nil, want an error")
	}
}

func TestMigrateUnknownGuest(t *testing.T) {
	e := newEnv(t)
	err := e.run(t, "migrate", "vm", "nope", "--to", "pve2")
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("migrate of an unknown guest: error = %v, want ErrNotFound", err)
	}
}

func addContainer(e *testEnv, status string) {
	e.fake.AddGuest(api.GuestSummary{VMID: 110, Name: "ct-box", Node: "pve1", Type: "lxc", Status: status}, nil)
}

func TestMigrateStoppedLinuxContainer(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "stopped")

	if err := e.run(t, "migrate", "vm", "ct-box", "--to", "pve2"); err != nil {
		t.Fatalf("migrate of a stopped container: %v", err)
	}
	if len(e.fake.Migrations) != 1 {
		t.Fatalf("MigrateGuest called %d times, want 1", len(e.fake.Migrations))
	}
	if call := e.fake.Migrations[0]; call.Ref.Type != "lxc" || call.Online {
		t.Errorf("migrate call = %+v, want an offline lxc migration", call)
	}
}

// TestMigrateRunningLinuxContainerAsksToStop: a container cannot be
// live-migrated, and pvectl does not stop it on the user's behalf.
// The name resolves before the type matters, so the guard holds for an
// lxc reached under any TYPE (today "vm", later "ct").
func TestMigrateRunningLinuxContainerAsksToStop(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "running")

	err := e.run(t, "migrate", "vm", "ct-box", "--to", "pve2")
	if err == nil {
		t.Fatal("migrate of a running container: error = nil, want a refusal")
	}
	for _, want := range []string{"running", "pvectl stop vm ct-box"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(e.fake.Migrations) != 0 {
		t.Error("migrate called the API for a running container")
	}
}

func TestMigrateLinuxContainerRejectsOnline(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "stopped")

	err := e.run(t, "migrate", "vm", "ct-box", "--to", "pve2", "--online")
	if err == nil {
		t.Fatal("migrate --online of a container: error = nil, want a refusal")
	}
	for _, want := range []string{"--online", "pvectl stop vm ct-box"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(e.fake.Migrations) != 0 {
		t.Error("migrate called the API despite --online on a container")
	}
}

// TestMigrateLinuxContainerStatusLookupFailure: when the running state
// cannot be read, migrate must not guess "stopped" and go ahead.
func TestMigrateLinuxContainerStatusLookupFailure(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "stopped")
	e.fake.Err = map[string]error{"ListGuests": errors.New("boom")}

	if err := e.run(t, "migrate", "vm", "ct-box", "--to", "pve2"); err == nil {
		t.Fatal("migrate with an unreadable status: error = nil, want the lookup error")
	}
	if len(e.fake.Migrations) != 0 {
		t.Error("migrate called the API without knowing the container's state")
	}
}

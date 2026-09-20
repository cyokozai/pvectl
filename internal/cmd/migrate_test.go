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

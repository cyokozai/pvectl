package cmd

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
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

// containerHandler stands in for the LinuxContainer handler, which is
// registered in a later change. exec and migrate only need its kind and
// aliases to resolve TYPE "ct".
type containerHandler struct{}

func (containerHandler) GVK() runtime.GVK {
	return runtime.GVK{Group: "pve.io", Version: "v1alpha1", Kind: "LinuxContainer"}
}
func (containerHandler) Aliases() []string {
	return []string{"lxc", "ct", "linuxcontainer", "linuxcontainers"}
}
func (containerHandler) Columns(bool) []printer.Column { return nil }
func (containerHandler) Get(context.Context, api.Client, string) (printer.Object, error) {
	return nil, nil
}
func (containerHandler) List(context.Context, api.Client) ([]printer.Object, error) {
	return nil, nil
}
func (containerHandler) Describe(context.Context, api.Client, string, io.Writer) error { return nil }

// containerRegistry is the built-in registry plus LinuxContainer.
func containerRegistry() *resource.Registry {
	reg := DefaultRegistry()
	reg.Register(containerHandler{})
	return reg
}

func TestMigrateStoppedLinuxContainer(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "stopped")

	if err := e.runWith(t, containerRegistry(), "migrate", "ct", "ct-box", "--to", "pve2"); err != nil {
		t.Fatalf("migrate of a stopped container: %v", err)
	}
	if len(e.fake.Migrations) != 1 {
		t.Fatalf("MigrateGuest called %d times, want 1", len(e.fake.Migrations))
	}
	if call := e.fake.Migrations[0]; call.Ref.Type != "lxc" || call.Online {
		t.Errorf("migrate call = %+v, want an offline lxc migration", call)
	}
	if out := e.stdout.String(); !strings.Contains(out, "linuxcontainer/ct-box migrated from pve1 to pve2") {
		t.Errorf("stdout = %q", out)
	}
}

// TestMigrateRunningLinuxContainerAsksToStop: a container cannot be
// live-migrated, and pvectl does not stop it on the user's behalf. The
// hint names the container's own TYPE.
func TestMigrateRunningLinuxContainerAsksToStop(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "running")

	err := e.runWith(t, containerRegistry(), "migrate", "ct", "ct-box", "--to", "pve2")
	if err == nil {
		t.Fatal("migrate of a running container: error = nil, want a refusal")
	}
	for _, want := range []string{"running", "pvectl stop ct ct-box"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(e.fake.Migrations) != 0 {
		t.Error("migrate called the API for a running container")
	}
}

// TestMigrateStopHintUsesTheKindsTypeName: the hint names "ct" even when
// the user reached the container through another alias, so the
// suggestion is the same short form everywhere.
func TestMigrateStopHintUsesTheKindsTypeName(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "running")

	err := e.runWith(t, containerRegistry(), "migrate", "linuxcontainer", "ct-box", "--to", "pve2")
	if err == nil || !strings.Contains(err.Error(), "pvectl stop ct ct-box") {
		t.Fatalf("error = %v, want a stop hint with TYPE ct", err)
	}
}

func TestMigrateLinuxContainerRejectsOnline(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "stopped")

	err := e.runWith(t, containerRegistry(), "migrate", "ct", "ct-box", "--to", "pve2", "--online")
	if err == nil {
		t.Fatal("migrate --online of a container: error = nil, want a refusal")
	}
	for _, want := range []string{"--online", "pvectl stop ct ct-box"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(e.fake.Migrations) != 0 {
		t.Error("migrate called the API despite --online on a container")
	}
}

// TestMigrateLinuxContainerStatusLookupFailure: when the guest list
// (which carries the running state) cannot be read, migrate must not
// guess "stopped" and go ahead.
func TestMigrateLinuxContainerStatusLookupFailure(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "stopped")
	e.fake.Err = map[string]error{"ListGuests": errors.New("boom")}

	if err := e.runWith(t, containerRegistry(), "migrate", "ct", "ct-box", "--to", "pve2"); err == nil {
		t.Fatal("migrate with an unreadable status: error = nil, want the lookup error")
	}
	if len(e.fake.Migrations) != 0 {
		t.Error("migrate called the API without knowing the container's state")
	}
}

// TestMigrateRejectsTypeMismatch: until names were resolved per kind,
// "migrate vm <container>" moved the container. TYPE now has to match
// the guest's kind, and the error names the right one.
func TestMigrateRejectsTypeMismatch(t *testing.T) {
	t.Run("vm naming a container", func(t *testing.T) {
		e := newEnv(t)
		addContainer(e, "stopped")
		err := e.runWith(t, containerRegistry(), "migrate", "vm", "ct-box", "--to", "pve2")
		if !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("migrate vm <container>: error = %v, want ErrNotFound", err)
		}
		for _, want := range []string{"virtualmachine/ct-box", "LinuxContainer", `TYPE "ct"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
		if len(e.fake.Migrations) != 0 {
			t.Error("migrate moved a container reached as TYPE vm")
		}
	})

	t.Run("vm naming a container, container kind not built in", func(t *testing.T) {
		e := newEnv(t)
		addContainer(e, "stopped")
		err := e.run(t, "migrate", "vm", "ct-box", "--to", "pve2")
		if !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("migrate vm <container>: error = %v, want ErrNotFound", err)
		}
		if !strings.Contains(err.Error(), "LinuxContainer") {
			t.Errorf("error %q does not name the guest's kind", err)
		}
		if len(e.fake.Migrations) != 0 {
			t.Error("migrate moved a container reached as TYPE vm")
		}
	})

	t.Run("ct naming a VM", func(t *testing.T) {
		e := newEnv(t)
		err := e.runWith(t, containerRegistry(), "migrate", "ct", "web-server", "--to", "pve2")
		if !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("migrate ct <VM>: error = %v, want ErrNotFound", err)
		}
		for _, want := range []string{"linuxcontainer/web-server", "VirtualMachine", `TYPE "vm"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
		if len(e.fake.Migrations) != 0 {
			t.Error("migrate moved a VM reached as TYPE ct")
		}
	})

	t.Run("not a guest kind", func(t *testing.T) {
		e := newEnv(t)
		reg := DefaultRegistry()
		reg.Register(readOnlyHandler{})
		err := e.runWith(t, reg, "migrate", "node", "pve1", "--to", "pve2")
		if err == nil || !strings.Contains(err.Error(), "Node") {
			t.Fatalf("migrate node: error = %v, want a refusal naming Node", err)
		}
		if len(e.fake.Migrations) != 0 {
			t.Error("migrate called the API for a non-guest kind")
		}
	})
}

// TestMigratePicksTheGuestOfTheTypedKind: a VM and a container sharing a
// name are told apart by TYPE instead of failing as ambiguous.
func TestMigratePicksTheGuestOfTheTypedKind(t *testing.T) {
	seed := func(t *testing.T) *testEnv {
		e := newEnv(t)
		e.fake.AddGuest(api.GuestSummary{VMID: 120, Name: "web-server", Node: "pve1", Type: "lxc", Status: "stopped"}, nil)
		return e
	}

	t.Run("vm", func(t *testing.T) {
		e := seed(t)
		if err := e.runWith(t, containerRegistry(), "migrate", "vm", "web-server", "--to", "pve2"); err != nil {
			t.Fatalf("migrate vm web-server: %v", err)
		}
		if len(e.fake.Migrations) != 1 || e.fake.Migrations[0].Ref.VMID != 100 {
			t.Errorf("Migrations = %+v, want vmid 100", e.fake.Migrations)
		}
	})

	t.Run("ct", func(t *testing.T) {
		e := seed(t)
		if err := e.runWith(t, containerRegistry(), "migrate", "ct", "web-server", "--to", "pve2"); err != nil {
			t.Fatalf("migrate ct web-server: %v", err)
		}
		if len(e.fake.Migrations) != 1 || e.fake.Migrations[0].Ref.VMID != 120 {
			t.Errorf("Migrations = %+v, want vmid 120", e.fake.Migrations)
		}
	})

	t.Run("two VMs with the name stay ambiguous", func(t *testing.T) {
		e := seed(t)
		e.fake.AddGuest(api.GuestSummary{VMID: 130, Name: "web-server", Node: "pve2", Type: "qemu", Status: "stopped"}, nil)
		err := e.runWith(t, containerRegistry(), "migrate", "vm", "web-server", "--to", "pve2")
		if !errors.Is(err, api.ErrAmbiguousName) {
			t.Fatalf("migrate with two VMs named alike: error = %v, want ErrAmbiguousName", err)
		}
		if len(e.fake.Migrations) != 0 {
			t.Error("migrate guessed between two VMs")
		}
	})
}

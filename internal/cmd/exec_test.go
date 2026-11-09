package cmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/api/apitest"
)

func TestExecPassesCommandThroughAndSucceeds(t *testing.T) {
	e := newEnv(t)
	e.fake.Exec = apitest.ExecScript{Stdout: "active\n"}

	if err := e.run(t, "exec", "vm", "web-server", "--", "systemctl", "is-active", "nginx"); err != nil {
		t.Fatalf("exec: %v\nstderr: %s", err, e.stderr.String())
	}
	if out := e.stdout.String(); out != "active\n" {
		t.Errorf("stdout = %q, want the guest's stdout verbatim", out)
	}
	if len(e.fake.Execs) != 1 {
		t.Fatalf("GuestExec called %d times, want 1", len(e.fake.Execs))
	}
	call := e.fake.Execs[0]
	if want := []string{"systemctl", "is-active", "nginx"}; !reflect.DeepEqual(call.Command, want) {
		t.Errorf("command = %q, want %q", call.Command, want)
	}
	if call.Ref.VMID != 100 {
		t.Errorf("exec ran against vmid %d, want 100", call.Ref.VMID)
	}
}

// TestExecReportsGuestExitCode is the property that lets pvectl exec
// stand in for a local command in a shell pipeline.
func TestExecReportsGuestExitCode(t *testing.T) {
	e := newEnv(t)
	e.fake.Exec = apitest.ExecScript{ExitCode: 3, Stderr: "nginx: not found\n"}

	err := e.run(t, "exec", "vm", "web-server", "--", "systemctl", "is-active", "nginx")

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("exec error = %v, want an ExitError", err)
	}
	if exitErr.Code != 3 {
		t.Errorf("exit code = %d, want the guest's 3", exitErr.Code)
	}
	if got := e.stderr.String(); got != "nginx: not found\n" {
		t.Errorf("stderr = %q, want the guest's stderr verbatim", got)
	}
}

func TestExecReportsSignalledCommand(t *testing.T) {
	e := newEnv(t)
	e.fake.Exec = apitest.ExecScript{Signal: 9}

	err := e.run(t, "exec", "vm", "web-server", "--", "sleep", "100")

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("exec error = %v, want an ExitError", err)
	}
	if exitErr.Code != 137 {
		t.Errorf("exit code = %d, want 128+9", exitErr.Code)
	}
}

// TestExecTimeoutIsItsOwnFlag proves --exec-timeout bounds the guest
// command independently of --timeout, which bounds Proxmox task waits.
func TestExecTimeoutIsItsOwnFlag(t *testing.T) {
	e := newEnv(t)
	e.fake.Exec = apitest.ExecScript{NeverExits: true}

	err := e.run(t, "--timeout", "30m", "exec", "vm", "web-server",
		"--exec-timeout", "50ms", "--", "tail", "-f", "/dev/null")

	if !errors.Is(err, api.ErrExecTimeout) {
		t.Fatalf("exec error = %v, want ErrExecTimeout", err)
	}
	if !strings.Contains(err.Error(), "--exec-timeout") {
		t.Errorf("error %q does not name the flag that widens the wait", err)
	}
}

func TestExecFlagsAfterDashGoToTheGuest(t *testing.T) {
	e := newEnv(t)
	e.fake.Exec = apitest.ExecScript{}

	// "-o" is a pvectl flag; after "--" it must reach the guest untouched.
	if err := e.run(t, "exec", "vm", "web-server", "--", "ls", "-o", "--color=never"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if want := []string{"ls", "-o", "--color=never"}; !reflect.DeepEqual(e.fake.Execs[0].Command, want) {
		t.Errorf("command = %q, want %q", e.fake.Execs[0].Command, want)
	}
}

func TestExecRequiresDoubleDash(t *testing.T) {
	e := newEnv(t)
	err := e.run(t, "exec", "vm", "web-server", "uname")
	if err == nil {
		t.Fatal("exec without \"--\": error = nil, want an explanation")
	}
	if !strings.Contains(err.Error(), "--") {
		t.Errorf("error %q does not mention the separator", err)
	}
	if len(e.fake.Execs) != 0 {
		t.Error("exec reached the API despite the bad invocation")
	}
}

// TestExecRejectsLinuxContainerWithReason names the actual limitation:
// Proxmox has no REST exec for containers, only the node-local pct exec.
// The container is reached under its own TYPE ("ct").
func TestExecRejectsLinuxContainerWithReason(t *testing.T) {
	e := newEnv(t)
	addContainer(e, "running")

	err := e.runWith(t, containerRegistry(), "exec", "ct", "ct-box", "--", "uname")
	if err == nil {
		t.Fatal("exec on an lxc guest: error = nil, want a refusal")
	}
	for _, want := range []string{"LXC", "no exec in the REST API", "pct exec", "ssh"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(e.fake.Execs) != 0 {
		t.Error("exec reached the guest agent API for a container")
	}
}

// TestExecRejectsTypeMismatch: TYPE selects the kind, and a guest of
// another kind with that name is not a match. Before names were resolved
// per kind, "exec ct <VM name>" would have run the command inside the VM.
func TestExecRejectsTypeMismatch(t *testing.T) {
	t.Run("ct naming a VM", func(t *testing.T) {
		e := newEnv(t)
		err := e.runWith(t, containerRegistry(), "exec", "ct", "web-server", "--", "uname")
		if !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("exec ct <VM>: error = %v, want ErrNotFound", err)
		}
		for _, want := range []string{"linuxcontainer/web-server", "VirtualMachine", `TYPE "vm"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
		if len(e.fake.Execs) != 0 {
			t.Error("exec ran inside the VM although TYPE was ct")
		}
	})

	t.Run("vm naming a container", func(t *testing.T) {
		e := newEnv(t)
		addContainer(e, "running")
		err := e.runWith(t, containerRegistry(), "exec", "vm", "ct-box", "--", "uname")
		if !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("exec vm <container>: error = %v, want ErrNotFound", err)
		}
		for _, want := range []string{"virtualmachine/ct-box", "LinuxContainer", `TYPE "ct"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
		if len(e.fake.Execs) != 0 {
			t.Error("exec reached the guest agent API for a container")
		}
	})

	t.Run("container kind not built in", func(t *testing.T) {
		e := newEnv(t)
		addContainer(e, "running")
		err := e.runWith(t, vmOnlyRegistry(), "exec", "vm", "ct-box", "--", "uname")
		if !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("exec vm <container>: error = %v, want ErrNotFound", err)
		}
		if !strings.Contains(err.Error(), "LinuxContainer") {
			t.Errorf("error %q does not name the guest's kind", err)
		}
		if strings.Contains(err.Error(), `TYPE "ct"`) {
			t.Errorf("error %q points at a TYPE this registry does not have", err)
		}
		if len(e.fake.Execs) != 0 {
			t.Error("exec reached the guest agent API for a container")
		}
	})
}

// TestExecPicksTheGuestOfTheTypedKind: a VM and a container may share a
// name (ADR-010 §1); TYPE decides which one is meant, so the shared
// name is not ambiguous.
func TestExecPicksTheGuestOfTheTypedKind(t *testing.T) {
	e := newEnv(t)
	e.fake.AddGuest(api.GuestSummary{VMID: 120, Name: "web-server", Node: "pve2", Type: "lxc", Status: "running"}, nil)
	e.fake.Exec = apitest.ExecScript{}

	if err := e.runWith(t, containerRegistry(), "exec", "vm", "web-server", "--", "uname"); err != nil {
		t.Fatalf("exec vm web-server: %v", err)
	}
	if len(e.fake.Execs) != 1 || e.fake.Execs[0].Ref.VMID != 100 {
		t.Fatalf("Execs = %+v, want one run on vmid 100", e.fake.Execs)
	}

	err := e.runWith(t, containerRegistry(), "exec", "ct", "web-server", "--", "uname")
	if err == nil || !strings.Contains(err.Error(), "pct exec 120") {
		t.Fatalf("exec ct web-server: error = %v, want the container refusal for vmid 120", err)
	}
	if len(e.fake.Execs) != 1 {
		t.Errorf("Execs = %+v, the container was reached through the agent", e.fake.Execs)
	}
}

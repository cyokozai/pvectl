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

func TestExecRejectsNonQemuGuest(t *testing.T) {
	e := newEnv(t)
	e.fake.AddGuest(api.GuestSummary{VMID: 110, Name: "ct-box", Node: "pve1", Type: "lxc"}, nil)

	err := e.run(t, "exec", "vm", "ct-box", "--", "uname")
	if err == nil || !strings.Contains(err.Error(), "guest agent") {
		t.Fatalf("exec on an lxc guest: error = %v, want a guest-agent explanation", err)
	}
}

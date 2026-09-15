package e2e

import (
	"errors"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/cmd"
	"github.com/cyokozai/pvectl/test/pvefake"
)

func TestExecThroughTheRealStack(t *testing.T) {
	e := newEnv(t)
	e.server.AddGuest(pvefake.Guest{
		VMID: 220, Name: "agent-vm", Node: "pve1", Status: "running",
		Config: map[string]any{"name": "agent-vm", "agent": "1"},
	})
	e.server.SetExec(pvefake.ExecScript{ExitCode: 7, Stdout: "up 3 days\n", PollsBeforeExit: 2})

	stdout, _, err := e.run(t, "exec", "vm", "agent-vm", "--exec-timeout", "10s", "--", "uptime", "-p")

	var exitErr *cmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 7 {
		t.Fatalf("exec error = %v, want exit code 7 from the guest", err)
	}
	if stdout != "up 3 days\n" {
		t.Errorf("stdout = %q, want the guest's stdout", stdout)
	}
	commands := e.server.ExecCommands()
	if len(commands) != 1 || strings.Join(commands[0], " ") != "uptime -p" {
		t.Errorf("agent exec received %q, want one \"uptime -p\"", commands)
	}
	if !e.server.SawRequest("POST", "/api2/json/nodes/pve1/qemu/220/agent/exec") {
		t.Errorf("agent/exec was never called; requests: %v", e.server.Requests())
	}
	// PollsBeforeExit: 2 means the client had to come back for the result
	// rather than reading it off the initial POST.
	polls := 0
	for _, req := range e.server.Requests() {
		if req == "GET /api2/json/nodes/pve1/qemu/220/agent/exec-status" {
			polls++
		}
	}
	if polls < 3 {
		t.Errorf("agent/exec-status polled %d times, want the command to be waited on", polls)
	}
}

func TestExecWithoutGuestAgentExplainsItself(t *testing.T) {
	e := newEnv(t)
	e.server.AddGuest(pvefake.Guest{VMID: 221, Name: "bare-vm", Node: "pve1", Status: "running"})
	e.server.SetAgentUnavailable("QEMU guest agent is not running")

	_, _, err := e.run(t, "exec", "vm", "bare-vm", "--", "uname", "-a")
	if err == nil {
		t.Fatal("exec without an agent: error = nil")
	}
	if !strings.Contains(err.Error(), "agent=1") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

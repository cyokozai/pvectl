package api

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/pvectl/test/pvefake"
)

// fastExec keeps the polling loop quick enough for a unit test while
// still exercising more than one round trip.
var fastExec = ExecOptions{Timeout: 5 * time.Second, PollInterval: time.Millisecond}

func TestExecWaitReturnsGuestOutputAndExitCode(t *testing.T) {
	s := newTestServer(t)
	s.SetExec(pvefake.ExecScript{ExitCode: 42, Stdout: "hello\n", Stderr: "oops\n"})
	c := newTestClient(t, s)

	status, err := ExecWait(context.Background(), c, testRef, []string{"/bin/sh", "-c", "exit 42"}, fastExec)
	if err != nil {
		t.Fatalf("ExecWait() error = %v", err)
	}
	if !status.Exited {
		t.Error("status.Exited = false, want true")
	}
	if status.ExitCode != 42 {
		t.Errorf("status.ExitCode = %d, want 42", status.ExitCode)
	}
	if status.Stdout != "hello\n" || status.Stderr != "oops\n" {
		t.Errorf("status streams = %q / %q", status.Stdout, status.Stderr)
	}

	// The endpoint must actually have been reached, with the command
	// passed through as separate arguments.
	commands := s.ExecCommands()
	if len(commands) != 1 {
		t.Fatalf("agent exec called %d times, want 1", len(commands))
	}
	if want := []string{"/bin/sh", "-c", "exit 42"}; !reflect.DeepEqual(commands[0], want) {
		t.Errorf("command = %q, want %q", commands[0], want)
	}
}

func TestExecWaitPollsUntilExit(t *testing.T) {
	s := newTestServer(t)
	s.SetExec(pvefake.ExecScript{PollsBeforeExit: 3, Stdout: "done\n"})
	c := newTestClient(t, s)

	status, err := ExecWait(context.Background(), c, testRef, []string{"sleep", "1"}, fastExec)
	if err != nil {
		t.Fatalf("ExecWait() error = %v", err)
	}
	if !status.Exited || status.Stdout != "done\n" {
		t.Errorf("status = %+v, want an exited command with output", status)
	}
}

// execTimeoutSlack is the upper bound the timeout tests hold ExecWait
// to. The bound they ask for is 150-200ms and a fixed ExecWait returns
// within a few milliseconds of it; 2s leaves a loaded CI runner room to
// be slow without letting a regression through, because the failure
// this guards against — the SDK sleeping 1+2+3 seconds between retries
// without looking at the context — costs six seconds, not two.
const execTimeoutSlack = 2 * time.Second

// TestExecWaitTimeout is the case the guest agent cannot resolve on its
// own: the command never finishes, so the client has to give up.
func TestExecWaitTimeout(t *testing.T) {
	s := newTestServer(t)
	s.SetExec(pvefake.ExecScript{NeverExits: true})
	c := newTestClient(t, s)

	start := time.Now()
	_, err := ExecWait(context.Background(), c, testRef, []string{"tail", "-f", "/dev/null"},
		ExecOptions{Timeout: 150 * time.Millisecond, PollInterval: 10 * time.Millisecond})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrExecTimeout) {
		t.Fatalf("ExecWait() error = %v, want ErrExecTimeout", err)
	}
	if !strings.Contains(err.Error(), "--exec-timeout") {
		t.Errorf("error %q does not point at --exec-timeout", err)
	}
	if elapsed > execTimeoutSlack {
		t.Errorf("ExecWait() waited %s, want it to give up near the 150ms bound", elapsed)
	}
}

// TestExecWaitTimeoutWhileStatusFails checks the bound on the path that
// broke it. exec-status answers with a plain-text HTTP 500, which
// proxmox-api-go classifies as retryable: GetJsonRetryable then sleeps
// 1+2+3 seconds across its three attempts and never consults the
// context, so a 200ms --exec-timeout used to take six seconds. The
// polling read must therefore not retry, and must be abandoned the
// moment the deadline passes.
func TestExecWaitTimeoutWhileStatusFails(t *testing.T) {
	s := newTestServer(t)
	s.SetExec(pvefake.ExecScript{NeverExits: true, StatusHTTPError: "communication failure"})
	c := newTestClient(t, s)

	start := time.Now()
	_, err := ExecWait(context.Background(), c, testRef, []string{"tail", "-f", "/dev/null"},
		ExecOptions{Timeout: 200 * time.Millisecond, PollInterval: 10 * time.Millisecond})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrExecTimeout) {
		t.Fatalf("ExecWait() error = %v, want ErrExecTimeout", err)
	}
	if elapsed > execTimeoutSlack {
		t.Errorf("ExecWait() waited %s past a 200ms bound; the SDK retry sleeps are back in the path", elapsed)
	}
	// The read must have been attempted, not skipped.
	if !s.SawRequest("GET", "/api2/json/nodes/pve1/qemu/100/agent/exec-status") {
		t.Error("exec-status was never polled")
	}
}

func TestExecWaitHonorsCallerCancellation(t *testing.T) {
	s := newTestServer(t)
	s.SetExec(pvefake.ExecScript{NeverExits: true})
	c := newTestClient(t, s)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := ExecWait(ctx, c, testRef, []string{"tail", "-f", "/dev/null"},
		ExecOptions{Timeout: time.Minute, PollInterval: time.Millisecond})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecWait() error = %v, want context.Canceled", err)
	}
}

func TestExecWaitRejectsEmptyCommand(t *testing.T) {
	c := newTestClient(t, newTestServer(t))
	if _, err := ExecWait(context.Background(), c, testRef, nil, fastExec); err == nil {
		t.Fatal("ExecWait() with no command: error = nil, want an error")
	}
}

// TestGuestExecWithoutAgent covers the message a user sees most often:
// the VM has no guest agent, and the raw API error explains nothing.
func TestGuestExecWithoutAgent(t *testing.T) {
	s := newTestServer(t)
	s.SetAgentUnavailable("QEMU guest agent is not running")
	c := newTestClient(t, s)

	_, err := c.GuestExec(context.Background(), testRef, []string{"uname", "-a"})
	if !errors.Is(err, ErrGuestAgentUnavailable) {
		t.Fatalf("GuestExec() error = %v, want ErrGuestAgentUnavailable", err)
	}
	if !strings.Contains(err.Error(), "agent=1") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

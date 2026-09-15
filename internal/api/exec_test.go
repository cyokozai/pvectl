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
	if elapsed > 3*time.Second {
		t.Errorf("ExecWait() waited %s, want it to give up near the 150ms bound", elapsed)
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

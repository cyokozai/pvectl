package api

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Defaults for ExecWait. The exec window is deliberately much shorter
// than Options.TaskTimeout: waiting on a Proxmox task and waiting on a
// command someone typed are different kinds of waiting, and inheriting
// the 5 minute task bound would make a hung command feel like a hang.
const (
	// DefaultExecTimeout bounds how long ExecWait waits for the guest
	// command to finish.
	DefaultExecTimeout = 60 * time.Second
	// DefaultExecPollInterval is the gap between exec-status reads.
	DefaultExecPollInterval = 250 * time.Millisecond
)

// ExecOptions tune one ExecWait call. Zero values mean the defaults.
type ExecOptions struct {
	Timeout      time.Duration
	PollInterval time.Duration
}

// ExecWait runs command inside the guest and blocks until the guest
// agent reports it finished, then returns the final status. It is a
// package-level helper rather than a Client method so the polling loop
// is tested once, against any Client implementation.
//
// A timeout returns an error matching ErrExecTimeout; the command keeps
// running inside the guest, since the agent offers no way to cancel it.
func ExecWait(ctx context.Context, c Client, ref *GuestRef, command []string, opts ExecOptions) (*ExecStatus, error) {
	if len(command) == 0 {
		return nil, errors.New("no command to execute")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultExecTimeout
	}
	interval := opts.PollInterval
	if interval <= 0 {
		interval = DefaultExecPollInterval
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pid, err := c.GuestExec(ctx, ref, command)
	if err != nil {
		return nil, err
	}

	// Fires immediately for the first read, then every interval.
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, execWaitCtxErr(ctx, ref, pid, timeout)
		case <-timer.C:
		}

		status, err := c.GuestExecStatus(ctx, ref, pid)
		if err != nil {
			if ctx.Err() != nil {
				return nil, execWaitCtxErr(ctx, ref, pid, timeout)
			}
			return nil, err
		}
		if status.Exited {
			return status, nil
		}
		timer.Reset(interval)
	}
}

// execWaitCtxErr turns a finished context into the error the user sees:
// a timeout names the knob that widens it, a cancellation stays a
// cancellation.
func execWaitCtxErr(ctx context.Context, ref *GuestRef, pid int, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: pid %d is still running in vm %d after %s; raise --exec-timeout to wait longer",
			ErrExecTimeout, pid, ref.VMID, timeout)
	}
	return ctx.Err()
}

// rxAgentUnavailable matches the ways Proxmox VE reports that the guest
// agent could not be reached, so the user gets an actionable message
// instead of a bare HTTP error.
var rxAgentUnavailable = regexp.MustCompile(`(?i)guest agent|guest-agent|\bqga\b|not running|no such file or directory`)

// agentError wraps a guest-agent API failure, tagging the ones the user
// can act on with ErrGuestAgentUnavailable.
func agentError(ref *GuestRef, what string, err error) error {
	if rxAgentUnavailable.MatchString(err.Error()) {
		return fmt.Errorf("%w on vm %d: set agent=1 in the VM config and make sure qemu-guest-agent is installed and running in the guest (%s: %w)",
			ErrGuestAgentUnavailable, ref.VMID, what, err)
	}
	return fmt.Errorf("%s on vm %d: %w", what, ref.VMID, err)
}

// parseExecStatus reads the loosely typed /agent/exec-status payload.
// Proxmox sends booleans as 0/1 numbers and exit codes as numbers, but
// JSON decoding hands both over as float64.
func parseExecStatus(data map[string]any) *ExecStatus {
	status := &ExecStatus{
		Exited:       asBool(data["exited"]),
		Stdout:       asString(data["out-data"]),
		Stderr:       asString(data["err-data"]),
		OutTruncated: asBool(data["out-truncated"]),
		ErrTruncated: asBool(data["err-truncated"]),
	}
	if v, ok := data["exitcode"]; ok {
		status.ExitCode = asInt(v)
	}
	if v, ok := data["signal"]; ok {
		status.Signal = asInt(v)
		status.Signaled = true
	}
	return status
}

// asInt accepts the number shapes a JSON body can carry.
func asInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

// asBool treats Proxmox's 0/1 numbers and strings as booleans.
func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x != "" && x != "0"
	}
	return false
}

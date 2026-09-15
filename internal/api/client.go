// Package api wraps the Telmate proxmox-api-go SDK behind a narrow
// interface owned by pvectl. Nothing outside this package imports the
// SDK, so upstream churn (the SDK has no semver releases) is absorbed
// here. All mutating calls block until the underlying Proxmox task
// (UPID) completes and surface its exit status as an error.
package api

import (
	"context"
	"errors"
	"time"

	"github.com/cyokozai/pvectl/internal/config"
)

// Sentinel errors for callers to match with errors.Is.
var (
	// ErrNotFound means no guest matched the name or vmid.
	ErrNotFound = errors.New("not found")
	// ErrAmbiguousName means several guests share the requested name;
	// the caller should identify the guest by vmid instead.
	ErrAmbiguousName = errors.New("ambiguous name")
	// ErrGuestAgentUnavailable means the QEMU guest agent did not
	// answer: it is switched off in the VM config, not installed in the
	// guest, or not running.
	ErrGuestAgentUnavailable = errors.New("qemu guest agent unavailable")
	// ErrExecTimeout means a guest command did not finish inside the
	// allotted window. The command keeps running inside the guest.
	ErrExecTimeout = errors.New("guest command did not finish in time")
)

// GuestSummary is one row of /cluster/resources?type=vm.
type GuestSummary struct {
	VMID   int
	Name   string
	Node   string
	Type   string // qemu | lxc
	Status string
	Pool   string
	Tags   []string
	CPU    float64 // current usage fraction
	MaxCPU int
	Mem    uint64
	MaxMem uint64
	Uptime int64
}

// GuestRef identifies a guest precisely enough to build API paths.
type GuestRef struct {
	VMID int
	Node string
	Type string // qemu | lxc
}

// ExecStatus is one sample of /agent/exec-status: the guest agent runs
// commands asynchronously, so a command is observed through repeated
// reads until Exited flips.
type ExecStatus struct {
	// Exited reports whether the guest-side command has finished. Every
	// other field is only meaningful once it is true.
	Exited bool
	// ExitCode is the guest-side exit status of a normally terminated
	// command.
	ExitCode int
	// Signal is the signal that killed the command; Signaled says
	// whether the agent reported one instead of an exit code.
	Signal   int
	Signaled bool

	Stdout string
	Stderr string
	// OutTruncated / ErrTruncated are set when the guest agent's capture
	// buffer overflowed and the streams above are incomplete.
	OutTruncated bool
	ErrTruncated bool
}

// Client is pvectl's seam to the Proxmox VE API. Resource handlers are
// tested against a fake implementation of this interface.
type Client interface {
	// ListGuests returns all qemu VMs and lxc containers in the cluster.
	ListGuests(ctx context.Context) ([]GuestSummary, error)
	// FindGuest resolves a guest by name. Returns ErrNotFound or
	// ErrAmbiguousName when the name matches zero or several guests.
	FindGuest(ctx context.Context, name string) (*GuestRef, error)
	// GuestByID resolves a guest by vmid. Returns ErrNotFound.
	GuestByID(ctx context.Context, vmid int) (*GuestRef, error)

	// QemuConfig returns the raw current config of a qemu VM.
	QemuConfig(ctx context.Context, ref *GuestRef) (map[string]any, error)
	// CreateQemu creates a VM from flat Proxmox params and waits for the task.
	CreateQemu(ctx context.Context, node string, vmid int, params map[string]any) error
	// UpdateQemuConfig PUTs the given keys to the VM config and waits.
	UpdateQemuConfig(ctx context.Context, ref *GuestRef, params map[string]any) error
	// CloneQemu clones src; params take the raw clone endpoint keys
	// (newid, name, target, full, pool, description) and waits.
	CloneQemu(ctx context.Context, src *GuestRef, params map[string]any) error

	// DeleteGuest removes a guest and waits for the task.
	DeleteGuest(ctx context.Context, ref *GuestRef) error
	// StartGuest starts a guest and waits for the task.
	StartGuest(ctx context.Context, ref *GuestRef) error
	// StopGuest force-stops a guest and waits for the task.
	StopGuest(ctx context.Context, ref *GuestRef) error

	// MigrateGuest moves a guest to the target node and waits for the
	// task. online selects live migration for a running guest.
	MigrateGuest(ctx context.Context, ref *GuestRef, target string, online bool) error

	// GuestExec starts a command inside the guest through the QEMU guest
	// agent and returns the guest-side pid. It does not wait: the API is
	// asynchronous, so callers poll GuestExecStatus (or call ExecWait,
	// which does both).
	GuestExec(ctx context.Context, ref *GuestRef, command []string) (int, error)
	// GuestExecStatus samples a command previously started by GuestExec.
	GuestExecStatus(ctx context.Context, ref *GuestRef, pid int) (*ExecStatus, error)

	// NextID asks the cluster for the next free vmid.
	NextID(ctx context.Context) (int, error)

	// Raw exposes path-level API access for resources this interface
	// does not model yet. It reuses the same session and task waiting.
	Raw() RawClient
}

// RawClient is the escape hatch for unmodeled endpoints (M3/M4).
type RawClient interface {
	Get(ctx context.Context, path string) (map[string]any, error)
	PostTask(ctx context.Context, path string, params map[string]any) error
	PutTask(ctx context.Context, path string, params map[string]any) error
	DeleteTask(ctx context.Context, path string) error
}

// Options tune the connection.
type Options struct {
	// TaskTimeout bounds how long mutating calls wait for task
	// completion. Zero means 5 minutes.
	TaskTimeout time.Duration
	Debug       bool
}

// DefaultTaskTimeout is used when Options.TaskTimeout is zero.
const DefaultTaskTimeout = 5 * time.Minute

// New connects to the node with the user's credentials and returns the
// Telmate-backed Client. Password users are logged in eagerly so
// credential problems surface here.
func New(ctx context.Context, node config.Node, user config.User, opts Options) (Client, error) {
	return newTelmateClient(ctx, node, user, opts)
}

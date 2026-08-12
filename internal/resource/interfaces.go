// Package resource defines the handler contract every Proxmox VE
// resource kind implements, and the registry the generic verbs
// (get/describe/apply/diff/delete/...) dispatch through. Adding a new
// kind to pvectl means implementing Handler and registering it — the
// verb commands need no changes.
package resource

import (
	"context"
	"io"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/diff"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/runtime"
)

// DryRunMode selects how much of an apply actually happens.
type DryRunMode string

// Dry-run modes, mirroring kubectl.
const (
	DryRunNone   DryRunMode = ""       // mutate normally
	DryRunClient DryRunMode = "client" // validate/convert only, no API calls
	DryRunServer DryRunMode = "server" // read live state and diff, no writes
)

// Action says what apply did (or would do, under dry-run).
type Action string

// Apply outcomes.
const (
	ActionCreated    Action = "created"
	ActionConfigured Action = "configured"
	ActionUnchanged  Action = "unchanged"
	// ActionValidated is reported by client-side dry-runs, which never
	// contact the server and so cannot know created vs configured.
	ActionValidated Action = "validated"
)

// ApplyOptions tune a single apply operation.
type ApplyOptions struct {
	DryRun DryRunMode
}

// ApplyResult reports what apply decided for one object.
type ApplyResult struct {
	Action Action
	Name   string
	// Diff carries the changed keys when Action is ActionConfigured.
	Diff *diff.Result
	// Warnings are non-fatal notes (e.g. create-only fields ignored on
	// update) the command prints to stderr.
	Warnings []string
}

// Handler implements every generic verb for one resource kind.
type Handler interface {
	// Kind is the manifest kind, e.g. "VirtualMachine".
	Kind() string
	// APIVersion is the manifest apiVersion this handler accepts.
	APIVersion() string
	// Aliases are the names accepted on the command line (all lowercase),
	// e.g. "vm", "vms", "virtualmachine", "virtualmachines".
	Aliases() []string
	// Columns describes the table output for this kind.
	Columns(wide bool) []printer.Column

	Get(ctx context.Context, c api.Client, name string) (printer.Object, error)
	List(ctx context.Context, c api.Client) ([]printer.Object, error)
	Delete(ctx context.Context, c api.Client, name string) error
	Apply(ctx context.Context, c api.Client, obj *runtime.Unstructured, opts ApplyOptions) (*ApplyResult, error)
	Diff(ctx context.Context, c api.Client, obj *runtime.Unstructured) (*diff.Result, error)
	Describe(ctx context.Context, c api.Client, name string, w io.Writer) error
}

// Starter is implemented by kinds that support "pvectl start <kind> <name>".
type Starter interface {
	Start(ctx context.Context, c api.Client, name string) error
}

// Stopper is implemented by kinds that support "pvectl stop <kind> <name>".
type Stopper interface {
	Stop(ctx context.Context, c api.Client, name string) error
}

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

// Handler is the read side every resource kind must implement. Kinds
// that cannot be declared (a node exists whether or not a manifest says
// so) implement only this; writing verbs come from the optional
// capability interfaces below (ADR-006).
type Handler interface {
	// GVK is the manifest type identity, e.g.
	// {Group: "pve.io", Version: "v1alpha1", Kind: "VirtualMachine"}.
	GVK() runtime.GVK
	// Aliases are the names accepted on the command line (all lowercase),
	// e.g. "vm", "vms", "virtualmachine", "virtualmachines".
	Aliases() []string
	// Columns describes the table output for this kind.
	Columns(wide bool) []printer.Column

	Get(ctx context.Context, c api.Client, name string) (printer.Object, error)
	List(ctx context.Context, c api.Client) ([]printer.Object, error)
	Describe(ctx context.Context, c api.Client, name string, w io.Writer) error
}

// Applier is implemented by kinds that can be declared in a manifest,
// i.e. that support "pvectl apply" and "pvectl diff".
type Applier interface {
	Apply(ctx context.Context, c api.Client, obj *runtime.Unstructured, opts ApplyOptions) (*ApplyResult, error)
	Diff(ctx context.Context, c api.Client, obj *runtime.Unstructured) (*diff.Result, error)
}

// Deleter is implemented by kinds that support "pvectl delete".
type Deleter interface {
	Delete(ctx context.Context, c api.Client, name string) error
}

// Starter is implemented by kinds that support "pvectl start <kind> <name>".
type Starter interface {
	Start(ctx context.Context, c api.Client, name string) error
}

// Stopper is implemented by kinds that support "pvectl stop <kind> <name>".
type Stopper interface {
	Stop(ctx context.Context, c api.Client, name string) error
}

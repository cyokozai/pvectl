package resource

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/diff"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/runtime"
)

// stubHandler implements Handler with no behavior.
type stubHandler struct {
	kind       string
	apiVersion string
	aliases    []string
}

func (s stubHandler) Kind() string                  { return s.kind }
func (s stubHandler) APIVersion() string            { return s.apiVersion }
func (s stubHandler) Aliases() []string             { return s.aliases }
func (s stubHandler) Columns(bool) []printer.Column { return nil }
func (s stubHandler) Get(context.Context, api.Client, string) (printer.Object, error) {
	return nil, nil
}
func (s stubHandler) List(context.Context, api.Client) ([]printer.Object, error) { return nil, nil }
func (s stubHandler) Delete(context.Context, api.Client, string) error           { return nil }
func (s stubHandler) Apply(context.Context, api.Client, *runtime.Unstructured, ApplyOptions) (*ApplyResult, error) {
	return nil, nil
}
func (s stubHandler) Diff(context.Context, api.Client, *runtime.Unstructured) (*diff.Result, error) {
	return nil, nil
}
func (s stubHandler) Describe(context.Context, api.Client, string, io.Writer) error { return nil }

func vmStub() stubHandler {
	return stubHandler{
		kind:       "VirtualMachine",
		apiVersion: "pve.io/v1alpha1",
		aliases:    []string{"vm", "vms", "virtualmachine", "virtualmachines"},
	}
}

func TestRegistryLookup(t *testing.T) {
	r := NewRegistry()
	r.Register(vmStub())

	for _, name := range []string{"vm", "VM", "vms", "VirtualMachine", "virtualmachines"} {
		h, err := r.Lookup(name)
		if err != nil {
			t.Fatalf("Lookup(%q) error = %v", name, err)
		}
		if h.Kind() != "VirtualMachine" {
			t.Errorf("Lookup(%q).Kind() = %q", name, h.Kind())
		}
	}
}

func TestRegistryLookupUnknown(t *testing.T) {
	r := NewRegistry()
	r.Register(vmStub())

	_, err := r.Lookup("pods")
	if err == nil {
		t.Fatal("Lookup(pods) error = nil, want unknown-type error")
	}
	if !strings.Contains(err.Error(), `"pods"`) || !strings.Contains(err.Error(), "vm") {
		t.Errorf("error %q should name the unknown type and list available kinds", err)
	}
}

func TestRegistryForObject(t *testing.T) {
	r := NewRegistry()
	r.Register(vmStub())

	t.Run("dispatches by apiVersion and kind", func(t *testing.T) {
		u := &runtime.Unstructured{
			TypeMeta: runtime.TypeMeta{APIVersion: "pve.io/v1alpha1", Kind: "VirtualMachine"},
		}
		h, err := r.ForObject(u)
		if err != nil || h.Kind() != "VirtualMachine" {
			t.Fatalf("ForObject() = %v, %v", h, err)
		}
	})

	t.Run("unknown kind names the source", func(t *testing.T) {
		u := &runtime.Unstructured{
			TypeMeta: runtime.TypeMeta{APIVersion: "pve.io/v1alpha1", Kind: "Widget"},
			Source:   "widget.yaml",
		}
		if _, err := r.ForObject(u); err == nil || !strings.Contains(err.Error(), "widget.yaml") {
			t.Fatalf("ForObject() error = %v, want error naming widget.yaml", err)
		}
	})

	t.Run("wrong apiVersion is rejected", func(t *testing.T) {
		u := &runtime.Unstructured{
			TypeMeta: runtime.TypeMeta{APIVersion: "pve.io/v9", Kind: "VirtualMachine"},
			Source:   "vm.yaml",
		}
		if _, err := r.ForObject(u); err == nil || !strings.Contains(err.Error(), "pve.io/v9") {
			t.Fatalf("ForObject() error = %v, want unsupported apiVersion error", err)
		}
	})
}

func TestRegistryKinds(t *testing.T) {
	r := NewRegistry()
	r.Register(vmStub())
	r.Register(stubHandler{kind: "Container", apiVersion: "pve.io/v1alpha1", aliases: []string{"lxc"}})

	kinds := r.Kinds()
	if len(kinds) != 2 || kinds[0] != "Container" || kinds[1] != "VirtualMachine" {
		t.Errorf("Kinds() = %v, want sorted [Container VirtualMachine]", kinds)
	}
}

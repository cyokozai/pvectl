package resource

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/runtime"
)

// stubHandler implements Handler with no behavior.
type stubHandler struct {
	gvk     runtime.GVK
	aliases []string
}

func (s stubHandler) GVK() runtime.GVK              { return s.gvk }
func (s stubHandler) Aliases() []string             { return s.aliases }
func (s stubHandler) Columns(bool) []printer.Column { return nil }
func (s stubHandler) Get(context.Context, api.Client, string) (printer.Object, error) {
	return nil, nil
}
func (s stubHandler) List(context.Context, api.Client) ([]printer.Object, error) { return nil, nil }
func (s stubHandler) Describe(context.Context, api.Client, string, io.Writer) error {
	return nil
}

func vmStub() stubHandler {
	return stubHandler{
		gvk:     runtime.GVK{Group: "pve.io", Version: "v1alpha1", Kind: "VirtualMachine"},
		aliases: []string{"vm", "vms", "virtualmachine", "virtualmachines"},
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
		if h.GVK().Kind != "VirtualMachine" {
			t.Errorf("Lookup(%q).GVK().Kind = %q", name, h.GVK().Kind)
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
		if err != nil || h.GVK().Kind != "VirtualMachine" {
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

func TestRegistryLookupAmbiguous(t *testing.T) {
	r := NewRegistry()
	r.Register(stubHandler{
		gvk:     runtime.GVK{Group: "pve.io", Version: "v1alpha1", Kind: "Zone"},
		aliases: []string{"zone"},
	})
	r.Register(stubHandler{
		gvk:     runtime.GVK{Group: "sdn.pve.io", Version: "v1alpha1", Kind: "Zone"},
		aliases: []string{"zone"},
	})

	for _, name := range []string{"zone", "Zone"} {
		_, err := r.Lookup(name)
		if err == nil {
			t.Fatalf("Lookup(%q) error = nil, want ambiguity error", name)
		}
		for _, want := range []string{
			`"` + name + `"`,
			"pve.io/v1alpha1, Kind=Zone",
			"sdn.pve.io/v1alpha1, Kind=Zone",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Lookup(%q) error = %q, want it to mention %q", name, err, want)
			}
		}
	}

	// A name unique to one of the two still resolves.
	r.Register(stubHandler{
		gvk:     runtime.GVK{Group: "sdn.pve.io", Version: "v1alpha1", Kind: "VNet"},
		aliases: []string{"vnet"},
	})
	h, err := r.Lookup("vnet")
	if err != nil {
		t.Fatalf("Lookup(vnet) error = %v", err)
	}
	if got := h.GVK().Kind; got != "VNet" {
		t.Errorf("Lookup(vnet).GVK().Kind = %q, want VNet", got)
	}
}

func TestRegistryForObjectDistinguishesGroups(t *testing.T) {
	r := NewRegistry()
	r.Register(stubHandler{gvk: runtime.GVK{Group: "pve.io", Version: "v1alpha1", Kind: "Zone"}})
	r.Register(stubHandler{gvk: runtime.GVK{Group: "sdn.pve.io", Version: "v1alpha1", Kind: "Zone"}})

	for _, apiVersion := range []string{"pve.io/v1alpha1", "sdn.pve.io/v1alpha1"} {
		u := &runtime.Unstructured{
			TypeMeta: runtime.TypeMeta{APIVersion: apiVersion, Kind: "Zone"},
		}
		h, err := r.ForObject(u)
		if err != nil {
			t.Fatalf("ForObject(%s) error = %v", apiVersion, err)
		}
		if got := h.GVK().APIVersion(); got != apiVersion {
			t.Errorf("ForObject(%s) resolved to %s", apiVersion, got)
		}
	}
}

func TestRegistryKinds(t *testing.T) {
	r := NewRegistry()
	r.Register(vmStub())
	r.Register(stubHandler{gvk: runtime.GVK{Group: "pve.io", Version: "v1alpha1", Kind: "Container"}, aliases: []string{"lxc"}})

	kinds := r.Kinds()
	if len(kinds) != 2 || kinds[0] != "Container" || kinds[1] != "VirtualMachine" {
		t.Errorf("Kinds() = %v, want sorted [Container VirtualMachine]", kinds)
	}
}

package cmd

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/printer"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
)

// readOnlyHandler stands in for a kind that exists whether or not a
// manifest declares it (a node): it implements resource.Handler and
// neither resource.Applier nor resource.Deleter.
type readOnlyHandler struct{}

func (readOnlyHandler) GVK() runtime.GVK {
	return runtime.GVK{Group: "pve.io", Version: "v1alpha1", Kind: "Node"}
}
func (readOnlyHandler) Aliases() []string             { return []string{"node", "nodes"} }
func (readOnlyHandler) Columns(bool) []printer.Column { return nil }
func (readOnlyHandler) Get(context.Context, api.Client, string) (printer.Object, error) {
	return nil, nil
}
func (readOnlyHandler) List(context.Context, api.Client) ([]printer.Object, error) {
	return nil, nil
}
func (readOnlyHandler) Describe(context.Context, api.Client, string, io.Writer) error { return nil }

const nodeDoc = `apiVersion: pve.io/v1alpha1
kind: Node
metadata:
  name: pve1
`

func readOnlyRegistry() *resource.Registry {
	reg := resource.NewRegistry()
	reg.Register(readOnlyHandler{})
	return reg
}

func TestApplyRejectsKindWithoutApplier(t *testing.T) {
	e := newEnv(t)
	path := writeManifest(t, nodeDoc)

	err := e.runWith(t, readOnlyRegistry(), "apply", "-f", path)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("apply error = %v, want ExitError code 1", err)
	}
	if !strings.Contains(e.stderr.String(), "resource type Node does not support apply") {
		t.Errorf("stderr = %q, want a \"does not support apply\" message", e.stderr.String())
	}
}

func TestDiffRejectsKindWithoutApplier(t *testing.T) {
	e := newEnv(t)
	path := writeManifest(t, nodeDoc)

	err := e.runWith(t, readOnlyRegistry(), "diff", "-f", path)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("diff error = %v, want ExitError code 2", err)
	}
	if !strings.Contains(err.Error(), "resource type Node does not support diff") {
		t.Errorf("error = %v, want a \"does not support diff\" message", err)
	}
}

func TestDeleteRejectsKindWithoutDeleter(t *testing.T) {
	t.Run("by name", func(t *testing.T) {
		e := newEnv(t)
		err := e.runWith(t, readOnlyRegistry(), "delete", "node", "pve1")
		if err == nil || !strings.Contains(err.Error(), "resource type Node does not support delete") {
			t.Fatalf("delete error = %v, want a \"does not support delete\" message", err)
		}
	})

	t.Run("by manifest", func(t *testing.T) {
		e := newEnv(t)
		path := writeManifest(t, nodeDoc)
		err := e.runWith(t, readOnlyRegistry(), "delete", "-f", path)
		if err == nil || !strings.Contains(err.Error(), "resource type Node does not support delete") {
			t.Fatalf("delete error = %v, want a \"does not support delete\" message", err)
		}
	})
}

func TestStartRejectsKindWithoutStarter(t *testing.T) {
	e := newEnv(t)
	err := e.runWith(t, readOnlyRegistry(), "start", "node", "pve1")
	if err == nil || !strings.Contains(err.Error(), "resource type Node does not support start") {
		t.Fatalf("start error = %v, want a \"does not support start\" message", err)
	}
}

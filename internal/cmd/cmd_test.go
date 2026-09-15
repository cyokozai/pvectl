package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/api/apitest"
	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/config"
)

const testConfig = `
apiVersion: v1
kind: Config
users:
  - name: admin@pam
    user:
      token: admin@pam!ci=secret
nodes:
  - name: prod
    node:
      server: https://prod.example.com:8006
  - name: dev
    node:
      server: https://dev.example.com:8006
contexts:
  - name: production
    context:
      user: admin@pam
      node: prod
  - name: development
    context:
      user: admin@pam
      node: dev
current-context: production
`

type testEnv struct {
	fake       *apitest.Fake
	configPath string
	lastServer string
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

// run executes pvectl with the given args against the fake cluster,
// always passing --config. Returns the command error.
func (e *testEnv) run(t *testing.T, args ...string) error {
	t.Helper()
	f := &cliopt.Factory{
		NewClient: func(ctx context.Context, node config.Node, user config.User, opts api.Options) (api.Client, error) {
			e.lastServer = node.Server
			return e.fake, nil
		},
	}
	root := NewRootCmd(f, DefaultRegistry())
	root.SetOut(&e.stdout)
	root.SetErr(&e.stderr)
	root.SetArgs(append([]string{"--config", e.configPath}, args...))
	return root.Execute()
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	fake := &apitest.Fake{NextIDValue: 105}
	fake.AddGuest(api.GuestSummary{
		VMID: 100, Name: "web-server", Node: "pve1", Status: "running", MaxCPU: 2, MaxMem: 2048 << 20,
	}, map[string]any{
		"name": "web-server", "cores": "2", "memory": "2048",
		"scsi0": "local-lvm:vm-100-disk-0,size=32G",
	})
	fake.AddGuest(api.GuestSummary{VMID: 101, Name: "db", Node: "pve2", Status: "stopped", MaxCPU: 4, MaxMem: 4096 << 20}, map[string]any{
		"name": "db", "cores": "4", "memory": "4096",
	})

	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(testConfig), 0600); err != nil {
		t.Fatal(err)
	}
	return &testEnv{fake: fake, configPath: path}
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vm.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const newVMDoc = `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: new-vm
spec:
  targetNode: pve1
  resources:
    cpu:
      cores: 2
    memory: 1024
`

func TestGetTable(t *testing.T) {
	e := newEnv(t)
	if err := e.run(t, "get", "vm"); err != nil {
		t.Fatalf("get vm: %v", err)
	}
	out := e.stdout.String()
	for _, want := range []string{"NAME", "web-server", "db", "running", "stopped"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestGetYAML(t *testing.T) {
	e := newEnv(t)
	if err := e.run(t, "get", "vm", "web-server", "-o", "yaml"); err != nil {
		t.Fatalf("get -o yaml: %v", err)
	}
	out := e.stdout.String()
	for _, want := range []string{"apiVersion: pve.io/v1alpha1", "kind: VirtualMachine", "name: web-server", "targetNode: pve1"} {
		if !strings.Contains(out, want) {
			t.Errorf("yaml missing %q:\n%s", want, out)
		}
	}
}

func TestGetUnknownType(t *testing.T) {
	e := newEnv(t)
	err := e.run(t, "get", "pods")
	if err == nil || !strings.Contains(err.Error(), "pods") {
		t.Fatalf("get pods error = %v, want unknown type", err)
	}
}

func TestContextFlagIsHonored(t *testing.T) {
	e := newEnv(t)
	if err := e.run(t, "get", "vm"); err != nil {
		t.Fatal(err)
	}
	if e.lastServer != "https://prod.example.com:8006" {
		t.Errorf("default context server = %q", e.lastServer)
	}

	e2 := newEnv(t)
	if err := e2.run(t, "get", "vm", "--context", "development"); err != nil {
		t.Fatal(err)
	}
	if e2.lastServer != "https://dev.example.com:8006" {
		t.Errorf("--context development server = %q, want dev server", e2.lastServer)
	}
}

func TestApplyCreatesAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	path := writeManifest(t, newVMDoc)

	if err := e.run(t, "apply", "-f", path); err != nil {
		t.Fatalf("apply: %v\nstderr: %s", err, e.stderr.String())
	}
	if !strings.Contains(e.stdout.String(), "virtualmachine/new-vm created") {
		t.Errorf("stdout = %q", e.stdout.String())
	}
	if len(e.fake.Creates) != 1 {
		t.Fatalf("Creates = %+v", e.fake.Creates)
	}

	// Second apply of the identical manifest must be a no-op.
	e.stdout.Reset()
	if err := e.run(t, "apply", "-f", path); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !strings.Contains(e.stdout.String(), "virtualmachine/new-vm unchanged") {
		t.Errorf("stdout = %q, want unchanged", e.stdout.String())
	}
	if len(e.fake.Creates) != 1 || len(e.fake.Updates) != 0 {
		t.Errorf("idempotency violated: creates=%d updates=%d", len(e.fake.Creates), len(e.fake.Updates))
	}
}

func TestApplyServerDryRun(t *testing.T) {
	e := newEnv(t)
	path := writeManifest(t, newVMDoc)
	if err := e.run(t, "apply", "-f", path, "--dry-run=server"); err != nil {
		t.Fatalf("apply --dry-run=server: %v", err)
	}
	if !strings.Contains(e.stdout.String(), "created (server dry run)") {
		t.Errorf("stdout = %q", e.stdout.String())
	}
	if len(e.fake.Creates) != 0 {
		t.Error("server dry run must not create")
	}
}

func TestApplyContinuesOnError(t *testing.T) {
	e := newEnv(t)
	// First doc invalid (no targetNode), second valid.
	path := writeManifest(t, `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: broken
spec:
  resources:
    memory: 512
---
`+strings.TrimPrefix(newVMDoc, "\n"))

	err := e.run(t, "apply", "-f", path)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("apply error = %v, want ExitError code 1", err)
	}
	if !strings.Contains(e.stdout.String(), "virtualmachine/new-vm created") {
		t.Errorf("valid doc should still apply: %q", e.stdout.String())
	}
	if !strings.Contains(e.stderr.String(), "targetNode") {
		t.Errorf("stderr should carry the validation error: %q", e.stderr.String())
	}
}

func TestDiffExitCodes(t *testing.T) {
	t.Run("no differences exits 0", func(t *testing.T) {
		e := newEnv(t)
		path := writeManifest(t, `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server
spec:
  targetNode: pve1
  vmid: 100
  resources:
    cpu:
      cores: 2
    memory: 2048
`)
		if err := e.run(t, "diff", "-f", path); err != nil {
			t.Fatalf("diff error = %v, want nil", err)
		}
	})

	t.Run("differences exit 1", func(t *testing.T) {
		e := newEnv(t)
		path := writeManifest(t, `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server
spec:
  targetNode: pve1
  vmid: 100
  resources:
    cpu:
      cores: 8
    memory: 2048
`)
		err := e.run(t, "diff", "-f", path)
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 1 || exitErr.Err != nil {
			t.Fatalf("diff error = %v, want silent ExitError code 1", err)
		}
		out := e.stdout.String()
		if !strings.Contains(out, "-cores: 2") || !strings.Contains(out, "+cores: 8") {
			t.Errorf("diff output = %q", out)
		}
	})
}

func TestDeleteByNameAndFile(t *testing.T) {
	e := newEnv(t)
	if err := e.run(t, "delete", "vm", "db"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(e.stdout.String(), "virtualmachine/db deleted") {
		t.Errorf("stdout = %q", e.stdout.String())
	}
	if len(e.fake.Deletes) != 1 || e.fake.Deletes[0].VMID != 101 {
		t.Errorf("Deletes = %+v", e.fake.Deletes)
	}
}

func TestStartStop(t *testing.T) {
	e := newEnv(t)
	if err := e.run(t, "start", "vm", "db"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := e.run(t, "stop", "vm", "web-server"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(e.fake.Starts) != 1 || len(e.fake.Stops) != 1 {
		t.Errorf("starts=%+v stops=%+v", e.fake.Starts, e.fake.Stops)
	}
}

func TestConfigCommands(t *testing.T) {
	t.Run("get-contexts marks current", func(t *testing.T) {
		e := newEnv(t)
		if err := e.run(t, "config", "get-contexts"); err != nil {
			t.Fatal(err)
		}
		out := e.stdout.String()
		if !strings.Contains(out, "production") || !strings.Contains(out, "development") || !strings.Contains(out, "*") {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("current-context", func(t *testing.T) {
		e := newEnv(t)
		if err := e.run(t, "config", "current-context"); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(e.stdout.String()) != "production" {
			t.Errorf("output = %q", e.stdout.String())
		}
	})

	t.Run("use-context persists to the loaded config path", func(t *testing.T) {
		e := newEnv(t)
		if err := e.run(t, "config", "use-context", "development"); err != nil {
			t.Fatal(err)
		}
		saved, err := config.Load(e.configPath)
		if err != nil {
			t.Fatal(err)
		}
		if saved.CurrentContext != "development" {
			t.Errorf("saved current-context = %q", saved.CurrentContext)
		}
	})

	t.Run("view redacts secrets", func(t *testing.T) {
		e := newEnv(t)
		if err := e.run(t, "config", "view"); err != nil {
			t.Fatal(err)
		}
		out := e.stdout.String()
		if strings.Contains(out, "secret") {
			t.Errorf("secret leaked: %q", out)
		}
		if !strings.Contains(out, "REDACTED") {
			t.Errorf("output = %q", out)
		}
	})
}

func TestVersion(t *testing.T) {
	e := newEnv(t)
	if err := e.run(t, "version"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.stdout.String(), "pvectl version") {
		t.Errorf("output = %q", e.stdout.String())
	}
}

// Package e2e drives the real command stack — cobra commands, factory,
// Telmate SDK client — against the in-memory fake Proxmox VE API.
// No real cluster is required.
package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/cmd"
	"github.com/cyokozai/pvectl/test/pvefake"
)

const token = "ci@pam!pvectl=00000000-0000-0000-0000-000000000000"

type env struct {
	server     *pvefake.Server
	configPath string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := pvefake.New()
	t.Cleanup(s.Close)
	s.Token = token
	s.AddGuest(pvefake.Guest{
		VMID: 100, Name: "template-ubuntu", Node: "pve1", Status: "stopped",
		Config: map[string]any{"name": "template-ubuntu", "cores": "1", "memory": "512", "template": "1"},
	})

	configYAML := fmt.Sprintf(`
apiVersion: v1
kind: Config
users:
  - name: ci@pam
    user:
      token: %s
nodes:
  - name: fake
    node:
      server: %s
contexts:
  - name: e2e
    context:
      user: ci@pam
      node: fake
current-context: e2e
`, token, s.URL())

	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	return &env{server: s, configPath: path}
}

// run executes one pvectl invocation and returns stdout, stderr, error.
func (e *env) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := cmd.NewRootCmd(&cliopt.Factory{}, cmd.DefaultRegistry())
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--config", e.configPath}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

const manifestTemplate = `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: e2e-vm
spec:
  targetNode: pve1
  vmid: 200
  resources:
    cpu:
      cores: %d
    memory: %d
  disks:
    - name: scsi0
      size: 16G
      storage: local-lvm
  networks:
    - name: net0
      bridge: vmbr0
      tag: 42
`

func writeManifest(t *testing.T, cores, memory int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vm.yaml")
	if err := os.WriteFile(path, fmt.Appendf(nil, manifestTemplate, cores, memory), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestApplyLifecycleRoundTrip walks the full IaC loop:
// apply(create) → diff(empty) → apply(unchanged) → mutate manifest →
// diff(exit 1) → apply(configured) → get -o yaml → start/stop → delete.
func TestApplyLifecycleRoundTrip(t *testing.T) {
	e := newEnv(t)
	v1 := writeManifest(t, 2, 1024)

	// 1. create
	stdout, stderr, err := e.run(t, "apply", "-f", v1)
	if err != nil {
		t.Fatalf("apply(create): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/e2e-vm created") {
		t.Fatalf("stdout = %q", stdout)
	}
	if g := e.server.Guest(200); g == nil || g.Config["cores"] != "2" || g.Config["net0"] != "virtio,bridge=vmbr0,tag=42" {
		t.Fatalf("created guest = %+v", g)
	}

	// 2. diff against unchanged manifest exits 0
	if _, stderr, err = e.run(t, "diff", "-f", v1); err != nil {
		t.Fatalf("diff(unchanged): %v\nstderr: %s", err, stderr)
	}

	// 3. re-apply is a no-op
	stdout, stderr, err = e.run(t, "apply", "-f", v1)
	if err != nil {
		t.Fatalf("apply(unchanged): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/e2e-vm unchanged") {
		t.Fatalf("stdout = %q", stdout)
	}

	// 4. mutate → diff exits 1 and shows the change
	v2 := writeManifest(t, 4, 2048)
	stdout, _, err = e.run(t, "diff", "-f", v2)
	var exitErr *cmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("diff(changed) error = %v, want exit code 1", err)
	}
	if !strings.Contains(stdout, "+cores: 4") {
		t.Fatalf("diff output = %q", stdout)
	}

	// 5. apply the change
	stdout, stderr, err = e.run(t, "apply", "-f", v2)
	if err != nil {
		t.Fatalf("apply(update): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/e2e-vm configured") {
		t.Fatalf("stdout = %q", stdout)
	}
	if g := e.server.Guest(200); g.Config["cores"] != "4" || g.Config["memory"] != "2048" {
		t.Fatalf("updated guest config = %+v", g.Config)
	}

	// 6. get -o yaml reflects live state
	stdout, _, err = e.run(t, "get", "vm", "e2e-vm", "-o", "yaml")
	if err != nil {
		t.Fatalf("get -o yaml: %v", err)
	}
	for _, want := range []string{"kind: VirtualMachine", "cores: 4", "memory: 2048", "vmid: 200"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("yaml missing %q:\n%s", want, stdout)
		}
	}

	// 7. lifecycle
	if _, _, err = e.run(t, "start", "vm", "e2e-vm"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if e.server.Guest(200).Status != "running" {
		t.Error("guest not running after start")
	}
	if _, _, err = e.run(t, "stop", "vm", "e2e-vm"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// 8. delete
	stdout, _, err = e.run(t, "delete", "vm", "e2e-vm")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(stdout, "virtualmachine/e2e-vm deleted") {
		t.Fatalf("stdout = %q", stdout)
	}
	if e.server.Guest(200) != nil {
		t.Error("guest still exists after delete")
	}
}

// TestCloneFlow covers clone-based creation with post-clone
// reconfiguration through the real client.
func TestCloneFlow(t *testing.T) {
	e := newEnv(t)
	manifest := `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: cloned
spec:
  targetNode: pve1
  vmid: 300
  clone: template-ubuntu
  fullClone: true
  resources:
    cpu:
      cores: 4
    memory: 4096
`
	path := filepath.Join(t.TempDir(), "clone.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := e.run(t, "apply", "-f", path)
	if err != nil {
		t.Fatalf("apply(clone): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/cloned created") {
		t.Fatalf("stdout = %q", stdout)
	}
	g := e.server.Guest(300)
	if g == nil {
		t.Fatal("clone target 300 not created")
	}
	// Post-clone reconfiguration must have overridden the template sizing.
	if g.Config["cores"] != "4" || g.Config["memory"] != "4096" {
		t.Errorf("clone config = %+v, want cores=4 memory=4096", g.Config)
	}
}

// TestTaskFailureSurfaces proves task exit statuses become command errors.
func TestTaskFailureSurfaces(t *testing.T) {
	e := newEnv(t)
	e.server.FailNext("create", "unable to create VM 200 - storage full")

	path := writeManifest(t, 2, 1024)
	_, _, err := e.run(t, "apply", "-f", path)
	if err == nil {
		t.Fatal("apply error = nil, want task failure")
	}
}

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/test/pvefake"
)

// ctTemplateVolID is the OS template the volid-form manifests create from.
const ctTemplateVolID = "local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst"

// seedTemplate puts the volid-form template on pve1's "local" storage,
// the way an operator would have downloaded it beforehand, and makes
// the fake refuse a create whose template is not on storage.
func seedTemplate(e *env) {
	e.server.VerifyOSTemplate = true
	e.server.AddVolume("pve1", "local", pvefake.Volume{
		VolID: ctTemplateVolID, Content: "vztmpl", Format: "tzst",
	})
}

const ctManifest = `
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata:
  name: e2e-ct
spec:
  targetNode: pve1
  vmid: 300
  unprivileged: true
  osTemplate:
    volid: local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst
  resources:
    cores: 1
    memory: 512
  rootfs:
    storage: local-lvm
    size: 8G
  networks:
    - name: net0
      bridge: vmbr0
      ip: dhcp
`

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLinuxContainerExportRoundTrip is the core of the definition of
// done: create from a volid, export with get -o yaml, and apply the
// export unchanged.
func TestLinuxContainerExportRoundTrip(t *testing.T) {
	e := newEnv(t)
	seedTemplate(e)

	stdout, stderr, err := e.run(t, "apply", "-f", writeFile(t, "ct.yaml", ctManifest))
	if err != nil {
		t.Fatalf("apply(create): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "linuxcontainer/e2e-ct created") {
		t.Fatalf("stdout = %q", stdout)
	}

	exported, stderr, err := e.run(t, "get", "ct", "e2e-ct", "-o", "yaml")
	if err != nil {
		t.Fatalf("get -o yaml: %v\nstderr: %s", err, stderr)
	}

	stdout, stderr, err = e.run(t, "apply", "-f", writeFile(t, "export.yaml", exported))
	if err != nil {
		t.Fatalf("apply(export): %v\nstderr: %s\nexport:\n%s", err, stderr, exported)
	}
	if !strings.Contains(stdout, "linuxcontainer/e2e-ct unchanged") {
		t.Fatalf("apply(export) stdout = %q, want unchanged\nexport:\n%s", stdout, exported)
	}
}

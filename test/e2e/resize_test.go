package e2e

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/cmd"
)

const diskManifestTemplate = `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: disk-vm
spec:
  targetNode: pve1
  vmid: 210
  resources:
    cpu:
      cores: 2
    memory: 1024
  disks:
    - name: scsi0
      size: %s
      storage: local-lvm
`

func writeDiskManifest(t *testing.T, size string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "disk.yaml")
	if err := os.WriteFile(path, fmt.Appendf(nil, diskManifestTemplate, size), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDiskGrowthReachesTheResizeEndpoint closes the gap that unit tests
// could not: growing a disk goes through PUT .../resize rather than the
// config endpoint, and the larger size is what the next read sees.
func TestDiskGrowthReachesTheResizeEndpoint(t *testing.T) {
	e := newEnv(t)

	small := writeDiskManifest(t, "16G")
	if _, stderr, err := e.run(t, "apply", "-f", small); err != nil {
		t.Fatalf("apply(create): %v\nstderr: %s", err, stderr)
	}
	created := fmt.Sprintf("%v", e.server.Guest(210).Config["scsi0"])
	if !strings.Contains(created, "size=16G") {
		t.Fatalf("created disk = %q, want a 16G volume", created)
	}

	// Growing the manifest is a difference the diff must report...
	big := writeDiskManifest(t, "32G")
	stdout, _, err := e.run(t, "diff", "-f", big)
	var exitErr *cmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("diff(grown) error = %v, want exit code 1", err)
	}
	if !strings.Contains(stdout, "scsi0") {
		t.Fatalf("diff output = %q, want the disk key", stdout)
	}

	// ...and apply must carry it out through the resize endpoint.
	stdout, stderr, err := e.run(t, "apply", "-f", big)
	if err != nil {
		t.Fatalf("apply(grow): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/disk-vm configured") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !e.server.SawRequest("PUT", "/api2/json/nodes/pve1/qemu/210/resize") {
		t.Fatalf("the resize endpoint was never called; requests: %v", e.server.Requests())
	}
	resized := fmt.Sprintf("%v", e.server.Guest(210).Config["scsi0"])
	if !strings.Contains(resized, "size=32G") {
		t.Fatalf("resized disk = %q, want size=32G", resized)
	}
	// The volume itself must survive: a resize grows a disk, it does not
	// reallocate one.
	if !strings.Contains(resized, "vm-210-disk-0") {
		t.Errorf("resized disk = %q, want the original volume", resized)
	}

	// The grown state is now the desired state, so apply is a no-op.
	stdout, stderr, err = e.run(t, "apply", "-f", big)
	if err != nil {
		t.Fatalf("apply(settled): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "virtualmachine/disk-vm unchanged") {
		t.Fatalf("stdout = %q, want unchanged after the resize", stdout)
	}
}

// TestDiskShrinkIsRefused checks the guard fires before any API call.
func TestDiskShrinkIsRefused(t *testing.T) {
	e := newEnv(t)

	if _, stderr, err := e.run(t, "apply", "-f", writeDiskManifest(t, "32G")); err != nil {
		t.Fatalf("apply(create): %v\nstderr: %s", err, stderr)
	}
	_, _, err := e.run(t, "apply", "-f", writeDiskManifest(t, "16G"))
	if err == nil {
		t.Fatal("apply(shrink): error = nil, want a refusal")
	}
	if got := fmt.Sprintf("%v", e.server.Guest(210).Config["scsi0"]); !strings.Contains(got, "size=32G") {
		t.Errorf("disk = %q, want it left at 32G", got)
	}
}

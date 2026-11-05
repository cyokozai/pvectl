package e2e

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/cmd"
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

// ctFullTemplate exercises pool, swap, features, tags and runStrategy
// on top of ctManifest; cores, the rootfs size and the net0 bridge vary.
const ctFullTemplate = `
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
  pool: lab
  description: e2e container
  resources:
    cores: %d
    memory: 512
    swap: 256
  rootfs:
    storage: local-lvm
    size: %s
  mountPoints:
    - name: mp0
      storage: local-lvm
      size: 4G
      path: /data
  networks:
    - name: net0
      bridge: %s
      ip: dhcp
  features:
    nesting: true
  tags: [web, e2e]
`

func ctFull(t *testing.T, cores int, rootfs, bridge string) string {
	t.Helper()
	return writeFile(t, "ct.yaml", fmt.Sprintf(ctFullTemplate, cores, rootfs, bridge))
}

// TestLinuxContainerLifecycle walks the whole loop on the fake node:
// create → diff empty → re-apply unchanged (pool included) → named and
// unnamed export apply unchanged → diff exit 1 → update with resize →
// the NIC keeps its MAC → start/stop → table → delete.
func TestLinuxContainerLifecycle(t *testing.T) {
	e := newEnv(t)
	seedTemplate(e)
	v1 := ctFull(t, 1, "8G", "vmbr0")

	// 1. create
	if _, stderr, err := e.run(t, "apply", "-f", v1); err != nil {
		t.Fatalf("apply(create): %v\nstderr: %s", err, stderr)
	}
	g := e.server.Guest(300)
	if g == nil || g.Type != "lxc" || g.Pool != "lab" || g.Config["unprivileged"] != "1" || g.Config["rootfs"] != "local-lvm:vm-300-disk-0,size=8G" {
		t.Fatalf("created guest = %+v", g)
	}
	if _, has := g.Config["ostemplate"]; has {
		t.Error("ostemplate leaked into the config")
	}
	mac := hwaddrOf(t, g.Config["net0"])

	// 2. diff against the unchanged manifest exits 0
	if _, stderr, err := e.run(t, "diff", "-f", v1); err != nil {
		t.Fatalf("diff(unchanged): %v\nstderr: %s", err, stderr)
	}

	// 3. re-apply, pool and all, is a no-op
	stdout, stderr, err := e.run(t, "apply", "-f", v1)
	if err != nil || !strings.Contains(stdout, "linuxcontainer/e2e-ct unchanged") {
		t.Fatalf("apply(unchanged) = %q, %v\nstderr: %s", stdout, err, stderr)
	}

	// 4. both export forms apply back unchanged
	for _, args := range [][]string{
		{"get", "ct", "e2e-ct", "-o", "yaml"},
		{"get", "ct", "-o", "yaml"},
		{"get", "lxc", "-o", "json"},
	} {
		exported, stderr, err := e.run(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\nstderr: %s", args, err, stderr)
		}
		if !strings.Contains(exported, "unprivileged") || !strings.Contains(exported, "mp0") {
			t.Errorf("%v lacks the config-only fields:\n%s", args, exported)
		}
		name := "export.yaml"
		if args[len(args)-1] == "json" {
			name = "export.json"
		}
		stdout, stderr, err = e.run(t, "apply", "-f", writeFile(t, name, exported))
		if err != nil || !strings.Contains(stdout, "linuxcontainer/e2e-ct unchanged") {
			t.Errorf("apply(%v) = %q, %v\nstderr: %s\nexport:\n%s", args, stdout, err, stderr, exported)
		}
	}

	// 5. a change makes diff exit 1
	v2 := ctFull(t, 2, "16G", "vmbr0")
	stdout, _, err = e.run(t, "diff", "-f", v2)
	var exitErr *cmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("diff(changed) error = %v, want exit code 1", err)
	}
	if !strings.Contains(stdout, "+cores: 2") || !strings.Contains(stdout, "rootfs") {
		t.Errorf("diff output = %q", stdout)
	}

	// 6. apply the change: the config PUT plus a resize
	stdout, stderr, err = e.run(t, "apply", "-f", v2)
	if err != nil || !strings.Contains(stdout, "linuxcontainer/e2e-ct configured") {
		t.Fatalf("apply(update) = %q, %v\nstderr: %s", stdout, err, stderr)
	}
	g = e.server.Guest(300)
	if g.Config["cores"] != "2" || g.Config["rootfs"] != "local-lvm:vm-300-disk-0,size=16G" {
		t.Fatalf("updated config = %+v", g.Config)
	}
	if !e.server.SawRequest("PUT", "/api2/json/nodes/pve1/lxc/300/resize") {
		t.Errorf("rootfs grew without the resize endpoint: %v", e.server.Requests())
	}
	if stdout, _, _ := e.run(t, "apply", "-f", v2); !strings.Contains(stdout, "unchanged") {
		t.Errorf("apply after update = %q, want unchanged", stdout)
	}

	// 7. rewriting the NIC keeps the MAC Proxmox generated (ADR-010 §2)
	v3 := ctFull(t, 2, "16G", "vmbr1")
	if _, stderr, err := e.run(t, "apply", "-f", v3); err != nil {
		t.Fatalf("apply(bridge): %v\nstderr: %s", err, stderr)
	}
	net0 := fmt.Sprintf("%v", e.server.Guest(300).Config["net0"])
	if !strings.Contains(net0, "bridge=vmbr1") || hwaddrOf(t, net0) != mac {
		t.Errorf("net0 = %s, want bridge=vmbr1 and the original hwaddr %s", net0, mac)
	}

	// 8. lifecycle and the table
	if _, _, err := e.run(t, "start", "ct", "e2e-ct"); err != nil || e.server.Guest(300).Status != "running" {
		t.Fatalf("start: %v", err)
	}
	stdout, _, err = e.run(t, "get", "ct", "-o", "wide")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"e2e-ct", "300", "running", "2", "512Mi", "1h0m", "web"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("get ct -o wide missing %q:\n%s", want, stdout)
		}
	}
	if _, _, err := e.run(t, "stop", "ct", "e2e-ct"); err != nil || e.server.Guest(300).Status != "stopped" {
		t.Fatalf("stop: %v", err)
	}
	stdout, _, err = e.run(t, "describe", "ct", "e2e-ct")
	if err != nil || !strings.Contains(stdout, "Unprivileged:") || !strings.Contains(stdout, "local-lvm 16G") {
		t.Errorf("describe = %q, %v", stdout, err)
	}

	// 9. delete
	stdout, _, err = e.run(t, "delete", "ct", "e2e-ct")
	if err != nil || !strings.Contains(stdout, "linuxcontainer/e2e-ct deleted") {
		t.Fatalf("delete = %q, %v", stdout, err)
	}
	if e.server.Guest(300) != nil {
		t.Error("container still exists after delete")
	}
}

func hwaddrOf(t *testing.T, net any) string {
	t.Helper()
	for _, part := range strings.Split(fmt.Sprintf("%v", net), ",") {
		if mac, ok := strings.CutPrefix(part, "hwaddr="); ok {
			return mac
		}
	}
	t.Fatalf("no hwaddr in %v", net)
	return ""
}

// ctSHA is a made-up 64-digit digest; the fake does not verify it.
const ctSHA = "3f2a9c0d1e4b5a690123456789abcdef0123456789abcdef0123456789abcdef"

const ctURLManifest = `
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata:
  name: %s
spec:
  targetNode: pve1
  unprivileged: true
  osTemplate:
    url: https://download.example.test/debian-12-standard_12.7-1_amd64.tar.zst
    sha256: ` + ctSHA + `
    storage: local
  rootfs:
    storage: local-lvm
    size: 8G
`

const ctDerivedVolID = "local:vztmpl/pvectl-3f2a9c0d1e4b5a69-debian-12-standard_12.7-1_amd64.tar.zst"

func TestLinuxContainerDownloadsTemplateOnce(t *testing.T) {
	e := newEnv(t)
	e.server.VerifyOSTemplate = true
	first := writeFile(t, "a.yaml", fmt.Sprintf(ctURLManifest, "url-ct"))

	// The fake refuses the create unless ostemplate names exactly the
	// file the download produced.
	if _, stderr, err := e.run(t, "apply", "-f", first); err != nil {
		t.Fatalf("apply(url): %v\nstderr: %s", err, stderr)
	}
	downloads := e.server.Downloads()
	if len(downloads) != 1 {
		t.Fatalf("downloads = %v, want 1", downloads)
	}
	if got := "local:vztmpl/" + fmt.Sprint(downloads[0]["filename"]); got != ctDerivedVolID {
		t.Errorf("downloaded %s, want %s", got, ctDerivedVolID)
	}
	if downloads[0]["checksum"] != ctSHA || downloads[0]["checksum-algorithm"] != "sha256" {
		t.Errorf("download params = %v", downloads[0])
	}

	// Re-applying, and creating a second container from the same
	// template, both find it on storage.
	if stdout, _, err := e.run(t, "apply", "-f", first); err != nil || !strings.Contains(stdout, "unchanged") {
		t.Errorf("re-apply = %q, %v", stdout, err)
	}
	if _, stderr, err := e.run(t, "apply", "-f", writeFile(t, "b.yaml", fmt.Sprintf(ctURLManifest, "url-ct-2"))); err != nil {
		t.Fatalf("apply(second): %v\nstderr: %s", err, stderr)
	}
	if n := len(e.server.Downloads()); n != 1 {
		t.Errorf("downloads = %d, want the template fetched only once", n)
	}
}

func TestLinuxContainerFailedDownloadCreatesNothing(t *testing.T) {
	t.Run("task failure", func(t *testing.T) {
		e := newEnv(t)
		e.server.FailNext("download", "checksum verification failed")
		_, stderr, err := e.run(t, "apply", "-f", writeFile(t, "ct.yaml", fmt.Sprintf(ctURLManifest, "dl-fail")))
		if err == nil || !strings.Contains(stderr, "checksum verification failed") {
			t.Errorf("apply = %v\nstderr: %s", err, stderr)
		}
		if e.server.SawRequest("POST", "/api2/json/nodes/pve1/lxc") {
			t.Error("the container was created after a failed download")
		}
	})
	t.Run("forbidden", func(t *testing.T) {
		e := newEnv(t)
		e.server.DenyDownload(true)
		_, stderr, err := e.run(t, "apply", "-f", writeFile(t, "ct.yaml", fmt.Sprintf(ctURLManifest, "dl-403")))
		if err == nil || !strings.Contains(stderr, "403") || !strings.Contains(stderr, "Datastore.AllocateTemplate") {
			t.Errorf("apply = %v\nstderr: %s", err, stderr)
		}
		if e.server.SawRequest("POST", "/api2/json/nodes/pve1/lxc") {
			t.Error("the container was created after a refused download")
		}
	})
}

func TestLinuxContainerServerDryRunCreatesNothing(t *testing.T) {
	e := newEnv(t)
	stdout, stderr, err := e.run(t, "apply", "-f", writeFile(t, "ct.yaml", fmt.Sprintf(ctURLManifest, "dry-ct")), "--dry-run=server")
	if err != nil {
		t.Fatalf("apply --dry-run=server: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "linuxcontainer/dry-ct created (server dry run)") || !strings.Contains(stderr, "would be downloaded") {
		t.Errorf("stdout = %q\nstderr = %q", stdout, stderr)
	}
	if len(e.server.Downloads()) != 0 || e.server.SawRequest("POST", "/api2/json/nodes/pve1/lxc") {
		t.Errorf("server dry run wrote: %v", e.server.Requests())
	}
}

func TestLinuxContainerRequiresUnprivileged(t *testing.T) {
	e := newEnv(t)
	seedTemplate(e)
	doc := strings.Replace(ctManifest, "  unprivileged: true\n", "", 1)
	_, stderr, err := e.run(t, "apply", "-f", writeFile(t, "ct.yaml", doc))
	if err == nil || !strings.Contains(stderr, "spec.unprivileged is required") {
		t.Errorf("apply = %v\nstderr: %s", err, stderr)
	}
	if e.server.Guest(300) != nil {
		t.Error("a container was created without spec.unprivileged")
	}
}

// TestLinuxContainerRootSecretOnlyAtCreate: the manifest kept as it was
// at create time applies again on a machine without the secret.
func TestLinuxContainerRootSecretOnlyAtCreate(t *testing.T) {
	e := newEnv(t)
	seedTemplate(e)
	doc := ctManifest + `  root:
    passwordFrom: env:PVECTL_E2E_CT_ROOT
    sshKeys: [ssh-ed25519 AAAA e2e@example.test]
`
	path := writeFile(t, "ct.yaml", doc)

	t.Setenv("PVECTL_E2E_CT_ROOT", "e2e-secret")
	if _, stderr, err := e.run(t, "apply", "-f", path); err != nil {
		t.Fatalf("apply(create): %v\nstderr: %s", err, stderr)
	}
	if cfg := e.server.Guest(300).Config; cfg["password"] != nil || cfg["ssh-public-keys"] != nil {
		t.Errorf("credentials stored in the config: %v", cfg)
	}

	if err := os.Unsetenv("PVECTL_E2E_CT_ROOT"); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := e.run(t, "apply", "-f", path)
	if err != nil || !strings.Contains(stdout, "unchanged") {
		t.Fatalf("re-apply without the secret = %q, %v\nstderr: %s", stdout, err, stderr)
	}
	if !strings.Contains(stderr, "applied only at create") {
		t.Errorf("stderr = %q, want the spec.root warning", stderr)
	}
}

// TestLinuxContainerIgnoresVMOfSameName checks the container side of
// name resolution: it looks among lxc guests only. newEnv seeds a VM
// named template-ubuntu.
func TestLinuxContainerIgnoresVMOfSameName(t *testing.T) {
	e := newEnv(t)
	seedTemplate(e)
	doc := strings.Replace(ctManifest, "name: e2e-ct", "name: template-ubuntu", 1)

	stdout, stderr, err := e.run(t, "apply", "-f", writeFile(t, "ct.yaml", doc))
	if err != nil || !strings.Contains(stdout, "linuxcontainer/template-ubuntu created") {
		t.Fatalf("apply = %q, %v\nstderr: %s", stdout, err, stderr)
	}
	stdout, _, err = e.run(t, "get", "ct", "template-ubuntu", "-o", "yaml")
	if err != nil || !strings.Contains(stdout, "vmid: 300") {
		t.Errorf("get ct = %q, %v", stdout, err)
	}
	if _, _, err := e.run(t, "delete", "ct", "template-ubuntu"); err != nil {
		t.Fatal(err)
	}
	if e.server.Guest(300) != nil || e.server.Guest(100) == nil {
		t.Errorf("delete ct removed the wrong guest: ct=%v vm=%v", e.server.Guest(300), e.server.Guest(100))
	}
	if _, _, err := e.run(t, "get", "ct", "template-ubuntu"); err == nil {
		t.Error("get ct found the VM after the container was deleted")
	}
}

package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cyokozai/pvectl/internal/api/apitest"
	"github.com/cyokozai/pvectl/internal/resource"
	"github.com/cyokozai/pvectl/internal/runtime"
)

// manifest builds an Unstructured from YAML for apply tests.
func manifest(t *testing.T, yamlDoc string) *runtime.Unstructured {
	t.Helper()
	objs, err := runtime.Decode(strings.NewReader(yamlDoc), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("expected 1 object, got %d", len(objs))
	}
	return objs[0]
}

const newVMManifest = `
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
  disks:
    - name: scsi0
      size: 16G
      storage: local-lvm
`

// existingVMManifest matches liveConfig()/seededFake() exactly.
const existingVMManifest = `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server
spec:
  targetNode: pve1
  vmid: 100
  description: test vm
  resources:
    cpu:
      cores: 2
      sockets: 1
      type: host
    memory: 2048
  disks:
    - name: scsi0
      size: 32G
      storage: local-lvm
      format: qcow2
      cache: writeback
  networks:
    - name: net0
      bridge: vmbr0
      model: virtio
      tag: 100
  cloudInit:
    user: admin
    sshKeys:
      - ssh-ed25519 AAAA test@example
    ipConfig: ip=10.0.0.5/24,gw=10.0.0.1
    nameserver: 1.1.1.1
  runStrategy: Always
  tags: [web, prod]
`

func apply(t *testing.T, f *apitest.Fake, doc string, opts resource.ApplyOptions) (*resource.ApplyResult, error) {
	t.Helper()
	return NewHandler().Apply(context.Background(), f, manifest(t, doc), opts)
}

func TestApplyCreate(t *testing.T) {
	t.Run("without vmid uses next free id", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, newVMManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionCreated || res.Name != "new-vm" {
			t.Errorf("result = %+v", res)
		}
		if len(f.Creates) != 1 {
			t.Fatalf("Creates = %+v", f.Creates)
		}
		c := f.Creates[0]
		if c.VMID != 105 || c.Node != "pve1" {
			t.Errorf("create call = %+v, want vmid 105 on pve1", c)
		}
		if c.Params["scsi0"] != "local-lvm:16" || c.Params["cores"] != 2 {
			t.Errorf("create params = %+v", c.Params)
		}
	})

	t.Run("with pinned vmid", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(newVMManifest, "targetNode: pve1", "targetNode: pve1\n  vmid: 300", 1)
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Creates) != 1 || f.Creates[0].VMID != 300 {
			t.Errorf("Creates = %+v, want vmid 300", f.Creates)
		}
	})

	t.Run("name collision with different vmid errors", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "vmid: 100", "vmid: 999", 1)
		_, err := apply(t, f, doc, resource.ApplyOptions{})
		if err == nil || !strings.Contains(err.Error(), "100") {
			t.Fatalf("Apply() error = %v, want vmid conflict naming 100", err)
		}
	})
}

func TestApplyClone(t *testing.T) {
	cloneManifest := `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: cloned-vm
spec:
  targetNode: pve2
  clone: web-server
  fullClone: true
  resources:
    cpu:
      cores: 4
    memory: 8192
`
	t.Run("clones then reconfigures", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, cloneManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionCreated {
			t.Errorf("Action = %v", res.Action)
		}
		if len(f.Clones) != 1 {
			t.Fatalf("Clones = %+v", f.Clones)
		}
		cl := f.Clones[0]
		if cl.Src.VMID != 100 || cl.Params["target"] != "pve2" || cl.Params["full"] != 1 || cl.Params["newid"] != 105 {
			t.Errorf("clone call = %+v", cl)
		}
		// The defect this design fixes: post-clone reconfiguration.
		if len(f.Updates) != 1 {
			t.Fatalf("Updates = %+v, want post-clone config PUT", f.Updates)
		}
		up := f.Updates[0]
		if up.Ref.VMID != 105 || up.Params["cores"] != 4 || up.Params["memory"] != 8192 {
			t.Errorf("post-clone update = %+v", up)
		}
	})

	t.Run("declared disks are warned about and ignored", func(t *testing.T) {
		f := seededFake()
		doc := cloneManifest + `
  disks:
    - name: scsi0
      size: 64G
      storage: local-lvm
`
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, " "), "disk") {
			t.Errorf("Warnings = %+v, want disk warning", res.Warnings)
		}
		if _, has := f.Clones[0].Params["scsi0"]; has {
			t.Error("clone params must not contain disks")
		}
	})

	t.Run("missing clone source errors", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(cloneManifest, "clone: web-server", "clone: ghost-template", 1)
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err == nil {
			t.Fatal("Apply() error = nil, want missing source error")
		}
	})
}

func TestApplyUpdate(t *testing.T) {
	t.Run("unchanged", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, existingVMManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionUnchanged {
			t.Errorf("Action = %v, want unchanged (diff: %+v)", res.Action, res.Diff)
		}
		if len(f.Updates) != 0 || len(f.Creates) != 0 {
			t.Errorf("no mutations expected: updates=%+v creates=%+v", f.Updates, f.Creates)
		}
	})

	t.Run("changed keys only are written", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "cores: 2", "cores: 4", 1)
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionConfigured {
			t.Errorf("Action = %v", res.Action)
		}
		if len(f.Updates) != 1 {
			t.Fatalf("Updates = %+v", f.Updates)
		}
		params := f.Updates[0].Params
		if params["cores"] != 4 {
			t.Errorf("params[cores] = %#v", params["cores"])
		}
		if _, has := params["memory"]; has {
			t.Error("unchanged memory must not be in the PUT payload")
		}
		if res.Diff == nil || len(res.Diff.Entries) != 1 || res.Diff.Entries[0].Key != "cores" {
			t.Errorf("Diff = %+v", res.Diff)
		}
	})

	t.Run("disk grow issues a resize call", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "size: 32G", "size: 64G", 1)
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionConfigured {
			t.Errorf("Action = %v", res.Action)
		}
		if len(f.Updates) != 0 {
			t.Errorf("Updates = %+v, want none (size-only change)", f.Updates)
		}
		if len(f.Raws) != 1 || f.Raws[0].Method != "PUT" || !strings.HasSuffix(f.Raws[0].Path, "/nodes/pve1/qemu/100/resize") {
			t.Fatalf("Raws = %+v, want one resize PUT", f.Raws)
		}
		if f.Raws[0].Params["disk"] != "scsi0" || f.Raws[0].Params["size"] != "64G" {
			t.Errorf("resize params = %+v", f.Raws[0].Params)
		}
	})

	t.Run("disk shrink errors", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "size: 32G", "size: 16G", 1)
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "shrink") {
			t.Fatalf("Apply() error = %v, want shrink error", err)
		}
	})

	t.Run("disk storage change errors", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "storage: local-lvm\n      format: qcow2", "storage: other-pool\n      format: qcow2", 1)
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "storage") {
			t.Fatalf("Apply() error = %v, want storage error", err)
		}
	})

	t.Run("disk option change rewrites the volume value", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "cache: writeback", "cache: none", 1)
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Updates) != 1 {
			t.Fatalf("Updates = %+v", f.Updates)
		}
		got, _ := f.Updates[0].Params["scsi0"].(string)
		if !strings.HasPrefix(got, "local-lvm:vm-100-disk-0") || !strings.Contains(got, "cache=none") {
			t.Errorf("scsi0 = %q, want existing volume with cache=none", got)
		}
	})

	t.Run("targetNode mismatch is immutable", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "targetNode: pve1", "targetNode: pve9", 1)
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "pve9") {
			t.Fatalf("Apply() error = %v, want immutable targetNode error", err)
		}
	})

	t.Run("create-only fields warn on update", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "vmid: 100", "vmid: 100\n  pool: some-pool", 1)
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, " "), "pool") {
			t.Errorf("Warnings = %+v, want pool warning", res.Warnings)
		}
	})
}

func TestApplyRaw(t *testing.T) {
	t.Run("raw keys reach the create payload", func(t *testing.T) {
		f := seededFake()
		doc := newVMManifest + `
  raw:
    hookscript: "local:snippets/hook.pl"
    bios: ovmf
`
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Creates) != 1 {
			t.Fatalf("Creates = %+v", f.Creates)
		}
		p := f.Creates[0].Params
		if p["hookscript"] != "local:snippets/hook.pl" || p["bios"] != "ovmf" {
			t.Errorf("create params = %+v", p)
		}
	})

	t.Run("only declared raw keys are diffed and written", func(t *testing.T) {
		f := seededFake()
		f.Configs[100]["bios"] = "seabios"
		f.Configs[100]["hotplug"] = "disk,network"
		doc := existingVMManifest + `
  raw:
    bios: ovmf
    hotplug: "disk,network"
`
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionConfigured {
			t.Fatalf("Action = %v (diff %+v)", res.Action, res.Diff)
		}
		if len(f.Updates) != 1 {
			t.Fatalf("Updates = %+v", f.Updates)
		}
		p := f.Updates[0].Params
		if p["bios"] != "ovmf" {
			t.Errorf("params[bios] = %#v, want ovmf", p["bios"])
		}
		if _, has := p["hotplug"]; has {
			t.Error("unchanged raw key must not be in the PUT payload")
		}
	})

	t.Run("undeclared live keys stay unmanaged", func(t *testing.T) {
		f := seededFake()
		f.Configs[100]["hookscript"] = "local:snippets/other.pl"
		res, err := apply(t, f, existingVMManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionUnchanged {
			t.Errorf("Action = %v, want unchanged (diff %+v)", res.Action, res.Diff)
		}
	})

	t.Run("duplicating a typed key errors", func(t *testing.T) {
		doc := existingVMManifest + `
  raw:
    cores: "8"
    scsi0: "local-lvm:99"
`
		_, err := apply(t, seededFake(), doc, resource.ApplyOptions{})
		if err == nil {
			t.Fatal("Apply() error = nil, want raw conflict error")
		}
		for _, want := range []string{"spec.raw.cores", "spec.raw.scsi0"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err.Error(), want)
			}
		}
	})
}

func TestApplyRunStrategy(t *testing.T) {
	// existingVMManifest declares Always and vmid 100, which seededFake
	// reports as already running.
	halted := strings.Replace(existingVMManifest, "runStrategy: Always", "runStrategy: Halted", 1)
	manual := strings.Replace(existingVMManifest, "runStrategy: Always", "runStrategy: Manual", 1)
	undeclared := strings.Replace(existingVMManifest, "  runStrategy: Always\n", "", 1)

	t.Run("Always starts a stopped VM", func(t *testing.T) {
		f := seededFake()
		f.GuestList[0].Status = "stopped"
		res, err := apply(t, f, existingVMManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Starts) != 1 || f.Starts[0].VMID != 100 {
			t.Fatalf("Starts = %+v, want one start of vmid 100", f.Starts)
		}
		// A power transition is a change even when the config matched.
		if res.Action != resource.ActionConfigured {
			t.Errorf("Action = %v, want configured", res.Action)
		}
	})

	t.Run("Always leaves a running VM alone", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, existingVMManifest, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Starts) != 0 || res.Action != resource.ActionUnchanged {
			t.Errorf("Starts = %+v, Action = %v", f.Starts, res.Action)
		}
	})

	t.Run("Halted stops a running VM and sets onboot=0", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, halted, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Stops) != 1 || f.Stops[0].VMID != 100 {
			t.Fatalf("Stops = %+v, want one stop of vmid 100", f.Stops)
		}
		if len(f.Updates) != 1 || f.Updates[0].Params["onboot"] != 0 {
			t.Errorf("Updates = %+v, want onboot=0", f.Updates)
		}
		if res.Action != resource.ActionConfigured {
			t.Errorf("Action = %v", res.Action)
		}
	})

	t.Run("Manual never touches the power state", func(t *testing.T) {
		for label, doc := range map[string]string{"explicit": manual, "omitted": undeclared} {
			f := seededFake()
			res, err := apply(t, f, doc, resource.ApplyOptions{})
			if err != nil {
				t.Fatalf("%s: Apply() error = %v", label, err)
			}
			if len(f.Starts)+len(f.Stops) != 0 {
				t.Errorf("%s: power calls = %+v %+v", label, f.Starts, f.Stops)
			}
			// onboot stays unmanaged: live has onboot=1 and the manifest
			// does not declare it, so it is not a diff.
			if res.Action != resource.ActionUnchanged {
				t.Errorf("%s: Action = %v, want unchanged (diff %+v)", label, res.Action, res.Diff)
			}
		}
	})

	t.Run("Always starts a freshly created VM", func(t *testing.T) {
		f := seededFake()
		doc := newVMManifest + "  runStrategy: Always\n"
		if _, err := apply(t, f, doc, resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if len(f.Starts) != 1 || f.Starts[0].VMID != 105 {
			t.Errorf("Starts = %+v, want the new vmid started", f.Starts)
		}
		if f.Creates[0].Params["onboot"] != 1 {
			t.Errorf("create params onboot = %#v", f.Creates[0].Params["onboot"])
		}
	})

	t.Run("server dry-run reports the transition without performing it", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, halted, resource.ApplyOptions{DryRun: resource.DryRunServer})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionConfigured {
			t.Errorf("Action = %v", res.Action)
		}
		if len(f.Stops) != 0 {
			t.Error("server dry-run must not change the power state")
		}
	})

	t.Run("invalid value errors", func(t *testing.T) {
		doc := strings.Replace(existingVMManifest, "runStrategy: Always", "runStrategy: RunOnce", 1)
		_, err := apply(t, seededFake(), doc, resource.ApplyOptions{})
		if err == nil || !strings.Contains(err.Error(), "spec.runStrategy") {
			t.Fatalf("Apply() error = %v, want runStrategy validation error", err)
		}
	})

	t.Run("the removed startOnBoot field points at runStrategy", func(t *testing.T) {
		doc := strings.Replace(existingVMManifest, "runStrategy: Always", "startOnBoot: true", 1)
		_, err := apply(t, seededFake(), doc, resource.ApplyOptions{})
		if err == nil || !strings.Contains(err.Error(), "runStrategy") {
			t.Fatalf("Apply() error = %v, want a hint naming runStrategy", err)
		}
	})
}

func TestApplyPasswordFrom(t *testing.T) {
	withPassword := func(ref string) string {
		return strings.Replace(newVMManifest, "  disks:",
			"  cloudInit:\n    user: admin\n    passwordFrom: "+ref+"\n  disks:", 1)
	}

	t.Run("env reference reaches cipassword on create", func(t *testing.T) {
		t.Setenv("PVECTL_TEST_VM_PASSWORD", "s3cret")
		f := seededFake()
		if _, err := apply(t, f, withPassword("env:PVECTL_TEST_VM_PASSWORD"), resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if got := f.Creates[0].Params["cipassword"]; got != "s3cret" {
			t.Errorf("cipassword = %#v, want the resolved value", got)
		}
	})

	t.Run("file reference reaches cipassword on create", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "vmpw")
		if err := os.WriteFile(path, []byte("from-file\n"), 0600); err != nil {
			t.Fatal(err)
		}
		f := seededFake()
		if _, err := apply(t, f, withPassword("file:"+path), resource.ApplyOptions{}); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if got := f.Creates[0].Params["cipassword"]; got != "from-file" {
			t.Errorf("cipassword = %#v, want the file contents without the newline", got)
		}
	})

	t.Run("unresolvable reference errors on a real apply", func(t *testing.T) {
		_, err := apply(t, seededFake(), withPassword("env:PVECTL_TEST_VM_MISSING"), resource.ApplyOptions{})
		if err == nil || !strings.Contains(err.Error(), "passwordFrom") {
			t.Fatalf("Apply() error = %v, want an unresolved secret error", err)
		}
	})

	t.Run("client dry-run warns instead of failing", func(t *testing.T) {
		res, err := apply(t, seededFake(), withPassword("env:PVECTL_TEST_VM_MISSING"),
			resource.ApplyOptions{DryRun: resource.DryRunClient})
		if err != nil {
			t.Fatalf("Apply() error = %v, want the manifest to validate", err)
		}
		if res.Action != resource.ActionValidated {
			t.Errorf("Action = %v", res.Action)
		}
		if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "passwordFrom") {
			t.Errorf("Warnings = %+v, want one unresolvable-reference warning", res.Warnings)
		}
	})

	t.Run("bad syntax fails even under client dry-run", func(t *testing.T) {
		_, err := apply(t, seededFake(), withPassword("hunter2"), resource.ApplyOptions{DryRun: resource.DryRunClient})
		if err == nil || !strings.Contains(err.Error(), "passwordFrom") {
			t.Fatalf("Apply() error = %v, want a syntax error", err)
		}
	})

	t.Run("cipassword stays out of diffs and updates", func(t *testing.T) {
		t.Setenv("PVECTL_TEST_VM_PASSWORD", "s3cret")
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "    user: admin",
			"    user: admin\n    passwordFrom: env:PVECTL_TEST_VM_PASSWORD", 1)
		res, err := apply(t, f, doc, resource.ApplyOptions{})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		// The API masks cipassword; the write-only rule keeps the mask
		// from showing up as a permanent diff.
		if res.Action != resource.ActionUnchanged {
			t.Errorf("Action = %v, want unchanged (diff %+v)", res.Action, res.Diff)
		}
	})

	t.Run("the removed password field points at passwordFrom", func(t *testing.T) {
		doc := strings.Replace(existingVMManifest, "    user: admin", "    user: admin\n    password: hunter2", 1)
		_, err := apply(t, seededFake(), doc, resource.ApplyOptions{})
		if err == nil || !strings.Contains(err.Error(), "passwordFrom") {
			t.Fatalf("Apply() error = %v, want a hint naming passwordFrom", err)
		}
	})
}

func TestApplyDryRun(t *testing.T) {
	t.Run("client dry-run makes no API calls", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, newVMManifest, resource.ApplyOptions{DryRun: resource.DryRunClient})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionValidated {
			t.Errorf("Action = %v", res.Action)
		}
		if len(f.Creates)+len(f.Updates)+len(f.Clones)+len(f.Raws) != 0 {
			t.Error("client dry-run must not call the API")
		}
	})

	t.Run("server dry-run reports create without writing", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, newVMManifest, resource.ApplyOptions{DryRun: resource.DryRunServer})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionCreated || len(f.Creates) != 0 {
			t.Errorf("result = %+v, creates = %+v", res, f.Creates)
		}
	})

	t.Run("server dry-run reports diff without writing", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existingVMManifest, "cores: 2", "cores: 8", 1)
		res, err := apply(t, f, doc, resource.ApplyOptions{DryRun: resource.DryRunServer})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if res.Action != resource.ActionConfigured || res.Diff == nil || len(res.Diff.Entries) == 0 {
			t.Errorf("result = %+v", res)
		}
		if len(f.Updates) != 0 {
			t.Error("server dry-run must not write")
		}
	})
}

func TestApplyValidation(t *testing.T) {
	invalid := `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: bad-vm
spec:
  resources:
    memory: 0
`
	_, err := apply(t, seededFake(), invalid, resource.ApplyOptions{})
	if err == nil {
		t.Fatal("Apply() error = nil, want validation error")
	}
	for _, want := range []string{"targetNode", "cores", "memory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validation error %q missing %q", err.Error(), want)
		}
	}
}

func TestApplyUnknownSpecField(t *testing.T) {
	doc := strings.Replace(newVMManifest, "targetNode: pve1", "targetNode: pve1\n  bogusField: true", 1)
	if _, err := apply(t, seededFake(), doc, resource.ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "bogusField") {
		t.Fatalf("Apply() error = %v, want unknown field error", err)
	}
}

func TestHandlerDiff(t *testing.T) {
	h := NewHandler()

	t.Run("existing with change", func(t *testing.T) {
		doc := strings.Replace(existingVMManifest, "cores: 2", "cores: 8", 1)
		res, err := h.Diff(context.Background(), seededFake(), manifest(t, doc))
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if len(res.Entries) != 1 || res.Entries[0].Key != "cores" || res.Entries[0].Desired != "8" {
			t.Errorf("Diff = %+v", res.Entries)
		}
	})

	t.Run("missing VM shows all desired keys as additions", func(t *testing.T) {
		res, err := h.Diff(context.Background(), seededFake(), manifest(t, newVMManifest))
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if res.Empty() {
			t.Fatal("Diff on missing VM must not be empty")
		}
		for _, e := range res.Entries {
			if e.Current != "" {
				t.Errorf("entry %+v should have empty Current", e)
			}
		}
	})
}

func TestGetYAMLRoundTrip(t *testing.T) {
	// get -o yaml output must decode and apply as unchanged.
	h := NewHandler()
	f := seededFake()
	obj, err := h.Get(context.Background(), f, "web-server")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	data, err := yaml.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	res, err := apply(t, f, string(data), resource.ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply(get output) error = %v\nyaml:\n%s", err, data)
	}
	if res.Action != resource.ActionUnchanged {
		t.Errorf("round-trip Action = %v, want unchanged (diff %+v)\nyaml:\n%s", res.Action, res.Diff, data)
	}
}

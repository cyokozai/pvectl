package vm

import (
	"context"
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
  startOnBoot: true
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

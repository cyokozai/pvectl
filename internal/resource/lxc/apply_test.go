package lxc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
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

const volidTemplate = "local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst"

// newCT renders a manifest for a container that does not exist yet.
func newCT(extra string) string {
	return `
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata:
  name: new-ct
spec:
  targetNode: pve1
  unprivileged: true
  resources:
    cores: 1
    memory: 512
  rootfs:
    storage: local-lvm
    size: 8G
` + extra
}

const volidForm = `  osTemplate:
    volid: ` + volidTemplate + "\n"

var urlForm = `  osTemplate:
    url: https://download.example/debian-12-standard_12.7-1_amd64.tar.zst
    sha256: ` + testSHA + `
    storage: local
`

// derivedTemplate is what urlForm resolves to (ADR-010 §4).
const derivedTemplate = "local:vztmpl/pvectl-3f2a9c0d1e4b5a69-debian-12-standard_12.7-1_amd64.tar.zst"

// existingCT is fullSpec() in manifest form, matching liveConfig().
var existingCT = `
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata:
  name: web-01
spec:
  targetNode: pve1
  vmid: 200
  unprivileged: true
  pool: lab
  description: test ct
  resources:
    cores: %d
    memory: 1024
    swap: 512
  rootfs:
    storage: local-lvm
    size: %s
  mountPoints:
    - name: mp0
      storage: local-lvm
      size: 32G
      path: /data
  networks:
    - name: net0
      bridge: vmbr0
      ip: dhcp
    - name: net1
      interface: lan
      bridge: vmbr1
      ip: 10.0.0.5/24
      gw: 10.0.0.1
      tag: 10
  nameserver: 10.0.0.53
  features:
    nesting: true
    keyctl: true
  runStrategy: Always
  tags: [prod, web]
`

func existing(cores int, rootfs string) string { return fmt.Sprintf(existingCT, cores, rootfs) }

func apply(t *testing.T, f *apitest.Fake, doc string, mode resource.DryRunMode) (*resource.ApplyResult, error) {
	t.Helper()
	return NewHandler().Apply(context.Background(), f, manifest(t, doc), resource.ApplyOptions{DryRun: mode})
}

func templateFake() *apitest.Fake {
	f := seededFake()
	f.AddVolume("pve1", "local", api.StorageVolume{VolID: volidTemplate, Content: "vztmpl"})
	return f
}

func TestApplyCreateFromVolID(t *testing.T) {
	f := templateFake()
	res, err := apply(t, f, newCT(volidForm+"  vmid: 300\n"), resource.DryRunNone)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != resource.ActionCreated {
		t.Errorf("action = %s", res.Action)
	}
	if len(f.LxcCreates) != 1 {
		t.Fatalf("creates = %+v", f.LxcCreates)
	}
	c := f.LxcCreates[0]
	if c.Node != "pve1" || c.VMID != 300 || c.Params["ostemplate"] != volidTemplate || c.Params["unprivileged"] != 1 || c.Params["hostname"] != "new-ct" {
		t.Errorf("create = %+v", c)
	}
	if len(f.Downloads) != 0 || len(f.Starts) != 0 {
		t.Errorf("downloads %v starts %v, want none", f.Downloads, f.Starts)
	}
}

func TestApplyCreateUsesNextID(t *testing.T) {
	f := templateFake()
	if _, err := apply(t, f, newCT(volidForm), resource.DryRunNone); err != nil {
		t.Fatal(err)
	}
	if len(f.LxcCreates) != 1 || f.LxcCreates[0].VMID != 300 {
		t.Errorf("creates = %+v, want the next free id 300", f.LxcCreates)
	}
}

func TestApplyCreateDownloadsOnce(t *testing.T) {
	f := seededFake()
	if _, err := apply(t, f, newCT(urlForm), resource.DryRunNone); err != nil {
		t.Fatal(err)
	}
	if len(f.Downloads) != 1 {
		t.Fatalf("downloads = %+v, want 1", f.Downloads)
	}
	dl := f.Downloads[0]
	if dl.Node != "pve1" || dl.Storage != "local" || dl.Params["checksum"] != testSHA || dl.Params["checksum-algorithm"] != "sha256" {
		t.Errorf("download = %+v", dl)
	}
	if got := f.LxcCreates[0].Params["ostemplate"]; got != derivedTemplate {
		t.Errorf("ostemplate = %v, want %s", got, derivedTemplate)
	}
	if got := fmt.Sprintf("local:vztmpl/%v", dl.Params["filename"]); got != derivedTemplate {
		t.Errorf("downloaded %s, but created from %s", got, derivedTemplate)
	}

	// A second container from the same template finds it on storage.
	doc := strings.Replace(newCT(urlForm), "name: new-ct", "name: second-ct", 1)
	if _, err := apply(t, f, doc, resource.DryRunNone); err != nil {
		t.Fatal(err)
	}
	if len(f.Downloads) != 1 {
		t.Errorf("downloads = %d, want the template fetched only once", len(f.Downloads))
	}
}

func TestApplyCreateDownloadFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"plain", errors.New("checksum verification failed"), []string{"checksum verification failed"}},
		{"forbidden", errors.New("failed to download x: api error: code: 403 message: "),
			[]string{"403", "Datastore.AllocateTemplate", "/storage/local", "Sys.Modify"}},
		{"timeout", errors.New("failed to download x: Wait timeout for:UPID:pve1:1"),
			[]string{"may still be running", "--timeout"}},
		{"deadline", fmt.Errorf("failed to download x: %w", context.DeadlineExceeded),
			[]string{"may still be running", "--timeout"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := seededFake()
			f.Err = map[string]error{"DownloadURL": tt.err}
			_, err := apply(t, f, newCT(urlForm), resource.DryRunNone)
			if err == nil {
				t.Fatal("apply succeeded, want the download failure")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want it to mention %q", err, want)
				}
			}
			if len(f.LxcCreates) != 0 {
				t.Errorf("created %+v after a failed download", f.LxcCreates)
			}
		})
	}
}

func TestApplyCreateNeedsOSTemplate(t *testing.T) {
	f := seededFake()
	_, err := apply(t, f, newCT(""), resource.DryRunNone)
	if err == nil || !strings.Contains(err.Error(), "spec.osTemplate is required") {
		t.Errorf("error = %v", err)
	}
	if len(f.LxcCreates) != 0 {
		t.Error("created without a template")
	}
}

func TestApplyServerDryRunCreate(t *testing.T) {
	t.Run("url not on storage", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, newCT(urlForm), resource.DryRunServer)
		if err != nil {
			t.Fatal(err)
		}
		if res.Action != resource.ActionCreated || !strings.Contains(strings.Join(res.Warnings, "\n"), "would be downloaded") {
			t.Errorf("result = %+v", res)
		}
		if len(f.Downloads) != 0 || len(f.LxcCreates) != 0 {
			t.Errorf("server dry run wrote: downloads %v creates %v", f.Downloads, f.LxcCreates)
		}
	})
	t.Run("url already on storage", func(t *testing.T) {
		f := seededFake()
		f.AddVolume("pve1", "local", api.StorageVolume{VolID: derivedTemplate, Content: "vztmpl"})
		res, err := apply(t, f, newCT(urlForm), resource.DryRunServer)
		if err != nil || len(res.Warnings) != 0 {
			t.Errorf("result = %+v, %v; want no warning", res, err)
		}
	})
	t.Run("volid missing", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, newCT(volidForm), resource.DryRunServer)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(res.Warnings, "\n"), "is not on storage local") {
			t.Errorf("warnings = %v", res.Warnings)
		}
	})
}

const rootSecret = `  root:
    passwordFrom: env:PVECTL_TEST_CT_ROOT_UNSET
    sshKeys: [ssh-ed25519 AAAA a@example]
`

func TestApplyRootSecret(t *testing.T) {
	t.Run("client dry run warns", func(t *testing.T) {
		res, err := apply(t, nil, newCT(volidForm+rootSecret), resource.DryRunClient)
		if err != nil {
			t.Fatal(err)
		}
		if res.Action != resource.ActionValidated || !strings.Contains(strings.Join(res.Warnings, "\n"), "not resolvable here") {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("create fails", func(t *testing.T) {
		f := templateFake()
		if _, err := apply(t, f, newCT(volidForm+rootSecret), resource.DryRunNone); err == nil || !strings.Contains(err.Error(), "spec.root.passwordFrom") {
			t.Errorf("error = %v", err)
		}
		if len(f.LxcCreates) != 0 {
			t.Error("created without the root password")
		}
	})
	t.Run("create sends it", func(t *testing.T) {
		t.Setenv("PVECTL_TEST_CT_ROOT_SET", "s3cret")
		f := templateFake()
		doc := newCT(volidForm + strings.Replace(rootSecret, "UNSET", "SET", 1))
		if _, err := apply(t, f, doc, resource.DryRunNone); err != nil {
			t.Fatal(err)
		}
		if p := f.LxcCreates[0].Params; p["password"] != "s3cret" || p["ssh-public-keys"] != "ssh-ed25519 AAAA a@example" {
			t.Errorf("create params = %+v", p)
		}
	})
	t.Run("update never resolves it", func(t *testing.T) {
		f := seededFake()
		res, err := apply(t, f, existing(2, "8G")+rootSecret, resource.DryRunNone)
		if err != nil {
			t.Fatalf("apply(update) error = %v; the secret must not be needed after create", err)
		}
		if res.Action != resource.ActionUnchanged || !strings.Contains(strings.Join(res.Warnings, "\n"), "applied only at create") {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestApplyUnchanged(t *testing.T) {
	f := seededFake()
	res, err := apply(t, f, existing(2, "8G"), resource.DryRunNone)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != resource.ActionUnchanged {
		t.Errorf("action = %s, diff = %+v", res.Action, res.Diff)
	}
	if len(f.LxcUpdates) != 0 || len(f.Raws) != 0 || len(f.Starts) != 0 {
		t.Errorf("unchanged apply wrote: %v %v %v", f.LxcUpdates, f.Raws, f.Starts)
	}
}

func TestApplyUpdateConfigAndResize(t *testing.T) {
	f := seededFake()
	res, err := apply(t, f, existing(4, "16G"), resource.DryRunNone)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != resource.ActionConfigured {
		t.Errorf("action = %s", res.Action)
	}
	if len(f.LxcUpdates) != 1 || f.LxcUpdates[0].Params["cores"] != 4 || f.LxcUpdates[0].Ref != (api.GuestRef{VMID: 200, Node: "pve1", Type: "lxc"}) {
		t.Errorf("updates = %+v", f.LxcUpdates)
	}
	if _, has := f.LxcUpdates[0].Params["rootfs"]; has {
		t.Error("a size-only change must go through resize, not the config PUT")
	}
	if len(f.Raws) != 1 || f.Raws[0].Method != "PUT" || f.Raws[0].Path != "/nodes/pve1/lxc/200/resize" ||
		f.Raws[0].Params["disk"] != "rootfs" || f.Raws[0].Params["size"] != "16G" {
		t.Errorf("raws = %+v", f.Raws)
	}
}

func TestApplyServerDryRunUpdateWritesNothing(t *testing.T) {
	f := seededFake()
	res, err := apply(t, f, existing(4, "16G"), resource.DryRunServer)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != resource.ActionConfigured || res.Diff == nil || res.Diff.Empty() {
		t.Errorf("result = %+v", res)
	}
	if len(f.LxcUpdates) != 0 || len(f.Raws) != 0 {
		t.Errorf("server dry run wrote: %v %v", f.LxcUpdates, f.Raws)
	}
}

func TestApplyUpdateRejects(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"other node", strings.Replace(existing(2, "8G"), "targetNode: pve1", "targetNode: pve2", 1), "apply does not migrate"},
		{"pool", strings.Replace(existing(2, "8G"), "pool: lab", "pool: other", 1), "spec.pool is create-only"},
		{"unprivileged", strings.Replace(existing(2, "8G"), "unprivileged: true", "unprivileged: false", 1), "spec.unprivileged is create-only"},
		{"shrink", existing(2, "4G"), "cannot shrink"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := seededFake()
			_, err := apply(t, f, tt.doc, resource.DryRunNone)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			if len(f.LxcUpdates) != 0 || len(f.Raws) != 0 {
				t.Errorf("rejected apply wrote: %v %v", f.LxcUpdates, f.Raws)
			}
		})
	}
}

func TestApplyPowerConvergence(t *testing.T) {
	t.Run("create with Always starts it", func(t *testing.T) {
		f := templateFake()
		if _, err := apply(t, f, newCT(volidForm+"  runStrategy: Always\n"), resource.DryRunNone); err != nil {
			t.Fatal(err)
		}
		if len(f.Starts) != 1 || f.Starts[0].VMID != 300 || f.Starts[0].Type != "lxc" {
			t.Errorf("starts = %+v", f.Starts)
		}
		if _, has := f.LxcCreates[0].Params["start"]; has {
			t.Error("create must not ask Proxmox to start the container itself")
		}
	})
	t.Run("Halted stops a running container", func(t *testing.T) {
		f := seededFake()
		doc := strings.Replace(existing(2, "8G"), "runStrategy: Always", "runStrategy: Halted", 1)
		res, err := apply(t, f, doc, resource.DryRunNone)
		if err != nil {
			t.Fatal(err)
		}
		if res.Action != resource.ActionConfigured || len(f.Stops) != 1 || f.Stops[0].VMID != 200 {
			t.Errorf("result %+v stops %+v", res, f.Stops)
		}
	})
}

// TestApplyResolvesOnlyContainers: a VM's name never stands in for a
// container (ADR-010 §1).
func TestApplyResolvesOnlyContainers(t *testing.T) {
	t.Run("a VM of the same name does not block create", func(t *testing.T) {
		f := templateFake()
		doc := strings.Replace(newCT(volidForm), "name: new-ct", "name: vm-only", 1)
		res, err := apply(t, f, doc, resource.DryRunNone)
		if err != nil || res.Action != resource.ActionCreated {
			t.Fatalf("apply = %+v, %v; want created", res, err)
		}
		if len(f.Updates) != 0 || len(f.LxcUpdates) != 0 {
			t.Error("the VM was touched")
		}
	})
	t.Run("pinned vmid of a VM", func(t *testing.T) {
		_, err := apply(t, templateFake(), newCT(volidForm+"  vmid: 100\n"), resource.DryRunNone)
		if err == nil || !strings.Contains(err.Error(), "not a LinuxContainer") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("pinned free vmid but the name is taken", func(t *testing.T) {
		doc := strings.Replace(newCT(volidForm+"  vmid: 350\n"), "name: new-ct", "name: db", 1)
		_, err := apply(t, templateFake(), doc, resource.DryRunNone)
		if err == nil || !strings.Contains(err.Error(), "already exists as vmid 201") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestDiff(t *testing.T) {
	f := seededFake()
	h := NewHandler()
	d, err := h.Diff(context.Background(), f, manifest(t, existing(2, "8G")))
	if err != nil || !d.Empty() {
		t.Errorf("Diff(unchanged) = %+v, %v", d, err)
	}
	d, err = h.Diff(context.Background(), f, manifest(t, existing(4, "8G")))
	if err != nil || len(d.Entries) != 1 || d.Entries[0].Key != "cores" {
		t.Errorf("Diff(cores) = %+v, %v", d, err)
	}
	d, err = h.Diff(context.Background(), f, manifest(t, newCT(volidForm)))
	if err != nil || d.Empty() {
		t.Errorf("Diff(new) = %+v, %v; want every declared key", d, err)
	}
}

package lxc

import (
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/runtime"
)

// validateErr runs validateSpec and returns its message, or "" when the
// spec is valid.
func validateErr(t *testing.T, spec *Spec) string {
	t.Helper()
	err := validateSpec("x", spec)
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestValidateFullSpec(t *testing.T) {
	if msg := validateErr(t, fullSpec()); msg != "" {
		t.Errorf("fullSpec rejected: %v", msg)
	}
	if msg := validateErr(t, minimalSpec()); msg != "" {
		t.Errorf("minimalSpec rejected: %v", msg)
	}
}

func TestValidateRequired(t *testing.T) {
	err := validateSpec("", &Spec{})
	if err == nil {
		t.Fatal("empty spec accepted")
	}
	for _, want := range []string{"metadata.name", "spec.targetNode", "spec.unprivileged"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestValidateUnprivilegedFalseIsAccepted(t *testing.T) {
	spec := minimalSpec()
	spec.Unprivileged = boolPtr(false)
	if msg := validateErr(t, spec); msg != "" {
		t.Errorf("explicit privileged rejected: %v", msg)
	}
}

func TestValidateOSTemplate(t *testing.T) {
	tests := []struct {
		name string
		tmpl OSTemplate
		want string // "" means valid
	}{
		{"volid", OSTemplate{VolID: "local:vztmpl/debian-12.tar.zst"}, ""},
		{"url", OSTemplate{URL: "https://e.example/d.tar.zst", SHA256: testSHA, Storage: "local"}, ""},
		{"neither", OSTemplate{}, "spec.osTemplate needs either volid or url"},
		{"both", OSTemplate{VolID: "local:vztmpl/a.tar.zst", URL: "https://e.example/d.tar.zst", SHA256: testSHA, Storage: "local"}, "exclusive"},
		{"volid with storage", OSTemplate{VolID: "local:vztmpl/a.tar.zst", Storage: "local"}, "spec.osTemplate.storage"},
		{"volid with sha256", OSTemplate{VolID: "local:vztmpl/a.tar.zst", SHA256: testSHA}, "spec.osTemplate.sha256"},
		{"volid not vztmpl", OSTemplate{VolID: "local:iso/a.iso"}, "storage:vztmpl/"},
		{"volid without storage", OSTemplate{VolID: "vztmpl/a.tar.zst"}, "storage:vztmpl/"},
		{"url without sha256", OSTemplate{URL: "https://e.example/d.tar.zst", Storage: "local"}, "spec.osTemplate.sha256"},
		{"url with short sha256", OSTemplate{URL: "https://e.example/d.tar.zst", SHA256: "abc", Storage: "local"}, "64 hex"},
		{"url with non-hex sha256", OSTemplate{URL: "https://e.example/d.tar.zst", SHA256: strings.Repeat("g", 64), Storage: "local"}, "64 hex"},
		{"url without storage", OSTemplate{URL: "https://e.example/d.tar.zst", SHA256: testSHA}, "spec.osTemplate.storage"},
		{"url not http", OSTemplate{URL: "ftp://e.example/d.tar.zst", SHA256: testSHA, Storage: "local"}, "http"},
		{"url without file name", OSTemplate{URL: "https://e.example/", SHA256: testSHA, Storage: "local"}, "file name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalSpec()
			tmpl := tt.tmpl
			spec.OSTemplate = &tmpl
			msg := validateErr(t, spec)
			if tt.want == "" {
				if msg != "" {
					t.Errorf("rejected: %v", msg)
				}
				return
			}
			if !strings.Contains(msg, tt.want) {
				t.Errorf("validateSpec() = %v, want it to mention %q", msg, tt.want)
			}
		})
	}
}

func TestValidateForCreate(t *testing.T) {
	// get -o yaml omits osTemplate, so it is optional for validateSpec
	// (updates) and required only when the container is created.
	spec := minimalSpec()
	if err := validateForCreate("ct", spec); err == nil || !strings.Contains(err.Error(), "spec.osTemplate") {
		t.Errorf("validateForCreate() = %v, want an osTemplate error", err)
	}
	spec.OSTemplate = &OSTemplate{VolID: "local:vztmpl/a.tar.zst"}
	if err := validateForCreate("ct", spec); err != nil {
		t.Errorf("validateForCreate() = %v, want nil", err)
	}
}

func TestValidateRunStrategy(t *testing.T) {
	for _, s := range []RunStrategy{"", RunStrategyHalted, RunStrategyAlways, RunStrategyManual} {
		spec := minimalSpec()
		spec.RunStrategy = s
		if msg := validateErr(t, spec); msg != "" {
			t.Errorf("runStrategy %q rejected: %v", s, msg)
		}
	}
	for _, s := range []RunStrategy{"always", "Running", "true"} {
		spec := minimalSpec()
		spec.RunStrategy = s
		msg := validateErr(t, spec)
		if !strings.Contains(msg, "spec.runStrategy") || !strings.Contains(msg, "Halted / Always / Manual") {
			t.Errorf("runStrategy %q = %v, want the accepted values listed", s, msg)
		}
	}
}

func TestValidateVMID(t *testing.T) {
	spec := minimalSpec()
	spec.VMID = intPtr(99)
	if msg := validateErr(t, spec); !strings.Contains(msg, "spec.vmid") {
		t.Errorf("validateSpec() = %v, want a vmid error", msg)
	}
}

func TestValidateVolumes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{"rootfs without storage", func(s *Spec) { s.RootFS = &RootFS{Size: "8G"} }, "spec.rootfs.storage"},
		{"rootfs bad size", func(s *Spec) { s.RootFS = &RootFS{Storage: "local-lvm", Size: "big"} }, "spec.rootfs.size"},
		{"mp bad slot", func(s *Spec) {
			s.MountPoints = []MountPoint{{Name: "data", Storage: "local-lvm", Size: "8G", Path: "/data"}}
		}, "spec.mountPoints[0].name"},
		{"mp rootfs slot", func(s *Spec) {
			s.MountPoints = []MountPoint{{Name: "rootfs", Storage: "local-lvm", Size: "8G", Path: "/data"}}
		}, "spec.mountPoints[0].name"},
		{"mp without storage", func(s *Spec) {
			s.MountPoints = []MountPoint{{Name: "mp0", Size: "8G", Path: "/data"}}
		}, "spec.mountPoints[0].storage"},
		{"mp bad size", func(s *Spec) {
			s.MountPoints = []MountPoint{{Name: "mp0", Storage: "local-lvm", Size: "0", Path: "/data"}}
		}, "spec.mountPoints[0].size"},
		{"mp relative path", func(s *Spec) {
			s.MountPoints = []MountPoint{{Name: "mp0", Storage: "local-lvm", Size: "8G", Path: "data"}}
		}, "spec.mountPoints[0].path"},
		{"mp duplicate slot", func(s *Spec) {
			s.MountPoints = []MountPoint{
				{Name: "mp0", Storage: "local-lvm", Size: "8G", Path: "/a"},
				{Name: "mp0", Storage: "local-lvm", Size: "8G", Path: "/b"},
			}
		}, "spec.mountPoints[1].name \"mp0\" is declared more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalSpec()
			tt.mutate(spec)
			if msg := validateErr(t, spec); !strings.Contains(msg, tt.want) {
				t.Errorf("validateSpec() = %v, want it to mention %q", msg, tt.want)
			}
		})
	}
}

func TestValidateNetworks(t *testing.T) {
	valid := []Network{
		{Name: "net0", Bridge: "vmbr0"},
		{Name: "net0", Bridge: "vmbr0", IP: "dhcp"},
		{Name: "net0", Bridge: "vmbr0", IP: "manual"},
		{Name: "net0", Bridge: "vmbr0", IP: "10.0.0.5/24", GW: "10.0.0.1", Tag: 4094, HWAddr: "BC:24:11:00:00:01"},
	}
	for _, n := range valid {
		spec := minimalSpec()
		spec.Networks = []Network{n}
		if msg := validateErr(t, spec); msg != "" {
			t.Errorf("%+v rejected: %v", n, msg)
		}
	}

	invalid := []struct {
		name string
		nets []Network
		want string
	}{
		{"bad slot", []Network{{Name: "eth0", Bridge: "vmbr0"}}, "spec.networks[0].name"},
		{"no bridge", []Network{{Name: "net0"}}, "spec.networks[0].bridge"},
		{"bad ip", []Network{{Name: "net0", Bridge: "vmbr0", IP: "10.0.0.5"}}, "spec.networks[0].ip"},
		{"bad gw", []Network{{Name: "net0", Bridge: "vmbr0", IP: "10.0.0.5/24", GW: "gateway"}}, "spec.networks[0].gw"},
		{"bad tag", []Network{{Name: "net0", Bridge: "vmbr0", Tag: 4095}}, "spec.networks[0].tag"},
		{"bad hwaddr", []Network{{Name: "net0", Bridge: "vmbr0", HWAddr: "nope"}}, "spec.networks[0].hwaddr"},
		{"duplicate slot", []Network{{Name: "net0", Bridge: "vmbr0"}, {Name: "net0", Bridge: "vmbr1"}}, "spec.networks[1].name \"net0\" is declared more than once"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalSpec()
			spec.Networks = tt.nets
			if msg := validateErr(t, spec); !strings.Contains(msg, tt.want) {
				t.Errorf("validateSpec() = %v, want it to mention %q", msg, tt.want)
			}
		})
	}
}

func TestValidateRootPasswordFrom(t *testing.T) {
	spec := minimalSpec()
	spec.Root = &Root{PasswordFrom: "env:CT_ROOT_PASSWORD"}
	if msg := validateErr(t, spec); msg != "" {
		t.Errorf("valid reference rejected: %v", msg)
	}
	spec.Root.PasswordFrom = "hunter2"
	if msg := validateErr(t, spec); !strings.Contains(msg, "spec.root.passwordFrom") {
		t.Errorf("validateSpec() = %v, want a passwordFrom error", msg)
	}
}

func TestValidateRaw(t *testing.T) {
	spec := minimalSpec()
	spec.Networks = []Network{{Name: "net0", Bridge: "vmbr0"}}
	spec.Raw = map[string]string{"net0": "x", "lxc.apparmor.profile": "unconfined", "": "y"}
	msg := validateErr(t, spec)
	if !strings.Contains(msg, "spec.raw.net0") {
		t.Errorf("validateSpec() = %v, want a raw conflict on net0", msg)
	}
	if !strings.Contains(msg, "spec.raw has an empty key") {
		t.Errorf("validateSpec() = %v, want an empty-key error", msg)
	}
	if strings.Contains(msg, "lxc.apparmor.profile") {
		t.Errorf("validateSpec() = %v, unmodelled raw key must pass", msg)
	}
}

func TestDecodeSpec(t *testing.T) {
	decode := func(t *testing.T, doc string) (*Spec, error) {
		t.Helper()
		objs, err := runtime.Decode(strings.NewReader(doc), "test.yaml")
		if err != nil {
			t.Fatal(err)
		}
		return decodeSpec(objs[0])
	}

	t.Run("full manifest", func(t *testing.T) {
		spec, err := decode(t, `
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata:
  name: web-01
spec:
  targetNode: pve1
  unprivileged: true
  osTemplate:
    volid: local:vztmpl/debian-12.tar.zst
  rootfs: {storage: local-lvm, size: 8G}
  features: {nesting: true}
  root:
    passwordFrom: env:CT_ROOT_PASSWORD
`)
		if err != nil {
			t.Fatalf("decodeSpec() error = %v", err)
		}
		if spec.Unprivileged == nil || !*spec.Unprivileged || spec.OSTemplate.VolID == "" ||
			spec.RootFS.Size != "8G" || spec.Features.Nesting == nil || spec.Root.PasswordFrom == "" {
			t.Errorf("decoded = %+v", spec)
		}
	})

	t.Run("unknown field errors", func(t *testing.T) {
		_, err := decode(t, `
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata: {name: web-01}
spec:
  targetNode: pve1
  unprivileged: true
  cpu: 2
`)
		if err == nil {
			t.Fatal("unknown field accepted")
		}
	})

	for field, hint := range map[string]string{
		"hostname: web-01": "metadata.name",
		"password: x":      "spec.root.passwordFrom",
	} {
		t.Run("hint for "+field, func(t *testing.T) {
			_, err := decode(t, `
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata: {name: web-01}
spec:
  targetNode: pve1
  unprivileged: true
  `+field+`
`)
			if err == nil || !strings.Contains(err.Error(), hint) {
				t.Errorf("decodeSpec() = %v, want a hint mentioning %s", err, hint)
			}
		})
	}
}

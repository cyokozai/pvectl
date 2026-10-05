package lxc

import (
	"reflect"
	"strings"
	"testing"
)

func intPtr(i int) *int     { return &i }
func boolPtr(b bool) *bool { return &b }

// fullSpec exercises every convertible field.
func fullSpec() *Spec {
	return &Spec{
		TargetNode:   "pve1",
		VMID:         intPtr(200),
		Unprivileged: boolPtr(true),
		OSTemplate: &OSTemplate{
			URL:     "https://download.example/debian-12-standard_12.7-1_amd64.tar.zst",
			SHA256:  testSHA,
			Storage: "local",
		},
		Pool:        "lab",
		Description: "test ct",
		Resources:   Resources{Cores: 2, Memory: 1024, Swap: intPtr(512)},
		RootFS:      &RootFS{Storage: "local-lvm", Size: "8G"},
		MountPoints: []MountPoint{
			{Name: "mp0", Storage: "local-lvm", Size: "32G", Path: "/data"},
		},
		Networks: []Network{
			{Name: "net0", Bridge: "vmbr0", IP: "dhcp"},
			{Name: "net1", Interface: "lan", Bridge: "vmbr1", IP: "10.0.0.5/24", GW: "10.0.0.1", Tag: 10, HWAddr: "bc:24:11:00:00:01"},
		},
		Nameserver: "10.0.0.53",
		Features:   &Features{Nesting: boolPtr(true), Keyctl: boolPtr(true)},
		Root: &Root{
			// The converters see the resolved value, never the reference.
			PasswordFrom:     "env:PVECTL_TEST_CT_PASSWORD",
			resolvedPassword: "s3cret",
			SSHKeys:          []string{"ssh-ed25519 AAAA a@example", "ssh-ed25519 BBBB b@example"},
		},
		RunStrategy: RunStrategyAlways,
		Tags:        []string{"web", "prod"},
	}
}

func TestCreateParams(t *testing.T) {
	params := createParams("web-01", fullSpec())

	want := map[string]any{
		"hostname":        "web-01",
		"unprivileged":    1,
		"ostemplate":      "local:vztmpl/pvectl-3f2a9c0d1e4b5a69-debian-12-standard_12.7-1_amd64.tar.zst",
		"pool":            "lab",
		"description":     "test ct",
		"cores":           2,
		"memory":          1024,
		"swap":            512,
		"rootfs":          "local-lvm:8",
		"mp0":             "local-lvm:32,mp=/data",
		"net0":            "name=eth0,bridge=vmbr0,ip=dhcp",
		"net1":            "name=lan,bridge=vmbr1,ip=10.0.0.5/24,gw=10.0.0.1,tag=10,hwaddr=BC:24:11:00:00:01",
		"nameserver":      "10.0.0.53",
		"features":        "keyctl=1,nesting=1",
		"password":        "s3cret",
		"ssh-public-keys": "ssh-ed25519 AAAA a@example\nssh-ed25519 BBBB b@example",
		"onboot":          1,
		"tags":            "prod;web",
	}
	if !reflect.DeepEqual(params, want) {
		for k := range mergedKeys(params, want) {
			if !reflect.DeepEqual(params[k], want[k]) {
				t.Errorf("%s = %#v, want %#v", k, params[k], want[k])
			}
		}
	}
}

func mergedKeys(a, b map[string]any) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

func TestCreateParamsVolID(t *testing.T) {
	spec := minimalSpec()
	spec.OSTemplate = &OSTemplate{VolID: "local:vztmpl/alpine.tar.xz"}
	if got := createParams("ct", spec)["ostemplate"]; got != "local:vztmpl/alpine.tar.xz" {
		t.Errorf("ostemplate = %v", got)
	}
}

func TestCreateParamsPrivileged(t *testing.T) {
	spec := minimalSpec()
	spec.Unprivileged = boolPtr(false)
	// false is written explicitly: the manifest declared it.
	if got, ok := createParams("ct", spec)["unprivileged"]; !ok || got != 0 {
		t.Errorf("unprivileged = %v (present %v), want 0", got, ok)
	}
}

func TestDesiredParamsExcludeCreateOnly(t *testing.T) {
	desired := desiredParams("web-01", fullSpec())
	for _, key := range []string{"ostemplate", "pool", "password", "ssh-public-keys"} {
		if _, has := desired[key]; has {
			t.Errorf("desiredParams contains create-only key %q", key)
		}
	}
	for _, key := range []string{"hostname", "unprivileged", "rootfs", "mp0", "net0", "features", "swap"} {
		if _, has := desired[key]; !has {
			t.Errorf("desiredParams is missing %q", key)
		}
	}
}

func TestParamsOmitUndeclared(t *testing.T) {
	// ADR-005: a field the manifest does not write does not exist.
	params := createParams("ct", minimalSpec())
	for _, key := range []string{"cores", "memory", "swap", "features", "nameserver", "description", "tags", "onboot", "rootfs", "password", "ssh-public-keys", "pool", "ostemplate"} {
		if _, has := params[key]; has {
			t.Errorf("createParams wrote undeclared key %q = %v", key, params[key])
		}
	}
}

func TestSwapZeroIsDeclared(t *testing.T) {
	spec := minimalSpec()
	spec.Resources.Swap = intPtr(0)
	if got, ok := desiredParams("ct", spec)["swap"]; !ok || got != 0 {
		t.Errorf("swap = %v (present %v), want 0", got, ok)
	}
}

func TestFeaturesValue(t *testing.T) {
	tests := []struct {
		name     string
		features *Features
		want     string
		present  bool
	}{
		{"nil", nil, "", false},
		{"empty block", &Features{}, "", false},
		{"only written sub-keys", &Features{Nesting: boolPtr(true)}, "nesting=1", true},
		{"false is written", &Features{Nesting: boolPtr(false), Fuse: boolPtr(true)}, "fuse=1,nesting=0", true},
		{"all", &Features{Nesting: boolPtr(true), Keyctl: boolPtr(true), Fuse: boolPtr(true), Mknod: boolPtr(true)}, "fuse=1,keyctl=1,mknod=1,nesting=1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalSpec()
			spec.Features = tt.features
			got, ok := desiredParams("ct", spec)["features"]
			if ok != tt.present || (ok && got != tt.want) {
				t.Errorf("features = %v (present %v), want %q (present %v)", got, ok, tt.want, tt.present)
			}
		})
	}
}

func TestNetworkInterfaceDefault(t *testing.T) {
	for slot, want := range map[string]string{"net0": "name=eth0,bridge=vmbr0", "net3": "name=eth3,bridge=vmbr0"} {
		if got := networkValue(Network{Name: slot, Bridge: "vmbr0"}); got != want {
			t.Errorf("networkValue(%s) = %q, want %q", slot, got, want)
		}
	}
}

func TestRunStrategyOnboot(t *testing.T) {
	for s, want := range map[RunStrategy]any{RunStrategyAlways: 1, RunStrategyHalted: 0} {
		spec := minimalSpec()
		spec.RunStrategy = s
		if got := desiredParams("ct", spec)["onboot"]; got != want {
			t.Errorf("%s: onboot = %v, want %v", s, got, want)
		}
	}
	spec := minimalSpec()
	spec.RunStrategy = RunStrategyManual
	if _, has := desiredParams("ct", spec)["onboot"]; has {
		t.Error("Manual must leave onboot unmanaged")
	}
}

func TestRawMerged(t *testing.T) {
	spec := minimalSpec()
	spec.Raw = map[string]string{"lxc.apparmor.profile": "unconfined", "mp1": "/host/data,mp=/srv"}
	for _, params := range []map[string]any{createParams("ct", spec), desiredParams("ct", spec)} {
		if params["mp1"] != "/host/data,mp=/srv" || params["lxc.apparmor.profile"] != "unconfined" {
			t.Errorf("raw not merged: %v", params)
		}
	}
}

func TestRawConflicts(t *testing.T) {
	spec := fullSpec()
	spec.Raw = map[string]string{
		"hostname":        "x",
		"mp0":             "x",
		"net1":            "x",
		"password":        "x",
		"ostemplate":      "x",
		"lxc.cgroup2.foo": "ok",
	}
	got := rawConflicts("web-01", spec)
	want := []string{"hostname", "mp0", "net1", "ostemplate", "password"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rawConflicts() = %v, want %v", got, want)
	}
}

func TestRawConflictUnresolvedPassword(t *testing.T) {
	// Validation runs before secrets are resolved; the generated key set
	// must still include password.
	spec := minimalSpec()
	spec.Root = &Root{PasswordFrom: "env:X"}
	spec.Raw = map[string]string{"password": "x"}
	if got := rawConflicts("ct", spec); !reflect.DeepEqual(got, []string{"password"}) {
		t.Errorf("rawConflicts() = %v, want [password]", got)
	}
}

func TestResolveRootPassword(t *testing.T) {
	t.Setenv("PVECTL_TEST_CT_PASSWORD", "from-env")
	spec := minimalSpec()
	spec.Root = &Root{PasswordFrom: "env:PVECTL_TEST_CT_PASSWORD"}
	if err := resolveRootPassword(spec); err != nil {
		t.Fatalf("resolveRootPassword() error = %v", err)
	}
	if got := createParams("ct", spec)["password"]; got != "from-env" {
		t.Errorf("password = %v, want from-env", got)
	}

	t.Run("unset variable errors", func(t *testing.T) {
		spec := minimalSpec()
		spec.Root = &Root{PasswordFrom: "env:PVECTL_TEST_CT_PASSWORD_UNSET"}
		err := resolveRootPassword(spec)
		if err == nil || !strings.Contains(err.Error(), "spec.root.passwordFrom") {
			t.Errorf("resolveRootPassword() = %v, want an error naming the field", err)
		}
	})

	t.Run("no root is a no-op", func(t *testing.T) {
		if err := resolveRootPassword(minimalSpec()); err != nil {
			t.Errorf("resolveRootPassword() = %v", err)
		}
	})
}

package lxc

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/diff"
	"github.com/cyokozai/pvectl/internal/runtime"
)

func summary() api.GuestSummary {
	return api.GuestSummary{
		VMID: 200, Name: "web-01", Node: "pve1", Type: "lxc", Status: "running",
		Pool: "lab", Tags: []string{"web", "prod"}, MaxCPU: 2, MaxMem: 1 << 30, Uptime: 42,
	}
}

func TestNewFromSummary(t *testing.T) {
	ct := newLinuxContainerFromSummary(summary())
	if ct.Kind != KindName || ct.APIVersion != APIVersion || ct.GetName() != "web-01" {
		t.Errorf("identity = %+v", ct)
	}
	if ct.Spec.TargetNode != "pve1" || *ct.Spec.VMID != 200 || ct.Spec.Resources.Cores != 2 || ct.Spec.Resources.Memory != 1024 {
		t.Errorf("spec = %+v", ct.Spec)
	}
	if ct.Status == nil || ct.Status.State != "running" || ct.Status.VMID != 200 {
		t.Errorf("status = %+v", ct.Status)
	}
}

func TestNewFromConfig(t *testing.T) {
	ct := newLinuxContainerFromConfig(summary(), liveConfig())
	s := ct.Spec

	if ct.GetName() != "web-01" || ct.GetKind() != KindName {
		t.Errorf("identity = %s/%s", ct.GetKind(), ct.GetName())
	}
	if s.Unprivileged == nil || !*s.Unprivileged {
		t.Errorf("unprivileged = %v", s.Unprivileged)
	}
	if s.OSTemplate != nil || s.Root != nil {
		t.Error("osTemplate and root must never be exported")
	}
	if s.Resources.Cores != 2 || s.Resources.Memory != 1024 || s.Resources.Swap == nil || *s.Resources.Swap != 512 {
		t.Errorf("resources = %+v", s.Resources)
	}
	if s.RootFS == nil || *s.RootFS != (RootFS{Storage: "local-lvm", Size: "8G"}) {
		t.Errorf("rootfs = %+v", s.RootFS)
	}
	if len(s.MountPoints) != 1 || s.MountPoints[0] != (MountPoint{Name: "mp0", Storage: "local-lvm", Size: "32G", Path: "/data"}) {
		t.Errorf("mountPoints = %+v", s.MountPoints)
	}
	wantNets := []Network{
		{Name: "net0", Interface: "eth0", Bridge: "vmbr0", IP: "dhcp"},
		{Name: "net1", Interface: "lan", Bridge: "vmbr1", IP: "10.0.0.5/24", GW: "10.0.0.1", Tag: 10},
	}
	if len(s.Networks) != 2 || s.Networks[0] != wantNets[0] || s.Networks[1] != wantNets[1] {
		t.Errorf("networks = %+v, want %+v (hwaddr is generated and not exported)", s.Networks, wantNets)
	}
	if s.Features == nil || s.Features.Nesting == nil || !*s.Features.Nesting || s.Features.Keyctl == nil || s.Features.Fuse != nil {
		t.Errorf("features = %+v", s.Features)
	}
	if s.RunStrategy != RunStrategyAlways || s.Nameserver != "10.0.0.53" || s.Description != "test ct" {
		t.Errorf("spec = %+v", s)
	}
	if strings.Join(s.Tags, ",") != "prod,web" {
		t.Errorf("tags = %v", s.Tags)
	}
}

func TestNewFromConfigPrivileged(t *testing.T) {
	cfg := liveConfig()
	delete(cfg, "unprivileged")
	ct := newLinuxContainerFromConfig(summary(), cfg)
	if ct.Spec.Unprivileged == nil || *ct.Spec.Unprivileged {
		t.Errorf("unprivileged = %v, want an explicit false", ct.Spec.Unprivileged)
	}
	data, err := yaml.Marshal(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "unprivileged: false") {
		t.Errorf("unprivileged must always be printed:\n%s", data)
	}
}

func TestNewFromConfigSkipsBindMounts(t *testing.T) {
	cfg := liveConfig()
	cfg["mp1"] = "/srv/host,mp=/host"
	ct := newLinuxContainerFromConfig(summary(), cfg)
	if len(ct.Spec.MountPoints) != 1 {
		t.Errorf("mountPoints = %+v, bind mounts cannot be expressed and are skipped", ct.Spec.MountPoints)
	}
}

func TestNewFromConfigUnknownFeatures(t *testing.T) {
	cfg := liveConfig()
	cfg["features"] = "mount=nfs;cifs"
	if ct := newLinuxContainerFromConfig(summary(), cfg); ct.Spec.Features != nil {
		t.Errorf("features = %+v, want nil when no modelled flag is set", ct.Spec.Features)
	}
}

// TestGetYAMLRoundTrip guarantees the ADR-010 §3 promise: get -o yaml
// output decodes, validates, and applies with no difference.
func TestGetYAMLRoundTrip(t *testing.T) {
	variants := map[string]func(map[string]any){
		"full":       func(map[string]any) {},
		"privileged": func(c map[string]any) { delete(c, "unprivileged") },
		"minimal": func(c map[string]any) {
			for _, k := range []string{"cores", "swap", "description", "onboot", "mp0", "net1", "nameserver", "features", "tags"} {
				delete(c, k)
			}
		},
		"onboot off": func(c map[string]any) { c["onboot"] = float64(0) },
	}
	for name, mutate := range variants {
		t.Run(name, func(t *testing.T) {
			cfg := liveConfig()
			mutate(cfg)

			data, err := yaml.Marshal(newLinuxContainerFromConfig(summary(), cfg))
			if err != nil {
				t.Fatal(err)
			}
			objs, err := runtime.Decode(strings.NewReader(string(data)), "get.yaml")
			if err != nil {
				t.Fatalf("Decode() error = %v\nyaml:\n%s", err, data)
			}
			obj := objs[0]
			if gvk, err := obj.GVK(); err != nil || gvk != GroupVersionKind {
				t.Errorf("GVK = %v, %v", gvk, err)
			}
			spec, err := decodeSpec(obj)
			if err != nil {
				t.Fatalf("decodeSpec() error = %v\nyaml:\n%s", err, data)
			}
			if err := validateSpec(obj.Metadata.Name, spec); err != nil {
				t.Fatalf("validateSpec() error = %v\nyaml:\n%s", err, data)
			}

			desired := desiredParams(obj.Metadata.Name, spec)
			d := diff.Compute(cfg, desired, NormalizeRules(desired))
			if !d.Empty() {
				t.Errorf("round-trip diff = %+v\nyaml:\n%s", d.Entries, data)
			}
			put, resizes, err := planUpdate(obj.Metadata.Name, cfg, desired, d)
			if err != nil || len(put) != 0 || len(resizes) != 0 {
				t.Errorf("planUpdate() = %v, %v, %v; want nothing to do", put, resizes, err)
			}
		})
	}
}

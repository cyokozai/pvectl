package lxc

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/diff"
)

// liveConfig is what GET /nodes/pve1/lxc/200/config returns for the
// container fullSpec describes (numbers arrive as float64 from JSON).
func liveConfig() map[string]any {
	return map[string]any{
		"hostname":     "web-01",
		"arch":         "amd64",
		"ostype":       "debian",
		"unprivileged": float64(1),
		"cores":        float64(2),
		"memory":       float64(1024),
		"swap":         float64(512),
		"description":  "test ct",
		"onboot":       float64(1),
		"rootfs":       "local-lvm:vm-200-disk-0,size=8G",
		"mp0":          "local-lvm:vm-200-disk-1,mp=/data,size=32G",
		"net0":         "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:AA:BB:CC,ip=dhcp,type=veth",
		"net1":         "name=lan,bridge=vmbr1,gw=10.0.0.1,hwaddr=BC:24:11:00:00:01,ip=10.0.0.5/24,tag=10,type=veth",
		"nameserver":   "10.0.0.53",
		"features":     "keyctl=1,nesting=1",
		"tags":         "prod;web",
		"digest":       "abc",
	}
}

// plan diffs spec against current and plans the update.
func plan(t *testing.T, spec *Spec, current map[string]any) (map[string]any, []resizeOp, error) {
	t.Helper()
	desired := desiredParams("web-01", spec)
	d := diff.Compute(current, desired, NormalizeRules(desired))
	return planUpdate("web-01", current, desired, d)
}

func TestPlanUpdateNoChange(t *testing.T) {
	put, resizes, err := plan(t, fullSpec(), liveConfig())
	if err != nil || len(put) != 0 || len(resizes) != 0 {
		t.Errorf("planUpdate() = %v, %v, %v; want nothing to do", put, resizes, err)
	}
}

func TestPlanUpdateResize(t *testing.T) {
	spec := fullSpec()
	spec.RootFS.Size = "16G"
	spec.MountPoints[0].Size = "64G"
	put, resizes, err := plan(t, spec, liveConfig())
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	if len(put) != 0 {
		t.Errorf("size-only growth must not PUT config: %v", put)
	}
	want := []resizeOp{{disk: "mp0", size: "64G"}, {disk: "rootfs", size: "16G"}}
	if !reflect.DeepEqual(resizes, want) {
		t.Errorf("resizes = %+v, want %+v", resizes, want)
	}
}

func TestPlanUpdateRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{"rootfs shrink", func(s *Spec) { s.RootFS.Size = "4G" }, "cannot shrink rootfs"},
		{"mp shrink", func(s *Spec) { s.MountPoints[0].Size = "16G" }, "cannot shrink mp0"},
		{"rootfs storage", func(s *Spec) { s.RootFS.Storage = "other" }, "cannot change storage of rootfs"},
		{"unprivileged", func(s *Spec) { s.Unprivileged = boolPtr(false) }, "spec.unprivileged is create-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := fullSpec()
			tt.mutate(spec)
			_, _, err := plan(t, spec, liveConfig())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("planUpdate() = %v, want an error mentioning %q", err, tt.want)
			}
		})
	}
}

func TestPlanUpdateMountPath(t *testing.T) {
	spec := fullSpec()
	spec.MountPoints[0].Path = "/srv"
	put, resizes, err := plan(t, spec, liveConfig())
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	// The existing volume is kept; only mp= changes.
	if got := put["mp0"]; got != "local-lvm:vm-200-disk-1,mp=/srv,size=32G" {
		t.Errorf("mp0 = %v", got)
	}
	if len(resizes) != 0 {
		t.Errorf("resizes = %v", resizes)
	}
}

func TestPlanUpdateNewMountPoint(t *testing.T) {
	spec := fullSpec()
	spec.MountPoints = append(spec.MountPoints, MountPoint{Name: "mp1", Storage: "local-lvm", Size: "4G", Path: "/logs"})
	put, _, err := plan(t, spec, liveConfig())
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	// Allocation syntax works on config PUT for a new slot.
	if got := put["mp1"]; got != "local-lvm:4,mp=/logs" {
		t.Errorf("mp1 = %v", got)
	}
}

func TestPlanUpdateNetworkKeepsUnwrittenSubKeys(t *testing.T) {
	spec := fullSpec()
	spec.Networks[0].Bridge = "vmbr9"
	put, _, err := plan(t, spec, liveConfig())
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	// A PUT replaces the whole value, so the generated hwaddr (and type)
	// must be carried over or Proxmox would assign a new MAC.
	if got := put["net0"]; got != "name=eth0,bridge=vmbr9,hwaddr=BC:24:11:AA:BB:CC,ip=dhcp,type=veth" {
		t.Errorf("net0 = %v", got)
	}
}

func TestPlanUpdateNewNetwork(t *testing.T) {
	spec := fullSpec()
	spec.Networks = append(spec.Networks, Network{Name: "net2", Bridge: "vmbr2"})
	put, _, err := plan(t, spec, liveConfig())
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	if got := put["net2"]; got != "name=eth2,bridge=vmbr2" {
		t.Errorf("net2 = %v", got)
	}
}

func TestPlanUpdateFeaturesKeepUnwrittenSubKeys(t *testing.T) {
	spec := fullSpec()
	spec.Features = &Features{Nesting: boolPtr(false)}
	put, _, err := plan(t, spec, liveConfig())
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	// keyctl is not written, so it stays as it is live (ADR-010 §2).
	if got := put["features"]; got != "keyctl=1,nesting=0" {
		t.Errorf("features = %v", got)
	}
}

func TestPlanUpdatePlainKeys(t *testing.T) {
	spec := fullSpec()
	spec.Resources.Memory = 2048
	spec.Nameserver = "1.1.1.1"
	put, _, err := plan(t, spec, liveConfig())
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	want := map[string]any{"memory": 2048, "nameserver": "1.1.1.1"}
	if !reflect.DeepEqual(put, want) {
		t.Errorf("put = %v, want %v", put, want)
	}
}

func TestCheckCreateOnly(t *testing.T) {
	tests := []struct {
		name    string
		pool    string
		summary *api.GuestSummary
		wantErr bool
	}{
		{"pool not written", "", &api.GuestSummary{Pool: "other"}, false},
		{"pool matches", "lab", &api.GuestSummary{Pool: "lab"}, false},
		{"pool differs", "lab", &api.GuestSummary{Pool: "other"}, true},
		{"pool missing live", "lab", &api.GuestSummary{}, true},
		{"summary not read", "lab", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalSpec()
			spec.Pool = tt.pool
			err := checkCreateOnly("ct", spec, tt.summary)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkCreateOnly() = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "spec.pool is create-only") {
				t.Errorf("checkCreateOnly() = %v", err)
			}
		})
	}
}

func TestUpdateWarnings(t *testing.T) {
	if w := updateWarnings("ct", minimalSpec()); len(w) != 0 {
		t.Errorf("updateWarnings() = %v, want none", w)
	}
	spec := minimalSpec()
	spec.Root = &Root{SSHKeys: []string{"ssh-ed25519 AAAA"}}
	w := updateWarnings("ct", spec)
	if len(w) != 1 || !strings.Contains(w[0], "spec.root") {
		t.Errorf("updateWarnings() = %v, want one spec.root warning", w)
	}
}

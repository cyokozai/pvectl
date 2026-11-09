package lxc

import (
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/diff"
)

// The NIC tag follows the VirtualMachine rule: not written = unmanaged,
// 0 = declared untagged, 1-4094 = the tag.

func TestCreateParamsNeverSendEmptyTag(t *testing.T) {
	spec := minimalSpec()
	spec.Networks = []Network{
		{Name: "net0", Bridge: "vmbr0", Tag: intPtr(0)},
		{Name: "net1", Bridge: "vmbr1", Tag: intPtr(10)},
		{Name: "net2", Bridge: "vmbr2"},
	}
	params := createParams("ct", spec)
	if got := params["net0"]; got != "name=eth0,bridge=vmbr0" {
		t.Errorf("net0 = %v, want no tag sub-key at all", got)
	}
	if got := params["net1"]; got != "name=eth1,bridge=vmbr1,tag=10" {
		t.Errorf("net1 = %v", got)
	}
	if got := params["net2"]; got != "name=eth2,bridge=vmbr2" {
		t.Errorf("net2 = %v", got)
	}
}

func tagPlan(t *testing.T, tag *int, live string) (diff.Result, map[string]any) {
	t.Helper()
	spec := minimalSpec()
	spec.Networks = []Network{{Name: "net0", Bridge: "vmbr0", Tag: tag}}
	current := map[string]any{"hostname": "ct", "unprivileged": float64(1), "net0": live}
	desired := desiredParams("ct", spec)
	d := diff.Compute(current, desired, NormalizeRules(desired))
	put, _, err := planUpdate("ct", current, desired, d)
	if err != nil {
		t.Fatalf("planUpdate() error = %v", err)
	}
	return d, put
}

func TestTagZeroRemovesLiveTag(t *testing.T) {
	d, put := tagPlan(t, intPtr(0), "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:AA:BB:CC,tag=10,type=veth")
	if d.Empty() {
		t.Fatal("tag: 0 against a tagged NIC must be a difference")
	}
	if got := put["net0"]; got != "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:AA:BB:CC,type=veth" {
		t.Errorf("net0 = %v, want the tag removed and the MAC kept", got)
	}
}

func TestTagZeroMatchesUntaggedLive(t *testing.T) {
	if d, _ := tagPlan(t, intPtr(0), "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:AA:BB:CC,type=veth"); !d.Empty() {
		t.Errorf("diff = %+v, want none", d.Entries)
	}
}

func TestTagUnwrittenIsUnmanaged(t *testing.T) {
	d, _ := tagPlan(t, nil, "name=eth0,bridge=vmbr0,tag=10,type=veth")
	if !d.Empty() {
		t.Errorf("diff = %+v, want none: an unwritten tag is not compared", d.Entries)
	}
	_, put := tagPlan(t, nil, "name=eth1,bridge=vmbr0,tag=10,type=veth")
	if got := put["net0"]; got != "name=eth0,bridge=vmbr0,tag=10,type=veth" {
		t.Errorf("net0 = %v, want the live tag carried over", got)
	}
}

func TestTagZeroOnNewSlotSendsNoTag(t *testing.T) {
	spec := minimalSpec()
	spec.Networks = []Network{{Name: "net1", Bridge: "vmbr1", Tag: intPtr(0)}}
	current := map[string]any{"hostname": "ct", "unprivileged": float64(1)}
	desired := desiredParams("ct", spec)
	d := diff.Compute(current, desired, NormalizeRules(desired))
	put, _, err := planUpdate("ct", current, desired, d)
	if err != nil {
		t.Fatal(err)
	}
	if got := put["net1"]; got != "name=eth1,bridge=vmbr1" {
		t.Errorf("net1 = %v, want no empty tag= sent", got)
	}
}

func TestLiveTagOnlyWhenTagged(t *testing.T) {
	if n := netFromValue("net0", "name=eth0,bridge=vmbr0,type=veth"); n.Tag != nil {
		t.Errorf("tag = %v, want nil for an untagged NIC", *n.Tag)
	}
	if n := netFromValue("net0", "name=eth0,bridge=vmbr0,tag=42"); n.Tag == nil || *n.Tag != 42 {
		t.Errorf("tag = %v, want 42", n.Tag)
	}
}

func TestValidateTagZero(t *testing.T) {
	spec := minimalSpec()
	spec.Networks = []Network{{Name: "net0", Bridge: "vmbr0", Tag: intPtr(0)}}
	if msg := validateErr(t, spec); msg != "" {
		t.Errorf("tag: 0 rejected: %s", msg)
	}
	spec.Networks[0].Tag = intPtr(-1)
	if msg := validateErr(t, spec); !strings.Contains(msg, "0 for no tag") {
		t.Errorf("validateSpec() = %q, want the 0-for-no-tag wording", msg)
	}
}

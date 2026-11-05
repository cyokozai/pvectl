package lxc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/api/apitest"
	"github.com/cyokozai/pvectl/internal/printer"
)

// seededFake holds two containers, a VM, and a VM that shares its name
// with a container.
func seededFake() *apitest.Fake {
	f := &apitest.Fake{NextIDValue: 300}
	f.AddGuest(summary(), liveConfig())
	f.AddGuest(api.GuestSummary{VMID: 201, Name: "db", Node: "pve2", Type: "lxc", Status: "stopped", MaxCPU: 1, MaxMem: 512 << 20},
		map[string]any{"hostname": "db", "cores": float64(1), "memory": float64(512), "rootfs": "local-lvm:vm-201-disk-0,size=4G"})
	f.AddGuest(api.GuestSummary{VMID: 100, Name: "vm-only", Node: "pve1", Type: "qemu", Status: "running"}, nil)
	f.AddGuest(api.GuestSummary{VMID: 101, Name: "web-01", Node: "pve1", Type: "qemu", Status: "running"}, nil)
	return f
}

func TestHandlerIdentity(t *testing.T) {
	h := NewHandler()
	if gvk := h.GVK(); gvk != GroupVersionKind || gvk.APIVersion() != "pve.io/v1alpha1" {
		t.Errorf("GVK = %v", gvk)
	}
	if got := strings.Join(h.Aliases(), ","); got != "lxc,ct,linuxcontainer,linuxcontainers" {
		t.Errorf("aliases = %s", got)
	}
	var names []string
	for _, c := range h.Columns(true) {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "NAME,VMID,NODE,STATUS,CORES,MEMORY,UPTIME,TAGS" {
		t.Errorf("wide columns = %s", got)
	}
}

func TestListOnlyContainersSorted(t *testing.T) {
	f := seededFake()
	objs, err := NewHandler().List(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[0].GetName() != "web-01" || objs[1].GetName() != "db" {
		t.Fatalf("List() = %v, want web-01 (200) and db (201) only", names(objs))
	}
	var buf bytes.Buffer
	if err := printer.Print(&buf, printer.FormatWide, objs, NewHandler().Columns(true)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"web-01", "200", "pve1", "running", "2", "1024Mi", "42s", "web,prod"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("table missing %q:\n%s", want, buf.String())
		}
	}
}

// TestListTableReadsNoConfig keeps the table one API call: a failing
// config read must not matter until the objects are serialized.
func TestListTableReadsNoConfig(t *testing.T) {
	f := seededFake()
	f.Err = map[string]error{"LxcConfig": errors.New("config read")}
	h := NewHandler()
	objs, err := h.List(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := printer.Print(&buf, printer.FormatTable, objs, h.Columns(false)); err != nil {
		t.Fatalf("table output read the config: %v", err)
	}
	if err := printer.Print(&buf, printer.FormatYAML, objs, nil); err == nil || !strings.Contains(err.Error(), "config read") {
		t.Errorf("yaml output error = %v, want the config read failure", err)
	}
}

// TestListYAMLRoundTrips is the unnamed `get ct -o yaml`: each document
// carries what only the config has (unprivileged, volumes, NICs).
func TestListYAMLRoundTrips(t *testing.T) {
	objs, err := NewHandler().List(context.Background(), seededFake())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := printer.Print(&buf, printer.FormatYAML, objs, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"unprivileged: true", "unprivileged: false", "name: net0", "storage: local-lvm", "size: 4G"} {
		if !strings.Contains(out, want) {
			t.Errorf("yaml missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	if err := printer.Print(&buf, printer.FormatJSON, objs, nil); err != nil {
		t.Fatal(err)
	}
	var docs []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if spec, _ := docs[1]["spec"].(map[string]any); spec["unprivileged"] != false {
		t.Errorf("json spec = %v, want unprivileged false", spec)
	}
}

func TestGetResolvesOnlyContainers(t *testing.T) {
	f := seededFake()
	obj, err := NewHandler().Get(context.Background(), f, "web-01")
	if err != nil {
		t.Fatalf("Get(web-01) error = %v; the VM of the same name must not interfere", err)
	}
	ct := obj.(*LinuxContainer)
	if *ct.Spec.VMID != 200 || ct.Spec.Unprivileged == nil || !*ct.Spec.Unprivileged {
		t.Errorf("Get(web-01) = %+v", ct.Spec)
	}
	data, err := yaml.Marshal(ct)
	if err != nil || !strings.Contains(string(data), "kind: LinuxContainer") {
		t.Errorf("yaml = %s, %v", data, err)
	}
}

func TestGetNotFoundHintsAtVM(t *testing.T) {
	_, err := NewHandler().Get(context.Background(), seededFake(), "vm-only")
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("Get(vm-only) error = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "VirtualMachine") {
		t.Errorf("error = %v, want a hint that a VirtualMachine has this name", err)
	}
}

func TestGetAmbiguous(t *testing.T) {
	f := seededFake()
	f.AddGuest(api.GuestSummary{VMID: 202, Name: "db", Node: "pve1", Type: "lxc"}, nil)
	_, err := NewHandler().Get(context.Background(), f, "db")
	if !errors.Is(err, api.ErrAmbiguousName) || !strings.Contains(err.Error(), "201") || !strings.Contains(err.Error(), "202") {
		t.Errorf("error = %v, want ambiguous with both vmids", err)
	}
}

func TestLifecycleVerbsTargetTheContainer(t *testing.T) {
	f := seededFake()
	h := NewHandler()
	ctx := context.Background()
	if err := h.Stop(ctx, f, "web-01"); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(ctx, f, "web-01"); err != nil {
		t.Fatal(err)
	}
	if err := h.Delete(ctx, f, "web-01"); err != nil {
		t.Fatal(err)
	}
	want := api.GuestRef{VMID: 200, Node: "pve1", Type: "lxc"}
	if len(f.Stops) != 1 || f.Stops[0] != want || len(f.Starts) != 1 || f.Starts[0] != want || len(f.Deletes) != 1 || f.Deletes[0] != want {
		t.Errorf("stops %v starts %v deletes %v, want %v each", f.Stops, f.Starts, f.Deletes, want)
	}
	if err := h.Delete(ctx, f, "vm-only"); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("Delete(vm-only) error = %v, want ErrNotFound (never the VM)", err)
	}
}

func TestDescribe(t *testing.T) {
	var buf bytes.Buffer
	if err := NewHandler().Describe(context.Background(), seededFake(), "web-01", &buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Name:", "web-01", "Kind:", "LinuxContainer", "Unprivileged:", "true",
		"RootFS:", "local-lvm 8G", "mp0:", "/data", "net0:", "bridge=vmbr0", "ip=dhcp", "vlan=10", "Features:", "nesting", "RunStrategy:", "Always"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("describe missing %q:\n%s", want, buf.String())
		}
	}
}

func names(objs []printer.Object) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.GetName()
	}
	return out
}

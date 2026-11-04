package apitest

import (
	"context"
	"errors"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
)

func TestFakeCreateLxc(t *testing.T) {
	f := &Fake{}
	ctx := context.Background()
	params := map[string]any{
		"hostname":        "ct-web",
		"ostemplate":      "local:vztmpl/debian.tar.zst",
		"unprivileged":    1,
		"password":        "hunter2",
		"ssh-public-keys": "ssh-ed25519 AAAA",
	}
	if err := f.CreateLxc(ctx, "pve1", 200, params); err != nil {
		t.Fatalf("CreateLxc() error = %v", err)
	}

	if len(f.LxcCreates) != 1 || f.LxcCreates[0].Node != "pve1" || f.LxcCreates[0].VMID != 200 {
		t.Fatalf("LxcCreates = %+v, want one call for 200 on pve1", f.LxcCreates)
	}
	if f.LxcCreates[0].Params["password"] != "hunter2" {
		t.Errorf("recorded params lost the create-only password: %+v", f.LxcCreates[0].Params)
	}
	if len(f.Creates) != 0 {
		t.Errorf("an lxc create was recorded as a qemu create: %+v", f.Creates)
	}

	ref, err := f.FindGuest(ctx, "ct-web")
	if err != nil {
		t.Fatalf("FindGuest(ct-web) error = %v", err)
	}
	if ref.Type != "lxc" || ref.VMID != 200 || ref.Node != "pve1" {
		t.Errorf("ref = %+v, want lxc 200 on pve1", ref)
	}

	cfg, err := f.LxcConfig(ctx, ref)
	if err != nil {
		t.Fatalf("LxcConfig() error = %v", err)
	}
	if cfg["hostname"] != "ct-web" || cfg["unprivileged"] != "1" {
		t.Errorf("config = %+v, want hostname and unprivileged echoed as strings", cfg)
	}
	for _, key := range []string{"ostemplate", "password", "ssh-public-keys"} {
		if _, has := cfg[key]; has {
			t.Errorf("config kept create-only %q: %+v", key, cfg)
		}
	}
}

func TestFakeLxcConfigRejectsQemu(t *testing.T) {
	f := &Fake{}
	f.AddGuest(api.GuestSummary{VMID: 100, Name: "vm", Node: "pve1"}, nil)
	if _, err := f.LxcConfig(context.Background(), &api.GuestRef{VMID: 100, Node: "pve1", Type: "lxc"}); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("LxcConfig(qemu guest) error = %v, want ErrNotFound", err)
	}
}

func TestFakeUpdateLxcConfig(t *testing.T) {
	f := &Fake{}
	f.AddGuest(api.GuestSummary{VMID: 200, Name: "ct-web", Node: "pve1", Type: "lxc"},
		map[string]any{"hostname": "ct-web", "cores": "1"})
	ref := &api.GuestRef{VMID: 200, Node: "pve1", Type: "lxc"}

	if err := f.UpdateLxcConfig(context.Background(), ref, map[string]any{"cores": 2, "hostname": "ct-new"}); err != nil {
		t.Fatalf("UpdateLxcConfig() error = %v", err)
	}
	if len(f.LxcUpdates) != 1 || f.LxcUpdates[0].Ref != *ref {
		t.Fatalf("LxcUpdates = %+v", f.LxcUpdates)
	}
	if len(f.Updates) != 0 {
		t.Errorf("an lxc update was recorded as a qemu update: %+v", f.Updates)
	}
	if f.Configs[200]["cores"] != "2" {
		t.Errorf("config = %+v, want cores=2", f.Configs[200])
	}
	if _, err := f.FindGuest(context.Background(), "ct-new"); err != nil {
		t.Errorf("hostname update did not rename the guest: %v", err)
	}
}

func TestFakeStorageAndDownload(t *testing.T) {
	f := &Fake{}
	ctx := context.Background()
	f.AddVolume("pve1", "local", api.StorageVolume{VolID: "local:iso/x.iso", Content: "iso"})

	vols, err := f.StorageContent(ctx, "pve1", "local", "vztmpl")
	if err != nil || len(vols) != 0 {
		t.Fatalf("StorageContent(vztmpl) = %+v, %v; want none", vols, err)
	}

	params := map[string]any{
		"content":            "vztmpl",
		"filename":           "pvectl-0123456789abcdef-debian.tar.zst",
		"url":                "https://download.example/debian.tar.zst",
		"checksum":           "00",
		"checksum-algorithm": "sha256",
	}
	if err := f.DownloadURL(ctx, "pve1", "local", params); err != nil {
		t.Fatalf("DownloadURL() error = %v", err)
	}
	if len(f.Downloads) != 1 || f.Downloads[0].Node != "pve1" || f.Downloads[0].Storage != "local" {
		t.Fatalf("Downloads = %+v", f.Downloads)
	}

	vols, err = f.StorageContent(ctx, "pve1", "local", "vztmpl")
	if err != nil {
		t.Fatalf("StorageContent() error = %v", err)
	}
	if len(vols) != 1 || vols[0].VolID != "local:vztmpl/pvectl-0123456789abcdef-debian.tar.zst" || vols[0].Content != "vztmpl" {
		t.Errorf("StorageContent() after download = %+v", vols)
	}
	if other, _ := f.StorageContent(ctx, "pve2", "local", "vztmpl"); len(other) != 0 {
		t.Errorf("download leaked to another node: %+v", other)
	}
}

func TestFakeDownloadFailureStoresNothing(t *testing.T) {
	f := &Fake{Err: map[string]error{"DownloadURL": errors.New("checksum verification failed")}}
	err := f.DownloadURL(context.Background(), "pve1", "local", map[string]any{"content": "vztmpl", "filename": "x.tar.zst"})
	if err == nil {
		t.Fatal("DownloadURL() error = nil, want injected failure")
	}
	if vols, _ := f.StorageContent(context.Background(), "pve1", "local", ""); len(vols) != 0 {
		t.Errorf("a failed download left a volume: %+v", vols)
	}
}

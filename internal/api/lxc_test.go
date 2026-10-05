package api

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/pvectl/internal/config"
	"github.com/cyokozai/pvectl/internal/verbose"
	"github.com/cyokozai/pvectl/test/pvefake"
)

const testTemplate = "local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst"

func newLxcServer(t *testing.T) *pvefake.Server {
	t.Helper()
	s := newTestServer(t)
	s.AddGuest(pvefake.Guest{
		VMID: 300, Name: "ct-web", Node: "pve1", Type: "lxc", Status: "running",
		Config: map[string]any{"hostname": "ct-web", "cores": "1", "memory": "512", "unprivileged": "1"},
	})
	return s
}

func TestLxcConfig(t *testing.T) {
	c := newTestClient(t, newLxcServer(t))
	cfg, err := c.LxcConfig(context.Background(), &GuestRef{VMID: 300, Node: "pve1", Type: "lxc"})
	if err != nil {
		t.Fatalf("LxcConfig() error = %v", err)
	}
	if cfg["hostname"] != "ct-web" || cfg["memory"] != "512" {
		t.Errorf("config = %+v, want hostname=ct-web memory=512", cfg)
	}
}

func TestLxcConfigMissing(t *testing.T) {
	c := newTestClient(t, newLxcServer(t))
	if _, err := c.LxcConfig(context.Background(), &GuestRef{VMID: 399, Node: "pve1", Type: "lxc"}); err == nil {
		t.Fatal("LxcConfig(399) error = nil, want failure for a missing container")
	}
}

func TestCreateLxc(t *testing.T) {
	t.Run("container appears as lxc under its hostname", func(t *testing.T) {
		s := newLxcServer(t)
		c := newTestClient(t, s)
		ctx := context.Background()
		params := map[string]any{
			"hostname":        "ct-new",
			"ostemplate":      testTemplate,
			"unprivileged":    1,
			"rootfs":          "local-lvm:8",
			"password":        "hunter2",
			"ssh-public-keys": "ssh-ed25519 AAAAC3Nza test@example",
		}
		if err := c.CreateLxc(ctx, "pve1", 301, params); err != nil {
			t.Fatalf("CreateLxc() error = %v", err)
		}
		if !s.SawRequest("POST", "/api2/json/nodes/pve1/lxc") {
			t.Errorf("create did not reach POST /nodes/pve1/lxc: %v", s.Requests())
		}

		ref, err := c.FindGuest(ctx, "ct-new")
		if err != nil {
			t.Fatalf("FindGuest(ct-new) error = %v", err)
		}
		if ref.VMID != 301 || ref.Type != "lxc" || ref.Node != "pve1" {
			t.Errorf("ref = %+v, want vmid 301 lxc on pve1", ref)
		}

		cfg, err := c.LxcConfig(ctx, ref)
		if err != nil {
			t.Fatalf("LxcConfig() error = %v", err)
		}
		if cfg["ostemplate"] != nil {
			// Proxmox does not keep the template in the config.
			t.Errorf("config kept ostemplate: %+v", cfg)
		}
		if cfg["unprivileged"] != "1" || cfg["hostname"] != "ct-new" {
			t.Errorf("config = %+v, want unprivileged=1 hostname=ct-new", cfg)
		}
		for _, key := range []string{"password", "ssh-public-keys"} {
			if _, has := cfg[key]; has {
				t.Errorf("config kept create-only %q: %+v", key, cfg)
			}
		}
		if got := cfg["rootfs"]; got != "local-lvm:vm-301-disk-0,size=8G" {
			t.Errorf("rootfs = %v, want materialized volume", got)
		}
	})

	t.Run("task failure surfaces exit status", func(t *testing.T) {
		s := newLxcServer(t)
		c := newTestClient(t, s)
		s.FailNext("create", "unable to create CT 301 - template not found")
		err := c.CreateLxc(context.Background(), "pve1", 301, map[string]any{"hostname": "x", "ostemplate": testTemplate})
		if err == nil || !strings.Contains(err.Error(), "template not found") {
			t.Fatalf("CreateLxc() error = %v, want task exit status", err)
		}
	})
}

// TestCreateLxcNeverLogsCredentials drives a create through the whole
// client at the most verbose level: the root password and the ssh keys
// sent at create time must not reach the output by any route.
func TestCreateLxcNeverLogsCredentials(t *testing.T) {
	s := newLxcServer(t)
	var buf bytes.Buffer
	c, err := New(context.Background(),
		config.Node{Server: s.URL()},
		config.User{Token: testToken},
		Options{TaskTimeout: 10 * time.Second, Logger: verbose.New(verbose.LevelFullBody, &buf)},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const password = "hunter2-root"
	const keyBody = "AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	err = c.CreateLxc(context.Background(), "pve1", 302, map[string]any{
		"hostname":        "ct-secret",
		"ostemplate":      testTemplate,
		"password":        password,
		"ssh-public-keys": "ssh-ed25519 " + keyBody + " ops@example",
	})
	if err != nil {
		t.Fatalf("CreateLxc() error = %v", err)
	}

	got := buf.String()
	for _, secret := range []string{password, keyBody} {
		if strings.Contains(got, secret) {
			t.Errorf("credential %q reached -v=9 output:\n%s", secret, got)
		}
	}
	for _, want := range []string{"password=REDACTED", "ssh-public-keys=REDACTED"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q not logged as redacted:\n%s", want, got)
		}
	}
}

func TestUpdateLxcConfig(t *testing.T) {
	s := newLxcServer(t)
	c := newTestClient(t, s)
	ctx := context.Background()
	ref := &GuestRef{VMID: 300, Node: "pve1", Type: "lxc"}

	// Proxmox answers an lxc config PUT with "data": null, not a UPID.
	if err := c.UpdateLxcConfig(ctx, ref, map[string]any{"cores": 4, "hostname": "ct-renamed"}); err != nil {
		t.Fatalf("UpdateLxcConfig() error = %v", err)
	}
	if !s.SawRequest("PUT", "/api2/json/nodes/pve1/lxc/300/config") {
		t.Errorf("update did not reach the lxc config endpoint: %v", s.Requests())
	}
	g := s.Guest(300)
	if g.Config["cores"] != "4" || g.Config["memory"] != "512" {
		t.Errorf("config after update = %+v, want cores=4 memory kept", g.Config)
	}
	if g.Name != "ct-renamed" {
		t.Errorf("name after hostname update = %q, want ct-renamed", g.Name)
	}
}

func TestUpdateLxcConfigFailure(t *testing.T) {
	c := newTestClient(t, newLxcServer(t))
	err := c.UpdateLxcConfig(context.Background(), &GuestRef{VMID: 399, Node: "pve1", Type: "lxc"}, map[string]any{"cores": 2})
	if err == nil {
		t.Fatal("UpdateLxcConfig(399) error = nil, want failure for a missing container")
	}
}

func TestStorageContent(t *testing.T) {
	s := newTestServer(t)
	s.AddVolume("pve1", "local", pvefake.Volume{VolID: testTemplate, Content: "vztmpl", Format: "tzst", Size: 126 << 20})
	s.AddVolume("pve1", "local", pvefake.Volume{VolID: "local:iso/debian-12.iso", Content: "iso", Format: "iso", Size: 600 << 20})
	s.AddVolume("pve2", "local", pvefake.Volume{VolID: "local:vztmpl/other-node.tar.zst", Content: "vztmpl", Format: "tzst"})
	c := newTestClient(t, s)

	vols, err := c.StorageContent(context.Background(), "pve1", "local", "vztmpl")
	if err != nil {
		t.Fatalf("StorageContent() error = %v", err)
	}
	want := []StorageVolume{{VolID: testTemplate, Content: "vztmpl", Format: "tzst", Size: 126 << 20}}
	if !slices.Equal(vols, want) {
		t.Errorf("StorageContent() = %+v, want %+v", vols, want)
	}

	empty, err := c.StorageContent(context.Background(), "pve1", "local-lvm", "vztmpl")
	if err != nil {
		t.Fatalf("StorageContent(empty storage) error = %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("StorageContent(empty storage) = %+v, want none", empty)
	}
}

func TestDownloadURL(t *testing.T) {
	const filename = "pvectl-3f2a0000aaaabbbb-debian-12-standard_12.7-1_amd64.tar.zst"
	params := map[string]any{
		"content":            "vztmpl",
		"filename":           filename,
		"url":                "https://download.example/debian-12-standard_12.7-1_amd64.tar.zst",
		"checksum":           strings.Repeat("3f2a", 16),
		"checksum-algorithm": "sha256",
	}

	t.Run("downloaded file shows up in the storage content", func(t *testing.T) {
		s := newTestServer(t)
		c := newTestClient(t, s)
		ctx := context.Background()

		if err := c.DownloadURL(ctx, "pve1", "local", params); err != nil {
			t.Fatalf("DownloadURL() error = %v", err)
		}
		if !s.SawRequest("POST", "/api2/json/nodes/pve1/storage/local/download-url") {
			t.Errorf("download did not reach download-url: %v", s.Requests())
		}
		downloads := s.Downloads()
		if len(downloads) != 1 || downloads[0]["checksum-algorithm"] != "sha256" || downloads[0]["checksum"] != params["checksum"] {
			t.Errorf("downloads = %+v, want one sha256-checked download", downloads)
		}

		vols, err := c.StorageContent(ctx, "pve1", "local", "vztmpl")
		if err != nil {
			t.Fatalf("StorageContent() error = %v", err)
		}
		if len(vols) != 1 || vols[0].VolID != "local:vztmpl/"+filename {
			t.Errorf("StorageContent() after download = %+v, want local:vztmpl/%s", vols, filename)
		}
	})

	t.Run("checksum mismatch fails the task and stores nothing", func(t *testing.T) {
		s := newTestServer(t)
		c := newTestClient(t, s)
		s.FailNext("download", "checksum verification failed")

		err := c.DownloadURL(context.Background(), "pve1", "local", params)
		if err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
			t.Fatalf("DownloadURL() error = %v, want task exit status", err)
		}
		vols, err := c.StorageContent(context.Background(), "pve1", "local", "vztmpl")
		if err != nil {
			t.Fatalf("StorageContent() error = %v", err)
		}
		if len(vols) != 0 {
			t.Errorf("StorageContent() after failed download = %+v, want none", vols)
		}
	})

	t.Run("missing filename is rejected", func(t *testing.T) {
		c := newTestClient(t, newTestServer(t))
		err := c.DownloadURL(context.Background(), "pve1", "local", map[string]any{"content": "vztmpl", "url": "https://x/y.tar.zst"})
		if err == nil {
			t.Fatal("DownloadURL() without filename error = nil, want rejection")
		}
	})
}

func TestStorageContentFailure(t *testing.T) {
	s := newTestServer(t)
	c := newTestClient(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.StorageContent(ctx, "pve1", "local", "vztmpl"); !errors.Is(err, context.Canceled) {
		t.Fatalf("StorageContent() on cancelled context error = %v, want context.Canceled", err)
	}
}

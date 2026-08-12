package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cyokozai/pvectl/internal/config"
	"github.com/cyokozai/pvectl/test/pvefake"
)

const testToken = "ci@pam!pvectl=00000000-0000-0000-0000-000000000000"

func newTestServer(t *testing.T) *pvefake.Server {
	t.Helper()
	s := pvefake.New()
	t.Cleanup(s.Close)
	s.Token = testToken
	s.AddGuest(pvefake.Guest{
		VMID: 100, Name: "web-server", Node: "pve1", Status: "running",
		Config: map[string]any{"cores": "2", "memory": "2048", "name": "web-server"},
	})
	s.AddGuest(pvefake.Guest{VMID: 101, Name: "db", Node: "pve2", Status: "stopped"})
	return s
}

func newTestClient(t *testing.T, s *pvefake.Server) Client {
	t.Helper()
	c, err := New(context.Background(),
		config.Node{Server: s.URL()},
		config.User{Token: testToken},
		Options{TaskTimeout: 10 * time.Second},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

func TestNewTokenAuth(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		c := newTestClient(t, newTestServer(t))
		if _, err := c.ListGuests(context.Background()); err != nil {
			t.Fatalf("ListGuests() error = %v", err)
		}
	})

	t.Run("wrong token is rejected", func(t *testing.T) {
		s := newTestServer(t)
		c, err := New(context.Background(),
			config.Node{Server: s.URL()},
			config.User{Token: "wrong@pam!x=y"},
			Options{},
		)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if _, err := c.ListGuests(context.Background()); err == nil {
			t.Fatal("ListGuests() with wrong token: error = nil, want auth failure")
		}
	})

	t.Run("server URL without /api2/json suffix is normalized", func(t *testing.T) {
		s := newTestServer(t)
		base := strings.TrimSuffix(s.URL(), "/api2/json")
		c, err := New(context.Background(), config.Node{Server: base}, config.User{Token: testToken}, Options{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if _, err := c.ListGuests(context.Background()); err != nil {
			t.Fatalf("ListGuests() error = %v", err)
		}
	})
}

func TestNewPasswordAuth(t *testing.T) {
	s := newTestServer(t)
	c, err := New(context.Background(),
		config.Node{Server: s.URL()},
		config.User{Username: "dev@pam", Password: "hunter2"},
		Options{},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.ListGuests(context.Background()); err != nil {
		t.Fatalf("ListGuests() after login error = %v", err)
	}
}

func TestNewNoCredentials(t *testing.T) {
	s := newTestServer(t)
	if _, err := New(context.Background(), config.Node{Server: s.URL()}, config.User{}, Options{}); err == nil {
		t.Fatal("New() error = nil, want no-authentication error")
	}
}

func TestListGuests(t *testing.T) {
	c := newTestClient(t, newTestServer(t))
	guests, err := c.ListGuests(context.Background())
	if err != nil {
		t.Fatalf("ListGuests() error = %v", err)
	}
	if len(guests) != 2 {
		t.Fatalf("ListGuests() = %d guests, want 2", len(guests))
	}
	byName := map[string]GuestSummary{}
	for _, g := range guests {
		byName[g.Name] = g
	}
	web := byName["web-server"]
	if web.VMID != 100 || web.Node != "pve1" || web.Status != "running" || web.Type != "qemu" {
		t.Errorf("web-server = %+v", web)
	}
}

func TestFindGuest(t *testing.T) {
	s := newTestServer(t)
	c := newTestClient(t, s)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		ref, err := c.FindGuest(ctx, "web-server")
		if err != nil {
			t.Fatalf("FindGuest() error = %v", err)
		}
		if ref.VMID != 100 || ref.Node != "pve1" || ref.Type != "qemu" {
			t.Errorf("ref = %+v", ref)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := c.FindGuest(ctx, "ghost")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("FindGuest(ghost) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("ambiguous name", func(t *testing.T) {
		s.AddGuest(pvefake.Guest{VMID: 200, Name: "web-server", Node: "pve2"})
		_, err := c.FindGuest(ctx, "web-server")
		if !errors.Is(err, ErrAmbiguousName) {
			t.Fatalf("FindGuest(dup) error = %v, want ErrAmbiguousName", err)
		}
	})
}

func TestGuestByID(t *testing.T) {
	c := newTestClient(t, newTestServer(t))
	ctx := context.Background()

	ref, err := c.GuestByID(ctx, 101)
	if err != nil || ref.Node != "pve2" {
		t.Fatalf("GuestByID(101) = %+v, %v", ref, err)
	}
	if _, err := c.GuestByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GuestByID(999) error = %v, want ErrNotFound", err)
	}
}

func TestQemuConfig(t *testing.T) {
	c := newTestClient(t, newTestServer(t))
	cfg, err := c.QemuConfig(context.Background(), &GuestRef{VMID: 100, Node: "pve1", Type: "qemu"})
	if err != nil {
		t.Fatalf("QemuConfig() error = %v", err)
	}
	if cfg["cores"] != "2" || cfg["memory"] != "2048" {
		t.Errorf("config = %+v, want cores=2 memory=2048", cfg)
	}
}

func TestCreateQemu(t *testing.T) {
	t.Run("success stores config", func(t *testing.T) {
		s := newTestServer(t)
		c := newTestClient(t, s)
		params := map[string]any{"name": "new-vm", "cores": 4, "memory": 4096}
		if err := c.CreateQemu(context.Background(), "pve1", 105, params); err != nil {
			t.Fatalf("CreateQemu() error = %v", err)
		}
		g := s.Guest(105)
		if g == nil || g.Name != "new-vm" || g.Node != "pve1" || g.Config["cores"] != "4" {
			t.Fatalf("fake guest = %+v, want created with config", g)
		}
	})

	t.Run("task failure surfaces exit status", func(t *testing.T) {
		s := newTestServer(t)
		c := newTestClient(t, s)
		s.FailNext("create", "unable to create VM 105 - no space")
		err := c.CreateQemu(context.Background(), "pve1", 105, map[string]any{"name": "x"})
		if err == nil || !strings.Contains(err.Error(), "no space") {
			t.Fatalf("CreateQemu() error = %v, want task exit status", err)
		}
	})
}

func TestUpdateQemuConfig(t *testing.T) {
	s := newTestServer(t)
	c := newTestClient(t, s)
	err := c.UpdateQemuConfig(context.Background(), &GuestRef{VMID: 100, Node: "pve1", Type: "qemu"},
		map[string]any{"cores": 8})
	if err != nil {
		t.Fatalf("UpdateQemuConfig() error = %v", err)
	}
	if g := s.Guest(100); g.Config["cores"] != "8" || g.Config["memory"] != "2048" {
		t.Errorf("config after update = %+v, want cores=8 memory kept", g.Config)
	}
}

func TestCloneQemu(t *testing.T) {
	s := newTestServer(t)
	c := newTestClient(t, s)
	src := &GuestRef{VMID: 100, Node: "pve1", Type: "qemu"}
	err := c.CloneQemu(context.Background(), src, map[string]any{
		"newid": 150, "name": "web-copy", "target": "pve2", "full": 1,
	})
	if err != nil {
		t.Fatalf("CloneQemu() error = %v", err)
	}
	if g := s.Guest(150); g == nil || g.Name != "web-copy" || g.Node != "pve2" {
		t.Fatalf("clone = %+v, want web-copy on pve2", g)
	}
}

func TestLifecycleAndDelete(t *testing.T) {
	s := newTestServer(t)
	c := newTestClient(t, s)
	ctx := context.Background()
	ref := &GuestRef{VMID: 101, Node: "pve2", Type: "qemu"}

	if err := c.StartGuest(ctx, ref); err != nil {
		t.Fatalf("StartGuest() error = %v", err)
	}
	if s.Guest(101).Status != "running" {
		t.Error("guest not running after StartGuest")
	}
	if err := c.StopGuest(ctx, ref); err != nil {
		t.Fatalf("StopGuest() error = %v", err)
	}
	if s.Guest(101).Status != "stopped" {
		t.Error("guest not stopped after StopGuest")
	}
	if err := c.DeleteGuest(ctx, ref); err != nil {
		t.Fatalf("DeleteGuest() error = %v", err)
	}
	if s.Guest(101) != nil {
		t.Error("guest still present after DeleteGuest")
	}
}

func TestNextID(t *testing.T) {
	c := newTestClient(t, newTestServer(t))
	id, err := c.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if id != 102 {
		t.Errorf("NextID() = %d, want 102", id)
	}
}

func TestRawGet(t *testing.T) {
	c := newTestClient(t, newTestServer(t))
	res, err := c.Raw().Get(context.Background(), "/cluster/nextid")
	if err != nil {
		t.Fatalf("Raw().Get() error = %v", err)
	}
	if res["data"] != "102" {
		t.Errorf("Raw().Get(/cluster/nextid) = %+v, want data=102", res)
	}
}

package api

import (
	"context"
	"strings"
	"testing"
)

// testRef is the qemu guest seeded by newTestServer.
var testRef = &GuestRef{VMID: 100, Node: "pve1", Type: "qemu"}

func TestMigrateGuest(t *testing.T) {
	s := newTestServer(t)
	c := newTestClient(t, s)

	if err := c.MigrateGuest(context.Background(), testRef, "pve2", true); err != nil {
		t.Fatalf("MigrateGuest() error = %v", err)
	}
	if node := s.Guest(100).Node; node != "pve2" {
		t.Errorf("guest node = %q, want pve2", node)
	}

	ref, err := c.FindGuest(context.Background(), "web-server")
	if err != nil {
		t.Fatalf("FindGuest() error = %v", err)
	}
	if ref.Node != "pve2" {
		t.Errorf("cluster resources still report node %q", ref.Node)
	}
}

func TestMigrateGuestSurfacesTaskFailure(t *testing.T) {
	s := newTestServer(t)
	s.FailNext("migrate", "migration aborted: target storage unavailable")
	c := newTestClient(t, s)

	err := c.MigrateGuest(context.Background(), testRef, "pve2", false)
	if err == nil {
		t.Fatal("MigrateGuest() error = nil, want the task failure")
	}
	if !strings.Contains(err.Error(), "target storage unavailable") {
		t.Errorf("error %q does not carry the task exit status", err)
	}
}

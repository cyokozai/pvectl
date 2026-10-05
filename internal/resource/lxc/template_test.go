package lxc

import (
	"reflect"
	"strings"
	"testing"
)

// testSHA is a 64-digit digest whose first 16 digits are 3f2a9c0d1e4b5a69.
const testSHA = "3f2a9c0d1e4b5a69" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef"

func TestDerivedVolID(t *testing.T) {
	tests := []struct {
		name string
		tmpl OSTemplate
		want string
	}{
		{
			"volid is returned as written",
			OSTemplate{VolID: "local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst"},
			"local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst",
		},
		{
			"url form embeds the first 16 hex digits and the basename",
			OSTemplate{
				URL:     "https://download.example/images/debian-12-standard_12.7-1_amd64.tar.zst",
				SHA256:  testSHA,
				Storage: "local",
			},
			"local:vztmpl/pvectl-3f2a9c0d1e4b5a69-debian-12-standard_12.7-1_amd64.tar.zst",
		},
		{
			"uppercase digest is lowered so the name is stable",
			OSTemplate{
				URL:     "https://download.example/alpine.tar.xz",
				SHA256:  strings.ToUpper(testSHA),
				Storage: "nfs-tmpl",
			},
			"nfs-tmpl:vztmpl/pvectl-3f2a9c0d1e4b5a69-alpine.tar.xz",
		},
		{
			"query string is not part of the basename",
			OSTemplate{
				URL:     "https://download.example/alpine.tar.xz?token=abc",
				SHA256:  testSHA,
				Storage: "local",
			},
			"local:vztmpl/pvectl-3f2a9c0d1e4b5a69-alpine.tar.xz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DerivedVolID(tt.tmpl); got != tt.want {
				t.Errorf("DerivedVolID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateStorage(t *testing.T) {
	if got := templateStorage(OSTemplate{VolID: "local:vztmpl/x.tar.zst"}); got != "local" {
		t.Errorf("volid form storage = %q, want local", got)
	}
	if got := templateStorage(OSTemplate{URL: "https://e/x.tar.zst", SHA256: testSHA, Storage: "nfs"}); got != "nfs" {
		t.Errorf("url form storage = %q, want nfs", got)
	}
}

func TestDownloadURLParams(t *testing.T) {
	tmpl := OSTemplate{
		URL:     "https://download.example/images/debian-12.tar.zst",
		SHA256:  testSHA,
		Storage: "local",
	}
	want := map[string]any{
		"content":            "vztmpl",
		"filename":           "pvectl-3f2a9c0d1e4b5a69-debian-12.tar.zst",
		"url":                "https://download.example/images/debian-12.tar.zst",
		"checksum":           testSHA,
		"checksum-algorithm": "sha256",
	}
	if got := downloadURLParams(tmpl); !reflect.DeepEqual(got, want) {
		t.Errorf("downloadURLParams() = %v, want %v", got, want)
	}
}

func TestNeedsDownload(t *testing.T) {
	if needsDownload(OSTemplate{VolID: "local:vztmpl/x.tar.zst"}) {
		t.Error("volid form must never download")
	}
	if !needsDownload(OSTemplate{URL: "https://e/x.tar.zst", SHA256: testSHA, Storage: "local"}) {
		t.Error("url form must be eligible for download")
	}
}

package runtime

import (
	"strings"
	"testing"
)

func TestTypeMetaGVK(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		kind       string
		want       GVK
	}{
		{
			name:       "group and version",
			apiVersion: "pve.io/v1alpha1",
			kind:       "VirtualMachine",
			want:       GVK{Group: "pve.io", Version: "v1alpha1", Kind: "VirtualMachine"},
		},
		{
			name:       "version only",
			apiVersion: "v1",
			kind:       "Config",
			want:       GVK{Group: "", Version: "v1", Kind: "Config"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TypeMeta{APIVersion: tt.apiVersion, Kind: tt.kind}.GVK()
			if err != nil {
				t.Fatalf("GVK() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("GVK() = %#v, want %#v", got, tt.want)
			}
			if got.APIVersion() != tt.apiVersion {
				t.Errorf("APIVersion() = %q, want %q", got.APIVersion(), tt.apiVersion)
			}
		})
	}
}

func TestTypeMetaGVKInvalid(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		kind       string
		wantIn     string
	}{
		{"missing apiVersion", "", "VirtualMachine", "apiVersion"},
		{"missing kind", "pve.io/v1alpha1", "", "kind"},
		{"too many segments", "pve.io/v1alpha1/extra", "VirtualMachine", "invalid apiVersion"},
		{"empty group", "/v1alpha1", "VirtualMachine", "invalid apiVersion"},
		{"empty version", "pve.io/", "VirtualMachine", "invalid apiVersion"},
		{"slash only", "/", "VirtualMachine", "invalid apiVersion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TypeMeta{APIVersion: tt.apiVersion, Kind: tt.kind}.GVK()
			if err == nil {
				t.Fatalf("GVK() = %#v, want error", got)
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("GVK() error = %q, want it to mention %q", err, tt.wantIn)
			}
		})
	}
}

func TestGVKString(t *testing.T) {
	tests := []struct {
		gvk  GVK
		want string
	}{
		{GVK{Group: "pve.io", Version: "v1alpha1", Kind: "VirtualMachine"}, "pve.io/v1alpha1, Kind=VirtualMachine"},
		{GVK{Version: "v1", Kind: "Config"}, "v1, Kind=Config"},
	}
	for _, tt := range tests {
		if got := tt.gvk.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

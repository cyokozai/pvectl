package vm

import (
	"strings"
	"testing"
)

// validateErr runs validateSpec and returns its message, or "" when the
// spec is valid.
func validateErr(t *testing.T, name string, spec *Spec) string {
	t.Helper()
	err := validateSpec(name, spec)
	if err == nil {
		return ""
	}
	return err.Error()
}

func minimalSpec() *Spec {
	return &Spec{
		TargetNode: "pve1",
		Resources:  Resources{CPU: CPU{Cores: 1}, Memory: 512},
	}
}

func TestValidateRaw(t *testing.T) {
	t.Run("unmodeled keys pass through", func(t *testing.T) {
		spec := minimalSpec()
		spec.Raw = map[string]string{
			"hookscript": "local:snippets/hook.pl",
			"args":       "-cpu host,+vmx",
			"bios":       "ovmf",
			"ipconfig1":  "ip=10.0.1.5/24",
		}
		if msg := validateErr(t, "x", spec); msg != "" {
			t.Errorf("validateSpec() = %v, want nil", msg)
		}
	})

	t.Run("key generated from a typed field errors", func(t *testing.T) {
		spec := minimalSpec()
		spec.Raw = map[string]string{"cores": "8"}
		msg := validateErr(t, "x", spec)
		if !strings.Contains(msg, "spec.raw.cores") {
			t.Errorf("validateSpec() = %v, want cores conflict", msg)
		}
	})

	t.Run("disk slot conflict is caught by the generated key set", func(t *testing.T) {
		spec := minimalSpec()
		spec.Disks = []Disk{{Name: "scsi0", Size: "8G", Storage: "local-lvm"}}
		spec.Raw = map[string]string{"scsi0": "local-lvm:16"}
		msg := validateErr(t, "x", spec)
		if !strings.Contains(msg, "spec.raw.scsi0") {
			t.Errorf("validateSpec() = %v, want scsi0 conflict", msg)
		}
	})

	t.Run("a slot not declared by a typed field is allowed", func(t *testing.T) {
		spec := minimalSpec()
		spec.Disks = []Disk{{Name: "scsi0", Size: "8G", Storage: "local-lvm"}}
		spec.Raw = map[string]string{"scsi1": "local-lvm:16"}
		if msg := validateErr(t, "x", spec); msg != "" {
			t.Errorf("validateSpec() = %v, want nil", msg)
		}
	})

	t.Run("cloud-init drive slot conflict is caught", func(t *testing.T) {
		spec := minimalSpec()
		spec.CloudInit = &CloudInit{User: "admin", Storage: "local-lvm"}
		spec.Raw = map[string]string{cloudInitSlot: "local-lvm:cloudinit"}
		msg := validateErr(t, "x", spec)
		if !strings.Contains(msg, "spec.raw."+cloudInitSlot) {
			t.Errorf("validateSpec() = %v, want %s conflict", msg, cloudInitSlot)
		}
	})

	t.Run("empty key errors", func(t *testing.T) {
		spec := minimalSpec()
		spec.Raw = map[string]string{"": "x"}
		if msg := validateErr(t, "x", spec); !strings.Contains(msg, "empty key") {
			t.Errorf("validateSpec() = %v, want empty key error", msg)
		}
	})
}

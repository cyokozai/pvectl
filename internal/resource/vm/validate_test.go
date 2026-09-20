package vm

import (
	"strings"
	"testing"
)

// validateErr runs validateSpec and returns its message, or "" when the
// spec is valid.
func validateErr(t *testing.T, spec *Spec) string {
	t.Helper()
	err := validateSpec("x", spec)
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

func TestValidateCloneExclusivity(t *testing.T) {
	t.Run("clone without disks is valid", func(t *testing.T) {
		spec := &Spec{TargetNode: "pve1", Clone: "ubuntu-template"}
		if msg := validateErr(t, spec); msg != "" {
			t.Errorf("validateSpec() = %v, want nil", msg)
		}
	})

	t.Run("clone with disks errors", func(t *testing.T) {
		spec := &Spec{
			TargetNode: "pve1",
			Clone:      "ubuntu-template",
			Disks:      []Disk{{Name: "scsi0", Size: "64G", Storage: "local-lvm"}},
		}
		msg := validateErr(t, spec)
		if !strings.Contains(msg, "spec.disks") || !strings.Contains(msg, "spec.clone") {
			t.Errorf("validateSpec() = %v, want an exclusivity error", msg)
		}
	})

	t.Run("disks without clone still need sizing", func(t *testing.T) {
		spec := &Spec{
			TargetNode: "pve1",
			Disks:      []Disk{{Name: "scsi0", Size: "64G", Storage: "local-lvm"}},
		}
		if msg := validateErr(t, spec); !strings.Contains(msg, "cores") {
			t.Errorf("validateSpec() = %v, want the direct-create sizing rules", msg)
		}
	})
}

func TestValidateRunStrategy(t *testing.T) {
	for _, s := range []RunStrategy{"", RunStrategyHalted, RunStrategyAlways, RunStrategyManual} {
		spec := minimalSpec()
		spec.RunStrategy = s
		if msg := validateErr(t, spec); msg != "" {
			t.Errorf("runStrategy %q rejected: %v", s, msg)
		}
	}
	for _, s := range []RunStrategy{"always", "Running", "RunOnce", "true"} {
		spec := minimalSpec()
		spec.RunStrategy = s
		msg := validateErr(t, spec)
		if !strings.Contains(msg, "spec.runStrategy") || !strings.Contains(msg, "Halted / Always / Manual") {
			t.Errorf("runStrategy %q = %v, want the accepted values listed", s, msg)
		}
	}
}

func TestValidatePasswordFrom(t *testing.T) {
	for _, ref := range []string{"env:PVE_VM_PASSWORD", "file:/run/secrets/vmpw"} {
		spec := minimalSpec()
		spec.CloudInit = &CloudInit{Storage: "local-lvm", PasswordFrom: ref}
		if msg := validateErr(t, spec); msg != "" {
			t.Errorf("passwordFrom %q rejected: %v", ref, msg)
		}
	}
	for _, ref := range []string{"hunter2", "env:", "vault:kv/vmpw"} {
		spec := minimalSpec()
		spec.CloudInit = &CloudInit{Storage: "local-lvm", PasswordFrom: ref}
		if msg := validateErr(t, spec); !strings.Contains(msg, "spec.cloudInit.passwordFrom") {
			t.Errorf("passwordFrom %q = %v, want a reference syntax error", ref, msg)
		}
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
		if msg := validateErr(t, spec); msg != "" {
			t.Errorf("validateSpec() = %v, want nil", msg)
		}
	})

	t.Run("key generated from a typed field errors", func(t *testing.T) {
		spec := minimalSpec()
		spec.Raw = map[string]string{"cores": "8"}
		msg := validateErr(t, spec)
		if !strings.Contains(msg, "spec.raw.cores") {
			t.Errorf("validateSpec() = %v, want cores conflict", msg)
		}
	})

	t.Run("disk slot conflict is caught by the generated key set", func(t *testing.T) {
		spec := minimalSpec()
		spec.Disks = []Disk{{Name: "scsi0", Size: "8G", Storage: "local-lvm"}}
		spec.Raw = map[string]string{"scsi0": "local-lvm:16"}
		msg := validateErr(t, spec)
		if !strings.Contains(msg, "spec.raw.scsi0") {
			t.Errorf("validateSpec() = %v, want scsi0 conflict", msg)
		}
	})

	t.Run("a slot not declared by a typed field is allowed", func(t *testing.T) {
		spec := minimalSpec()
		spec.Disks = []Disk{{Name: "scsi0", Size: "8G", Storage: "local-lvm"}}
		spec.Raw = map[string]string{"scsi1": "local-lvm:16"}
		if msg := validateErr(t, spec); msg != "" {
			t.Errorf("validateSpec() = %v, want nil", msg)
		}
	})

	t.Run("cloud-init drive slot conflict is caught", func(t *testing.T) {
		spec := minimalSpec()
		spec.CloudInit = &CloudInit{User: "admin", Storage: "local-lvm"}
		spec.Raw = map[string]string{cloudInitSlot: "local-lvm:cloudinit"}
		msg := validateErr(t, spec)
		if !strings.Contains(msg, "spec.raw."+cloudInitSlot) {
			t.Errorf("validateSpec() = %v, want %s conflict", msg, cloudInitSlot)
		}
	})

	t.Run("cipassword conflicts even before the reference is resolved", func(t *testing.T) {
		spec := minimalSpec()
		spec.CloudInit = &CloudInit{Storage: "local-lvm", PasswordFrom: "env:PVE_VM_PASSWORD"}
		spec.Raw = map[string]string{"cipassword": "hunter2"}
		if msg := validateErr(t, spec); !strings.Contains(msg, "spec.raw.cipassword") {
			t.Errorf("validateSpec() = %v, want cipassword conflict", msg)
		}
	})

	t.Run("empty key errors", func(t *testing.T) {
		spec := minimalSpec()
		spec.Raw = map[string]string{"": "x"}
		if msg := validateErr(t, spec); !strings.Contains(msg, "empty key") {
			t.Errorf("validateSpec() = %v, want empty key error", msg)
		}
	})
}

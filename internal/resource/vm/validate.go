package vm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cyokozai/pvectl/internal/secretref"
)

// validateSpec checks a decoded manifest before any API call. All
// problems are reported at once.
func validateSpec(name string, spec *Spec) error {
	var errs []string

	if name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if spec.TargetNode == "" {
		errs = append(errs, "spec.targetNode is required")
	}
	if spec.Clone == "" {
		// Direct creates need explicit sizing; clones inherit it.
		if spec.Resources.CPU.Cores <= 0 {
			errs = append(errs, "spec.resources.cpu.cores must be greater than 0")
		}
		if spec.Resources.Memory <= 0 {
			errs = append(errs, "spec.resources.memory (MB) must be greater than 0")
		}
	}
	if !validRunStrategy(spec.RunStrategy) {
		errs = append(errs, fmt.Sprintf("spec.runStrategy %q must be one of %s", spec.RunStrategy, strings.Join(runStrategyNames(), " / ")))
	}
	if spec.VMID != nil && *spec.VMID < 100 {
		errs = append(errs, fmt.Sprintf("spec.vmid must be >= 100, got %d", *spec.VMID))
	}

	for i, disk := range spec.Disks {
		where := fmt.Sprintf("spec.disks[%d]", i)
		if !rxDiskKey.MatchString(disk.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q must be a bus slot like scsi0 or virtio0", where, disk.Name))
		}
		if disk.Storage == "" {
			errs = append(errs, where+".storage is required")
		}
		if parseSizeBytes(disk.Size) <= 0 {
			errs = append(errs, fmt.Sprintf("%s.size %q must be a size like 32G", where, disk.Size))
		}
	}

	for i, net := range spec.Networks {
		where := fmt.Sprintf("spec.networks[%d]", i)
		if !rxNetKey.MatchString(net.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q must be an interface slot like net0", where, net.Name))
		}
		if net.Bridge == "" {
			errs = append(errs, where+".bridge is required")
		}
	}

	if ci := spec.CloudInit; ci != nil {
		if spec.Clone == "" && ci.Storage == "" && len(spec.Disks) == 0 {
			errs = append(errs, "spec.cloudInit needs spec.cloudInit.storage (or at least one disk) for the cloud-init drive")
		}
		// Only the syntax is checked here: whether the target exists is
		// a property of the machine running apply, not of the manifest.
		if ci.PasswordFrom != "" {
			if err := secretref.Validate(ci.PasswordFrom); err != nil {
				errs = append(errs, "spec.cloudInit.passwordFrom "+err.Error())
			}
		}
	}

	errs = append(errs, rawErrors(name, spec)...)

	if len(errs) > 0 {
		return fmt.Errorf("invalid VirtualMachine %q:\n  - %s", name, strings.Join(errs, "\n  - "))
	}
	return nil
}

// validRunStrategy accepts the three strategies plus the empty value,
// which means "not declared" and behaves as Manual.
func validRunStrategy(s RunStrategy) bool {
	if s == "" {
		return true
	}
	for _, known := range runStrategies {
		if s == known {
			return true
		}
	}
	return false
}

func runStrategyNames() []string {
	names := make([]string, len(runStrategies))
	for i, s := range runStrategies {
		names[i] = string(s)
	}
	return names
}

// rawErrors rejects spec.raw keys that pvectl already generates from a
// typed field. Double management is a silent-overwrite hazard: whichever
// of the two won would depend on map iteration, not on the manifest.
func rawErrors(name string, spec *Spec) []string {
	var errs []string
	for _, key := range sortedRawKeys(spec) {
		if strings.TrimSpace(key) == "" {
			errs = append(errs, "spec.raw has an empty key")
		}
	}
	for _, key := range rawConflicts(name, spec) {
		errs = append(errs, fmt.Sprintf(
			"spec.raw.%s duplicates a key pvectl already generates from a typed spec field; declare it in one place only", key))
	}
	return errs
}

func sortedRawKeys(spec *Spec) []string {
	keys := make([]string, 0, len(spec.Raw))
	for k := range spec.Raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

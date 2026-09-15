package vm

import (
	"fmt"
	"strings"
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

	if spec.CloudInit != nil && spec.Clone == "" && spec.CloudInit.Storage == "" && len(spec.Disks) == 0 {
		errs = append(errs, "spec.cloudInit needs spec.cloudInit.storage (or at least one disk) for the cloud-init drive")
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid VirtualMachine %q:\n  - %s", name, strings.Join(errs, "\n  - "))
	}
	return nil
}

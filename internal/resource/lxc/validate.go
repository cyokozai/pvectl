package lxc

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/cyokozai/pvectl/internal/secretref"
)

var (
	rxMountKey = regexp.MustCompile(`^mp\d+$`)
	rxNetKey   = regexp.MustCompile(`^net\d+$`)
	rxSHA256   = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	// rxTemplateVolID is "storage:vztmpl/file".
	rxTemplateVolID = regexp.MustCompile(`^[^:\s/]+:vztmpl/\S+$`)
)

// maxVLANTag is the largest valid 802.1Q VLAN id.
const maxVLANTag = 4094

// validateSpec checks a decoded manifest before any API call. All
// problems are reported at once. It accepts both create and update
// manifests; validateForCreate adds what only a create needs.
func validateSpec(name string, spec *Spec) error {
	var errs []string

	if name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if spec.TargetNode == "" {
		errs = append(errs, "spec.targetNode is required")
	}
	if spec.Unprivileged == nil {
		errs = append(errs, "spec.unprivileged is required (true or false): Proxmox creates a privileged container "+
			"when it is omitted, and it cannot be changed after create (ADR-010 §3)")
	}
	if spec.VMID != nil && *spec.VMID < 100 {
		errs = append(errs, fmt.Sprintf("spec.vmid must be >= 100, got %d", *spec.VMID))
	}
	if !validRunStrategy(spec.RunStrategy) {
		errs = append(errs, fmt.Sprintf("spec.runStrategy %q must be one of %s", spec.RunStrategy, strings.Join(runStrategyNames(), " / ")))
	}
	if spec.OSTemplate != nil {
		errs = append(errs, osTemplateErrors(*spec.OSTemplate)...)
	}
	errs = append(errs, volumeErrors(spec)...)
	errs = append(errs, networkErrors(spec)...)

	// Only the syntax is checked here: whether the target exists is a
	// property of the machine running apply, not of the manifest.
	if spec.Root != nil && spec.Root.PasswordFrom != "" {
		if err := secretref.Validate(spec.Root.PasswordFrom); err != nil {
			errs = append(errs, "spec.root.passwordFrom "+err.Error())
		}
	}

	errs = append(errs, rawErrors(name, spec)...)

	if len(errs) > 0 {
		return fmt.Errorf("invalid LinuxContainer %q:\n  - %s", name, strings.Join(errs, "\n  - "))
	}
	return nil
}

// validateForCreate adds the checks only a create needs. osTemplate is
// not required by validateSpec because a manifest exported with
// get -o yaml carries none and must still apply cleanly.
func validateForCreate(name string, spec *Spec) error {
	if spec.OSTemplate == nil {
		return fmt.Errorf("invalid LinuxContainer %q: spec.osTemplate is required to create a container "+
			"(volid, or url + sha256 + storage)", name)
	}
	return nil
}

// osTemplateErrors checks the two exclusive osTemplate forms (ADR-010 §4).
func osTemplateErrors(t OSTemplate) []string {
	var errs []string
	switch {
	case t.VolID == "" && t.URL == "":
		return []string{"spec.osTemplate needs either volid or url (with sha256 and storage)"}
	case t.VolID != "" && t.URL != "":
		return []string{"spec.osTemplate.volid and spec.osTemplate.url are exclusive; write one or the other"}
	case t.VolID != "":
		if !rxTemplateVolID.MatchString(t.VolID) {
			errs = append(errs, fmt.Sprintf("spec.osTemplate.volid %q must look like storage:vztmpl/file", t.VolID))
		}
		// Declared but unused fields would be silently ignored.
		if t.SHA256 != "" {
			errs = append(errs, "spec.osTemplate.sha256 is only used with url; drop it or replace volid with url")
		}
		if t.Storage != "" {
			errs = append(errs, "spec.osTemplate.storage is only used with url; the storage of a volid is its prefix")
		}
	default:
		if u, err := url.Parse(t.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Sprintf("spec.osTemplate.url %q must be an http or https URL", t.URL))
		} else if base := path.Base(u.Path); base == "/" || base == "." || strings.HasSuffix(u.Path, "/") {
			errs = append(errs, fmt.Sprintf("spec.osTemplate.url %q must end in a file name", t.URL))
		}
		if t.SHA256 == "" {
			errs = append(errs, "spec.osTemplate.sha256 is required with url")
		} else if !rxSHA256.MatchString(t.SHA256) {
			errs = append(errs, fmt.Sprintf("spec.osTemplate.sha256 %q must be 64 hex digits", t.SHA256))
		}
		if t.Storage == "" {
			errs = append(errs, "spec.osTemplate.storage is required with url")
		}
	}
	return errs
}

// volumeErrors checks rootfs and the mount points.
func volumeErrors(spec *Spec) []string {
	var errs []string
	if fs := spec.RootFS; fs != nil {
		if fs.Storage == "" {
			errs = append(errs, "spec.rootfs.storage is required")
		}
		if parseSizeBytes(fs.Size) <= 0 {
			errs = append(errs, fmt.Sprintf("spec.rootfs.size %q must be a size like 8G", fs.Size))
		}
	}
	seen := map[string]bool{}
	for i, mp := range spec.MountPoints {
		where := fmt.Sprintf("spec.mountPoints[%d]", i)
		switch {
		case !rxMountKey.MatchString(mp.Name):
			errs = append(errs, fmt.Sprintf("%s.name %q must be a mount point slot like mp0", where, mp.Name))
		case seen[mp.Name]:
			errs = append(errs, fmt.Sprintf("%s.name %q is declared more than once", where, mp.Name))
		}
		seen[mp.Name] = true
		if mp.Storage == "" {
			errs = append(errs, where+".storage is required")
		}
		if parseSizeBytes(mp.Size) <= 0 {
			errs = append(errs, fmt.Sprintf("%s.size %q must be a size like 32G", where, mp.Size))
		}
		if !strings.HasPrefix(mp.Path, "/") {
			errs = append(errs, fmt.Sprintf("%s.path %q must be an absolute path inside the container", where, mp.Path))
		}
	}
	return errs
}

// networkErrors checks the NICs.
func networkErrors(spec *Spec) []string {
	var errs []string
	seen := map[string]bool{}
	for i, n := range spec.Networks {
		where := fmt.Sprintf("spec.networks[%d]", i)
		switch {
		case !rxNetKey.MatchString(n.Name):
			errs = append(errs, fmt.Sprintf("%s.name %q must be an interface slot like net0", where, n.Name))
		case seen[n.Name]:
			errs = append(errs, fmt.Sprintf("%s.name %q is declared more than once", where, n.Name))
		}
		seen[n.Name] = true
		if n.Bridge == "" {
			errs = append(errs, where+".bridge is required")
		}
		if !validIP(n.IP) {
			errs = append(errs, fmt.Sprintf("%s.ip %q must be dhcp, manual, or a CIDR like 10.0.0.5/24", where, n.IP))
		}
		if n.GW != "" && net.ParseIP(n.GW) == nil {
			errs = append(errs, fmt.Sprintf("%s.gw %q must be an IP address", where, n.GW))
		}
		if n.Tag < 0 || n.Tag > maxVLANTag {
			errs = append(errs, fmt.Sprintf("%s.tag %d must be between 1 and %d", where, n.Tag, maxVLANTag))
		}
		if n.HWAddr != "" {
			if _, err := net.ParseMAC(n.HWAddr); err != nil {
				errs = append(errs, fmt.Sprintf("%s.hwaddr %q must be a MAC address like BC:24:11:00:00:01", where, n.HWAddr))
			}
		}
	}
	return errs
}

func validIP(ip string) bool {
	switch ip {
	case "", "dhcp", "manual":
		return true
	}
	_, _, err := net.ParseCIDR(ip)
	return err == nil
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
// typed field. Double management is a silent-overwrite hazard.
func rawErrors(name string, spec *Spec) []string {
	var errs []string
	keys := make([]string, 0, len(spec.Raw))
	for k := range spec.Raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
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

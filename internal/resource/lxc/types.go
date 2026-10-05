// Package lxc implements the LinuxContainer resource kind (Proxmox VE
// lxc guests): manifest schema, conversion to flat API params,
// live-state normalization, and the resource.Handler wiring (ADR-010).
//
// Nothing is shared with internal/resource/vm on purpose (ADR-010 §1):
// the small helpers both kinds need are duplicated here and will be
// extracted once a third kind exists.
package lxc

import (
	"github.com/cyokozai/pvectl/internal/runtime"
)

// Manifest type identity.
const (
	Group   = "pve.io"
	Version = "v1alpha1"
	// APIVersion is the manifest apiVersion, i.e. Group/Version.
	APIVersion = Group + "/" + Version
	KindName   = "LinuxContainer"
)

// GroupVersionKind is the registry dispatch key for LinuxContainer.
var GroupVersionKind = runtime.GVK{Group: Group, Version: Version, Kind: KindName}

// Spec is the LinuxContainer manifest spec. metadata.name is the
// container's hostname; there is deliberately no separate hostname
// field (ADR-010 §1).
type Spec struct {
	// TargetNode is the node the container lives on. Required. Immutable.
	TargetNode string `yaml:"targetNode" json:"targetNode"`
	// VMID pins the numeric id. Optional on create (next free id is
	// used); immutable afterwards.
	VMID *int `yaml:"vmid,omitempty" json:"vmid,omitempty"`
	// Unprivileged is required (ADR-010 §3): the API silently creates a
	// privileged container when it is omitted, and it cannot be changed
	// after create. A pointer so "not written" is distinguishable from
	// false.
	Unprivileged *bool `yaml:"unprivileged,omitempty" json:"unprivileged,omitempty"`
	// OSTemplate is where the root filesystem comes from. Create-time
	// only: it states provenance and is never compared against live
	// state (ADR-010 §4).
	OSTemplate *OSTemplate `yaml:"osTemplate,omitempty" json:"osTemplate,omitempty"`
	// Pool assigns the container to a resource pool. Create-time only.
	Pool        string       `yaml:"pool,omitempty" json:"pool,omitempty"`
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
	Resources   Resources    `yaml:"resources,omitempty" json:"resources,omitempty"`
	RootFS      *RootFS      `yaml:"rootfs,omitempty" json:"rootfs,omitempty"`
	MountPoints []MountPoint `yaml:"mountPoints,omitempty" json:"mountPoints,omitempty"`
	Networks    []Network    `yaml:"networks,omitempty" json:"networks,omitempty"`
	Nameserver  string       `yaml:"nameserver,omitempty" json:"nameserver,omitempty"`
	// Features lists the container feature flags. Only the sub-keys
	// written are managed; the rest stay as they are live.
	Features *Features `yaml:"features,omitempty" json:"features,omitempty"`
	// Root carries the create-time-only credentials (ADR-010 §5).
	Root *Root `yaml:"root,omitempty" json:"root,omitempty"`
	// RunStrategy is the desired power state apply converges to.
	// Defaults to Manual, which leaves the power state alone.
	RunStrategy RunStrategy `yaml:"runStrategy,omitempty" json:"runStrategy,omitempty"`
	Tags        []string    `yaml:"tags,omitempty" json:"tags,omitempty"`
	// Raw is the escape hatch for Proxmox API config keys pvectl does
	// not model yet (ADR-006 §7). Keys are flat API keys and values are
	// passed through unvalidated. Declaring a key pvectl already
	// generates from a typed field is an error.
	Raw map[string]string `yaml:"raw,omitempty" json:"raw,omitempty"`
}

// OSTemplate names the template a container is created from, either
// directly by volid or by {url, sha256, storage}. The two forms are
// exclusive (ADR-010 §4).
type OSTemplate struct {
	// VolID like "local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst".
	VolID string `yaml:"volid,omitempty" json:"volid,omitempty"`
	// URL to download the template from when it is not on Storage yet.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// SHA256 of the template file, 64 hex digits. Part of the derived
	// volid, so a changed file never hides behind an unchanged name.
	SHA256 string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	// Storage receives the downloaded template (content type vztmpl).
	Storage string `yaml:"storage,omitempty" json:"storage,omitempty"`
}

// RunStrategy says what power state apply should converge the
// container to (ADR-005 §3). It is a desired state, not a one-shot
// command: the imperative verbs live in `pvectl start` / `stop`.
type RunStrategy string

// The three run strategies.
const (
	// RunStrategyHalted sets onboot=0 and stops the container if it is running.
	RunStrategyHalted RunStrategy = "Halted"
	// RunStrategyAlways sets onboot=1 and starts the container if it is stopped.
	RunStrategyAlways RunStrategy = "Always"
	// RunStrategyManual is the default: apply never touches the power
	// state and leaves onboot unmanaged.
	RunStrategyManual RunStrategy = "Manual"
)

// runStrategies lists the accepted values for error messages.
var runStrategies = []RunStrategy{RunStrategyHalted, RunStrategyAlways, RunStrategyManual}

// Resources holds CPU, memory and swap sizing.
type Resources struct {
	Cores int `yaml:"cores,omitempty" json:"cores,omitempty"`
	// Memory in MiB.
	Memory int `yaml:"memory,omitempty" json:"memory,omitempty"`
	// Swap in MiB. A pointer because 0 (no swap) is a meaningful value.
	Swap *int `yaml:"swap,omitempty" json:"swap,omitempty"`
}

// RootFS is the container's root volume.
type RootFS struct {
	Storage string `yaml:"storage" json:"storage"`
	// Size like "8G". Grows are applied via resize; shrinking errors.
	Size string `yaml:"size" json:"size"`
}

// MountPoint is one additional storage-backed volume.
type MountPoint struct {
	// Name is the slot, e.g. "mp0".
	Name    string `yaml:"name" json:"name"`
	Storage string `yaml:"storage" json:"storage"`
	// Size like "32G". Grows are applied via resize; shrinking errors.
	Size string `yaml:"size" json:"size"`
	// Path is where the volume is mounted inside the container.
	Path string `yaml:"path" json:"path"`
}

// Network is one virtual NIC. Unlike a VM NIC it carries the IP
// configuration itself.
type Network struct {
	// Name is the interface slot, e.g. "net0".
	Name string `yaml:"name" json:"name"`
	// Interface is the device name inside the container. Defaults to
	// eth{N} for slot net{N}.
	Interface string `yaml:"interface,omitempty" json:"interface,omitempty"`
	Bridge    string `yaml:"bridge" json:"bridge"`
	// IP is "dhcp", "manual", or a CIDR like "10.0.0.5/24".
	IP string `yaml:"ip,omitempty" json:"ip,omitempty"`
	GW string `yaml:"gw,omitempty" json:"gw,omitempty"`
	// Tag is the VLAN tag.
	Tag int `yaml:"tag,omitempty" json:"tag,omitempty"`
	// HWAddr pins the MAC address. Proxmox generates one otherwise, and
	// it is compared only when written here.
	HWAddr string `yaml:"hwaddr,omitempty" json:"hwaddr,omitempty"`
}

// Features are the container feature flags. Each is a pointer so that
// "not written" (unmanaged) differs from false.
type Features struct {
	Nesting *bool `yaml:"nesting,omitempty" json:"nesting,omitempty"`
	Keyctl  *bool `yaml:"keyctl,omitempty" json:"keyctl,omitempty"`
	Fuse    *bool `yaml:"fuse,omitempty" json:"fuse,omitempty"`
	Mknod   *bool `yaml:"mknod,omitempty" json:"mknod,omitempty"`
}

// Root holds the initial root credentials. LXC accepts them only on
// create and never returns them, so they are write-only: sent once,
// never diffed or updated (ADR-010 §5).
type Root struct {
	// PasswordFrom points at the password instead of carrying it:
	// "env:VARIABLE" or "file:/path/to/file". Resolved when apply runs.
	PasswordFrom string   `yaml:"passwordFrom,omitempty" json:"passwordFrom,omitempty"`
	SSHKeys      []string `yaml:"sshKeys,omitempty" json:"sshKeys,omitempty"`

	// resolvedPassword holds what PasswordFrom pointed at, filled in by
	// resolveRootPassword. Unexported so it can never be read from or
	// written to a manifest.
	resolvedPassword string
}

// Status is the live state reported under a LinuxContainer object.
type Status struct {
	VMID   int     `yaml:"vmid" json:"vmid"`
	Node   string  `yaml:"node" json:"node"`
	State  string  `yaml:"state" json:"state"`
	Uptime int64   `yaml:"uptime,omitempty" json:"uptime,omitempty"`
	CPU    float64 `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	MaxMem uint64  `yaml:"maxMemBytes,omitempty" json:"maxMemBytes,omitempty"`
}

// LinuxContainer is the printable, manifest-shaped object returned by
// Get/List. Its yaml/json form round-trips into apply -f.
type LinuxContainer struct {
	runtime.TypeMeta `yaml:",inline"`
	Metadata         runtime.ObjectMeta `yaml:"metadata" json:"metadata"`
	Spec             Spec               `yaml:"spec" json:"spec"`
	Status           *Status            `yaml:"status,omitempty" json:"status,omitempty"`
}

// GetKind implements printer.Object.
func (c *LinuxContainer) GetKind() string { return c.Kind }

// GetName implements printer.Object.
func (c *LinuxContainer) GetName() string { return c.Metadata.Name }

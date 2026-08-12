// Package vm implements the VirtualMachine resource kind (Proxmox VE
// qemu guests): manifest schema, conversion to flat API params,
// live-state normalization, and the resource.Handler wiring.
package vm

import (
	"github.com/cyokozai/pvectl/internal/runtime"
)

// Manifest type identity.
const (
	APIVersion = "pve.io/v1alpha1"
	KindName   = "VirtualMachine"
)

// Spec is the VirtualMachine manifest spec.
type Spec struct {
	// TargetNode is the node the VM lives on. Required. Immutable.
	TargetNode string `yaml:"targetNode" json:"targetNode"`
	// VMID pins the numeric id. Optional on create (next free id is
	// used); immutable afterwards.
	VMID *int `yaml:"vmid,omitempty" json:"vmid,omitempty"`
	// Clone names a source VM/template to clone from. Create-time only.
	Clone string `yaml:"clone,omitempty" json:"clone,omitempty"`
	// FullClone makes the clone independent of the source. Create-time only.
	FullClone bool `yaml:"fullClone,omitempty" json:"fullClone,omitempty"`
	// Pool assigns the VM to a resource pool. Create-time only.
	Pool        string     `yaml:"pool,omitempty" json:"pool,omitempty"`
	Description string     `yaml:"description,omitempty" json:"description,omitempty"`
	Resources   Resources  `yaml:"resources,omitempty" json:"resources,omitempty"`
	Disks       []Disk     `yaml:"disks,omitempty" json:"disks,omitempty"`
	Networks    []Network  `yaml:"networks,omitempty" json:"networks,omitempty"`
	CloudInit   *CloudInit `yaml:"cloudInit,omitempty" json:"cloudInit,omitempty"`
	StartOnBoot bool       `yaml:"startOnBoot,omitempty" json:"startOnBoot,omitempty"`
	Tags        []string   `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// Resources holds CPU and memory sizing.
type Resources struct {
	CPU CPU `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	// Memory in MB.
	Memory int `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// CPU describes the virtual CPU.
type CPU struct {
	Cores   int `yaml:"cores,omitempty" json:"cores,omitempty"`
	Sockets int `yaml:"sockets,omitempty" json:"sockets,omitempty"`
	// Type is the emulated CPU type, e.g. "host" or "kvm64".
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// Disk is one virtual disk in a bus slot.
type Disk struct {
	// Name is the bus slot, e.g. "scsi0" or "virtio0".
	Name string `yaml:"name" json:"name"`
	// Size like "32G". Grows are applied via resize; shrinking errors.
	Size    string `yaml:"size" json:"size"`
	Storage string `yaml:"storage" json:"storage"`
	Format  string `yaml:"format,omitempty" json:"format,omitempty"`
	Cache   string `yaml:"cache,omitempty" json:"cache,omitempty"`
}

// Network is one virtual NIC.
type Network struct {
	// Name is the interface slot, e.g. "net0".
	Name   string `yaml:"name" json:"name"`
	Bridge string `yaml:"bridge" json:"bridge"`
	// Model defaults to virtio.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Tag is the VLAN tag.
	Tag int `yaml:"tag,omitempty" json:"tag,omitempty"`
}

// CloudInit configures the cloud-init drive and its settings.
type CloudInit struct {
	User string `yaml:"user,omitempty" json:"user,omitempty"`
	// Password is write-only: the API masks it, so it is set on create
	// but never diffed or updated by apply.
	Password string   `yaml:"password,omitempty" json:"password,omitempty"`
	SSHKeys  []string `yaml:"sshKeys,omitempty" json:"sshKeys,omitempty"`
	// IPConfig like "ip=dhcp" or "ip=10.0.0.5/24,gw=10.0.0.1".
	IPConfig   string `yaml:"ipConfig,omitempty" json:"ipConfig,omitempty"`
	Nameserver string `yaml:"nameserver,omitempty" json:"nameserver,omitempty"`
	// Storage for the cloud-init drive; defaults to the first disk's storage.
	Storage string `yaml:"storage,omitempty" json:"storage,omitempty"`
}

// Status is the live state reported under a VM object.
type Status struct {
	VMID   int     `yaml:"vmid" json:"vmid"`
	Node   string  `yaml:"node" json:"node"`
	State  string  `yaml:"state" json:"state"`
	Uptime int64   `yaml:"uptime,omitempty" json:"uptime,omitempty"`
	CPU    float64 `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	MaxMem uint64  `yaml:"maxMemBytes,omitempty" json:"maxMemBytes,omitempty"`
}

// VM is the printable, manifest-shaped object returned by Get/List.
// Its yaml/json form round-trips into apply -f.
type VM struct {
	runtime.TypeMeta `yaml:",inline"`
	Metadata         runtime.ObjectMeta `yaml:"metadata" json:"metadata"`
	Spec             Spec               `yaml:"spec" json:"spec"`
	Status           *Status            `yaml:"status,omitempty" json:"status,omitempty"`
}

// GetKind implements printer.Object.
func (v *VM) GetKind() string { return v.Kind }

// GetName implements printer.Object.
func (v *VM) GetName() string { return v.Metadata.Name }

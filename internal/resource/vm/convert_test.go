package vm

import (
	"net/url"
	"testing"
)

func intPtr(i int) *int { return &i }

// fullSpec exercises every convertible field, including the ones the
// legacy converter silently dropped (cpu type, disk cache, cloud-init).
func fullSpec() *Spec {
	return &Spec{
		TargetNode: "pve1",
		VMID:       intPtr(100),
		Resources: Resources{
			CPU:    CPU{Cores: 2, Sockets: 1, Type: "host"},
			Memory: 2048,
		},
		Disks: []Disk{
			{Name: "scsi0", Size: "32G", Storage: "local-lvm", Format: "qcow2", Cache: "writeback"},
		},
		Networks: []Network{
			{Name: "net0", Bridge: "vmbr0", Model: "virtio", Tag: 100},
		},
		CloudInit: &CloudInit{
			User:       "admin",
			Password:   "s3cret",
			SSHKeys:    []string{"ssh-ed25519 AAAA test@example"},
			IPConfig:   "ip=10.0.0.5/24,gw=10.0.0.1",
			Nameserver: "1.1.1.1",
		},
		StartOnBoot: true,
		Tags:        []string{"web", "prod"},
		Description: "test vm",
	}
}

func TestCreateParams(t *testing.T) {
	params := createParams("web-server", fullSpec())

	want := map[string]any{
		"name":        "web-server",
		"cores":       2,
		"sockets":     1,
		"cpu":         "host",
		"memory":      2048,
		"description": "test vm",
		"scsi0":       "local-lvm:32,cache=writeback,format=qcow2",
		"net0":        "virtio,bridge=vmbr0,tag=100",
		"ide2":        "local-lvm:cloudinit",
		"ciuser":      "admin",
		"cipassword":  "s3cret",
		"sshkeys":     url.PathEscape("ssh-ed25519 AAAA test@example"),
		"ipconfig0":   "ip=10.0.0.5/24,gw=10.0.0.1",
		"nameserver":  "1.1.1.1",
		"onboot":      1,
		"tags":        "prod;web",
	}
	for k, v := range want {
		if params[k] != v {
			t.Errorf("createParams[%q] = %#v, want %#v", k, params[k], v)
		}
	}
	if _, has := params["vmid"]; has {
		t.Error("createParams must not set vmid; the api layer owns it")
	}
}

func TestCreateParamsMinimal(t *testing.T) {
	spec := &Spec{
		TargetNode: "pve1",
		Resources:  Resources{CPU: CPU{Cores: 1}, Memory: 512},
	}
	params := createParams("tiny", spec)
	if params["cores"] != 1 || params["memory"] != 512 || params["name"] != "tiny" {
		t.Errorf("params = %+v", params)
	}
	for _, absent := range []string{"cpu", "sockets", "onboot", "tags", "description", "ide2", "ciuser", "cipassword", "sshkeys", "ipconfig0", "nameserver"} {
		if _, has := params[absent]; has {
			t.Errorf("createParams[%q] should be absent for minimal spec, got %#v", absent, params[absent])
		}
	}
}

func TestCreateParamsNetworkDefaultsToVirtio(t *testing.T) {
	spec := &Spec{
		TargetNode: "pve1",
		Resources:  Resources{CPU: CPU{Cores: 1}, Memory: 512},
		Networks:   []Network{{Name: "net1", Bridge: "vmbr1"}},
	}
	params := createParams("x", spec)
	if params["net1"] != "virtio,bridge=vmbr1" {
		t.Errorf("net1 = %#v, want virtio default without tag", params["net1"])
	}
}

func TestCloudInitDriveStorage(t *testing.T) {
	t.Run("explicit storage wins", func(t *testing.T) {
		spec := fullSpec()
		spec.CloudInit.Storage = "fast-nvme"
		if got := createParams("x", spec)["ide2"]; got != "fast-nvme:cloudinit" {
			t.Errorf("ide2 = %#v", got)
		}
	})

	t.Run("falls back to first disk storage", func(t *testing.T) {
		if got := createParams("x", fullSpec())["ide2"]; got != "local-lvm:cloudinit" {
			t.Errorf("ide2 = %#v", got)
		}
	})
}

func TestCloneParams(t *testing.T) {
	spec := fullSpec()
	spec.Clone = "ubuntu-template"
	spec.FullClone = true
	spec.Pool = "web-pool"

	params := cloneParams("web-server", spec, 105)
	want := map[string]any{
		"newid":       105,
		"name":        "web-server",
		"target":      "pve1",
		"full":        1,
		"pool":        "web-pool",
		"description": "test vm",
	}
	if len(params) != len(want) {
		t.Errorf("cloneParams = %+v, want exactly %+v", params, want)
	}
	for k, v := range want {
		if params[k] != v {
			t.Errorf("cloneParams[%q] = %#v, want %#v", k, params[k], v)
		}
	}
}

func TestConfigParams(t *testing.T) {
	params := configParams("web-server", fullSpec())

	// Config PUT payload: everything except disks (allocation syntax is
	// create-only) and create-only keys.
	if _, has := params["scsi0"]; has {
		t.Error("configParams must not contain disk allocation values")
	}
	if _, has := params["ide2"]; has {
		t.Error("configParams must not contain the cloud-init drive")
	}
	for k, v := range map[string]any{
		"cores": 2, "cpu": "host", "memory": 2048,
		"net0": "virtio,bridge=vmbr0,tag=100", "ciuser": "admin",
		"onboot": 1, "tags": "prod;web",
	} {
		if params[k] != v {
			t.Errorf("configParams[%q] = %#v, want %#v", k, params[k], v)
		}
	}
}

func TestDesiredParamsIncludesDisks(t *testing.T) {
	params := desiredParams("web-server", fullSpec())
	if params["scsi0"] != "local-lvm:32,cache=writeback,format=qcow2" {
		t.Errorf("desiredParams[scsi0] = %#v", params["scsi0"])
	}
	if params["cores"] != 2 {
		t.Errorf("desiredParams[cores] = %#v", params["cores"])
	}
	if _, has := params["ide2"]; has {
		t.Error("desiredParams must not diff the cloud-init drive")
	}
}

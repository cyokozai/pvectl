package config

// Config struct: represents the top-level structure of the YAML file
type Config struct {
	APIVersion     string            `yaml:"apiVersion"`
	Kind           string            `yaml:"kind"`
	Preferences    map[string]string `yaml:"preferences"`
	Users          []User            `yaml:"users"`
	Nodes          []Node            `yaml:"nodes"`
	Contexts       []Context         `yaml:"contexts"`
	CurrentContext string            `yaml:"current-context"`
}

// User struct: represents user authentication information
type User struct {
	Name string `yaml:"name"`
	User struct {
		Password string `yaml:"password"`
		Type 	 string `yaml:"type"`
		Token    string `yaml:"token,omitempty"`
		OTP      string `yaml:"otp,omitempty"` // Optional, so use omitempty
	} `yaml:"user"`
}

// Node struct: represents Proxmox node information
type Node struct {
	Name string `yaml:"name"`
	Node struct {
		Server      string            `yaml:"server"`
		HTTPHeaders map[string]string `yaml:"http_headers,omitempty"` // Optional, so use omitempty
	} `yaml:"node"`
}

// Context struct: represents context information
type Context struct {
	Name    string `yaml:"name"`
	Context struct {
		User string `yaml:"user"`
		Node string `yaml:"node"`
	} `yaml:"context"`
}

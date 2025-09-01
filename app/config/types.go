package config

// Config represents the Proxmox VE configuration file structure
type Config struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Preferences    Preferences    `yaml:"preferences"`
	Clusters       []NamedCluster `yaml:"clusters"`
	Users          []NamedUser    `yaml:"users"`
	Contexts       []NamedContext `yaml:"contexts"`
	CurrentContext string         `yaml:"current-context"`
	// Extensions holds additional information. This is useful for extenders so that reads and writes don't clobber unknown fields
	// +optional
	Extensions []NamedExtension `yaml:"extensions,omitempty"`
}

// Preferences holds general configuration preferences
type Preferences struct {
	// +optional
	Colors bool `yaml:"colors,omitempty"`
	// Extensions holds additional information. This is useful for extenders so that reads and writes don't clobber unknown fields
	// +optional
	Extensions []NamedExtension `yaml:"extensions,omitempty"`
	// ref: https://github.com/kubernetes/client-go/blob/master/tools/clientcmd/api/v1/types.go
}

// NamedCluster type: associates a name with a cluster
type NamedCluster struct {
	Name    string  `yaml:"name"`
	Cluster Cluster `yaml:"cluster"`
}

// Cluster type: holds information about how to communicate with a Proxmox VE cluster
type Cluster struct {
	Server                string `yaml:"server"`
	CertificateAuthority  string `yaml:"certificate-authority,omitempty"`
	InsecureSkipTLSVerify bool   `yaml:"insecure-skip-tls-verify,omitempty"`
	ProxyURL              string `yaml:"proxy-url,omitempty"`
}

// NamedUser type: associates a name with a user
type NamedUser struct {
	Name string `yaml:"name"`
	User User   `yaml:"user"`
}

// User type: holds information about how to authenticate as a user
type User struct {
	ClientCertificate string `yaml:"client-certificate,omitempty"`
	ClientKey         string `yaml:"client-key,omitempty"`
	Token             string `yaml:"token,omitempty"`
	Username          string `yaml:"username,omitempty"`
	Password          string `yaml:"password,omitempty"`
}

// NamedContext type: associates a name with a context
type NamedContext struct {
	Name    string  `yaml:"name"`
	Context Context `yaml:"context"`
}

// Context type: holds information about a context
type Context struct {
	Cluster   string `yaml:"cluster"`
	User      string `yaml:"user"`
	Namespace string `yaml:"namespace,omitempty"`
}

// ConfigManager type: manages configuration files
type ConfigManager struct {
	ConfigPath string
	Config     *Config
}

// NamedExtension type: holds additional information. This is useful for extenders so that reads and writes don't clobber unknown fields
type NamedExtension struct {
	Name      string         `yaml:"name"`
	Extension map[string]any `yaml:"extension,omitempty"`
}

// NewConfigManager function: creates a new configuration manager
func NewConfigManager(configPath string) *ConfigManager {
	return &ConfigManager{
		ConfigPath: configPath,
		Config:     &Config{},
	}
}

// DefaultConfig function: returns a default configuration template
func DefaultConfig() *Config {
	return &Config{
		APIVersion:  "v1",
		Kind:        "Config",
		Preferences: Preferences{},
		Clusters:    []NamedCluster{},
		Users:       []NamedUser{},
		Contexts:    []NamedContext{},
		Extensions:  []NamedExtension{},
	}
}

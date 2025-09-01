package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadConfig function: loads configuration from file
func (cm *ConfigManager) LoadConfig() error {
	// Read the configuration data from the file
	data, err := os.ReadFile(cm.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// If the file does not exist, create a default configuration
			cm.Config = DefaultConfig()

			return cm.SaveConfig()
		}

		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal the configuration data into the Config struct
	err = yaml.Unmarshal(data, cm.Config)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return cm.ValidateConfig()
}

// SaveConfig function: saves configuration to file
func (cm *ConfigManager) SaveConfig() error {
	// If the directory does not exist, create it
	dir := filepath.Dir(cm.ConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cm.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	err = os.WriteFile(cm.ConfigPath, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ValidateConfig function: validates the configuration
func (cm *ConfigManager) ValidateConfig() error {
	if cm.Config.APIVersion != "v1" {
		return fmt.Errorf("unsupported API version: %s", cm.Config.APIVersion)
	}

	if cm.Config.Kind != "Config" {
		return fmt.Errorf("invalid kind: %s", cm.Config.Kind)
	}

	// Check if the current context exists
	if cm.Config.CurrentContext != "" {
		found := false
		for _, ctx := range cm.Config.Contexts {
			if ctx.Name == cm.Config.CurrentContext {
				found = true

				break
			}
		}

		if !found {
			return fmt.Errorf("current context '%s' not found", cm.Config.CurrentContext)
		}
	}

	return nil
}

// GetCurrentContext function: returns the current context
func (cm *ConfigManager) GetCurrentContext() (*NamedContext, error) {
	if cm.Config.CurrentContext == "" {
		return nil, fmt.Errorf("no current context set")
	}

	for _, ctx := range cm.Config.Contexts {
		if ctx.Name == cm.Config.CurrentContext {
			return &ctx, nil
		}
	}

	return nil, fmt.Errorf("current context '%s' not found", cm.Config.CurrentContext)
}

// GetClusterByName function: returns a cluster by name
func (cm *ConfigManager) GetClusterByName(name string) (*NamedCluster, error) {
	for _, cluster := range cm.Config.Clusters {
		if cluster.Name == name {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("cluster '%s' not found", name)
}

// GetUserByName function: returns a user by name
func (cm *ConfigManager) GetUserByName(name string) (*NamedUser, error) {
	for _, user := range cm.Config.Users {
		if user.Name == name {
			return &user, nil
		}
	}

	return nil, fmt.Errorf("user '%s' not found", name)
}

// SetCurrentContext function: sets the current context
func (cm *ConfigManager) SetCurrentContext(contextName string) error {
	// Check if the context exists
	found := false
	for _, ctx := range cm.Config.Contexts {
		if ctx.Name == contextName {
			found = true

			break
		}
	}
	if !found {
		return fmt.Errorf("context '%s' not found", contextName)
	}

	cm.Config.CurrentContext = contextName

	return cm.SaveConfig()
}

// GetDefaultConfigPath function: returns the default configuration file path
func GetDefaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".pvectl/config"
	}

	return filepath.Join(homeDir, ".pvectl", "config")
}

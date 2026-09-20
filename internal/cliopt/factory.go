// Package cliopt holds the Factory: the single place where global flags
// (--config, --context, -o, --timeout, -v) become a loaded config, a
// resolved context, a debug logger, and a connected API client.
// Commands never construct clients themselves, so a flag that is
// accepted is always honored.
package cliopt

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/config"
	"github.com/cyokozai/pvectl/internal/verbose"
)

// Factory resolves global command-line state lazily and memoizes it.
// Precedence for every knob: flag > PVECTL_* env > config file default.
type Factory struct {
	ConfigPath  string        // --config
	ContextName string        // --context
	Output      string        // -o / --output
	Timeout     time.Duration // --timeout (task wait bound)
	Verbosity   int           // -v / --v (0 = silent, kubectl's scale)

	// ErrOut is where -v output goes. Never stdout: debug lines there
	// would corrupt -o yaml / -o json. Nil means os.Stderr; the root
	// command points it at cobra's error stream so tests capture it.
	ErrOut io.Writer

	// NewClient is a test seam; nil means api.New.
	NewClient func(ctx context.Context, node config.Node, user config.User, opts api.Options) (api.Client, error)

	cfg       *config.Config
	client    api.Client
	connected bool
	logger    *verbose.Logger
	logged    bool
}

// Logger returns the debug logger for this invocation, memoized. It is
// nil when -v is 0 or negative, which every verbose.Logger method
// tolerates.
func (f *Factory) Logger() *verbose.Logger {
	if f.logged {
		return f.logger
	}
	out := f.ErrOut
	if out == nil {
		out = os.Stderr
	}
	f.logger = verbose.New(f.Verbosity, out)
	f.logged = true
	return f.logger
}

// Config loads and validates the config file once.
func (f *Factory) Config() (*config.Config, error) {
	if f.cfg != nil {
		return f.cfg, nil
	}

	path, err := f.ConfigFilePath()
	if err != nil {
		return nil, err
	}
	f.Logger().Logf(verbose.LevelContext, "loading config file %s", path)
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	f.cfg = cfg
	return cfg, nil
}

// ConfigFilePath returns the path the config is loaded from and saved
// to: --config beats PVECTL_CONFIG beats ~/.pvectl/config.
func (f *Factory) ConfigFilePath() (string, error) {
	if f.ConfigPath != "" {
		return f.ConfigPath, nil
	}
	if env := os.Getenv("PVECTL_CONFIG"); env != "" {
		return env, nil
	}
	return config.DefaultConfigPath()
}

// CurrentContext resolves the selected context: --context beats
// PVECTL_CONTEXT beats the file's current-context.
func (f *Factory) CurrentContext() (*config.ResolvedContext, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	name := f.ContextName
	if name == "" {
		name = os.Getenv("PVECTL_CONTEXT")
	}
	rc, err := cfg.Resolve(name)
	if err != nil {
		return nil, err
	}
	f.Logger().Logf(verbose.LevelContext, "using context %q: node %s, insecureSkipTLSVerify=%t",
		rc.Name, rc.Node.Server, rc.Node.InsecureSkipTLSVerify)
	return rc, nil
}

// Client returns the API client for the selected context, connecting once.
func (f *Factory) Client(ctx context.Context) (api.Client, error) {
	if f.connected {
		return f.client, nil
	}
	rc, err := f.CurrentContext()
	if err != nil {
		return nil, err
	}

	newClient := f.NewClient
	if newClient == nil {
		newClient = api.New
	}
	client, err := newClient(ctx, rc.Node, rc.User, api.Options{
		TaskTimeout: f.Timeout,
		Logger:      f.Logger(),
	})
	if err != nil {
		return nil, err
	}
	f.client = client
	f.connected = true
	return client, nil
}

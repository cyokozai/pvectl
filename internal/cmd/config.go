package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cyokozai/pvectl/internal/cliopt"
	"github.com/cyokozai/pvectl/internal/config"
)

func newConfigCmd(f *cliopt.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage pvectl configuration and contexts",
	}
	cmd.AddCommand(
		newConfigGetContextsCmd(f),
		newConfigCurrentContextCmd(f),
		newConfigUseContextCmd(f),
		newConfigViewCmd(f),
	)
	return cmd
}

func newConfigGetContextsCmd(f *cliopt.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "get-contexts",
		Short: "List the contexts in the config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-10s %-20s %-20s %-20s\n", "CURRENT", "NAME", "NODE", "USER")
			for _, ctx := range cfg.Contexts {
				current := ""
				if ctx.Name == cfg.CurrentContext {
					current = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-10s %-20s %-20s %-20s\n",
					current, ctx.Name, ctx.Context.Node, ctx.Context.User)
			}
			return nil
		},
	}
}

func newConfigCurrentContextCmd(f *cliopt.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "current-context",
		Short: "Print the current context name",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if cfg.CurrentContext == "" {
				return errors.New("no current context is set")
			}
			fmt.Fprintln(cmd.OutOrStdout(), cfg.CurrentContext)
			return nil
		},
	}
}

func newConfigUseContextCmd(f *cliopt.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "use-context NAME",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if cfg.GetContext(args[0]) == nil {
				return fmt.Errorf("context %q not found", args[0])
			}
			cfg.CurrentContext = args[0]

			path, err := f.ConfigFilePath()
			if err != nil {
				return err
			}
			if err := config.Save(cfg, path); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q.\n", args[0])
			return nil
		},
	}
}

func newConfigViewCmd(f *cliopt.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Display the config file with secrets redacted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			masked := *cfg
			masked.Users = make([]config.NamedUser, len(cfg.Users))
			copy(masked.Users, cfg.Users)
			for i := range masked.Users {
				if masked.Users[i].User.Token != "" {
					masked.Users[i].User.Token = "REDACTED"
				}
				if masked.Users[i].User.Password != "" {
					masked.Users[i].User.Password = "REDACTED"
				}
			}
			data, err := yaml.Marshal(&masked)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
}

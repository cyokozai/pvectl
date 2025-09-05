package config

import (
	"fmt"
	"os"
	"gopkg.in/yaml.v3"
	"github.com/cyokozai/pvectl/app/cli"
)

var configFilePath = "/root/.pvectl/config"

var ConfigSubCommands = []cli.SubCommands{
	{
		Name:        "get-contexts",
		Description: "Get the specific contexts",
		Usage:       "pvectl config get-contexts [context-name] [flags...]",
		Flags: func() interface{} {
			return &cli.ConfigFlag{}
		},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			yamlFile, err := os.ReadFile(configFilePath)
            if err != nil {
                fmt.Fprintln(inout.StdErr, "Error reading config:", err)
                
				return 1
            }

            var cfg Config
            if err := yaml.Unmarshal(yamlFile, &cfg); err != nil {
                fmt.Fprintln(inout.StdErr, "Error decoding YAML:", err)
                
				return 1
            }

			if len(args) < 1 {
                fmt.Fprintln(inout.StdOut, "CURRENT\tNAME\tNODE\tUSER")

                for _, context := range cfg.Contexts {
                    prefix := " "
                    if context.Name == cfg.CurrentContext {
                        prefix = "*"
                    }

                    fmt.Fprintf(inout.StdOut, "%-8s  %-12s\t  %-12s\t  %-12s\n", prefix, context.Name, context.Context.Node, context.Context.User)
                }

                return 0
            }

			contextName := args[0]
			context 	:= getContext(&cfg, contextName)
			if context == nil {
                fmt.Fprintf(inout.StdErr, "Error: context '%s' not found\n", contextName)

                return 1
            }

            fmt.Fprintf(inout.StdOut, "Context '%s':\n", context.Name)
            fmt.Fprintf(inout.StdOut, "  Node: %s\n", context.Context.Node)
            fmt.Fprintf(inout.StdOut, "  User: %s\n", context.Context.User)
            
			return 0
		},
	},
}

func getContext(cfg *Config, name string) *Context {
    for i, context := range cfg.Contexts {
        if context.Name == name {
            return &cfg.Contexts[i]
        }
    }

    return nil
}

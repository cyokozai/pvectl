package config

import (
	"fmt"
	"github.com/cyokozai/pvectl/app/cli"
)

var ConfigSubCommands = []cli.SubCommands{
	{
		Name:        "get-context",
		Description: "Get a specific context",
		Usage:       "pvectl config get-context [context-name] [flags...]",
		Flags: func() interface{} {
			return 0
		},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			if len(args) < 1 {
				fmt.Println("Error: context name is required")

				return 1
			}

			contextName := args[0]
			context 	:= GetContext(contextName)
			if context == nil {
				fmt.Printf("Error: context '%s' not found\n", contextName)

				return 1
			}

			
			fmt.Printf("CURRENT\tNAME\tNODE\tUSER\n")
			for _, context := range cfg.Contexts {
				prefix := " "

				if context.Name == cfg.CurrentContext {
					prefix = "*"
				}

				fmt.Printf("%-8s  %-12s\t  %-12s\t  %-12s\n", prefix, context.Name, context.Context.Node, context.Context.User)
			}

			fmt.Printf("Context '%s':\n", context.Name)
			fmt.Printf("  Node: %s\n", context.Context.Node)
			fmt.Printf("  User: %s\n", context.Context.User)

			return 0
		},
	},
}

func GetContext(name string) *Context {
	var cfg Config
	if name == "" {
		return cfg.Contexts
	}
	for _, context := range cfg.Contexts {
		if context.Name == name {
			return &context
		}
	}

	return nil
}
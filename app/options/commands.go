package options

import (
	"fmt"
	"os"
	"time"
	"github.com/cyokozai/pvectl/app/cli"
	"github.com/cyokozai/pvectl/app/config"
	"gopkg.in/yaml.v3"
)

type ConfigFlag struct {
	Output string `name:"output" short:"o" description:"Output format (json|yaml|wide)"`
}

type HelpFlags struct {
	// No flags
}

type HelloFlags struct {
	Verbose bool `name:"verbose" short:"v" description:"Enable verbose output"`
}

// subCommands variable: implementation of subcommands
var subCommands = []cli.SubCommands{
	{
		Name:        "config",
		Description: "config command - show help",
		Usage:       "pvectl config [subcommand] [flags...] [arguments...]",
		Flags: func() interface{} {
			return &HelpFlags{}
		},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			yamlFile, err := os.ReadFile("/root/.pvectl/config")
			if err != nil {
				fmt.Printf(": %v\n", err)
			}

			var cfg config.Config
			err = yaml.Unmarshal(yamlFile, &cfg)
			if err != nil {
				fmt.Printf("Error: decoding YAML: %v\n", err)
			}
			
			for _, cmd := range config.ConfigSubCommands {
				fmt.Printf("  %s\t- %s\n", cmd.Name, cmd.Description)
			}

			return 0
		},
	},
	{
		Name:        "help",
		Description: "help command - show help",
		Usage:       "pvectl help [command]",
		Flags: func() interface{} {
			return &HelpFlags{}
		},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			return 0
		},
	},
	{
		Name:        "hello",
		Description: "hello command - display greeting message",
		Usage:       "pvectl hello [flags...] [arguments...]",
		Flags: func() interface{} {
			return &HelloFlags{}
		},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			if len(args) > 0 {
				name := args[0]
				if flags != nil {
					helloFlags := flags.(*HelloFlags)
					if helloFlags.Verbose {
						fmt.Fprintf(inout.StdOut, "Hello, %s! Today is %s\n", name, time.Now().Format("Monday"))
					} else {
						fmt.Fprintf(inout.StdOut, "Hello, %s!\n", name)
					}
				} else {
					fmt.Fprintf(inout.StdOut, "Hello, %s!\n", name)
				}
			} else {
				fmt.Fprintf(inout.StdOut, "Hello, World!\n")
			}

			return 0
		},
	},
}

// MainCommandRunner: create the main command runner
var MainCommandRunner = func() *cli.CommandRunner {
	runner := cli.NewCommandRunner(
		"pvectl",
		"Proxmox VE instance management CLI tool.",
		"pvectl <command> [flags...] [arguments...]",
	)

	// Add subcommands
	for _, cmd := range subCommands {
		runner.AddSubCommand(cmd)
	}

	return runner
}()

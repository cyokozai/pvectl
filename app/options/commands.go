package options

import (
	"fmt"
	"time"

	"github.com/cyokozai/pvectl/app/cli"
	"github.com/cyokozai/pvectl/app/config"
)

// subCommands variable: implementation of subcommands
var subCommands = []cli.SubCommands{
	{
		Name:        "config",
		Description: "config command - show help",
		Usage:       "pvectl config [subcommand] [flags...] [arguments...]",
		Flags: func() interface{} {
			return &cli.ConfigFlag{}
		},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			config.

			return 0
		},
	},
	{
		Name:        "help",
		Description: "help command - show help",
		Usage:       "pvectl help [command]",
		Flags: func() interface{} {
			return &cli.HelpFlags{}
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
			return &cli.HelloFlags{}
		},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			if len(args) > 0 {
				name := args[0]
				if flags != nil {
					helloFlags := flags.(*cli.HelloFlags)
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

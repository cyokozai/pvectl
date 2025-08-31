package options

import (
	"fmt"

	"github.com/cyokozai/pvectl/app/cli"
)

type HelpFlags struct {
	Help bool `name:"help" description:"Show help"`
}

// FooFlags struct: flags for the foo command
type FooFlags struct {
	Verbose bool `name:"verbose" description:"Enable verbose output"`
}

// BarFlags struct: flags for the bar command
type BarFlags struct {
	Output string `name:"output" description:"Output format (json|yaml|text)"`
	Quiet  bool   `name:"quiet" description:"Suppress output"`
}

type HelloFlags struct {
	Verbose bool `name:"verbose" description:"Enable verbose output"`
}

// subCommands variable: implementation of subcommands
var subCommands = []cli.SubCommands{
	{
		Name:        "help",
		Description: "help command - show help",
		Usage:       "pvectl help [command]",
		Flags:       &HelpFlags{},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			return 0
		},
	},
	{
		Name:        "hello",
		Description: "hello command - display greeting message",
		Usage:       "pvectl hello [NAME] [--verbose]",
		Flags:       &HelloFlags{},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			if flags != nil {
				helloFlags := flags.(*HelloFlags)
				if helloFlags.Verbose {
					fmt.Fprintf(inout.StdOut, "Hello World command executed with verbose output\n")
				} else {
					fmt.Fprintf(inout.StdOut, "Hello World command executed\n")
				}
			} else {
				fmt.Fprintf(inout.StdOut, "Hello World command executed\n")
			}
			
			// If an argument is specified, display the greeting message
			if len(args) > 0 {
				name := args[0]
				fmt.Fprintf(inout.StdOut, "Hello, %s!\n", name)
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
		"Proxmox VE instance management CLI tool",
		"pvectl [global-flags] <command> [command-flags] [arguments...]",
		&Options{},
	)

	// Add subcommands
	for _, cmd := range subCommands {
		runner.AddSubCommand(cmd)
	}

	return runner
}()

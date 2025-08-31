package options

import (
	"fmt"
	"github.com/cyokozai/pvectl/app/cli"
)

// FooFlags struct: flags for the foo command
type FooFlags struct {
	Verbose bool `name:"verbose" description:"Enable verbose output"`
}

// BarFlags struct: flags for the bar command
type BarFlags struct {
	Output string `name:"output" description:"Output format (json|yaml|text)"`
	Quiet  bool   `name:"quiet" description:"Suppress output"`
}

// subCommands variable: implementation of subcommands
var subCommands = []cli.SubCommands{
	{
		Name:        "foo",
		Description: "foo command - execute basic operations",
		Usage:       "pvectl foo [--verbose]",
		Flags:       &FooFlags{},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			if flags != nil {
				fooFlags := flags.(*FooFlags)
				if fooFlags.Verbose {
					fmt.Fprintf(inout.StdOut, "foo command executed successfully with verbose output\n")
				} else {
					fmt.Fprintf(inout.StdOut, "foo command executed successfully\n")
				}
			} else {
				fmt.Fprintf(inout.StdOut, "foo command executed successfully\n")
			}

			return 0
		},
	},
	{
		Name:        "bar",
		Description: "bar command - execute data processing",
		Usage:       "pvectl bar [--output FORMAT] [--quiet]",
		Flags:       &BarFlags{},
		Run: func(args []string, flags interface{}, inout *cli.InOut) int {
			if flags != nil {
				barFlags := flags.(*BarFlags)
				output := "default"
				if barFlags.Output != "" {
					output = barFlags.Output
				}
				if barFlags.Quiet {
					// Quiet
				} else {
					fmt.Fprintf(inout.StdOut, "bar command executed successfully with output format: %s\n", output)
				}
			} else {
				fmt.Fprintf(inout.StdOut, "bar command executed successfully\n")
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

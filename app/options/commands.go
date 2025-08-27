package options

import (
	"flag"
	"fmt"
	"log"
	"github.com/cyokozai/pvectl/app/cli"
)


// SubCommands for the main command
var subCmds = []cli.SubCommands{
	{
		Name:        "foo",
		Description: "foo command",
		Run: func(args []string, inout *cli.InOut) int {
			fmt.Fprintf(inout.StdOut, "foo command executed successfully\n")

			return 0
		},
	},
	{
		Name:        "bar",
		Description: "bar command",
		Run: func(args []string, inout *cli.InOut) int {
			fmt.Fprintf(inout.StdOut, "bar command executed successfully\n")

			return 0
		},
	},
}


func NewCommand(name string, commands []cli.SubCommands) cli.Commands {
	return func(args []string, inout *cli.InOut) int {
		flags := flag.NewFlagSet(name, flag.ContinueOnError)
		flags.SetOutput(inout.StdErr)
		flags.Usage = func() {
			fmt.Fprintf(inout.StdErr, "Usage: %s [options] [arguments]\n", name)
			flags.PrintDefaults()
		}
		if err := flags.Parse(args); err != nil {
			if err == flag.ErrHelp {
				fmt.Fprintf(inout.StdOut, "Hello\n")
				return 0
			}

			log.Println("Error parsing flags:", err)

			return 1
		}
		if flags.NArg() < 1 {
			fmt.Fprintf(inout.StdErr, "error: no command provided\n")
			flags.Usage()

			return 1
		}

		options, err := OptionParser(args, inout)
		if err != nil {
			log.Println("Error parsing options:", err)

			return 1
		}
		if options.Help {
			fmt.Fprintf(inout.StdOut, "Hello\n")
			return 0
		}

		cmdName := flags.Arg(0)
        cmdArgs := flags.Args()[1:]
		for _, c := range commands {
			if c.Name == cmdName {
				return c.Run(cmdArgs, inout)
			}
		}

		fmt.Fprintf(inout.StdErr, "error: unknown command %q\n", cmdName)
		flags.Usage()

		return 1
	}
}


// MainCommand is the entry point for the CLI
var MainCommand = NewCommand("pvectl", subCmds)

package options

import (
	"flag"
	"fmt"
	"log"
	"github.com/cyokozai/pvectl/app/cli"
)

// SubCommands variable: holds the list of subcommands
var SubCommands = []cli.SubCommand{
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


// MainCommand function: prints "Hello World" to standard output
func MainCommand(args []string, inout *cli.InOut) int {
	options, err := OptionParser(args, inout)
	if err != nil {
		log.Println("Error parsing options:", err)

		return 1
	}
	if options.Help {
		return 0
	}

	return 0
}


func NewCommand(name string, commands SubCommands) Commands {
	return func(args []string, inout *cli.InOut) int {
		flags := flag.NewFlagSet(name, flag.ContinueOnError)
		flags.SetOutput(inout.StdErr)

		flags.Usage = func() {
			fmt.Fprintf(inout.StdErr, "Usage: %s [options] [arguments]\n", name)
			flags.PrintDefaults()
		}

		if err := flags.Parse(args); err != nil {
			if err == flag.ErrHelp {
				return 0
			}

			log.Println("Error parsing flags:", err)

			return 1
		}

		if flags.NArg() < 1 {
			fmt.Fprintf(inout.Stderr, "error: no command provided\n")
			flags.Usage()
			return 1
		}

		for _, c := range commands {
			if c.Name == flags.Arg(0) {
				return c.Run(flags.Args()[1:], inout)
			}
		}

		return 1
	}
}
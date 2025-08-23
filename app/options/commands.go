package options

import (
	"fmt"
	"log"
	"github.com/cyokozai/pvectl/app/cli"
)

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

	fmt.Fprintf(inout.StdOut, "foo: %s\n", options.Foo)
	fmt.Fprintf(inout.StdOut, "bar: %s\n", options.Bar)

	return 0
}
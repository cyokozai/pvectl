package options

import (
	// "bufio"
	"fmt"
	"log"
	"github.com/cyokozai/pvectl/app/cli"
)

// MainCommand function: prints "Hello World" to standard output
var MainCommand = cli.Command("pvectl", []SubCommands{
	{
		Name:        "foo",
		Description: "foo command",
		Run: func(args []string, inout *cli.InOut) int {
			fmt.Fprintf(inout.StdOut, "foo command executed successfully\n")
			log.Println("foo command executed successfully")

			return 0
		},
	},
	{
		Name:        "bar",
		Description: "bar command",
		Run: func(args []string, inout *cli.InOut) int {
			fmt.Fprintf(inout.StdOut, "bar command executed successfully\n")
			log.Println("bar command executed successfully")

			return 0
		},
	},
})

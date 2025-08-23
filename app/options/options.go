package options


import (
	"errors"
	"flag"
	"fmt"
	"log"
	"github.com/cyokozai/pvectl/app/cli"
)


// Options struct: holds the command line options
type Options struct {
	Foo  string `name:"foo" description:"foo"`
	Bar  string `name:"bar" description:"bar"`
	Help bool   `name:"help" description:"show help"`
}


// OptionParser function: parses command-line arguments and returns configured Options.
func OptionParser(args []string, inout *cli.InOut) (*Options, error) {
	options := &Options{}
	
	if err := cli.FlagParser(`pvectl`, args, options); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			options.Help = true

			return options, err
		}

		log.Println("Error parsing flags:", err)
		
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	return options, nil
}

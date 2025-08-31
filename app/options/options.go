package options

import (
	"errors"
	"flag"
	"fmt"

	"github.com/cyokozai/pvectl/app/cli"
)

// Options struct: holds the command line options
type Options struct {
	Help bool `name:"help" short:"h" description:"show help"`
}

// OptionParser function: parses command-line arguments and returns configured Options.
func OptionParser(args []string, inout *cli.InOut) (*Options, error) {
	options := &Options{}

	// Parse flags
	err := cli.FlagParser(`pvectl`, args, options)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			options.Help = true // Set help flag to true

			return options, nil // Return options if help flag is set
		}

		return nil, fmt.Errorf("failed to parse flags: %w", err) // Return error if flags are not parsed
	}

	return options, nil // Return options if flags are parsed
}

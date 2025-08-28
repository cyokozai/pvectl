package options


import (
	"errors"
	"flag"
	"fmt"
	"github.com/cyokozai/pvectl/app/cli"
)


// Options struct: holds the command line options
type Options struct {
	Config  string `name:"config" description:"Modify config files using subcommands like \"pvectl config set current-context my-context\"." usage:"pvectl config SUBCOMMAND [options]"`
	Bar     string `name:"bar" description:"bar"`
	Help    bool   `name:"help" description:"show help"`
}


// OptionParser function: parses command-line arguments and returns configured Options.
func OptionParser(args []string, inout *cli.InOut) (*Options, error) {
	options := &Options{}

	err := cli.FlagParser(`pvectl`, args, options)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			options.Help = true
			
			return options, nil
		}
		
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	return options, nil
}

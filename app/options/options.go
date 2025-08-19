package options


import (
	"errors"
	"flag"
	"github.com/cyokozai/pvectl/app/cli"
)


// Options struct: holds the command line options
type Options struct {
	SomethingRequired string
	Help              bool
}


// OptionParser function: parses command-line arguments and returns configured Options.
func OptionParser(args []string, inout *cli.InOut) (*Options, error) {
	f := flag.NewFlagSet("pvectl", flag.ContinueOnError)
	f.SetOutput(inout.StdErr)

	options := &Options{}
	f.StringVar(&options.SomethingRequired, "something-required", "", "Something required")

	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			options.Help = true

			return options, nil
		}

		return nil, err
	}

	if options.SomethingRequired == "" {
		return nil, errors.New("something-required is required")
	}

	return options, nil
}

package options


import (
	"errors"
	"flag"
	"github.com/cyokozai/pvectl/app/cli"
)


// Options struct: holds the command line options
type Options struct {
	Foo string // Option for Foo
	Bar string // Option for Bar
	Version string // Option for Version
	Help bool  // Option for Help
}


// OptionParser function: parses command line options
func OptionParser(args []string, inout *cli.InOut) (*Options, error) {
	f := flag.NewFlagSet("pvectl", flag.ContinueOnError) // Create a new flag set
	f.SetOutput(inout.StdErr) 							 // Set the output for the flag set to standard error

	options := &Options{} // Create a new Options instance

	// ↓↓↓ Define the command-line options ↓↓↓

	// Define Foo option
	f.StringVar(&options.Foo, "foo", "hello", "Foo option 01")

	// Define Bar option
	f.StringVar(&options.Bar, "bar", "", "Bar option 02")

	// Define Version option
	f.StringVar(&options.Version, "version", "", "Show version information.")

	// ↑↑↑ Define the command-line options ↑↑↑
	
	// Help flag
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			options.Help = true

			return options, nil
		}

		return nil, err
	}

	return options, nil
}

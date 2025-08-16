package options


import (
	"errors"
	"flag"
	"github.com/cyokozai/pvectl/app/cli"
)


//
type Options struct {
	Foo string // Option for Foo
	Bar string // Option for Bar
	Help bool   // Option for Help
}


// 
func OptionParser(args []string, inout *cli.InOut) (*Options, error) {
	f := flag.NewFlagSet("pvectl", flag.ContinueOnError) // Create a new flag set
	f.SetOutput(inout.StdErr) 							 // Set the output for the flag set to standard error

	options := &Options{} // Create a new Options instance

	// ↓↓↓ Define the command-line options ↓↓↓

	// Define Foo option
	f.StringVar(&options.Foo, "foo", "", "Foo option")

	// Define Bar option
	f.StringVar(&options.Bar, "bar", "", "Bar option")

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

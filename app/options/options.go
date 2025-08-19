package options


import (
	"errors"
	"flag"
	"github.com/cyokozai/pvectl/app/cli"
)


// Options struct: holds the command line options
type Options struct {
	Foo  string
	Bar  string
	Help bool
}


// OptionParser function: parses command-line arguments and returns configured Options.
func OptionParser(args []string, inout *cli.InOut) (*Options, error) {
	f := flag.NewFlagSet("pvectl", flag.ContinueOnError)
	f.SetOutput(inout.StdErr)

	options := &Options{}
	f.StringVar(&options.Foo, "foo", "", "foo")
	f.StringVar(&options.Bar, "bar", "", "bar")

	f.Usage = func() {
		inout.StdErr.Write([]byte("Usage: pvectl [options]\n"))
		inout.StdErr.Write([]byte("OPTIONS\n"))
		f.PrintDefaults()
	}

	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			options.Help = true

			return options, nil
		}

		return nil, err
	}

	return options, nil
}

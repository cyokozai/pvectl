package options


import (
	// "bufio"
	"fmt"
	"log"
    "github.com/cyokozai/pvectl/app/cli"
)


// MainCommand function: prints "Hello World" to standard output
func MainCommand(args []string, inout *cli.InOut) int {
	options, err := OptionParser(args, inout) // Parse command line options
	if err != nil {
		fmt.Fprintf(inout.StdErr, "Error: %v\n", err) 	// Print error to standard error
		log.Println("Error parsing options:", err)		// Log the error if any
		
		return 1
	}

	if options.Help {
		return 0
	}


	if err := MainCommandByOptions(options, inout); err != nil {
		fmt.Fprintf(inout.StdErr, "Error: %v\n", err)  				 // Print error to standard error
		log.Println("Error executing main command by options:", err) // Log the error if any
		
		return 1
	}

	return 0
}


// MainCommandByOptions function: executes the main command with the given options
func MainCommandByOptions(options *Options, inout *cli.InOut) error {
	fmt.Fprintf(inout.Stdout, "pvectl executed successfully\n")

	return nil
}
package options


import (
	// "bufio"
	"fmt"
	"log"
    "github.com/cyokozai/pvectl/app/cli"
)


// MainCommand function: prints "Hello World" to standard output
func MainCommand(args []string, inout *cli.InOut) int {
	options, err := OptionsParser(args, inout) // Parse command line options
	if err != nil {
		fmt.Fprintf(inout.StdErr, "Error: %v\n", err) 	// Print error to standard error
		log.Println("Error parsing options:", err)		// Log the error if any
		
		return 1
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
	fmt.Fprintf(inout.StdOut, "foo: %s\n", options.Foo)
	fmt.Fprintf(inout.StdOut, "bar: %s\n", options.Bar)

	return nil
}


// // InteractiveCommand function: starts an interactive command line session
// func InteractiveCommand(_ []string, inout *cli.InOut) int {
// 	s := bufio.NewScanner(inout.StdIn) // Create a new scanner for standard input

// 	for s.Scan() {
// 		textline := s.Text() // Get the text line from the scanner
// 		fmt.Fprintf(inout.StdOut, "Hi, %s!\n", textline) // Print greeting to standard output
// 	}
	
// 	if err := s.Err(); err != nil {
// 		fmt.Fprintln(inout.StdErr, "Error: ", err) // Print error to standard error
// 		log.Println("Error reading input:", err) // Log the error if any

// 		return 1
// 	}
	
// 	return 0
// }
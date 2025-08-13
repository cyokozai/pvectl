package options


import (
	"bufio"
	"fmt"
	"log"
    "github.com/cyokozai/pvectl/app/cli"
)


// MainCommand function: prints "Hello World" to standard output
func MainCommand(_ []string, inout *cli.InOut) int {
	fmt.Fprintln(inout.StdOut, "Hello World") // Print "Hello World" to standard output

	return 0
}


// InteractiveCommand function: starts an interactive command line session
func InteractiveCommand(_ []string, inout *cli.InOut) int {
	s := bufio.NewScanner(inout.StdIn) // Create a new scanner for standard input

	for s.Scan() {
		textline := s.Text() // Get the text line from the scanner
		fmt.Fprintf(inout.StdOut, "Hi, %s!\n", textline) // Print greeting to standard output
	}
	
	if err := s.Err(); err != nil {
		fmt.Fprintln(inout.StdErr, "Error: ", err) // Print error to standard error
		log.Println("Error reading input:", err) // Log the error if any

		return 1
	}
	
	return 0
}
package cli

import (
	// "flag"
	// "fmt"
	"io"
	"os"
	// "reflect"
	// "strings"
)


// InOut struct: holds the standard input/output/error streams
type InOut struct {
	StdIn  io.Reader  // Standard input
	StdOut io.Writer  // Standard output
	StdErr io.Writer  // Standard error
}


// NewInOut function: creates a new InOut struct
func NewInOut() *InOut {
	return &InOut{
		StdIn:  os.Stdin,   // Standard input
		StdOut: os.Stdout,  // Standard output
		StdErr: os.Stderr,  // Standard error
	}
}


// Command type: defines a function that takes arguments and InOut struct
type Commands func(
	args []string, 	// Command line arguments
	inOut *InOut  	// Input/Output streams
) int


type SubCommands struct {
	Name        string
	Description string
	Run         func(
					args []string, 
					inout *InOut
				) int
}

// Run function: executes the command with the given input
func Run(c Commands) {
	args := os.Args[1:] 		// Get command line arguments
	inout := NewInOut()  		// Create a new InOut instance
	exitCode := c(args, inout) 	// Execute the command

	os.Exit(exitCode)  // Exit with the command's exit code
}

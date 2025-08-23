package cli


import (
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"log"
)


// InOut struct: holds the standard input/output/error streams
type InOut struct {
	StdIn  io.Reader 		 // Standard input
	StdOut io.Writer 		 // Standard output
	StdErr io.Writer  		 // Standard error
	Env    map[string]string // Environment variables
}


// NewInOut function: creates a new InOut struct
func NewInOut() *InOut {
	env := make(map[string]string)

	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		env[parts[0]] = parts[1]
	}

	return &InOut{
		StdIn:  os.Stdin,   // Standard input
		StdOut: os.Stdout,  // Standard output
		StdErr: os.Stderr,  // Standard error
		Env:    env,        // Environment variables
	}
}


// Command type: defines a function that takes arguments and InOut struct
type Commands func(args []string, inout *InOut) int


// SubCommands type: defines a sub-command with a name, description, and run function
type SubCommands struct {
	Name        string
	Description string
	Run         func(args []string, inout *InOut) int
}


// Run function: executes the command with the given input
func Run(c Commands) {
	args  	 := os.Args[1:] 	// Get command line arguments
	inout 	 := NewInOut()  	// Create a new InOut instance
	exitCode := c(args, inout) 	// Execute the command
	
	os.Exit(exitCode)  // Exit with the command's exit code
}


// Flag struct: holds information about a command-line flag
type Flag struct {
	FlagName    string
	Name        string
	Description string
	Type        string
}


// FlagAnalyzer function: analyzes the flags in the given options struct
func FlagAnalyzer(options any) []Flag {
	flags := make([]Flag, 0)
	t 	  := reflect.TypeOf(options).Elem()
	m 	  := t.NumField()

	for i := 0; i < m; i++ {
		field := t.Field(i)
		name  := field.Tag.Get("flag")
		desc  := field.Tag.Get("description")

		f := Flag{
			FlagName:    field.Name,
			Name:        name,
			Description: desc,
		}

		switch field.Type.Kind() {
		case reflect.String:
			f = "string"
		case reflect.Bool:
			f = "bool"
		case reflect.Int:
			f = "int"
		default:
			panic("unhandled default case")
		}
		flags = append(flags, f)
	}

	return flags
}
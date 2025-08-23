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
	flags  := make([]Flag, 0)
	typeOf := reflect.TypeOf(options).Elem()
	m 	   := typeOf.NumField()

	for i := 0; i < m; i++ {
		field := typeOf.Field(i)
		
		f := Flag{
			FlagName:    field.Name,
			Name:        field.Tag.Get("name"),
			Description: field.Tag.Get("description"),
		}
		switch field.Type.Kind() {
		case reflect.String:
			f.Type = "string"
		case reflect.Bool:
			f.Type = "bool"
		case reflect.Int:
			f.Type = "int"
		default:
			log.Fatal("unhandled flag type: ", field.Type.Kind())
			panic("unhandled flag type: " + field.Type.Kind().String())
		}

		flags = append(flags, f)
	}

	return flags
}


// FlagParser function: parses command-line flags
func FlagParser(name string, args []string, options any) error {
	fs 	  := FlagAnalyzer(options)
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	
	for _, fl := range fs {
		f := reflect.ValueOf(options).Elem().FieldByName(fl.FlagName)

		switch fl.Type {
		case "string":
			flags.StringVar(f.Addr().Interface().(*string), fl.Name, f.String(), fl.Description)
		case "bool":
			flags.BoolVar(f.Addr().Interface().(*bool), fl.Name, f.Bool(), fl.Description)
		case "int":
			flags.IntVar(f.Addr().Interface().(*int), fl.Name, int(f.Int()), fl.Description)
		}
	}

	return flags.Parse(args)
}


// CommandCompletion function: wraps a command with completion support
func CommandCompletion(c Commands, compf func(args []string) []string) Commands {
	return func(args []string, inout *InOut) int {
		if inout.Env["GO_FLAGS_COMPLETION"] == "1" {
			completions := compf(args)
			fmt.Fprintln(inout.StdOut, strings.Join(completions, "\n"))
			
			return 0
		}

		return c(args, inout)
	}
}


// CompletionByFlags function: creates a completion function based on the provided flags
func CompletionByFlags(fs []Flag) func(args []string) []string {
	return func(args []string) []string {
		return Completion(args, fs)
	}
}


// Completion function: generates possible completions based on the current arguments and available flags
func Completion(args []string, fs []Flag) []string {
	comps := []string{}
	if len(args) == 0 {
		for _, flag := range fs {
			comps = append(comps, "--" + flag.Name)
		}

		return comps
	}

	last := args[len(args) - 1]
	
	for _, flag := range fs {
		if strings.HasPrefix("--" + flag.Name, last) || strings.HasPrefix("-" + flag.Name, last) {
			comps = append(comps, "--" + flag.Name)
		}
	}

	return comps
}
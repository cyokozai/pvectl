package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
)

// InOut struct: holds the standard input/output/error streams
type InOut struct {
	StdIn  io.Reader         // Standard input
	StdOut io.Writer         // Standard output
	StdErr io.Writer         // Standard error
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
		StdIn:  os.Stdin,  // Standard input
		StdOut: os.Stdout, // Standard output
		StdErr: os.Stderr, // Standard error
		Env:    env,       // Environment variables
	}
}

// Command type: defines a function that takes arguments and InOut struct
type Commands func(args []string, inout *InOut) int

// SubCommands type: defines a sub-command with a name, description, and run function
type SubCommands struct {
	Name        string
	Description string
	Usage       string
	Run         func(args []string, flags interface{}, inout *InOut) int
	Flags       func() interface{}
}

// CommandRunner: Manage main command execution
type CommandRunner struct {
	Name        string
	Description string
	Usage       string
	SubCommands []SubCommands
}

// NewCommandRunner: Create a new command runner
func NewCommandRunner(name, description, usage string) *CommandRunner {
	return &CommandRunner{
		Name:        name,
		Description: description,
		Usage:       usage,
	}
}

// AddSubCommand: Add a subcommand
func (cr *CommandRunner) AddSubCommand(cmd SubCommands) {
	cr.SubCommands = append(cr.SubCommands, cmd)
}

// Run: Execute command
func (cr *CommandRunner) Run(args []string, inout *InOut) int {
	// If no subcommands are provided, display help
	if len(args) == 0 {
		cr.PrintGlobalHelp(inout)

		return 0 // Return error if no subcommands are provided
	}

	cmdName := args[0]
	cmdArgs := args[1:]

	// Process help command
	if cmdName == "help" {
		if len(cmdArgs) == 0 {
			cr.PrintGlobalHelp(inout)

			return 0 // Display help for global flags
		}

		return cr.PrintSubCommandHelp(cmdArgs[0], inout) // Display help for a specific subcommand
	}

	// Search for and execute subcommand
	for _, cmd := range cr.SubCommands {
		if cmd.Name == cmdName {
			return cr.runSubCommand(cmd, cmdArgs, inout)
		}
	}

	fmt.Fprintf(inout.StdErr, "error: unknown command %q\n", cmdName)
	cr.PrintGlobalHelp(inout)

	return 1
}

// runSubCommand: Execute subcommand
func (cr *CommandRunner) runSubCommand(cmd SubCommands, args []string, inout *InOut) int {
    if cmd.Flags != nil {
        flags := cmd.Flags() // ← ここで新しいインスタンスを生成
        flagSet := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
        flagSet.SetOutput(inout.StdErr)
        flagSet.Usage = func() {
            cr.PrintSubCommandHelp(cmd.Name, inout)
        }

        flagsValue := FlagAnalyzer(flags)
        flagsStruct := reflect.ValueOf(flags).Elem()

        for _, f := range flagsValue {
            field := flagsStruct.FieldByName(f.FlagName)
            if !field.IsValid() {
                continue
            }
            switch f.Type {
            case "string":
                flagSet.StringVar(field.Addr().Interface().(*string), f.Name, field.String(), f.Description)
            case "bool":
                flagSet.BoolVar(field.Addr().Interface().(*bool), f.Name, field.Bool(), f.Description)
                if f.Short != "" {
                    flagSet.BoolVar(field.Addr().Interface().(*bool), f.Short, field.Bool(), f.Description)
                }
            case "int":
                flagSet.IntVar(field.Addr().Interface().(*int), f.Name, int(field.Int()), f.Description)
            }
        }

        if err := flagSet.Parse(args); err != nil {
            if err == flag.ErrHelp {
                cr.PrintSubCommandHelp(cmd.Name, inout)
                return 0
            }
            fmt.Fprintf(inout.StdErr, "Error parsing flags for %s: %v\n", cmd.Name, err)
            return 1
        }

        args = flagSet.Args()
        return cmd.Run(args, flags, inout)
    }

    return cmd.Run(args, nil, inout)
}

// PrintGlobalHelp: Display global help
func (cr *CommandRunner) PrintGlobalHelp(inout *InOut) {
	fmt.Fprintf(inout.StdOut, "%s\n\n", cr.Description)
	fmt.Fprintf(inout.StdOut, "Usage:\n  %s\n\n", cr.Usage)
	fmt.Fprintf(inout.StdOut, "Available Commands:\n")
	for _, cmd := range cr.SubCommands {
		fmt.Fprintf(inout.StdOut, "  %-15s %s\n", cmd.Name, cmd.Description)
	}

	fmt.Fprintf(inout.StdOut, "\nUse \"%s help <command>\" for more information about a command.\n", cr.Name)
}

// PrintSubCommandHelp: Display subcommand help
func (cr *CommandRunner) PrintSubCommandHelp(cmdName string, inout *InOut) int {
    for _, cmd := range cr.SubCommands {
        if cmd.Name == cmdName {
            fmt.Fprintf(inout.StdOut, "%s\n\n", cmd.Description)
            fmt.Fprintf(inout.StdOut, "Usage:\n  %s\n\n", cmd.Usage)
            if cmd.Flags != nil {
                fmt.Fprintf(inout.StdOut, "Flags:\n")
				
                flags := cmd.Flags()
                flagsValue := FlagAnalyzer(flags)
                for _, f := range flagsValue {
                    fmt.Fprintf(inout.StdOut, "  --%-10s -%s\t%s\n", f.Name, f.Short, f.Description)
                }
            }
            return 0
        }
    }
    fmt.Fprintf(inout.StdErr, "error: unknown command %q\n", cmdName)
    return 1
}

// Flag struct: holds information about a command-line flag
type Flag struct {
	FlagName    string
	Name        string
	Short       string
	Description string
	Type        string
}

// FlagAnalyzer function: analyzes the flags in the given options struct
func FlagAnalyzer(options any) []Flag {
	flags := make([]Flag, 0)
	typeOf := reflect.TypeOf(options).Elem()
	m := typeOf.NumField()

	for i := 0; i < m; i++ {
		field := typeOf.Field(i)

		f := Flag{
			FlagName:    field.Name,
			Name:        field.Tag.Get("name"),
			Short:       field.Tag.Get("short"),
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
			fmt.Println("unhandled flag type: ", field.Type.Kind().String())

			return nil
		}

		flags = append(flags, f)
	}

	return flags
}

// FlagParser function: parses command-line flags
func FlagParser(name string, args []string, options any) error {
	fs := FlagAnalyzer(options)
	flags := flag.NewFlagSet(name, flag.ContinueOnError)

	for _, fl := range fs {
		f := reflect.ValueOf(options).Elem().FieldByName(fl.FlagName)

		switch fl.Type {
		case "string":
			flags.StringVar(f.Addr().Interface().(*string), fl.Name, f.String(), fl.Description)
		case "bool":
			flags.BoolVar(f.Addr().Interface().(*bool), fl.Name, f.Bool(), fl.Description)
			if fl.Short != "" {
				flags.BoolVar(f.Addr().Interface().(*bool), fl.Short, f.Bool(), fl.Description)
			}
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

// CompletionBySubCommands: Completion function for subcommands
func CompletionBySubCommands(commands []SubCommands) func(args []string) []string {
	return func(args []string) []string {
		comps := []string{}

		if len(args) == 0 {
			// Add subcommands
			for _, cmd := range commands {
				comps = append(comps, cmd.Name)
			}

			return comps
		}

		if len(args) == 1 {
			// Complete subcommand name
			last := args[0]
			for _, cmd := range commands {
				if strings.HasPrefix(cmd.Name, last) {
					comps = append(comps, cmd.Name)
				}
			}

			return comps
		}

		if len(args) == 2 {
			// Complete subcommand flags
			cmdName := args[0]
			last := args[1]

			for _, cmd := range commands {
				if cmd.Name == cmdName && cmd.Flags != nil {
					flags := FlagAnalyzer(cmd.Flags)
					for _, flag := range flags {
						if strings.HasPrefix("--"+flag.Name, last) {
							comps = append(comps, "--"+flag.Name)
						}
					}

					break
				}
			}
		}

		return comps
	}
}

// Completion function: generates possible completions based on the current arguments and available flags
func Completion(args []string, fs []Flag) []string {
	comps := []string{}
	if len(args) == 0 {
		for _, flag := range fs {
			comps = append(comps, "--"+flag.Name)
		}

		return comps
	}

	last := args[len(args)-1]

	for _, flag := range fs {
		if strings.HasPrefix("--"+flag.Name, last) || strings.HasPrefix("-"+flag.Name, last) {
			comps = append(comps, "--"+flag.Name)
		}
	}

	return comps
}

// Run function: executes the command with the given input
func Run(c Commands) {
	args := os.Args[1:]        // Get command line arguments
	inout := NewInOut()        // Create a new InOut instance
	exitCode := c(args, inout) // Execute the command

	os.Exit(exitCode) // Exit with the command's exit code
}

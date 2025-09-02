package cli

type ConfigFlag struct {
	Output string `name:"output" short:"o" description:"Output format (json|yaml|wide)"`
}

type HelpFlags struct {
	// No flags
}

type HelloFlags struct {
	Verbose bool `name:"verbose" short:"v" description:"Enable verbose output"`
}

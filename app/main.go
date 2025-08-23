package main


import (
    "github.com/cyokozai/pvectl/app/cli"
    "github.com/cyokozai/pvectl/app/options"
)


func main() {
	cli.Run(cli.CommandCompletion(options.MainCommand, cli.CompletionByFlags(cli.FlagAnalyzer(&options.Options{})))) // Run the main command
}

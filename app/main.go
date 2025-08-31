package main

import (
	"github.com/cyokozai/pvectl/app/cli"
	"github.com/cyokozai/pvectl/app/options"
)

func main() {
	cli.Run(cli.CommandCompletion(options.MainCommandRunner.Run, cli.CompletionBySubCommands(options.MainCommandRunner.SubCommands)))
}

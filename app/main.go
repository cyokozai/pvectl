package main


import (
    "github.com/cyokozai/pvectl/app/cli"
    "github.com/cyokozai/pvectl/app/options"
)


func main() {
	// Run the main command
	cli.Run(options.MainCommand)
}

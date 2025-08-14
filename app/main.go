package main


import (
    "github.com/cyokozai/pvectl/app/cli"
    "github.com/cyokozai/pvectl/app/options"
)


func main() {
	cli.Run(options.MainCommand)  // Run the main command
}

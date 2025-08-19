package options


import (
	"github.com/cyokozai/pvectl/app/cli"
)


type SubCommands struct {
	Name        string
	Description string
	Run         func(args []string, inout *cli.InOut) int
}

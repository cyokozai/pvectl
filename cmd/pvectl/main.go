package main

import (
	"os"

	"github.com/cyokozai/pvectl/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}

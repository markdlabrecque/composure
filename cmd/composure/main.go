package main

import (
	"os"

	"github.com/markdlabrecque/composure/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args, os.Stdout, os.Stderr))
}

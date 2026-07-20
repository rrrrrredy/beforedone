package main

import (
	"os"
	"runtime/debug"

	"github.com/rrrrrredy/beforedone/internal/buildversion"
	"github.com/rrrrrredy/beforedone/internal/cli"
)

var version = "dev"

func main() {
	app := cli.New()
	info, ok := debug.ReadBuildInfo()
	app.Version = buildversion.Resolve(version, info, ok)
	os.Exit(app.Run(os.Args[1:]))
}

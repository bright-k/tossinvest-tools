package main

import (
	"github.com/bright-k/tossinvest-tools/internal/cli"
	"os"
)

func main() { os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr)) }

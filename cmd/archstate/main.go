package main

import (
	"os"

	"archstate/internal/archstate"
)

func main() {
	os.Exit(archstate.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

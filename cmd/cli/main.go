package main

import (
	"os"

	"github.com/ethereum/go-ethereum/polygon/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}

package main

import (
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/polygon/common"
	"github.com/ethereum/go-ethereum/polygon/tests/fuzzers/difficulty"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: debug <file>")
		os.Exit(1)
	}
	crasher := os.Args[1]

	data := common.VerifyCrasher(crasher)
	if data == nil {
		return
	}

	difficulty.Fuzz(data)
}

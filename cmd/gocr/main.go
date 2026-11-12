package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/csx-3323730970/gocr/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		if errors.Is(err, cli.ErrBlockingFindings) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "gocr:", err)
		os.Exit(1)
	}
}

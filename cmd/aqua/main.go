package main

import (
	"fmt"
	"os"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

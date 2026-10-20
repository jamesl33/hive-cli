package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jamesl33/hive-cli/cmd"
)

// main runs 'hive'.
func main() {
	err := cmd.Execute()
	if err == nil {
		return
	}

	if errors.Is(err, context.Canceled) {
		os.Exit(130)
	}

	defer os.Exit(1)

	fmt.Fprintf(os.Stderr, "Error: %s\n", err)
}

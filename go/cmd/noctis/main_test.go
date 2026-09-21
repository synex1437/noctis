package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	for _, argument := range os.Args[1:] {
		if strings.HasPrefix(argument, "-") {
			continue
		}
		if _, isCommand := ported[argument]; isCommand {
			fmt.Fprintf(os.Stderr, "the test binary was started as a %q process: production code re-executed os.Executable(), "+
				"which under go test is this binary. Running the suite again here would spawn another copy, and another.\n", argument)
			os.Exit(1)
		}
		break
	}
	os.Exit(m.Run())
}

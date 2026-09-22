package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("NOCTIS_TEST_APP_SERVER_NOISE") != "" {
		for i := 0; ; i++ {
			if _, err := fmt.Printf("{\"method\":\"noise\",\"params\":{\"n\":%d}}\n", i); err != nil {
				os.Exit(0)
			}
		}
	}
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

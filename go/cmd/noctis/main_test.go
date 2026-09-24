package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	lingerEnv = "NOCTIS_TEST_LINGER"
	lingerFor = 5 * time.Second
)

const testAsNoctis = "NOCTIS_TEST_AS_NOCTIS"

func TestMain(m *testing.M) {
	if marker := os.Getenv(lingerEnv); marker != "" {
		time.Sleep(lingerFor)
		_ = os.WriteFile(marker, []byte("outlived the chain"), 0o644)
		os.Exit(0)
	}
	if os.Getenv("NOCTIS_TEST_APP_SERVER_NOISE") != "" {
		for i := 0; ; i++ {
			if _, err := fmt.Printf("{\"method\":\"noise\",\"params\":{\"n\":%d}}\n", i); err != nil {
				os.Exit(0)
			}
		}
	}
	if os.Getenv("NOCTIS_TEST_CODEX_WRAPPER") != "" {
		os.Exit(wrapCodexServer())
	}
	if mode := os.Getenv("NOCTIS_TEST_CODEX_SERVER"); mode != "" {
		os.Exit(serveCodex(mode))
	}
	if sid := os.Getenv("NOCTIS_TEST_RUNNER_SID"); sid != "" {
		os.Exit(runTestRunner(sid, os.Getenv("NOCTIS_TEST_RUNNER_ACCOUNT")))
	}
	if os.Getenv(testAsNoctis) != "" {
		main()
		os.Exit(0)
	}
	if raw := os.Getenv("NOCTIS_TEST_MAIN_ARGS"); raw != "" {
		var argv []string
		if json.Unmarshal([]byte(raw), &argv) != nil {
			os.Exit(2)
		}
		_ = os.Unsetenv("NOCTIS_TEST_MAIN_ARGS")
		os.Args = append([]string{os.Args[0]}, argv...)
		main()
		os.Exit(0)
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

func runTestRunner(sid, account string) int {
	_ = os.Unsetenv("NOCTIS_TEST_RUNNER_SID")
	_ = os.Unsetenv("NOCTIS_TEST_RUNNER_ACCOUNT")
	args = parseArgs(runnerArgs("resume", sid, account))
	initPaths()
	runResume()
	return 0
}

func wrapCodexServer() int {
	server := exec.Command(os.Args[0], os.Args[1:]...)
	server.Env = append(os.Environ(), "NOCTIS_TEST_CODEX_WRAPPER=")
	server.Stdin, server.Stdout, server.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := server.Run(); err != nil {
		return 1
	}
	return 0
}

func serveCodex(mode string) int {
	if err := os.WriteFile(os.Getenv("NOCTIS_TEST_CODEX_PIDFILE"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return 1
	}
	now := time.Now().Unix()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		switch request["method"] {
		case "initialize":
			fmt.Println(`{"id":0,"result":{"userAgent":"wrapped"}}`)
		case "account/rateLimits/read":
			switch mode {
			case "silent":
			case "refuse":
				fmt.Println(`{"id":1,"error":{"code":-32600,"message":"codex account authentication required to read rate limits"}}`)
			default:
				fmt.Printf("{\"id\":1,\"result\":{\"rateLimits\":{\"limitId\":\"codex\",\"primary\":{\"usedPercent\":12,\"windowDurationMins\":300,\"resetsAt\":%d},\"secondary\":{\"usedPercent\":34,\"windowDurationMins\":10080,\"resetsAt\":%d}}}}\n", now+3600, now+3*86400)
			}
		}
	}
	if mode == "deaf" {
		time.Sleep(time.Minute)
	}
	return 0
}

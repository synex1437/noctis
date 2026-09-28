package main

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

var r10WholeWordVariable = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)$`)

// r10SystemdExpands does to a service's command line what the service manager does before it
// starts it (systemd.service(5), "Command lines"): an argument that is exactly $NAME becomes the
// variable's value split at whitespace; elsewhere ${NAME} becomes its value and $$ one $.
func r10SystemdExpands(argv []string, env map[string]string) []string {
	expanded := []string{}
	for _, arg := range argv {
		if match := r10WholeWordVariable.FindStringSubmatch(arg); match != nil {
			expanded = append(expanded, strings.Fields(env[match[1]])...)
			continue
		}
		var out strings.Builder
		for index := 0; index < len(arg); index++ {
			if arg[index] == '$' && index+1 < len(arg) {
				if arg[index+1] == '$' {
					out.WriteByte('$')
					index++
					continue
				}
				if end := strings.IndexByte(arg[index+1:], '}'); arg[index+1] == '{' && end > 0 {
					out.WriteString(env[arg[index+2:index+1+end]])
					index += 1 + end
					continue
				}
			}
			out.WriteByte(arg[index])
		}
		expanded = append(expanded, out.String())
	}
	return expanded
}

func TestTheSystemdRunnerGetsItsArgumentsWhateverDollarSignsTheyHold(t *testing.T) {
	sandboxFiles(t)
	executable := "/opt/tools$$/noctis"
	command := runnerArgs("resume", "$HOME", "/home/me/cost ${HOME} $$ $HOME/.claude")
	argv := systemdRunArgs("noctis-r10", executable, command, float64(nowSec()+3600), false)

	// systemd-run looks the executable up as it is given, and the manager starts it unexpanded.
	start := slices.Index(argv, executable)
	if start < 0 {
		t.Fatalf("the executable %q is not passed to systemd-run as it is: %q", executable, argv)
	}
	if got := r10SystemdExpands(argv[start+1:], map[string]string{"HOME": "/root"}); !slices.Equal(got, command) {
		t.Fatalf("systemd starts the runner with %q, not with its arguments %q: the account and session it looks for are not this one's, and the resume is lost", got, command)
	}
}

package main

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestAScheduledRunnerGetsThePathButNoProxyOnItsCommandLine(t *testing.T) {
	path := "/home/u/.nvm/versions/node/v22/bin:/home/u/.local/bin:/usr/bin:/bin"
	t.Setenv("PATH", path)
	t.Setenv("HTTPS_PROXY", "http://proxy.corp:3128")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/etc/corp/ca & root.pem")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	arguments := systemdRunArgs("noctis-abc-1700000000", "/opt/noctis", []string{"resume", "--sid", "s1"}, float64(time.Now().Add(time.Hour).Unix()), false)
	executableAt := indexOf(arguments, "/opt/noctis")
	for _, want := range []string{"--setenv=PATH=" + path, "--setenv=NODE_EXTRA_CA_CERTS=/etc/corp/ca & root.pem", "--setenv=WAYLAND_DISPLAY=wayland-0"} {
		if at := indexOf(arguments, want); at < 0 || at > executableAt {
			t.Fatalf("the systemd timer does not hand the runner %s before the command: %v", want, arguments)
		}
	}
	if indexOf(arguments, "--setenv=DISPLAY=") >= 0 {
		t.Fatalf("an empty DISPLAY was handed on: %v", arguments)
	}
	if indexOf(arguments, "--setenv=HTTPS_PROXY=http://proxy.corp:3128") >= 0 {
		t.Fatalf("the proxy was put on the systemd-run command line: %v", arguments)
	}

	plist := launchdPlistBody("com.synex.noctis.abc", "/opt/noctis", []string{"resume"}, float64(time.Now().Add(time.Hour).Unix()), "/tmp")
	assertWellFormedXML(t, plist)
	for _, want := range []string{
		"<key>EnvironmentVariables</key><dict>",
		"<key>PATH</key><string>" + path + "</string>",
		"<key>NODE_EXTRA_CA_CERTS</key><string>/etc/corp/ca &amp; root.pem</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("the launchd agent does not carry %q:\n%s", want, plist)
		}
	}
}

var sessionProxies = map[string]string{"HTTPS_PROXY": "http://night:hunter2@proxy.corp:3128", "no_proxy": "localhost,.corp"}

func scheduleProxiedWait(t *testing.T, backend, sid string) (*[]recordedCommand, object, string) {
	t.Helper()
	config := object{"resume": object{"mode": "none"}, "alarm": object{"enabled": false}, "wait": object{"heartbeatGraceSeconds": float64(0)}}
	agents := ""
	var recorded *[]recordedCommand
	if backend == "launchd" {
		agents, recorded = sandboxLaunchd(t, config)
	} else {
		recorded = sandboxSystemd(t, config)
	}
	for name, value := range sessionProxies {
		t.Setenv(name, value)
	}
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "fable", "window": "fable", "until": now, "resumeAt": now + 60, "startedAt": now}
	})
	scheduled := scheduleRunner(loadConfig(), sid, now+60)
	if getString(scheduled, "method") != backend {
		t.Fatalf("the wait was not scheduled on %s: %v", backend, scheduled)
	}
	return recorded, scheduled, agents
}

func TestAProxyPasswordReachesNoCommandLineAndNoLaunchdPlist(t *testing.T) {
	for _, backend := range []string{"systemd", "launchd"} {
		t.Run(backend, func(t *testing.T) {
			recorded, scheduled, agents := scheduleProxiedWait(t, backend, "proxied-"+backend)
			for _, entry := range *recorded {
				if strings.Contains(strings.Join(entry.args, " "), "hunter2") {
					t.Fatalf("the proxy password is on the command line of %s: %q", filepath.Base(entry.args[0]), entry.args)
				}
			}
			if backend == "launchd" {
				body, err := os.ReadFile(filepath.Join(agents, getString(scheduled, "label")+".plist"))
				if err != nil {
					t.Fatalf("no plist was written: %v", err)
				}
				if strings.Contains(string(body), "hunter2") {
					t.Fatalf("the proxy password is written into the launchd plist:\n%s", body)
				}
			}
			_ = filepath.WalkDir(files.guardDir, func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return nil
				}
				body, _ := os.ReadFile(path)
				info, _ := entry.Info()
				if strings.Contains(string(body), "hunter2") && !isWindows && info.Mode().Perm() != 0o600 {
					t.Errorf("%s holds the proxy password with mode %v, not 0600", path, info.Mode().Perm())
				}
				return nil
			})
		})
	}
}

func jobEnvironment(t *testing.T, backend string, recorded []recordedCommand, scheduled object, agents string) map[string]string {
	t.Helper()
	environment := map[string]string{}
	if backend == "launchd" {
		body, err := os.ReadFile(filepath.Join(agents, getString(scheduled, "label")+".plist"))
		if err != nil {
			t.Fatalf("no plist was written: %v", err)
		}
		_, variables, _ := strings.Cut(string(body), "<key>EnvironmentVariables</key><dict>")
		variables, _, _ = strings.Cut(variables, "</dict>")
		for _, pair := range regexp.MustCompile(`<key>([^<]*)</key><string>([^<]*)</string>`).FindAllStringSubmatch(variables, -1) {
			environment[html.UnescapeString(pair[1])] = html.UnescapeString(pair[2])
		}
		return environment
	}
	command, found := findCommand(recorded, "--unit="+getString(scheduled, "unit"))
	if !found {
		t.Fatalf("no systemd-run command made %s: %v", getString(scheduled, "unit"), recorded)
	}
	for _, arg := range command.args {
		if assignment, isSetenv := strings.CutPrefix(arg, "--setenv="); isSetenv {
			name, value, _ := strings.Cut(assignment, "=")
			environment[name] = value
		}
	}
	return environment
}

func resumeFromTheJob(t *testing.T, backend, sid string, recorded []recordedCommand, scheduled object, agents string) {
	t.Helper()
	started := jobEnvironment(t, backend, recorded, scheduled, agents)
	for name := range sessionProxies {
		t.Setenv(name, started[name])
	}
	if backend == "launchd" {
		firedByLaunchd(t, sid, getString(scheduled, "label"))
	} else {
		firedBySystemd(t, sid, getString(scheduled, "unit"))
	}
	runResume()
	for name, want := range sessionProxies {
		if got := os.Getenv(name); got != want {
			t.Errorf("the runner %s started resumed the session with %s=%q, want the paused session's %q", backend, name, got, want)
		}
	}
}

func TestAScheduledRunnerStillGetsTheProxiesOfTheSessionThatPaused(t *testing.T) {
	for _, backend := range []string{"systemd", "launchd"} {
		t.Run(backend, func(t *testing.T) {
			sid := "proxied-runner-" + backend
			recorded, scheduled, agents := scheduleProxiedWait(t, backend, sid)
			resumeFromTheJob(t, backend, sid, *recorded, scheduled, agents)
			if getMap(getMap(readState(), "waits"), sid) != nil {
				t.Fatalf("the runner never got as far as closing the wait")
			}
		})
	}
}

func TestAHookThatJoinedAWaitLeavesTheProxiesToTheJobThatResumesIt(t *testing.T) {
	for _, backend := range []string{"systemd", "launchd"} {
		t.Run(backend, func(t *testing.T) {
			sid := "joined-" + backend
			recorded, scheduled, agents := scheduleProxiedWait(t, backend, sid)
			var held object
			updateState(func(state object) {
				wait := getMap(getMap(state, "waits"), sid)
				wait["resumeAt"], wait["inHook"], wait["holder"] = wait["until"], true, "owning-hook"
				held = cloneObject(wait)
			})
			until := numberOr(held, "until", 0)
			hold := holdWait("batch", sid, loadConfig(), &waitPlan{window: "fable", label: "fable", until: until}, until, held, false)
			if hold.owned || hold.cancelled || hold.stop != "" {
				t.Fatalf("the hook that joined the wait did not just stop holding it at the reset: %+v", hold)
			}
			if stored := getMap(getMap(readState(), "waits"), sid); !sameSchedule(getMap(stored, "scheduled"), scheduled) {
				t.Fatalf("the joined hook took the wait or its %s job away from the hook that owns it: %v", backend, stored)
			}
			resumeFromTheJob(t, backend, sid, *recorded, scheduled, agents)
		})
	}
}

func TestARunnerTheTaskSchedulerStartsGetsThePathCertificatesAndProxiesOfTheSessionThatPaused(t *testing.T) {
	windowsTaskSandbox(t)
	mustWriteJSON(files.config, object{"resume": object{"mode": "none"}, "alarm": object{"enabled": false}, "wait": object{"heartbeatGraceSeconds": float64(0)}})
	previous := args
	t.Cleanup(func() { args = previous })
	session := map[string]string{"PATH": os.Getenv("PATH"), "NODE_EXTRA_CA_CERTS": "/etc/corp/ca.pem"}
	for name, value := range sessionProxies {
		session[name] = value
	}
	for name, value := range session {
		t.Setenv(name, value)
	}
	sid := "task-environment"
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "fable", "window": "fable", "until": now, "resumeAt": now + 60, "startedAt": now}
	})
	if scheduled := scheduleRunner(loadConfig(), sid, now+60); getString(scheduled, "method") != "task" {
		t.Fatalf("the wait was not scheduled as a task: %v", scheduled)
	}
	for name := range session {
		t.Setenv(name, "")
	}
	t.Setenv("PATH", t.TempDir())

	args = parseArgs(runnerArgs("resume", sid, files.configDir))
	runResume()

	for name, want := range session {
		if got := os.Getenv(name); got != want {
			t.Errorf("the runner the scheduled task started resumed the session with %s=%q, want the paused session's %q", name, got, want)
		}
	}
	if getMap(getMap(readState(), "waits"), sid) != nil {
		t.Fatal("the runner never got as far as closing the wait")
	}
}

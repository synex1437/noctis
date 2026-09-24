package main

import (
	"strings"
	"testing"
	"time"
)

func TestAScheduledRunnerGetsThePathAndProxiesOfTheSessionThatPaused(t *testing.T) {
	path := "/home/u/.nvm/versions/node/v22/bin:/home/u/.local/bin:/usr/bin:/bin"
	t.Setenv("PATH", path)
	t.Setenv("HTTPS_PROXY", "http://proxy.corp:3128")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/etc/corp/ca & root.pem")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	arguments := systemdRunArgs("noctis-abc-1700000000", "/opt/noctis", []string{"resume", "--sid", "s1"}, float64(time.Now().Add(time.Hour).Unix()), false)
	executableAt := indexOf(arguments, "/opt/noctis")
	for _, want := range []string{"--setenv=PATH=" + path, "--setenv=HTTPS_PROXY=http://proxy.corp:3128", "--setenv=NODE_EXTRA_CA_CERTS=/etc/corp/ca & root.pem", "--setenv=WAYLAND_DISPLAY=wayland-0"} {
		if at := indexOf(arguments, want); at < 0 || at > executableAt {
			t.Fatalf("the systemd timer does not hand the runner %s before the command: %v", want, arguments)
		}
	}
	if indexOf(arguments, "--setenv=DISPLAY=") >= 0 {
		t.Fatalf("an empty DISPLAY was handed on: %v", arguments)
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

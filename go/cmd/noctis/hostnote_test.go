package main

import (
	"strings"
	"testing"
)

func TestAHostInstallWithoutLimitsSaysWhatStillWorksThere(t *testing.T) {
	root := cliPluginTree(t)
	for _, host := range hostOrder {
		spec := hostOf(host)
		if spec.limits {
			continue
		}
		t.Run(host, func(t *testing.T) {
			home := t.TempDir()

			run := runNoctisCLI(t, map[string]string{"NOCTIS_PLUGIN_ROOT": root}, "install", "--source", root, "--config-dir", home, "--host", host)

			if run.code != 0 {
				t.Fatalf("the %s install failed:\n%s", host, run)
			}
			claimsRetries := strings.Contains(run.stdout, "checkpoints and error backoff work")
			saysNoRetries := strings.Contains(run.stdout, "no checkpoint is written")
			if spec.stopFailure && (!claimsRetries || saysNoRetries) {
				t.Fatalf("%s has an error hook, so its note should say checkpoints and error backoff work:\n%s", host, run)
			}
			if !spec.stopFailure && (claimsRetries || !saysNoRetries) {
				t.Fatalf("%s has no error hook, so its note should say failed turns are not retried and no checkpoint is written:\n%s", host, run)
			}
		})
	}
}

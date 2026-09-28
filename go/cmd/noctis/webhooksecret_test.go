package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAMalformedWebhookURLStaysOutOfTheLogsAndIsCalledUnparsable(t *testing.T) {
	secret := "x1HookSecretPart"
	for name, target := range map[string]string{
		"one slash after the scheme":   "https:/hooks.slack.com/services/T000/B000/" + secret,
		"a space in the host":          "https://hooks.slack.com services/T000/B000/" + secret,
		"no scheme at all":             "hooks.slack.com/services/T000/B000/" + secret,
		"a self-hosted ntfy, no slash": "https:ntfy.example.org/" + secret,
	} {
		t.Run(name, func(t *testing.T) {
			home := webhookAccount(t, target)
			run := startNoctisCLIAt(t, home, "", nil, "webhook", "--title", "T", "--body", "B")()
			if run.code != 1 || !strings.Contains(run.stderr, "cannot be parsed") || strings.Contains(run.stderr, "must be https") {
				t.Errorf("a webhook address that does not parse must be refused as unparsable, not as plain http:\n%s", run)
			}
			if strings.Contains(run.stdout+run.stderr, secret) {
				t.Errorf("noctis webhook printed the address with its secret:\n%s", run)
			}
			for _, log := range []string{"guard.log", "errors.log"} {
				content, _ := os.ReadFile(filepath.Join(home, ".claude", pluginName, log))
				if !strings.Contains(string(content), "alarm.webhook.url") {
					t.Errorf("%s does not say that alarm.webhook.url was refused:\n%s", log, content)
				}
				if strings.Contains(string(content), secret) {
					t.Errorf("%s holds the webhook address with its secret:\n%s", log, content)
				}
			}
		})
	}
}

func TestTheDoctorHidesAWebhookSecretAnOlderVersionLogged(t *testing.T) {
	for _, target := range []string{"https:/hooks.slack.com/services/T0/B0/x1DoctorSecret", "https:/ntfy.example.org/x1DoctorSecret"} {
		sandboxFiles(t)
		cliWrite(t, files.errors, []byte(errorsLogEntry(time.Now().Add(-time.Minute), "alarm.webhook.url invalid: "+target)))
		line, _ := doctorErrorsLine(t, "claude")
		if strings.Contains(line, "x1DoctorSecret") {
			t.Errorf("the doctor shows the webhook secret an older version wrote to errors.log: %q", line)
		}
		if !strings.Contains(line, "alarm.webhook.url invalid") {
			t.Errorf("the doctor no longer names the problem in the latest entry: %q", line)
		}
	}
}

func TestABundleHidesWebhookURLsInAnyFormTheLogsHold(t *testing.T) {
	home := t.TempDir()
	account := filepath.Join(home, ".claude", pluginName)
	cliWrite(t, filepath.Join(account, "config.json"), []byte(`{"alarm": {"webhook": {"url": "https://hooks.slack.com/services/T0/B0/x1FixedNow", "preset": "slack"}}}`))
	old := []string{
		"https:/hooks.slack.com/services/T0/B0/x1SlackOneSlash",
		"https:hooks.slack.com/services/T0/B0/x1SlackNoSlash",
		"hooks.slack.com/services/T0/B0/x1SlackNoScheme",
		"HTTPS://HOOKS.SLACK.COM/services/T0/B0/x1SlackUpper",
		"https:/discord.com/api/webhooks/1/x1DiscordOneSlash",
		"https://discordapp.com/api/webhooks/1/x1DiscordApp",
		"https:/api.telegram.org/bot1:x1TelegramToken/sendMessage",
		"https:/ntfy.sh/x1NtfyTopic",
		"https:/ntfy.example.org/x1SelfHostedTopic",
		"ftp:/example.org/hook?key=x1QueryKey",
	}
	var logged strings.Builder
	at := time.Now().Add(-48 * time.Hour)
	for _, target := range old {
		logged.WriteString(errorsLogEntry(at, "alarm.webhook.url invalid: "+target))
	}
	logged.WriteString(errorsLogEntry(at, "webhook payload went to hooks.slack.com/services/T0/B0/x1InTheMiddle and failed"))
	hostOnly := "webhook failed (slack): Post hooks.slack.com: dial tcp: lookup hooks.slack.com: no such host"
	logged.WriteString(errorsLogEntry(at, hostOnly))
	cliWrite(t, filepath.Join(account, "errors.log"), []byte(logged.String()))
	cliWrite(t, filepath.Join(account, "guard.log"), []byte(logged.String()))
	target := filepath.Join(home, "bundle.zip")
	run := startNoctisCLIAt(t, home, "", nil, "report", "--bundle", target)()
	if run.code != 0 {
		t.Fatalf("noctis report --bundle failed:\n%s", run)
	}
	entries := bundleEntries(t, target)
	for _, name := range []string{"errors.log", "guard.log"} {
		if !strings.Contains(entries[name], "alarm.webhook.url invalid") || !strings.Contains(entries[name], hostOnly) {
			t.Errorf("%s in the bundle lost the log lines themselves, or a line that names only the host:\n%s", name, entries[name])
		}
	}
	for name, content := range entries {
		for _, secret := range []string{"x1FixedNow", "x1SlackOneSlash", "x1SlackNoSlash", "x1SlackNoScheme", "x1SlackUpper", "x1DiscordOneSlash", "x1DiscordApp", "x1TelegramToken", "x1NtfyTopic", "x1SelfHostedTopic", "x1QueryKey", "x1InTheMiddle"} {
			if strings.Contains(content, secret) {
				t.Errorf("%s in the bundle carries the webhook secret %s", name, secret)
			}
		}
	}
}

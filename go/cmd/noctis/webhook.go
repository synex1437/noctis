package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	webhookAttempts       = 3
	webhookBreakerTrip    = 5
	webhookBreakerSeconds = 1800
)

type webhookConfig struct {
	target string
	preset string
	chatID string
}

func webhookSettings(cfg object) webhookConfig {
	alarm := section(cfg, "alarm")
	settings := getMap(alarm, "webhook")
	config := webhookConfig{target: getString(settings, "url"), preset: strings.ToLower(getString(settings, "preset")), chatID: getString(settings, "chatId")}
	if config.target == "" {
		config.target = getString(alarm, "webhookUrl")
	}
	if config.preset == "" {
		config.preset = "generic"
	}
	return config
}

func webhookAllowed(target string) (*url.URL, bool) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		warn("alarm.webhook.url invalid: %s", target)
		return nil, false
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && localHosts[parsed.Hostname()]) {
		warn("alarm.webhook.url must be https (http only for localhost)")
		return nil, false
	}
	return parsed, true
}

func isASCII(text string) bool {
	for _, char := range text {
		if char > 127 {
			return false
		}
	}
	return true
}

func buildWebhookRequest(config webhookConfig, endpoint *url.URL, title, body string) (*http.Request, error) {
	var payload []byte
	contentType := "application/json"
	headers := map[string]string{}
	switch config.preset {
	case "telegram":
		payload = marshalCompact(object{"chat_id": config.chatID, "text": title + "\n" + body, "disable_web_page_preview": true})
	case "discord":
		payload = marshalCompact(object{"content": "**" + title + "**\n" + body})
	case "slack":
		payload = marshalCompact(object{"text": "*" + title + "*\n" + body})
	case "ntfy":
		payload = []byte(body)
		contentType = "text/plain; charset=utf-8"
		if isASCII(title) {
			headers["Title"] = title
		} else {
			headers["Title"] = "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(title)) + "?="
		}
	default:
		payload = marshalCompact(object{"source": pluginName, "account": files.configDir, "title": title, "body": body, "at": time.Now().UTC().Format(time.RFC3339)})
	}
	request, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", contentType)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return request, nil
}

func breakerOpen(state object, now int64) bool {
	return numberOr(getMap(state, "webhook"), "openUntil", 0) > float64(now)
}

func recordWebhookOutcome(success bool, now int64) {
	updateState(func(state object) {
		record := getMap(state, "webhook")
		if record == nil {
			record = object{}
		}
		if success {
			record["failures"] = float64(0)
			record["openUntil"] = float64(0)
			record["lastOkAt"] = float64(now)
		} else {
			failures := numberOr(record, "failures", 0) + 1
			record["failures"] = failures
			if failures >= webhookBreakerTrip {
				record["openUntil"] = float64(now + webhookBreakerSeconds)
				warn("webhook circuit opened after %d consecutive failures; skipping webhooks for %d min", int(failures), webhookBreakerSeconds/60)
			}
		}
		state["webhook"] = record
	})
}

func deliverWebhook(cfg object, title, body string, ignoreBreaker bool) (bool, string) {
	config := webhookSettings(cfg)
	if config.target == "" {
		return false, T("webhook.none", files.config)
	}
	endpoint, ok := webhookAllowed(config.target)
	if !ok {
		return false, T("webhook.refused")
	}
	now := nowSec()
	if state := readState(); !ignoreBreaker && breakerOpen(state, now) {
		logInfo("webhook skipped: circuit open")
		return false, T("webhook.circuitOpen", formatTime(numberOr(getMap(state, "webhook"), "openUntil", 0)))
	}
	client := &http.Client{Timeout: 5 * time.Second}
	delays := []time.Duration{0, time.Second, 3 * time.Second}
	var lastError string
	for attempt := 0; attempt < webhookAttempts; attempt++ {

		if attempt < len(delays) {
			time.Sleep(delays[attempt])
		} else {
			time.Sleep(delays[len(delays)-1])
		}
		request, err := buildWebhookRequest(config, endpoint, title, body)
		if err != nil {
			warn("webhook request build failed: %v", err)
			return false, err.Error()
		}
		response, err := client.Do(request)
		if err != nil {

			lastError = webhookErrorText(err)
			continue
		}
		response.Body.Close()
		if response.StatusCode < 300 {
			recordWebhookOutcome(true, now)
			return true, ""
		}
		lastError = "http-" + itoa(response.StatusCode)
		if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != 429 {
			break
		}
	}
	recordWebhookOutcome(false, now)
	warn("webhook failed (%s): %s", config.preset, lastError)
	return false, lastError
}

func webhookErrorText(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		host := "the endpoint"
		if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil && parsed.Host != "" {
			host = parsed.Host
		}
		return urlErr.Op + " " + host + ": " + unwrapMessage(urlErr.Err)
	}
	return err.Error()
}

func unwrapMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

func runWebhookCommand() {
	cfg := loadConfig()
	title, body := flagString("title"), flagString("body")
	test := !args.present["title"] && !args.present["body"]
	if test {
		title, body = pluginName, T("webhook.testBody")
	}
	sent, reason := deliverWebhook(cfg, title, body, test)
	if !sent {
		fmt.Fprintln(os.Stderr, T("webhook.failed", reason))
		os.Exit(1)
	}
	host := ""
	if parsed, err := url.Parse(webhookSettings(cfg).target); err == nil {
		host = parsed.Host
	}
	fmt.Println(T("webhook.sent", host, webhookSettings(cfg).preset))
}

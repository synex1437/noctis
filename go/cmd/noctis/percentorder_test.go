package main

import (
	"strings"
	"testing"
)

func TestTheUsageBadgeWritesPercentagesTheWayEachLanguageDoes(t *testing.T) {
	sandboxFiles(t)
	previous := locale
	t.Cleanup(func() { locale = previous })
	now := nowSec()
	usage := usageView{hasAny: true,
		fiveHour: &window{used: 41, resetsAt: float64(now + 3600)},
		sevenDay: &window{used: 23, resetsAt: float64(now + 6*86400)},
		fable:    &window{used: 7, resetsAt: float64(now + 6*86400)}}
	cfg := object{"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)}}
	for _, code := range []string{"en", "tr", "de", "fr", "es", "pt", "it", "nl", "pl", "ru", "ja", "zh", "ko", "ar"} {
		locale = code
		badge := usageBadgeAt(usage, cfg, now)
		want, wrong := []string{"41%", "23%", "Fable 7%"}, "%41"
		if code == "tr" {
			want, wrong = []string{"%41", "%23", "Fable %7"}, "41%"
		}
		for _, part := range want {
			if !strings.Contains(badge, part) {
				t.Errorf("%s: the usage badge %q does not write %q", code, badge, part)
			}
		}
		if strings.Contains(badge, wrong) {
			t.Errorf("%s: the usage badge %q writes the percentage as %q", code, badge, wrong)
		}
	}
}

package main

import "testing"

func TestTheWarningNamesTheWindowWhosePausePointIsNearest(t *testing.T) {
	cfg := object{"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89)}}
	now := float64(nowSec())
	cases := []struct {
		five, week float64
		want       string
	}{
		{90, 85, "five_hour"},
		{88, 88, "seven_day"},
		{86, 60, "five_hour"},
		{50, 86, "seven_day"},
	}
	for _, tc := range cases {
		usage := usageView{hasAny: true, fiveHour: &window{used: tc.five, resetsAt: now + 3600}, sevenDay: &window{used: tc.week, resetsAt: now + 86400}}
		result := evaluate(cfg, usage, "claude-opus-5-5", 0, false)
		if result.wait != nil || result.warnWindow == nil || result.warnWindow.window != tc.want {
			t.Errorf("five-hour %v%% (pauses at 92), weekly %v%% (pauses at 89): the warning names %+v, want %s", tc.five, tc.week, result.warnWindow, tc.want)
		}
	}
}

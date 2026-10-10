package main

import (
	"strings"
	"testing"
)

func TestTranslationsPutEachValueWhereTheirSentenceNeedsIt(t *testing.T) {
	previous := locale
	t.Cleanup(func() { locale = previous })
	cases := []struct {
		code, key string
		args      []any
		phrases   []string
	}{
		{"zh", "wait.earlyReset", []any{"LABEL", "WAITED"}, []string{"等待 WAITED 后", "LABEL 限额"}},
		{"ko", "wait.earlyReset", []any{"LABEL", "WAITED"}, []string{"WAITED 대기 후", "LABEL 한도"}},
		{"ja", "wait.earlyReset", []any{"LABEL", "WAITED"}, []string{"LABEL の上限", "WAITED の待機"}},
		{"zh", "report.saved", []any{"SAVED", "ACTUAL", "PRIMARY"}, []string{"约 SAVED", "花了 ACTUAL 而不是 PRIMARY"}},
		{"ko", "report.saved", []any{"SAVED", "ACTUAL", "PRIMARY"}, []string{"약 SAVED", "PRIMARY 대신 ACTUAL"}},
		{"ja", "report.saved", []any{"SAVED", "ACTUAL", "PRIMARY"}, []string{"約 SAVED", "PRIMARY ではなく ACTUAL"}},
		{"ja", "notice.burnEarly", []any{"RATE", "HOURS", "POINT", "ETA", "BEFORE"}, []string{"直近 HOURS 時間で 1 時間あたり RATE ポイント", "~ETA で一時停止点（POINT%）", "BEFORE 前"}},
		{"zh", "notice.burnEarly", []any{"RATE", "HOURS", "POINT", "ETA", "BEFORE"}, []string{"过去 HOURS 小时每小时 RATE 个百分点", "约 ETA 后到达暂停点（POINT%）", "早 BEFORE"}},
		{"ko", "notice.burnEarly", []any{"RATE", "HOURS", "POINT", "ETA", "BEFORE"}, []string{"최근 HOURS시간 동안 시간당 RATE포인트", "~ETA 후 일시정지 지점(POINT%)", "BEFORE 이릅니다"}},
		{"ja", "notice.burnStop", []any{"RATE", "HOURS", "POINT", "ETA", "BEFORE"}, []string{"直近 HOURS 時間で 1 時間あたり RATE ポイント", "~ETA で一時停止点（POINT%）", "BEFORE 前"}},
		{"zh", "notice.burnStop", []any{"RATE", "HOURS", "POINT", "ETA", "BEFORE"}, []string{"过去 HOURS 小时每小时 RATE 个百分点", "约 ETA 后到达暂停点（POINT%）", "早 BEFORE"}},
		{"ko", "notice.burnStop", []any{"RATE", "HOURS", "POINT", "ETA", "BEFORE"}, []string{"최근 HOURS시간 동안 시간당 RATE포인트", "~ETA 후 일시정지 지점(POINT%)", "BEFORE 이르므로"}},
		{"zh", "notice.subagentGrowthStop", []any{"WHO", "CALLS"}, []string{"在 CALLS 次工具调用后停止了子代理 WHO"}},
		{"ko", "notice.subagentGrowthStop", []any{"WHO", "CALLS"}, []string{"도구 호출 CALLS회 후 서브에이전트 WHO를"}},
		{"ja", "notice.subagentGrowthStop", []any{"WHO", "CALLS"}, []string{"サブエージェント WHO をツール呼び出し CALLS 回の後に"}},
	}
	for _, tc := range cases {
		locale = tc.code
		text := T(tc.key, tc.args...)
		for _, phrase := range tc.phrases {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s %s: %q does not say %q", tc.code, tc.key, text, phrase)
			}
		}
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowClearedAt(t *testing.T) {
	now := float64(nowSec())
	cases := []struct {
		name      string
		usage     usageView
		key       string
		threshold float64
		startedAt float64
		maxAge    float64
		until     float64
		want      bool
	}{
		{"cleared well below the threshold", usageView{hasAny: true, updatedAt: now, fiveHour: &window{used: 2, resetsAt: now + 18000}}, "five_hour", 92, now - 600, 660, now - 60, true},
		{"still full", usageView{hasAny: true, updatedAt: now, fiveHour: &window{used: 93, resetsAt: now + 900}}, "five_hour", 92, now - 600, 660, now - 60, false},
		{"just below the threshold is not a reset", usageView{hasAny: true, updatedAt: now, fiveHour: &window{used: 83, resetsAt: now + 900}}, "five_hour", 92, now - 600, 660, now - 60, false},
		{"data older than the wait proves nothing", usageView{hasAny: true, updatedAt: now - 1200, fiveHour: &window{used: 2, resetsAt: now + 18000}}, "five_hour", 92, now - 600, 660, now - 60, false},
		{"stale window is ignored", usageView{hasAny: true, updatedAt: now, fiveHour: &window{used: 2, resetsAt: now + 18000, staleness: 5000}}, "five_hour", 92, now - 600, 660, now - 60, false},
		{"absent window with no data at all is not a reset", usageView{}, "five_hour", 92, now - 600, 660, now - 60, false},
		{"absent window with stale data is not a reset", usageView{hasAny: true, updatedAt: now - 5000}, "five_hour", 92, now - 600, 660, now - 60, false},
		{"absent window in an otherwise empty payload is not a reset", usageView{hasAny: true, updatedAt: now}, "five_hour", 92, now - 600, 660, now - 60, false},
		{"unknown key", usageView{hasAny: true, updatedAt: now}, "nonsense", 92, now - 600, 660, now - 60, false},
		{"weekly window", usageView{hasAny: true, updatedAt: now, sevenDay: &window{used: 3, resetsAt: now + 86400}}, "seven_day", 89, now - 600, 660, now - 60, true},
		{"scoped window", usageView{hasAny: true, updatedAt: now, fable: &window{used: 10, resetsAt: now + 86400}}, "fable", 95, now - 600, 660, now - 60, true},
	}
	for _, tc := range cases {
		if got := windowClearedAt(tc.usage, tc.key, tc.threshold, tc.startedAt, tc.maxAge, tc.until); got != tc.want {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, got)
		}
	}

	complete := usageView{hasAny: true, updatedAt: now, sevenDay: &window{used: 20, resetsAt: now + 86400}}
	if !windowClearedAt(complete, "five_hour", 92, now-600, 660, now-10) {
		t.Error("an absent window in a complete payload after its reset time should count as reset")
	}
	if windowClearedAt(complete, "five_hour", 92, now-600, 660, now+3600) {
		t.Error("an absent window before its announced reset must not wake the session")
	}
	if windowClearedAt(usageView{hasAny: true, updatedAt: now}, "five_hour", 92, now-600, 660, now-10) {
		t.Error("an empty payload must never count as a reset")
	}
}

func TestToWindowClamps(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	cases := []struct {
		name    string
		percent any
		want    float64
		ok      bool
	}{
		{"ordinary", float64(41), 41, true},
		{"over 100 is clamped", float64(150), 100, true},
		{"negative is clamped", float64(-20), 0, true},
		{"numeric string", "77", 77, true},
		{"NaN is rejected", "NaN", 0, false},
		{"infinity is rejected", "Inf", 0, false},
		{"nonsense is rejected", "lots", 0, false},
	}
	for _, tc := range cases {
		win, ok := toWindow(tc.percent, reset)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if ok && numberOr(win, "used", -1) != tc.want {
			t.Errorf("%s: used = %v, want %v", tc.name, numberOr(win, "used", -1), tc.want)
		}
	}
	if _, ok := toWindow(float64(10), "not a date"); ok {
		t.Error("an unparseable reset time must be rejected")
	}
	if _, ok := toWindow(float64(10), float64(nowSec()+400*86400)); ok {
		t.Error("a reset time a year away must be rejected: it would park a session for a year")
	}
	if _, ok := toWindow(float64(10), time.Now().Add(-3*time.Hour).UTC().Format(time.RFC3339)); !ok {
		t.Error("a window whose reset has already passed is ordinary data, not an error")
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"5.3.0", "5.2.0", true},
		{"10.0.0", "9.0.0", true},
		{"9.0.0", "10.0.0", false},
		{"5.2.0", "5.2.0", false},
		{"1.2", "1.2.0", false},
		{"1.2.1", "1.2", true},
		{"v2.0.0", "1.9.9", true},
		{"1.2.0-rc1", "1.2.0", false},
		{"garbage", "1.0.0", false},
		{"", "1.0.0", false},
	}
	for _, tc := range cases {
		if got := newerVersion(tc.candidate, tc.current); got != tc.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
		}
	}
}

func TestPriorityFromLabels(t *testing.T) {
	cases := []struct {
		labels []string
		want   string
	}{
		{[]string{"P1"}, "(P1) "},
		{[]string{"priority: high"}, "(P1) "},
		{[]string{"critical"}, "(P0) "},
		{[]string{"bug", "P7"}, "(P7) "},
		{nil, ""},
		{[]string{"documentation"}, ""},
		{[]string{"highlight"}, ""},
		{[]string{"slowdown"}, ""},
		{[]string{"needs-triage", "priority/low"}, "(P7) "},
		{[]string{"priority/P1"}, "(P1) "},
		{[]string{"priority: P2"}, "(P2) "},
		{[]string{"Priority: Critical"}, "(P0) "},
		{[]string{"non-critical"}, ""},
		{[]string{"not-urgent"}, ""},
		{[]string{"low-hanging-fruit"}, ""},
		{[]string{"high-availability"}, ""},
		{[]string{"P10"}, ""},
		{[]string{"sev1"}, ""},
		{[]string{"severity/P0"}, "(P0) "},
	}
	for _, tc := range cases {
		if got := priorityFromLabels(tc.labels); got != tc.want {
			t.Errorf("priorityFromLabels(%v) = %q, want %q", tc.labels, got, tc.want)
		}
	}
}

func TestShellQuoting(t *testing.T) {
	values := []string{"plain", "with space", "it's", `double"quote`, "semi;colon", "dollar$sign", "back`tick`", "new\nline", "%PATH%", "*"}
	for _, value := range values {
		quoted := shellQuote(value)
		out, err := runWithTimeout(execCommand("sh", "-c", "printf '%s' "+quoted), 5*time.Second)
		if err != nil {
			t.Fatalf("sh rejected %q as %s: %v", value, quoted, err)
		}
		if string(out) != value {
			t.Errorf("shellQuote(%q) round-tripped as %q", value, string(out))
		}
	}
	joined := shellJoin([]string{"a b", "c'd"})
	if joined != `'a b' 'c'\''d'` {
		t.Errorf("shellJoin produced %s", joined)
	}
}

func TestAppleScriptEscape(t *testing.T) {
	if got := appleScriptEscape(`/tmp/a"b`); got != `/tmp/a\"b` {
		t.Errorf("quote not escaped: %s", got)
	}
	if got := appleScriptEscape(`/tmp/a\b`); got != `/tmp/a\\b` {
		t.Errorf("backslash not escaped: %s", got)
	}
	if got := appleScriptEscape(`/tmp/a\"b`); got != `/tmp/a\\\"b` {
		t.Errorf("backslash before a quote must be escaped first: %s", got)
	}
}

func TestLooksLikeSessionProcessNames(t *testing.T) {
	for _, name := range []string{"claude", "node", "/usr/bin/node", "powershell.exe", "bash", "CMD.EXE", "  node  ", "/usr/local/bin/node --inspect"} {
		if !looksLikeSessionName(name) {
			t.Errorf("%q should be recognised as a session process", name)
		}
	}
	for _, name := range []string{"sshd", "ssh", "fish", "dockerd", "python3", "systemd", "chrome", "", "nodemon", "bashful"} {
		if looksLikeSessionName(name) {
			t.Errorf("%q must not be mistaken for a session process", name)
		}
	}
}

func TestOwnHelperProcess(t *testing.T) {
	for _, name := range []string{"noctis", "NOCTIS", "/opt/plugins/noctis", "noctis.exe", "noctis sleeper --sid x"} {
		if !ownHelperProcess(name) {
			t.Errorf("%q is our own helper and should be recognised", name)
		}
	}
	for _, name := range []string{"", "node", "bash", "postgres", "noctisd", "my-noctis"} {
		if ownHelperProcess(name) {
			t.Errorf("%q is not our helper and must not be killed", name)
		}
	}
}

func TestQueueTrustKeyIsPerFile(t *testing.T) {
	deep := strings.Repeat("/very-long-directory-name", 4)
	first := queueTrustKey(deep + "/project-one/TASKS.md")
	second := queueTrustKey(deep + "/project-two/TASKS.md")
	if first == second {
		t.Fatalf("two different files share a trust key: %s", first)
	}
	if queueTrustKey("/a/TASKS.md") != queueTrustKey("/a/TASKS.md") {
		t.Fatal("the same file must always produce the same trust key")
	}
}

func TestSafeNameHasNoDotsOrCollisions(t *testing.T) {
	for _, input := range []string{"..", ".", "../..", "../escape"} {
		if got := safeName(input); strings.Trim(got, "_-") == "" && strings.Contains(got, ".") {
			t.Errorf("safeName(%q) = %q still resolves as a path", input, got)
		}
		if got := safeName(input); got == ".." || got == "." {
			t.Errorf("safeName(%q) = %q", input, got)
		}
	}
	long := strings.Repeat("a", 200)
	if safeName(long+"one") == safeName(long+"two") {
		t.Error("two long names collide after truncation")
	}
}

func TestMergeDefaultsIsDeep(t *testing.T) {
	defaults := object{"queue": object{"enabled": true, "github": object{"closeOnDone": false, "comment": "done"}}}
	user := object{"queue": object{"github": object{"closeOnDone": true}}}
	merged := mergeDefaults(defaults, user)
	github := getMap(getMap(merged, "queue"), "github")
	if !getBool(github, "closeOnDone", false) {
		t.Error("the person's value was lost")
	}
	if getString(github, "comment") != "done" {
		t.Error("the default beside it was dropped — this is the one-level merge bug")
	}
	if !getBool(getMap(merged, "queue"), "enabled", false) {
		t.Error("an untouched sibling key was dropped")
	}
}

func TestReadStoredStateRecovery(t *testing.T) {
	dir := t.TempDir()
	previous := files
	defer func() { files = previous }()
	files.guardDir = dir
	files.state = filepath.Join(dir, "state.json")
	files.stateBackup = filepath.Join(dir, "state.json.bak")
	files.log, files.errors = filepath.Join(dir, "guard.log"), filepath.Join(dir, "errors.log")
	good := object{"waits": object{"s1": object{"resumeAt": float64(123)}}}
	if err := writeJSONAtomic(files.stateBackup, good); err != nil {
		t.Fatal(err)
	}
	for _, broken := range []string{`{"waits":`, `null`, `[]`, `123`, `"wiped"`} {
		if err := os.WriteFile(files.state, []byte(broken), 0o644); err != nil {
			t.Fatal(err)
		}
		state := readStoredState()
		if getMap(getMap(state, "waits"), "s1") == nil {
			t.Fatalf("%s: the wait was not recovered from the backup", broken)
		}
		restored := readJSONStrict(files.state)
		if !restored.ok || restored.data == nil {
			t.Fatalf("%s: state.json was not repaired on disk", broken)
		}
	}
}

func TestHookSleeping(t *testing.T) {
	cases := []struct {
		name string
		wait object
		want bool
	}{
		{"plain parked wait", object{"resumeAt": float64(100)}, false},
		{"in-hook wait", object{"inHook": true}, true},
		{"waking wait", object{"inHook": false, "waking": float64(1789814746)}, true},
		{"waking cleared after the sleep", object{"inHook": false, "waking": float64(0)}, false},
		{"marker absent", object{}, false},
		{"waking as a bogus type", object{"waking": "yes"}, false},
	}
	for _, testCase := range cases {
		if got := hookSleeping(testCase.wait); got != testCase.want {
			t.Errorf("%s: hookSleeping = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

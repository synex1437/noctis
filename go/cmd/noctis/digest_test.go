package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const digestTasks = "# q\n- [x] set up the repository\n- [ ] migrate the users table\n- [ ] connect the payment provider #pay\n- [ ] charge the first customer (after #pay)\n- [ ] write the release notes\n- [ ] (human) pick the brand colours\n"

// digestInbox is a webhook endpoint that keeps each message it is sent, in the generic preset's shape.
type digestInbox struct {
	mu       sync.Mutex
	messages []object
	url      string
}

func newDigestInbox(t *testing.T) *digestInbox {
	t.Helper()
	inbox := &digestInbox{}
	server := httptest.NewServer(inbox)
	t.Cleanup(server.Close)
	inbox.url = server.URL + "/hook"
	return inbox
}

func (inbox *digestInbox) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	message := object{}
	_ = json.Unmarshal(raw, &message)
	inbox.mu.Lock()
	inbox.messages = append(inbox.messages, message)
	inbox.mu.Unlock()
}

func (inbox *digestInbox) received() []object {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	return append([]object{}, inbox.messages...)
}

// digestSandbox is a project whose trusted TASKS.md holds digestTasks, with the digest set for clock,
// a webhook that keeps what it is sent, no scheduler and no usage fetch.
func digestSandbox(t *testing.T, clock string) (object, string, string, string, *digestInbox) {
	t.Helper()
	cfg, project, frontend, _ := deferSandbox(t)
	path := writeQueueFile(t, project, digestTasks)
	trustQueueFile(path, true)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	inbox := newDigestInbox(t)
	alarm := section(cfg, "alarm")
	alarm["enabled"], alarm["digestAt"] = true, clock
	alarm["webhook"] = object{"url": inbox.url, "preset": "generic"}
	section(cfg, "fable")["source"] = "off"
	return cfg, project, frontend, path, inbox
}

func TestTheDigestSaysHowTheQueueStandsAndWhatWaitsOnTheUser(t *testing.T) {
	cfg, project, _, path, _ := digestSandbox(t, "21:00")
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason, "--until", "2d")
	section(cfg, "queue")["verifyCommand"] = "true"
	now := nowSec()
	updateState(func(next object) {
		stateMap(next, "queueVerify")[queueTrustKey(path)] = object{"ticked": []any{}, "failures": float64(2), "at": float64(now - 60)}
	})
	report := buildDigest(cfg, readState(), currentUsage(now), nil, now)
	if want := T("digest.title", pluginName, digestDay(now)); report.title != want {
		t.Fatalf("the digest is titled %q, want %q", report.title, want)
	}
	for _, want := range []string{
		T("digest.limits", usageBadgeAt(currentUsage(now), cfg, now)),
		T("digest.queue", "TASKS.md", 1, 5),
		"- " + T("digest.next", "migrate the users table"),
		"- " + T("digest.checkFailing", 2, formatTime(float64(now-60))),
		"- " + T("digest.human", 1, "(human) pick the brand colours"),
		"- " + T("digest.deferred", 1, "connect the payment provider #pay — "+deferReason),
	} {
		if !strings.Contains(report.body, want) {
			t.Fatalf("the digest does not say %q:\n%s", want, report.body)
		}
	}
	// With no digest sent before, none says what was done since: the item taken next follows the
	// queue's first line.
	lines := strings.Split(report.body, "\n")
	for index, line := range lines {
		if line == T("digest.queue", "TASKS.md", 1, 5) && (index+1 == len(lines) || lines[index+1] != "- "+T("digest.next", "migrate the users table")) {
			t.Fatalf("the queue's first line is not followed by the item taken next:\n%s", report.body)
		}
	}
	updateState(func(next object) {
		stateMap(stateMap(next, "queueVerify"), queueTrustKey(path))["held"] = float64(now - 30)
	})
	if body := buildDigest(cfg, readState(), currentUsage(now), nil, now).body; !strings.Contains(body, "- "+T("digest.checkHeld", formatTime(float64(now-30)), pluginName)) {
		t.Fatalf("a held queue is not reported as held:\n%s", body)
	}
}

func TestTheDigestTellsApartTheQueuesOfTwoProjectsThatShareAFileName(t *testing.T) {
	cfg, _, _, shop, _ := digestSandbox(t, "08:00")
	blog := writeQueueFile(t, t.TempDir(), "# q\n- [x] write the post\n- [x] add the RSS feed\n- [ ] (human) proofread the post\n")
	trustQueueFile(blog, true)
	now := nowSec()
	body := buildDigest(cfg, readState(), currentUsage(now), nil, now).body
	for _, want := range []string{T("digest.queue", homeAsTilde(shop), 1, 5), T("digest.queue", homeAsTilde(blog), 2, 1)} {
		if !strings.Contains(body, want) {
			t.Fatalf("the digest of two projects' %s does not head one of them %q:\n%s", filepath.Base(shop), want, body)
		}
	}
}

func TestTheDigestGoesOutAndTheNextOneNamesWhatWasDoneSince(t *testing.T) {
	cfg, project, _, _, inbox := digestSandbox(t, "21:00")
	if sent, reason := sendDigest(cfg, "test", false); !sent {
		t.Fatalf("the digest did not go out: %s", reason)
	}
	messages := inbox.received()
	if len(messages) != 1 || getString(messages[0], "title") != T("digest.title", pluginName, digestDay(nowSec())) || !strings.Contains(getString(messages[0], "body"), T("digest.queue", "TASKS.md", 1, 5)) {
		t.Fatalf("the webhook got %v", messages)
	}
	if record := getMap(readState(), "digest"); numberOr(record, "sentAt", 0) == 0 || getString(record, "failed") != "" {
		t.Fatalf("the digest's record once it went out: %v", record)
	}
	baseline := readJSON(digestFile())
	writeQueueFile(t, project, strings.Replace(digestTasks, "- [ ] migrate the users table", "- [x] migrate the users table", 1))
	now := nowSec()
	body := buildDigest(cfg, readState(), currentUsage(now), baseline, now).body
	if want := "- " + T("digest.doneSince", formatTime(numberOr(baseline, "at", 0)), 1, "migrate the users table"); !strings.Contains(body, want) {
		t.Fatalf("the next digest does not say %q:\n%s", want, body)
	}
	if want := T("digest.queue", "TASKS.md", 2, 4); !strings.Contains(body, want) {
		t.Fatalf("the next digest does not say %q:\n%s", want, body)
	}

	section(cfg, "alarm")["webhook"] = object{"url": "http://example.invalid/hook", "preset": "generic"}
	if sent, _ := sendDigest(cfg, "test", false); sent {
		t.Fatal("a digest sent to a webhook noctis refuses went out")
	}
	record := getMap(readState(), "digest")
	if getString(record, "failed") == "" {
		t.Fatalf("a digest that did not go out left no reason: %v", record)
	}
	if want := T("status.digestFailed", formatTime(numberOr(record, "sentAt", 0)), getString(record, "failed")); !strings.Contains(digestStatus(cfg, readState()), want) {
		t.Fatalf("noctis status does not say the last digest failed: %q", digestStatus(cfg, readState()))
	}
	if after := readJSON(digestFile()); numberOr(after, "at", 0) != numberOr(baseline, "at", 0) {
		t.Fatalf("a digest that did not go out became the baseline: %v", after)
	}
}

func TestTheDigestNamesAnItemDoneSinceWhenAnotherWithTheSameTextWasDoneBefore(t *testing.T) {
	cfg, project, _, _, _ := digestSandbox(t, "21:00")
	path := writeQueueFile(t, project, "# q\n- [x] update the docs\n- [ ] update the docs\n- [ ] ship it\n")
	trustQueueFile(path, true)
	now := nowSec()
	baseline := object{"at": float64(now - 3600), "queues": buildDigest(cfg, readState(), currentUsage(now), nil, now).done}
	writeQueueFile(t, project, "# q\n- [x] update the docs\n- [x] update the docs\n- [ ] ship it\n")
	body := buildDigest(cfg, readState(), currentUsage(now), baseline, now).body
	if want := "- " + T("digest.doneSince", formatTime(float64(now-3600)), 1, "update the docs"); !strings.Contains(body, want) {
		t.Fatalf("the second \"update the docs\" was ticked since the last digest, and the digest does not say %q:\n%s", want, body)
	}
}

func TestTheDigestRunnerSendsTheDueDigestOnceAndSetsUpTheNextDay(t *testing.T) {
	clock := time.Now().Add(-time.Minute).Format("15:04")
	cfg, _, _, _, inbox := digestSandbox(t, clock)
	due := float64(nowSec() - 60)
	// Another runner claimed this digest already: this one only sets up the next day's.
	updateState(func(next object) {
		next["digest"] = object{"time": clock, "at": due, "sent": due, "scheduled": object{"method": "manual", "at": due}}
	})
	fireDigest(cfg)
	if got := len(inbox.received()); got != 0 || numberOr(getMap(readState(), "digest"), "at", 0) <= due {
		t.Fatalf("a digest another runner claimed was sent again (%d sent) or the next day's was not set up", got)
	}
	updateState(func(next object) {
		next["digest"] = object{"time": clock, "at": due, "scheduled": object{"method": "manual", "at": due}}
	})
	fireDigest(cfg)
	record := getMap(readState(), "digest")
	if len(inbox.received()) != 1 || numberOr(record, "sent", 0) != due {
		t.Fatalf("the runner sent %d digest(s) for the one due; record %v", len(inbox.received()), record)
	}
	if next := nextDigestAt(nowSec(), clock); numberOr(record, "at", 0) != next || getString(getMap(record, "scheduled"), "method") != "manual" {
		t.Fatalf("the next digest is set for %v via %q, want %v via manual", numberOr(record, "at", 0), getString(getMap(record, "scheduled"), "method"), next)
	}
	fireDigest(cfg)
	if got := len(inbox.received()); got != 1 {
		t.Fatalf("a second run of the runner sent the digest again (%d sent)", got)
	}
}

func TestASessionStartsTheDigestRunnerWhenNoneIsSetForTheTimeOrItIsLate(t *testing.T) {
	cfg, project, frontend, _, _ := digestSandbox(t, "09:30")
	started := 0
	previous := startDigestRunner
	t.Cleanup(func() { startDigestRunner = previous })
	startDigestRunner = func() { started++ }
	setDigest := func(record object) { updateState(func(next object) { next["digest"] = record }) }
	now := nowSec()

	hookOutput(t, onSessionStart, agentHookInput("SessionStart", "dg1", project, object{"source": "startup"}), cfg)
	if started != 1 || numberOr(getMap(readState(), "digest"), "started", 0) == 0 {
		t.Fatalf("a session start with the digest asked for and no runner set started %d runner(s)", started)
	}
	stopHookOutput(t, stopInput("dg1", frontend), cfg)
	if started != 1 {
		t.Fatal("the runner was started again within digestRetrySeconds")
	}
	steps := []struct {
		name, clock string
		record      object
		want        int
	}{
		{"on time", "09:30", object{"time": "09:30", "at": float64(now + 3600)}, 1},
		{"late", "09:30", object{"time": "09:30", "at": float64(now - digestLateSeconds - 1)}, 2},
		{"a new time", "10:00", object{"time": "09:30", "at": float64(now + 3600)}, 3},
		{"turned off with a runner set", "", object{"time": "09:30", "at": float64(now + 3600)}, 4},
		{"off and nothing set", "", object{}, 4},
	}
	for _, step := range steps {
		section(cfg, "alarm")["digestAt"] = step.clock
		setDigest(step.record)
		stopHookOutput(t, stopInput("dg1", frontend), cfg)
		if started != step.want {
			t.Fatalf("%s: %d runner(s) started, want %d", step.name, started, step.want)
		}
	}
}

func TestTheJournalDatesTheStartAndSettingOfTheDigestRunnerWhenTheyHappenAndNamesTheDigestsTime(t *testing.T) {
	cfg, _, _, _, _ := digestSandbox(t, "09:30")
	previous := startDigestRunner
	t.Cleanup(func() { startDigestRunner = previous })
	startDigestRunner = func() {}
	now := nowSec()
	late := float64(now - digestLateSeconds - 1)
	updateState(func(next object) { next["digest"] = object{"time": "09:30", "at": late} })

	maybeDigest(cfg, readState(), now)
	armDigest(cfg, "09:30", now)

	next := nextDigestAt(now, "09:30")
	for action, due := range map[string]float64{"start-runner": late, "arm": next} {
		entry := journaledEntry("", action)
		if at := numberOr(entry, "at", 0); at < float64(now) || at > float64(nowSec()) || numberOr(entry, "dueAt", 0) != due {
			t.Errorf("the journal dates the digest's %s at %s, or does not name the digest due %s: %v", action, localISO(at), localISO(due), entry)
		}
	}
	if listed := whyOutput(t); !strings.Contains(listed, " 09:30 [dueAt="+formatTime(next)+"]\n") {
		t.Errorf("noctis why does not say when the next digest is due (%s):\n%s", formatTime(next), listed)
	}
}

func TestNextDigestAtIsTheNextTimeOfThatClock(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.Local).Unix()
	for clock, want := range map[string]time.Time{
		"21:00": time.Date(2026, 9, 30, 21, 0, 0, 0, time.Local),
		"09:30": time.Date(2026, 10, 1, 9, 30, 0, 0, time.Local),
		"10:00": time.Date(2026, 10, 1, 10, 0, 0, 0, time.Local),
	} {
		if got := nextDigestAt(now, clock); got != float64(want.Unix()) {
			t.Errorf("at 10:00 the next %s is %s, want %s", clock, time.Unix(int64(got), 0), want)
		}
	}
}

func TestTheDigestSaysWhyItIsOffAndDropsItsRunner(t *testing.T) {
	cfg, _, _, _, _ := digestSandbox(t, "25:99")
	if _, problem := digestTime(cfg); problem != T("digest.badTime", "25:99") {
		t.Fatalf("an unreadable time gives %q", problem)
	} else if got := digestStatus(cfg, readState()); got != T("status.digestOff", problem) {
		t.Fatalf("noctis status says %q, want %q", got, T("status.digestOff", problem))
	}
	alarm := section(cfg, "alarm")
	alarm["digestAt"], alarm["enabled"] = "7:05", false
	if _, problem := digestTime(cfg); problem != T("digest.alarmOff") {
		t.Fatalf("with the alarm off the digest gives %q", problem)
	}
	alarm["enabled"], alarm["webhook"] = true, object{"url": "", "preset": "generic"}
	if _, problem := digestTime(cfg); problem != T("digest.noWebhook") {
		t.Fatalf("with no webhook the digest gives %q", problem)
	}
	updateState(func(next object) {
		next["digest"] = object{"time": "07:05", "at": float64(nowSec() + 3600), "scheduled": object{"method": "manual"}}
	})
	fireDigest(cfg)
	if record := getMap(readState(), "digest"); len(record) != 0 {
		t.Fatalf("a digest turned off kept its runner: %v", record)
	}
	alarm["webhook"] = object{"url": "https://example.com/hook", "preset": "generic"}
	if clock, problem := digestTime(cfg); clock != "07:05" || problem != "" {
		t.Fatalf("7:05 reads as %q (%q), want 07:05", clock, problem)
	}
	alarm["digestAt"] = ""
	if got := digestStatus(cfg, readState()); got != "" {
		t.Fatalf("with no digest asked for noctis status still says %q", got)
	}
}

func TestUninstallDropsTheDigestRunner(t *testing.T) {
	cfg, _, _, _, _ := digestSandbox(t, "21:00")
	account := recordAccount(t, object{})
	sandbox := files
	t.Cleanup(func() { files = sandbox })
	files = pathsFor(account, files.pluginRoot)
	armDigest(cfg, "21:00", nowSec())
	if record := getMap(readState(), "digest"); getString(record, "time") != "21:00" {
		t.Fatalf("no runner was set up for the digest: %v", record)
	}
	if _, err := cancelAccountRelaunches(account); err != nil {
		t.Fatal(err)
	}
	if record := getMap(readState(), "digest"); len(record) != 0 {
		t.Fatalf("uninstall kept the digest runner: %v", record)
	}
}

func TestNoctisDigestPrintsTheDigestAndSendsItWithSend(t *testing.T) {
	inbox := newDigestInbox(t)
	home := t.TempDir()
	cliWrite(t, filepath.Join(home, ".claude", pluginName, "config.json"), []byte(`{"alarm": {"digestAt": "21:00", "webhook": {"url": "`+inbox.url+`", "preset": "generic"}}, "fable": {"source": "off"}}`))
	env := map[string]string{"NOCTIS_NO_SCHEDULE": "1"}
	preview := startNoctisCLIAt(t, home, "", env, "digest")()
	if preview.code != 0 || !strings.Contains(preview.stdout, "daily digest") || !strings.Contains(preview.stdout, "No queue file was driven in the last 7 days.") || !strings.Contains(preview.stdout, "[manual]") {
		t.Fatalf("noctis digest did not print the digest and when the next one goes out:\n%s", preview)
	}
	if got := len(inbox.received()); got != 0 {
		t.Fatalf("noctis digest without --send sent %d message(s)", got)
	}
	sent := startNoctisCLIAt(t, home, "", env, "digest", "--send")()
	if messages := inbox.received(); sent.code != 0 || !strings.Contains(sent.stdout, "delivered") || len(messages) != 1 || !strings.Contains(getString(messages[0], "title"), "daily digest") {
		t.Fatalf("noctis digest --send did not send the digest (%v):\n%s", messages, sent)
	}
	status := startNoctisCLIAt(t, home, "", env, "status")()
	if !strings.Contains(status.stdout, "Digest       : daily at 21:00, next ") {
		t.Fatalf("noctis status does not show the digest:\n%s", status)
	}
	cliWrite(t, filepath.Join(home, ".claude", pluginName, "config.json"), []byte(`{"alarm": {"digestAt": "", "webhook": {"url": "`+inbox.url+`", "preset": "generic"}}, "fable": {"source": "off"}}`))
	if off := startNoctisCLIAt(t, home, "", env, "digest")(); off.code != 0 || !strings.Contains(off.stdout, "once alarm.digestAt") {
		t.Fatalf("noctis digest with alarm.digestAt cleared does not say how to turn it on:\n%s", off)
	}
	var state object
	if err := json.Unmarshal(readFileOrEmpty(filepath.Join(home, ".claude", pluginName, "state.json")), &state); err != nil || len(getMap(state, "digest")) != 0 {
		t.Fatalf("noctis digest with alarm.digestAt cleared kept the runner set: %v (%v)", getMap(state, "digest"), err)
	}
}

package main

import (
	"os"
	"strings"
)

// queueEnv set to off keeps every queue out of a session and out of the processes it starts; set to
// on, it lets queues drive a headless run as well.
const queueEnv = "NOCTIS_QUEUE"

func queueEnvSetting() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(queueEnv))) {
	case "off", "0", "false", "no":
		return "off"
	case "on", "1", "true", "yes":
		return "on"
	}
	return ""
}

// headlessRun tells a run nobody watches (claude -p from a script, the Agent SDK, a GitHub action)
// from an interactive session. A cloud session has no terminal either, but it is the user's own
// session there, so it counts as interactive.
func headlessRun() bool {
	return nonInteractiveEntrypoints[os.Getenv("CLAUDE_CODE_ENTRYPOINT")] && !cloudSession()
}

// relaunchedByNoctis is true in a session noctis started to go on with a paused one: the session
// itself, resumed, or the fresh session that took its place. state may be nil; it is read only when
// the answer needs it.
func relaunchedByNoctis(state object, sid string) bool {
	from := os.Getenv(handoffEnv)
	switch {
	case from == "":
		return false
	case from == sid:
		return true
	}
	if state == nil {
		state = readState()
	}
	return getString(getMap(getMap(state, "freshStarts"), sid), "from") == from
}

// queuesDriveRun answers whether the folder's queue files and the checklists noctis makes from
// prompts may keep this session going. A headless run is left alone by default: a script that asks
// claude -p one question inside a project wants its answer, not the project's queue worked through.
// queue.headless or NOCTIS_QUEUE=on let queues drive it; NOCTIS_QUEUE=off keeps them out of any
// session. A session noctis relaunched goes on as the paused one did (see queueOffFor).
func queuesDriveRun(cfg, state object, sid string) bool {
	switch queueEnvSetting() {
	case "off":
		return false
	case "on":
		return true
	}
	return !headlessRun() || getBool(section(cfg, "queue"), "headless", false) || relaunchedByNoctis(state, sid)
}

// startedInRun is true when the user started sid's queue with /noctis:start: that was the run's own
// request, so it drives a headless run too.
func startedInRun(state object, sid string) bool {
	if state == nil {
		state = readState()
	}
	return getString(getMap(getMap(state, "autoQueues"), sid), "source") != ""
}

// drivenQueueFile is the queue that keeps this session going (sessionQueueFile's answer), or "" when
// no queue may (queuesDriveRun). state may be nil.
func drivenQueueFile(cfg, state, input object, sid string) string {
	if queueEnvSetting() == "off" {
		return ""
	}
	path := sessionQueueFile(cfg, sid, queueDirs(input)...)
	if path == "" || queuesDriveRun(cfg, state, sid) || (isAutoQueue(path) && startedInRun(state, sid)) {
		return path
	}
	logInfo("queue %s does not drive %s: a headless run (queue.headless or %s=on lets it)", path, sid, queueEnv)
	return ""
}

// queueOffFor tells whether no queue drives sid now. A pause records it, and the runner that
// relaunches the session sets NOCTIS_QUEUE=off for the relaunch, so a script's claude -p that hit a
// limit is not handed the project's queue when noctis resumes it in a terminal.
func queueOffFor(cfg object, sid string) bool {
	switch {
	case queueEnvSetting() == "off":
		return true
	case queuesDriveRun(cfg, nil, sid):
		return false
	}
	return !startedInRun(nil, sid)
}

package main

import "strings"

// A session whose turn ends while a subagent or a workflow it started still runs in the background
// is not done: Claude Code wakes it with the result. The Stop input lists that work in
// background_tasks (only what is in flight: running or pending, and in the background), so the Stop
// hook lets such a stop go without a continuation. The queue hands out no next item, the stop does not
// count as one without progress, and the check waits for the result. A background shell or monitor
// does not hold the queue: a dev server or a watcher runs for as long as the session does.

// backgroundResultTypes are the kinds of background work that end with a result the session wakes for.
var backgroundResultTypes = map[string]bool{"subagent": true, "workflow": true}

// backgroundWork names the subagents and workflows the Stop input lists as in flight.
func backgroundWork(input object) []string {
	work := []string{}
	for _, raw := range getList(input, "background_tasks") {
		task := toObject(raw)
		kind := strings.ToLower(getString(task, "type"))
		status := strings.ToLower(getString(task, "status"))
		if !backgroundResultTypes[kind] || status != "" && status != "running" && status != "pending" {
			continue
		}
		name := kind
		if named := orDefault(getString(task, "agent_type"), getString(task, "name")); named != "" {
			name += " " + named
		}
		if description := strings.TrimSpace(getString(task, "description")); description != "" {
			name += " (" + truncateText(description, 80) + ")"
		}
		work = append(work, name)
	}
	return work
}

// waitsOnBackgroundWork lets the stop of a session go without a continuation while subagents or
// workflows it started run in the background.
func waitsOnBackgroundWork(input object, sid string, open int) bool {
	work := backgroundWork(input)
	if len(work) == 0 {
		return false
	}
	names := strings.Join(work, "; ")
	journal(sid, "Stop", "allow-stop", "background work in flight", object{"open": open, "work": truncateText(names, 300)})
	logInfo("stop of %s while %s runs in the background: stop allowed, the queue waits for its result (%d open)", sid, truncateText(names, 200), open)
	return true
}

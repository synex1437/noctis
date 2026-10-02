package main

import "fmt"

// An item of a queue file can name the model it is for: "(opus)", "(sonnet)", "(haiku)" or
// "(fable)" in its text. A session that runs on another model hands the item to noctis's worker agent
// on that model and checks its work, so a hard item gets a stronger model and an easy one a cheaper
// one without a relaunch. The session's own model stays as it is, and the PreToolUse hook
// still puts the fallback in place of a scoped model whose quota is out.

// queueModelTag is a model tag in the text of an item.
var queueModelTag = lazyRegexp(`(?i)\((opus|sonnet|haiku|fable)\)`)

// itemModel is the model the text of an item is tagged for, in lower case, or "".
func itemModel(text string) string {
	if match := queueModelTag.FindStringSubmatch(foldTag(text)); match != nil {
		return match[1]
	}
	return ""
}

// itemModelRule tells Claude, as a queue session starts, what a model tag means, or is "" when no
// item a session can take now has one or the host runs no subagents.
func itemModelRule(view queueView) string {
	if !currentHost().agents {
		return ""
	}
	for _, model := range view.itemModels {
		if model != "" {
			return " An item tagged (opus), (sonnet), (haiku) or (fable) is for that model: when this session runs on another one, hand it to " + handOff(workerText("")+" on that model", handOffBrief)
		}
	}
	return ""
}

// itemModelNote tells Claude to hand the next item to a subagent of the model it is tagged for when
// the session runs on another one. It returns that model and the note; the note is "" when the item
// has no tag, the session runs on that model or the host runs no subagents.
func itemModelNote(cfg, state object, sid string, models []string) (model, note string) {
	if len(models) == 0 || !currentHost().agents {
		return "", ""
	}
	if model = models[0]; model == "" {
		return "", ""
	}
	session := modelFamily(resolveSessionModel(cfg, state, readJSON(files.usage), sid))
	if session == model {
		return model, ""
	}
	runs := ""
	if session != "" {
		runs = " and this session runs on " + session
	}
	return model, fmt.Sprintf(" The next item is tagged (%s)%s: hand it to %s", model, runs, handOff(workerText(model), handOffBrief))
}

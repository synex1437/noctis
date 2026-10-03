package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The decisions Claude takes on its own while it drives a queue. The queue tells Claude to decide
// rather than ask, so what it decided reaches the user another way: Claude notes each decision the
// user may want to revisit with noctis queue note, and noctis queue status, the daily digest and the
// queue instructions of the next session give them back, so a fresh context keeps to them as well.

const (
	// queueNotesKept is how many decisions a queue file keeps.
	queueNotesKept = 50
	// queueNotesShown is how many of the last decisions queue status and a session's queue
	// instructions give.
	queueNotesShown = 5
	// queueNoteMaxChars bounds the text of one decision.
	queueNoteMaxChars = 300
)

func queueNotesFile() string {
	return filepath.Join(files.guardDir, "queue-notes.json")
}

// queueNote is one decision: when it was noted and what it says.
type queueNote struct {
	at   float64
	text string
}

// queueNotesOf is the decisions noted on the queue file at path, oldest first.
func queueNotesOf(notes object, path string) []queueNote {
	list := []queueNote{}
	for _, raw := range getList(getMap(notes, queueTrustKey(path)), "notes") {
		values, ok := raw.([]any)
		if !ok || len(values) != 2 {
			continue
		}
		at, known := toNumber(values[0])
		text, _ := values[1].(string)
		if known && text != "" {
			list = append(list, queueNote{at: at, text: text})
		}
	}
	return list
}

// runQueueNote is noctis queue note: it notes a decision taken on the queue file at target. Claude
// runs it; it changes nothing but the list the user reads.
func runQueueNote(target string) {
	text := ""
	if len(args.positional) > 2 {
		text = strings.TrimSpace(strings.Join(args.positional[2:], " "))
	}
	if text == "" {
		fmt.Fprintln(os.Stderr, T("queue.usage"))
		os.Exit(2)
	}
	text = truncateText(printableItem(text), queueNoteMaxChars)
	now := float64(nowSec())
	written := false
	withFileLock(queueNotesFile()+".lock", func() {
		notes := readJSON(queueNotesFile())
		if notes == nil {
			notes = object{}
		}
		// The notes of a file nothing was noted on for a month are dropped.
		for key, raw := range notes {
			if record, ok := raw.(object); !ok || now-numberOr(record, "at", 0) > queueTrustTTLSeconds {
				delete(notes, key)
			}
		}
		kept := []any{}
		for _, note := range queueNotesOf(notes, target) {
			kept = append(kept, []any{note.at, note.text})
		}
		count := queueNotesCount(notes, target, len(kept)) + 1
		kept = append(kept, []any{now, text})
		notes[queueTrustKey(target)] = object{"path": target, "at": now, "count": count, "notes": kept[max(0, len(kept)-queueNotesKept):]}
		if err := writeJSONAtomic(queueNotesFile(), notes); err != nil {
			fail("queue-notes.json not written: %v", err)
			return
		}
		written = true
	})
	if !written {
		os.Exit(1)
	}
	label := filepath.Base(target)
	journal("", "cli", "note", text, object{"file": label})
	fmt.Println(T("queue.noted", label))
}

// lastQueueNotes is the last decisions noted on the queue file at path, oldest first, and how many
// were noted in all.
func lastQueueNotes(path string) ([]queueNote, int) {
	stored := readJSON(queueNotesFile())
	notes := queueNotesOf(stored, path)
	return notes[max(0, len(notes)-queueNotesShown):], queueNotesCount(stored, path, len(notes))
}

func queueNotesCount(notes object, path string, kept int) int {
	return max(kept, int(numberOr(getMap(notes, queueTrustKey(path)), "count", 0)))
}

// printQueueNotes prints the last decisions noted on the queue file at target, for noctis queue
// status.
func printQueueNotes(target string) {
	shown, count := lastQueueNotes(target)
	if count == 0 {
		return
	}
	fmt.Println(T("queue.notes", count))
	for _, note := range shown {
		fmt.Println("  " + formatTime(note.at) + "  " + note.text)
	}
}

// queueNotesFacts is the last decisions noted on the queue file at path, for noctis queue status
// --json.
func queueNotesFacts(path string) []any {
	shown, _ := lastQueueNotes(path)
	facts := []any{}
	for _, note := range shown {
		facts = append(facts, object{"at": note.at, "text": note.text})
	}
	return facts
}

// queueNotesSince is the texts of the decisions noted on the queue file at path after since.
func queueNotesSince(notes object, path string, since float64) []string {
	texts := []string{}
	for _, note := range queueNotesOf(notes, path) {
		if note.at > since {
			texts = append(texts, note.text)
		}
	}
	return texts
}

// queueNotesRule tells a session that drives the queue file at path to note the decisions it takes
// on its own, and gives it the last ones, so a fresh context keeps to them.
func queueNotesRule(path string) string {
	rule := fmt.Sprintf(` When you decide something the user may want to revisit (an approach chosen over another, the reading you took of an item that reads more than one way, something left out or done differently), note it in one line: %s queue note "<the decision, and why>" --file %s.`, pluginName, shellQuote(path))
	shown, count := lastQueueNotes(path)
	if count == 0 {
		return rule
	}
	texts := []string{}
	for _, note := range shown {
		texts = append(texts, note.text)
	}
	return rule + fmt.Sprintf(" Decisions noted so far (the last %d of %d): %s. Keep to them unless one proves wrong, and note it when you change one.", len(shown), count, strings.Join(texts, " | "))
}

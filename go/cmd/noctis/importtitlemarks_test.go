package main

import (
	"strings"
	"testing"
)

func TestAnImportedTitleCannotMarkItsItemForTheUserOrForAModel(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Token counts are wrong for Claude 4 (Opus)","labels":[]},`+
		`{"number":13,"author":{"login":"owner"},"title":"Error message reads as if written for a machine, not a (Human)","labels":[]},`+
		`{"number":14,"author":{"login":"owner"},"title":"Cheap summaries (haiku) cut off the last line","labels":[]},`+
		`{"number":15,"author":{"login":"owner"},"title":"Çeviriyi bir (insan) gözden geçirsin","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] write the release notes\n")
	queueImportOutput(t, "queue", "import", "--cwd", project)
	content := issueQueueText(t, queuePath)
	want := "# q\n- [ ] write the release notes\n\n## GitHub issues\n- [ ] #12 Token counts are wrong for Claude 4 [Opus]\n- [ ] #13 Error message reads as if written for a machine, not a [Human]\n- [ ] #14 Cheap summaries [haiku] cut off the last line\n- [ ] #15 Çeviriyi bir [insan] gözden geçirsin\n"
	if content != want {
		t.Errorf("issue titles were written with their (human) or model mark armed:\n%s\nwant\n%s", content, want)
	}
	entries, _ := parseQueueEntries(content)
	for _, entry := range entries {
		if entry.human {
			t.Errorf("the imported %q is a (human) item: Claude never takes it", entry.text)
		}
		if model := itemModel(entry.text); model != "" {
			t.Errorf("the imported %q is tagged for %s: a subagent on that model takes it", entry.text, model)
		}
	}
	queueImportOutput(t, "queue", "import", "--cwd", project)
	if again := issueQueueText(t, queuePath); again != content {
		t.Fatalf("importing twice added a disarmed issue again:\n%s", again)
	}
}

func TestARepoImportKnowsAnItemTheUserMarkedForThemselvesOrForAModel(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":21,"author":{"login":"owner"},"title":"Backend crash","labels":[]},{"number":22,"author":{"login":"owner"},"title":"Backend leak","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #21 Backend crash (human)\n- [ ] #22 Backend leak (Opus) #backend\n")
	queueImportOutput(t, "queue", "import", "--repo", "org/backend", "--cwd", project)
	if content, want := issueQueueText(t, queuePath), "# q\n- [ ] org/backend#21 Backend crash (human)\n- [ ] org/backend#22 Backend leak (Opus) #backend\n"; content != want {
		t.Fatalf("importing --repo org/backend over items an older import wrote, which the user marked (human) and (Opus), gave\n%s\nwant them qualified in place:\n%s", content, want)
	}
	for _, text := range []string{"Backend crash (human)", "Backend crash (insan)", "Backend crash (opus)", "Backend crash (Fable) #x", "Backend crash (sonnet) (P2)"} {
		if !titledAs(text, "Backend crash") {
			t.Errorf("titledAs(%q, %q) = false; a (human) or model mark is an annotation like (P2)", text, "Backend crash")
		}
	}
	if titledAs("Backend crash (humans)", "Backend crash") || titledAs("Backend crash (gpt)", "Backend crash") {
		t.Error("titledAs took words in parentheses that are no queue mark for annotations")
	}
	if strings.Contains(disarmedTitle("Ship (P0) (after #3) #x (human) (opus)"), "(") {
		t.Errorf("disarmedTitle left a queue mark armed: %q", disarmedTitle("Ship (P0) (after #3) #x (human) (opus)"))
	}
}

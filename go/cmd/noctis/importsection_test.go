package main

import "testing"

func TestQueueImportAddsIssuesUnderTheGitHubIssuesHeadingTheFileHas(t *testing.T) {
	queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":51,"title":"Cache the index","labels":[],"author":{"login":"owner"}},{"number":52,"title":"Retry the upload","labels":[],"author":{"login":"owner"}}]`)
	added := "- [ ] #51 Cache the index\n- [ ] #52 Retry the upload\n"
	for _, file := range []struct{ name, original, want string }{
		{"a section above another one",
			"# q\n## GitHub issues\n- [ ] #50 Old one\n  - a note under it\n\n## Later\n- [ ] write the changelog\n",
			"# q\n## GitHub issues\n- [ ] #50 Old one\n  - a note under it\n" + added + "\n## Later\n- [ ] write the changelog\n"},
		{"an empty section",
			"## GitHub issues\n\n## Later\n- [ ] write the changelog\n",
			"## GitHub issues\n" + added + "\n## Later\n- [ ] write the changelog\n"},
		{"prose after the items",
			"## GitHub issues\r\n- [ ] #50 Old one\r\n\r\nNotes about the issues.\r\n## Later\r\n",
			"## GitHub issues\r\n- [ ] #50 Old one\r\n- [ ] #51 Cache the index\r\n- [ ] #52 Retry the upload\r\n\r\nNotes about the issues.\r\n## Later\r\n"},
		{"blank lines at the end",
			"## GitHub issues\n- [ ] #50 Old one\n\n\n",
			"## GitHub issues\n- [ ] #50 Old one\n" + added + "\n\n"},
		{"a subheading in the section",
			"## GitHub issues\n### Backend\n- [ ] #50 Old one\n# Archive\n- [x] shipped\n",
			"## GitHub issues\n### Backend\n- [ ] #50 Old one\n" + added + "# Archive\n- [x] shipped\n"},
		{"the heading as the last line",
			"# q\n## GitHub issues",
			"# q\n## GitHub issues\n" + added},
		{"the heading only in a code block",
			"# q\n```\n## GitHub issues\n```\n",
			"# q\n```\n## GitHub issues\n```\n\n## GitHub issues\n" + added},
	} {
		project := t.TempDir()
		queuePath := writeQueueFile(t, project, file.original)
		queueImportOutput(t, "queue", "import", "--cwd", project)
		if content := issueQueueText(t, queuePath); content != file.want {
			t.Errorf("%s: the import gave\n%q\nwant\n%q", file.name, content, file.want)
		}
	}
}

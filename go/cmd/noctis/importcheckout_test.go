package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitCheckout(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAnIssueImportedIntoAFileOutsideItsCheckoutIsClosedInItsRepository(t *testing.T) {
	cfg, notes := queueTrustSandbox(t, true)
	api := gitCheckout(t, t.TempDir())
	calls := fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[],"url":"https://github.com/acme/api/issues/12"}]`)
	queuePath := writeQueueFile(t, notes, "# Work\n- [ ] write the release notes\n")
	queueImportOutput(t, "queue", "import", "--cwd", api, "--file", queuePath)
	if content := issueQueueText(t, queuePath); !strings.Contains(content, "- [ ] acme/api#12 Backend crash\n") {
		t.Fatalf("an issue gh listed in the acme/api checkout was written into a file outside it without its repository:\n%s", content)
	}
	trustQueueFile(queuePath, true)
	tickIssueItem(t, queuePath, "Backend crash")
	stopHookOutput(t, stopInput("gh-elsewhere", notes), cfg)
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 --repo acme/api ") {
		t.Fatalf("ticking the issue gh listed in the acme/api checkout ran %q; want issue close 12 with --repo acme/api, not #12 of whatever repository the file's folder belongs to", closes)
	}
}

func TestTwoCheckoutsImportedIntoOneFileKeepTheirOwnIssues(t *testing.T) {
	_, work := queueTrustSandbox(t, false)
	api, web := gitCheckout(t, t.TempDir()), gitCheckout(t, t.TempDir())
	queuePath := writeQueueFile(t, work, "# Work\n")
	apiIssues := `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[],"url":"https://github.com/acme/api/issues/12"}]`
	fakeGhCLI(t, apiIssues)
	queueImportOutput(t, "queue", "import", "--cwd", api, "--file", queuePath)
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Dark mode toggle resets","labels":[],"url":"https://github.com/acme/web/issues/12"}]`)
	second := queueImportOutput(t, "queue", "import", "--cwd", web, "--file", queuePath)
	want := "# Work\n\n## GitHub issues\n- [ ] acme/api#12 Backend crash\n- [ ] acme/web#12 Dark mode toggle resets\n"
	if content := issueQueueText(t, queuePath); content != want {
		t.Fatalf("importing #12 of acme/api and then #12 of acme/web into one file gave\n%s\nwant\n%s", content, want)
	}
	if strings.TrimSpace(second) != T("queue.importDone", 1, 0, "TASKS.md") {
		t.Errorf("the second import said %q; acme/web#12 is not acme/api#12", second)
	}
	fakeGhCLI(t, apiIssues)
	queueImportOutput(t, "queue", "import", "--cwd", api, "--file", queuePath)
	if again := issueQueueText(t, queuePath); again != want {
		t.Fatalf("importing acme/api again added its issue a second time:\n%s", again)
	}
}

func TestAnIssueFromAnEnterpriseHostImportedOutsideItsCheckoutNamesItsHost(t *testing.T) {
	cfg, notes := queueTrustSandbox(t, true)
	tool := gitCheckout(t, t.TempDir())
	calls := fakeGhCLI(t, `[{"number":3,"author":{"login":"owner"},"title":"API crash","labels":[],"url":"https://ghe.example.com/acme/tool/issues/3"}]`)
	t.Setenv("GH_HOST", "ghe.example.com")
	queuePath := writeQueueFile(t, notes, "# Work\n- [ ] write the release notes\n")
	queueImportOutput(t, "queue", "import", "--cwd", tool, "--file", queuePath)
	if content := issueQueueText(t, queuePath); !strings.Contains(content, "- [ ] ghe.example.com/acme/tool#3 API crash\n") {
		t.Fatalf("an issue gh listed on ghe.example.com was written into a file outside its checkout without its host and repository:\n%s", content)
	}
	tickIssueItem(t, queuePath, "API crash")
	syncDoneIssues(cfg, queuePath, issueQueueText(t, queuePath), notes)
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 3 --repo ghe.example.com/acme/tool ") {
		t.Fatalf("ticking the issue from ghe.example.com ran %q; want issue close 3 with --repo ghe.example.com/acme/tool", closes)
	}
}

func TestAnIssueImportedFromASubfolderIntoItsCheckoutsOwnListKeepsTheBareNumber(t *testing.T) {
	queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[],"url":"https://github.com/acme/api/issues/12"}]`)
	checkout := gitCheckout(t, t.TempDir())
	src := filepath.Join(checkout, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	queueImportOutput(t, "queue", "import", "--cwd", src, "--file", filepath.Join("..", "TASKS.md"))
	if content := issueQueueText(t, filepath.Join(checkout, "TASKS.md")); content != "# TASKS\n\n## GitHub issues\n- [ ] #12 Backend crash\n" {
		t.Errorf("an issue imported from a subfolder into the checkout's own TASKS.md was written as\n%s\nwant the bare #12 the checkout's folder closes", content)
	}
}

func TestAnIssueImportedIntoACheckoutNestedInTheListedOneNamesItsRepository(t *testing.T) {
	queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[],"url":"https://github.com/acme/api/issues/12"}]`)
	checkout := gitCheckout(t, t.TempDir())
	library := gitCheckout(t, filepath.Join(checkout, "vendor", "library"))
	queueImportOutput(t, "queue", "import", "--cwd", checkout, "--file", filepath.Join(library, "TASKS.md"))
	if content := issueQueueText(t, filepath.Join(library, "TASKS.md")); !strings.Contains(content, "- [ ] acme/api#12 Backend crash\n") {
		t.Errorf("an issue imported into a checkout nested in the one gh listed it from was written as\n%s\nwant acme/api#12, since gh run there picks the nested checkout's repository", content)
	}
}

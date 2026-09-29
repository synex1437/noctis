package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestATagInAnyAlphabetCarriesADependency(t *testing.T) {
	file := filepath.Join(t.TempDir(), "TASKS.md")
	content := "# plan\n- [ ] ödeme sayfasını kur #ödeme\n- [ ] ödeme testlerini yaz (after #Ödeme)\n- [ ] настроить сервер #сервер\n- [ ] deploy (after #сервер, #ödeme)\n"
	view := queueSnapshotOf(file, content)
	if view.total != 4 || view.blocked != 2 || len(view.unmatched) != 0 {
		t.Fatalf("the tags #ödeme and #сервер did not carry their dependencies: total %d, blocked %d, unmatched %q", view.total, view.blocked, view.unmatched)
	}
	done := strings.Replace(content, "- [ ] ödeme sayfasını kur", "- [x] ödeme sayfasını kur", 1)
	if view := queueSnapshotOf(file, done); view.total != 3 || view.blocked != 1 {
		t.Fatalf("ticking the #ödeme item did not free the item that waits for it alone: total %d, blocked %d", view.total, view.blocked)
	}
	if matched, _ := matchQueueItems(content, "#ÖDEME"); len(matched) != 1 || matched[0].ordinal != 1 {
		t.Fatalf("#ÖDEME named %v, not the item tagged #ödeme", matched)
	}
	turkish := "- [ ] ışık ayarı #IŞIK\n- [ ] işlem kaydı #İşlem\n- [ ] ekranı aç (after #ışık, #işlem)\n"
	if view := queueSnapshotOf(file, turkish); view.blocked != 1 || len(view.unmatched) != 0 {
		t.Fatalf("#IŞIK and #İşlem did not answer (after #ışık, #işlem): blocked %d, unmatched %q", view.blocked, view.unmatched)
	}
	if matched, _ := matchQueueItems(turkish, "#ışık"); len(matched) != 1 || matched[0].ordinal != 1 {
		t.Fatalf("#ışık named %v, not the item tagged #IŞIK", matched)
	}
	for text, want := range map[string]string{"fix the C# build": "", "see issue #12": "", "use the colour #fff": "fff", "sayfa#başlık": "başlık"} {
		got := ""
		for tag := range newQueueEntry(1, text, false).tags {
			got = tag
		}
		if got != foldTag(want) {
			t.Errorf("%q has the tag %q, want %q", text, got, foldTag(want))
		}
	}
}

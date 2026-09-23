package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type routerCase struct {
	kind   string
	prompt string
}

var routerCorpus = []routerCase{
	{"research", "what is the latest news about european ai regulation"},
	{"research", "compare postgres and mysql for time series workloads, which is better"},
	{"research", "en iyi vektör veritabanı hangisi karşılaştırma yapar mısın"},
	{"research", "summarize the pros and cons of remote work for a 40 person company"},
	{"research", "look up the current pricing of aws lambda and azure functions"},
	{"research", "2025 yılında çıkan en yeni dil modellerini araştır"},
	{"research", "https://example.com/whitepaper.pdf bunu özetle ve ana fikirleri çıkar"},
	{"research", "find recent papers on retrieval augmented generation evaluation"},
	{"research", "what are the market trends for electric bikes in europe this year"},
	{"research", "investigate whether kubernetes is worth it for a three person startup"},
	{"research", "write a short marketing email announcing our new pricing tiers"},
	{"research", "draft a linkedin post about our funding round, friendly tone"},
	{"research", "şirket için bir gizlilik politikası taslağı hazırla"},
	{"research", "give me a comparison table of the main european cloud providers"},
	{"research", "what is the difference between iso 27001 and soc 2"},
	{"research", "araştır bakalım türkiye'de kvkk uyumu için neler gerekiyor"},
	{"research", "summarize this long article for me: https://news.example.org/a/b"},
	{"research", "which is better for a small team, linear or jira"},
	{"research", "latest reviews of the framework 13 laptop"},
	{"research", "en son çıkan telefonların fiyatlarını karşılaştır"},
	{"research", "write the copy for our landing page hero section"},
	{"research", "explain the history of the bauhaus movement in a few paragraphs"},
	{"research", "best practices for onboarding remote employees, with sources"},
	{"research", "incele ve özetle: yapay zeka düzenlemeleri avrupa'da nasıl ilerliyor"},
	{"research", "tell me about the pros and cons of four day work weeks"},
	{"code", "fix the auth.js bug where the token refresh loops forever"},
	{"code", "add a unit test for the parser and make it pass"},
	{"code", "refactor the user service into smaller modules"},
	{"code", "npm install keeps failing with a peer dependency error, help"},
	{"code", "auth.js dosyasındaki hatayı düzelt"},
	{"code", "git rebase sırasında conflict aldım ne yapmalıyım"},
	{"code", "write a python script that renames files by exif date"},
	{"code", "the build is broken on ci, look at the logs and fix it"},
	{"code", "implement pagination in the products endpoint"},
	{"code", "veritabanı sorgusu çok yavaş, optimize eder misin"},
	{"code", "add a dockerfile for this project"},
	{"code", "migrate the schema to add a nullable email column"},
	{"code", "why does my regex not match multiline strings"},
	{"code", "set up eslint and prettier with our style"},
	{"code", "bu fonksiyonu daha okunabilir hale getir"},
	{"code", "deploy the staging environment and check the health endpoint"},
	{"code", "convert this class component to hooks"},
	{"code", "testleri çalıştır ve kırmızı olanları düzelt"},
	{"code", "add error handling around the http client calls"},
	{"code", "kubernetes deployment yaml dosyasını yaz"},
	{"code", "the app crashes on startup with a null pointer, debug it"},
	{"code", "rename the variable userId to accountId everywhere"},
	{"code", "create a github action that runs the tests on push"},
	{"code", "projeye typescript desteği ekle"},
	{"code", "optimize the image loading on the home page"},
	{"code", "research the best approach to implement caching in our api"},
	{"code", "look into the best way to migrate our database to postgres"},
	{"code", "look at https://github.com/org/repo/pull/34 and tell me what you think"},
	{"code", "why does http://localhost:8080/api/users return 500"},
	{"code", "http://127.0.0.1:3000/login sayfası boş geliyor"},
	{"code", "https://github.com/org/repo/blob/main/src/auth.js neden böyle yazılmış"},
	{"code", "check https://github.com/foo/bar/actions/runs/123 and tell me why it failed"},
	{"code", "https://gitlab.com/team/app/-/merge_requests/9 üzerinde ne değişmiş"},
	{"code", "http://[::1]:3000/login sayfası boş geliyor"},
	{"code", "http://[::1]/login sayfası boş geliyor"},
	{"code", "the dashboard stays blank at http://localhost:5173, why"},
	{"code", "phpmyadmin never opens at http://localhost."},
	{"code", "http://127.0.0.1/admin boş sayfa veriyor"},
	{"code", "why is http://0.0.0.0/ blank in the browser"},
	{"code", "https://bitbucket.org/team/app/pull-requests/12 bu değişiklik ne yapıyor"},
	{"code", "https://bitbucket.org/team/app/src/main/app.py neden böyle yazılmış"},
	{"code", "https://bitbucket.org/team/app/pipelines/results/41 neden kırmızı"},
	{"code", "https://github.com/org/repo/blame/main/lib/auth.js bu satırı kim değiştirmiş"},
	{"code", "https://github.com/org/repo/runs/987 neden kırmızı"},
	{"code", "what is going on in https://github.com/org/repo/issues."},
	{"code", "www.github.com/org/repo/pull/34 neden kapatıldı"},
	{"code", "http://192.168.1.20:8080/ açılmıyor neden"},
	{"code", "http://host.docker.internal:3000 cevap vermiyor"},
	{"code", "http://my_api:8080/health 503 dönüyor neden"},
	{"code", "http://localhost:3000が真っ白になる"},
	{"code", "https://github.com/org/repo/pull/34の変更を説明して"},
	{"code", "http://[::]:8080 açılmıyor neden"},
	{"code", "http://[::]/ boş sayfa veriyor"},
	{"code", "http://[0:0:0:0:0:0:0:1]/login sayfası boş geliyor"},
	{"code", "http://[fe80::1]:3000/ neden bağlanmıyor"},
	{"code", "https://git.company.com/team/app/-/merge_requests/9 üzerinde ne değişmiş"},
	{"code", "https://gitlab.example.org/group/sub/app/-/jobs/456 neden kırmızı"},
	{"code", "https://octo.ghe.com/org/repo/pull/34 bu değişiklik ne yapıyor"},
	{"neutral", "devam et"},
	{"neutral", "continue"},
	{"neutral", "evet lütfen"},
	{"neutral", "/status"},
	{"neutral", "hmm bekle"},
}

const routerRecallFloor = 21

func TestTheRouterNeverSendsCodeWorkToTheLiteAgent(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	routed := 0
	research := 0
	for _, entry := range routerCorpus {
		result := classifyPrompt(cfg, nil, entry.prompt, "", nowSec())
		switch entry.kind {
		case "code":
			if result.route {
				t.Errorf("a coding prompt was handed to the lite agent (%s): %q", result.reason, entry.prompt)
			}
		case "neutral":
			if result.route {
				t.Errorf("a bare continuation was routed (%s): %q", result.reason, entry.prompt)
			}
		case "research":
			research++
			if result.route {
				routed++
			}
		}
	}
	if routed < routerRecallFloor {
		t.Errorf("the router caught %d of %d research prompts; the floor is %d", routed, research, routerRecallFloor)
	}
}

func TestACodeWordDoesNotPinResearchToTheExpensiveModelInAColdSession(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	for _, prompt := range []string{
		"look up the current pricing of aws lambda and azure functions",
		"en iyi vektör veritabanı hangisi karşılaştırma yapar mısın",
	} {
		if !classifyPrompt(cfg, nil, prompt, "", nowSec()).route {
			t.Errorf("a comparison question in a session that has touched no file stayed on the main model: %q", prompt)
		}
	}
}

func TestALinkToWebContentStillRoutesAsResearch(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	for _, prompt := range []string{
		"https://example.com/whitepaper.pdf bunu özetle ve ana fikirleri çıkar",
		"summarize this long article for me: https://news.example.org/a/b",
		"Şu yazıyı özetle https://example.com/a/b.html",
		"http://localhost.example.com/guide bunu özetle",
		"summarize this talk for me https://example.com?t=10:30",
		"https://github.com/awesome/treesitter-list bunu özetle",
		"https://github.com/org/repo/wiki/Treehouse-Guide bunu özetle",
		"https://github.com/features/actions neler sunuyor",
		"https://news.example.jpの記事を10:30までに要約して",
		"https://example.cn的文章在10:30前总结一下",
		"https://example.com\u3000の記事を10:30までに要約して",
		"https://example.com,10:30 bunu özetle lütfen",
		"「https://example.jp」の記事を10:30までに要約して",
		"https://github.com/org/repoのスター数/issues数の推移を調べて",
		"https://github.com/org/repo的star数/issues数趋势帮我查一下",
		"https://www.example.gov/web/guest/-/jobs/ bu ilanları özetle",
	} {
		if result := classifyPrompt(cfg, nil, prompt, "", nowSec()); !result.route || result.reason != "url" {
			t.Errorf("a link to web content no longer routes as research (%s): %q", result.reason, prompt)
		}
	}
	if result := classifyPrompt(cfg, nil, "lite: https://github.com/org/repo/pull/34 bunu özetle", "", nowSec()); !result.route || result.reason != "forced" {
		t.Errorf("lite: no longer forces a pull request link to the lite agent (%s)", result.reason)
	}
}

func TestTheWritingSignalNeverBeatsACodeWord(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	for _, prompt := range []string{
		"write a python script that renames files by exif date",
		"kubernetes deployment yaml dosyasını yaz",
		"rewrite this function so it reads better",
	} {
		if classifyPrompt(cfg, nil, prompt, "", nowSec()).route {
			t.Errorf("a writing word pulled a coding prompt away from the main model: %q", prompt)
		}
	}
}

func sessionTurn(kind string, at int64, blocks ...object) object {
	content := make([]any, 0, len(blocks))
	for _, block := range blocks {
		content = append(content, block)
	}
	return object{"type": kind, "timestamp": time.Unix(at, 0).UTC().Format("2006-01-02T15:04:05.000Z"), "message": object{"role": kind, "content": content}}
}

func sessionTranscript(t *testing.T, turns ...object) string {
	t.Helper()
	var body strings.Builder
	for _, turn := range turns {
		line, err := json.Marshal(turn)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func editTurns(at int64) []object {
	return []object{
		sessionTurn("assistant", at, object{"type": "tool_use", "id": "toolu_edit", "name": "Edit", "input": object{"file_path": "/work/app/worker.go", "old_string": "sync.Mutex", "new_string": "chan struct{}"}}),
		sessionTurn("user", at+1, object{"type": "tool_result", "tool_use_id": "toolu_edit", "content": "The file /work/app/worker.go has been updated."}),
	}
}

func longThinking(at int64) object {
	return sessionTurn("assistant", at, object{"type": "thinking", "thinking": strings.Repeat("weighing the mutex against a channel for the worker pool. ", 1300)})
}

var promptsThatNeedTheSessionsCode = []string{
	"compare these two functions and tell me which one is faster",
	"what is the best way to handle errors in this codebase",
	"investigate the memory leak in the worker",
}

func TestALargeTranscriptLineDoesNotMakeACodingSessionLookCold(t *testing.T) {
	now := nowSec()
	turns := []object{sessionTurn("user", now-600, object{"type": "text", "text": "the worker pool stalls under load, make it faster"})}
	turns = append(turns, editTurns(now-180)...)
	turns = append(turns, longThinking(now-120), sessionTurn("assistant", now-60, object{"type": "text", "text": "The pool now hands work over a channel."}))
	path := sessionTranscript(t, turns...)
	if info, err := os.Stat(path); err != nil || info.Size() <= codingTailBytes {
		t.Fatalf("the fixture must be larger than the %d byte tail (%v)", codingTailBytes, err)
	}
	if !recentCodingActivity(path, now) {
		t.Error("an Edit three minutes ago was missed because a long thinking block came after it")
	}
	cfg := object{"router": object{"enabled": true}}
	for _, prompt := range promptsThatNeedTheSessionsCode {
		if result := classifyPrompt(cfg, nil, prompt, path, now); result.route {
			t.Errorf("a question about the code being edited went to the lite agent (%s/%s): %q", result.reason, result.signal, prompt)
		}
	}
}

func TestCodingHiddenPastTheLargestTailStillCountsAsCoding(t *testing.T) {
	now := nowSec()
	turns := []object{sessionTurn("user", now-900, object{"type": "text", "text": "the worker pool stalls under load, make it faster"})}
	turns = append(turns, editTurns(now-800)...)
	for i := int64(0); i < 20; i++ {
		turns = append(turns, longThinking(now-700+i*30))
	}
	path := sessionTranscript(t, turns...)
	if info, err := os.Stat(path); err != nil || info.Size() <= codingTailMaxBytes {
		t.Fatalf("the fixture must be larger than the whole scan budget (%v)", err)
	}
	if !recentCodingActivity(path, now) {
		t.Error("a session whose last 45 minutes do not fit the scan was read as one that touched no file")
	}
}

func TestASessionThatStoppedCodingLongAgoStillRoutesResearch(t *testing.T) {
	now := nowSec()
	cfg := object{"router": object{"enabled": true}}
	turns := []object{sessionTurn("user", now-3300, object{"type": "text", "text": "the worker pool stalls under load, make it faster"})}
	turns = append(turns, editTurns(now-3000)...)
	turns = append(turns, longThinking(now-120), sessionTurn("assistant", now-60, object{"type": "text", "text": "Here is the history you asked about."}))
	stale := sessionTranscript(t, turns...)
	if recentCodingActivity(stale, now) {
		t.Error("an Edit fifty minutes ago, behind a long thinking block, still counted as coding")
	}
	if result := classifyPrompt(cfg, nil, "Investigate the history of the Ottoman navy", stale, now); !result.route || result.reason != "investigate" {
		t.Errorf("research in a session that stopped coding long ago stayed on the main model (%s)", result.reason)
	}
	var old []object
	for i := int64(0); i < 20; i++ {
		old = append(old, longThinking(now-7200+i*30))
	}
	if long := sessionTranscript(t, append(old, sessionTurn("assistant", now-3600, object{"type": "text", "text": "Done for today."}))...); recentCodingActivity(long, now) {
		t.Error("a long transcript that went quiet an hour ago counted as coding")
	}
	quiet := sessionTranscript(t, sessionTurn("user", now-30, object{"type": "text", "text": "merhaba"}))
	if recentCodingActivity(quiet, now) {
		t.Error("a transcript with no tool call at all counted as coding")
	}
}

func TestATranscriptThatCannotBeReadCountsAsCodingButAMissingOneDoesNot(t *testing.T) {
	sandboxFiles(t)
	dir := t.TempDir()
	if recentCodingActivity(filepath.Join(dir, "absent.jsonl"), nowSec()) {
		t.Error("a transcript that does not exist yet counted as coding")
	}
	if !recentCodingActivity(dir, nowSec()) {
		t.Error("a transcript path that cannot be read as a file counted as a session that touched no file")
	}
}

func TestATranscriptWithoutReadPermissionCountsAsCoding(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes do not stop this user from reading")
	}
	sandboxFiles(t)
	now := nowSec()
	path := sessionTranscript(t, sessionTurn("assistant", now-3600, object{"type": "text", "text": "Done for today."}))
	if recentCodingActivity(path, now) {
		t.Fatal("the readable fixture must read as a cold session")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if !recentCodingActivity(path, now) {
		t.Error("a transcript this user may not read counted as a session that touched no file")
	}
}

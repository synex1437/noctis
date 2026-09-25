package main

import (
	"encoding/json"
	"fmt"
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

var labelledRouterCorpus = []routerCase{
	{"code", "review the diff before I commit"},
	{"code", "review my changes in the auth module"},
	{"code", "can you review this PR"},
	{"code", "show the recent commits on this branch"},
	{"code", "compare these two functions and tell me which one is faster"},
	{"code", "what's the best way to split this file"},
	{"code", "benchmark the parser against the old one"},
	{"code", "investigate the memory leak in the worker"},
	{"code", "look into why the nightly job keeps failing"},
	{"code", "research why our test suite got slower this week"},
	{"code", "compare the output of the old and new serializer"},
	{"code", "which is better here, a map or a switch"},
	{"code", "summarize the latest changes in this repo"},
	{"code", "check the history of the config loader and tell me who changed the timeout"},
	{"code", "look at my last three commits and write a changelog entry"},
	{"code", "review all 40 endpoints for missing auth checks"},
	{"code", "find the source of this flaky test"},
	{"code", "why is my build so much slower than yesterday"},
	{"code", "compare the performance of the two caching strategies I wrote"},
	{"code", "look up where we set the retry limit"},
	{"code", "investigate why the login page is blank after the deploy"},
	{"code", "what changed between v1.2 and v1.3 of our api"},
	{"code", "list the latest failing tests and fix them"},
	{"code", "review the error handling in the payment service"},
	{"code", "is this the best approach for the retry logic"},
	{"code", "research how our session tokens are stored"},
	{"code", "do a code review of the new upload handler"},
	{"code", "compare my implementation with the one in main"},
	{"code", "look into the pricing page bug, the totals are wrong"},
	{"code", "investigate the recent spike in 500 errors from our api"},
	{"code", "bu diff'i commit etmeden önce incele"},
	{"code", "değişikliklerimi gözden geçir"},
	{"code", "son commitleri göster"},
	{"code", "bu iki fonksiyonu karşılaştır, hangisi daha hızlı"},
	{"code", "bu dosyayı bölmenin en iyi yolu ne"},
	{"code", "worker'daki bellek sızıntısını araştır"},
	{"code", "gece çalışan job neden sürekli patlıyor, incele"},
	{"code", "test suitimiz bu hafta neden yavaşladı araştır"},
	{"code", "bağımlılıkları güncelle"},
	{"code", "bu hata nereden kaynaklanıyor"},
	{"code", "kod incelemesi yap lütfen"},
	{"code", "repodaki son commitleri karşılaştır"},
	{"code", "iki branchi karşılaştır ve farkları anlat"},
	{"code", "bu değişiklikleri incele"},
	{"code", "auth modülündeki değişikliklerimi incele"},
	{"code", "eski ve yeni serializer'ın çıktısını karşılaştır"},
	{"code", "burada map mi switch mi daha iyi"},
	{"code", "retry mantığı için en iyi yaklaşım bu mu"},
	{"code", "login sayfası deploy'dan sonra neden boş, araştır"},
	{"code", "son üç commitime bakıp changelog yaz"},
	{"code", "tüm endpointlerde yetki kontrolü eksik mi incele"},
	{"code", "bu flaky testin kaynağını bul"},
	{"code", "build'im dünden beri neden bu kadar yavaş"},
	{"code", "yazdığım iki cache stratejisinin performansını karşılaştır"},
	{"code", "retry limitini nerede ayarlıyoruz bak"},
	{"code", "ödeme servisindeki hata yönetimini gözden geçir"},
	{"code", "session tokenlarını nasıl sakladığımızı araştır"},
	{"code", "yeni upload handler'ı için kod incelemesi yap"},
	{"code", "benim yazdığımla main'deki implementasyonu karşılaştır"},
	{"code", "api'mizdeki son 500 hatası artışını araştır"},

	{"research", "what is the latest news about the eu ai act"},
	{"research", "compare postgres and mysql for analytics workloads"},
	{"research", "what are the best note taking apps for students"},
	{"research", "find recent papers on speculative decoding"},
	{"research", "look up the current price of a raspberry pi 5"},
	{"research", "investigate the history of the ottoman navy"},
	{"research", "research the pros and cons of a four day work week"},
	{"research", "summarize the main arguments for and against nuclear power"},
	{"research", "which is better for a small team, notion or confluence"},
	{"research", "write a short blog post about our new office in berlin"},
	{"research", "draft an email to a customer apologizing for the outage"},
	{"research", "what are the market trends for electric scooters in 2026"},
	{"research", "compare the iphone 17 and pixel 10 cameras"},
	{"research", "what's the best database for our startup"},
	{"research", "best practices for onboarding remote employees"},
	{"research", "what does the research say about intermittent fasting"},
	{"research", "find sources on the economic impact of remote work"},
	{"research", "look into the best vpn providers for travelers"},
	{"research", "translate this paragraph into german: our store opens at nine"},
	{"research", "write a linkedin post announcing our series a"},
	{"research", "what is the difference between a roth ira and a traditional ira"},
	{"research", "explain the history of the bauhaus movement"},
	{"research", "summarize this article for me: https://example.com/post"},
	{"research", "latest gpu benchmarks for llm inference"},
	{"research", "compare aws lambda and google cloud run pricing"},
	{"research", "research the best cities in europe for a tech meetup"},
	{"research", "what are good sources to learn about stoicism"},
	{"research", "compose a newsletter intro about our autumn sale"},
	{"research", "investigate whether solar panels pay off in northern germany"},
	{"research", "best python web frameworks in 2026"},
	{"research", "yapay zeka düzenlemeleriyle ilgili son haberler neler"},
	{"research", "postgres ile mysql'i analitik iş yükleri için karşılaştır"},
	{"research", "öğrenciler için en iyi not alma uygulamaları hangileri"},
	{"research", "speculative decoding üzerine yeni makaleleri bul"},
	{"research", "raspberry pi 5'in güncel fiyatı ne kadar"},
	{"research", "osmanlı donanmasının tarihini araştır"},
	{"research", "dört günlük çalışma haftasının avantaj ve dezavantajlarını araştır"},
	{"research", "nükleer enerji lehine ve aleyhine argümanları özetle"},
	{"research", "küçük bir ekip için notion mu confluence mu daha iyi"},
	{"research", "berlin'deki yeni ofisimiz hakkında kısa bir blog yazısı yaz"},
	{"research", "kesinti için müşteriden özür dileyen bir e-posta taslağı hazırla"},
	{"research", "2026'da elektrikli scooter pazarındaki trendler neler"},
	{"research", "iphone 17 ile pixel 10 kameralarını karşılaştır"},
	{"research", "şirketimiz için en iyi veritabanı hangisi"},
	{"research", "uzaktan çalışanları işe alıştırmak için en iyi uygulamalar"},
	{"research", "aralıklı oruç hakkında araştırmalar ne diyor"},
	{"research", "uzaktan çalışmanın ekonomik etkisi üzerine kaynak bul"},
	{"research", "yurt dışı seyahat için en iyi vpn sağlayıcılarını araştır"},
	{"research", "şu paragrafı almancaya çevir: mağazamız dokuzda açılıyor"},
	{"research", "a serisi yatırımımızı duyuran bir linkedin gönderisi yaz"},
	{"research", "roth ira ile geleneksel ira arasındaki fark nedir"},
	{"research", "bauhaus akımının tarihini anlat"},
	{"research", "şu yazıyı özetle: https://example.com/post"},
	{"research", "uyku ve hafıza üzerine en yeni bulgular neler"},
	{"research", "aws lambda ile google cloud run fiyatlarını karşılaştır"},
	{"research", "teknoloji buluşması için avrupa'daki en iyi şehirleri araştır"},
	{"research", "stoacılığı öğrenmek için iyi kaynaklar neler"},
	{"research", "sonbahar indirimimiz için bir bülten girişi yaz"},
	{"research", "kuzey almanya'da güneş panelleri kendini amorti eder mi araştır"},
	{"research", "en popüler üç crm aracını karşılaştır"},
}

var ownWorkStillSentToTheLiteAgent = map[string]bool{
	"compare the output of the old and new serializer": true,
	"repodaki son commitleri karşılaştır":              true,
	"iki branchi karşılaştır ve farkları anlat":        true,
	"eski ve yeni serializer'ın çıktısını karşılaştır": true,
}

var ownWorkSentToTheLiteAgentInAColdSession = map[string]bool{
	"investigate the memory leak in the worker":                     true,
	"look into why the nightly job keeps failing":                   true,
	"compare the performance of the two caching strategies I wrote": true,
	"look up where we set the retry limit":                          true,
	"research how our session tokens are stored":                    true,
	"look into the pricing page bug, the totals are wrong":          true,
	"worker'daki bellek sızıntısını araştır":                        true,
	"gece çalışan job neden sürekli patlıyor, incele":               true,
	"bu değişiklikleri incele":                                      true,
	"auth modülündeki değişikliklerimi incele":                      true,
	"tüm endpointlerde yetki kontrolü eksik mi incele":              true,
	"yazdığım iki cache stratejisinin performansını karşılaştır":    true,
	"session tokenlarını nasıl sakladığımızı araştır":               true,
	"which is better here, a map or a switch":                       true,
	"is this the best approach for the retry logic":                 true,
	"retry mantığı için en iyi yaklaşım bu mu":                      true,
}

var researchKeptOnTheMainModel = map[string]bool{
	"investigate whether kubernetes is worth it for a three person startup": true,
	"what is the difference between iso 27001 and soc 2":                    true,
	"explain the history of the bauhaus movement in a few paragraphs":       true,
	"incele ve özetle: yapay zeka düzenlemeleri avrupa'da nasıl ilerliyor":  true,
	"summarize the main arguments for and against nuclear power":            true,
	"what is the difference between a roth ira and a traditional ira":       true,
	"explain the history of the bauhaus movement":                           true,
	"compare aws lambda and google cloud run pricing":                       true,
	"öğrenciler için en iyi not alma uygulamaları hangileri":                true,
	"nükleer enerji lehine ve aleyhine argümanları özetle":                  true,
	"küçük bir ekip için notion mu confluence mu daha iyi":                  true,
	"berlin'deki yeni ofisimiz hakkında kısa bir blog yazısı yaz":           true,
	"uzaktan çalışanları işe alıştırmak için en iyi uygulamalar":            true,
	"a serisi yatırımımızı duyuran bir linkedin gönderisi yaz":              true,
	"roth ira ile geleneksel ira arasındaki fark nedir":                     true,
	"bauhaus akımının tarihini anlat":                                       true,
	"aws lambda ile google cloud run fiyatlarını karşılaştır":               true,
}

var researchKeptOnTheMainModelInACodingSession = map[string]bool{
	"en iyi vektör veritabanı hangisi karşılaştırma yapar mısın":      true,
	"look up the current pricing of aws lambda and azure functions":   true,
	"araştır bakalım türkiye'de kvkk uyumu için neler gerekiyor":      true,
	"investigate the history of the ottoman navy":                     true,
	"what's the best database for our startup":                        true,
	"what does the research say about intermittent fasting":           true,
	"investigate whether solar panels pay off in northern germany":    true,
	"best python web frameworks in 2026":                              true,
	"osmanlı donanmasının tarihini araştır":                           true,
	"şirketimiz için en iyi veritabanı hangisi":                       true,
	"aralıklı oruç hakkında araştırmalar ne diyor":                    true,
	"kuzey almanya'da güneş panelleri kendini amorti eder mi araştır": true,
}

type routerTally struct {
	ownWork, ownWorkKept, research, researchRouted, routed int
}

func percentOf(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}

func (tally routerTally) String() string {
	return fmt.Sprintf("own work kept on the main model %d of %d (%.1f%%), research precision %d of %d routed (%.1f%%), research recall %d of %d (%.1f%%)",
		tally.ownWorkKept, tally.ownWork, percentOf(tally.ownWorkKept, tally.ownWork),
		tally.researchRouted, tally.routed, percentOf(tally.researchRouted, tally.routed),
		tally.researchRouted, tally.research, percentOf(tally.researchRouted, tally.research))
}

func unionOf(sets ...map[string]bool) map[string]bool {
	union := map[string]bool{}
	for _, set := range sets {
		for prompt := range set {
			union[prompt] = true
		}
	}
	return union
}

func TestTheRouterCorpusReportsOwnWorkRecallAndResearchPrecision(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	now := nowSec()
	corpus := append(append([]routerCase{}, routerCorpus...), labelledRouterCorpus...)
	kinds := map[string]string{}
	for _, entry := range corpus {
		if kinds[entry.prompt] != "" {
			t.Errorf("%q is in the router corpus twice", entry.prompt)
		}
		kinds[entry.prompt] = entry.kind
	}
	for kind, sets := range map[string][]map[string]bool{
		"code":     {ownWorkStillSentToTheLiteAgent, ownWorkSentToTheLiteAgentInAColdSession},
		"research": {researchKeptOnTheMainModel, researchKeptOnTheMainModelInACodingSession},
	} {
		for prompt := range unionOf(sets...) {
			if kinds[prompt] != kind {
				t.Errorf("%q is listed as a known %s miss but is not a %s prompt of the corpus", prompt, kind, kind)
			}
		}
	}
	if len(labelledRouterCorpus) != 120 {
		t.Errorf("%d labelled prompts, want 120", len(labelledRouterCorpus))
	}
	sessions := []struct {
		name          string
		transcript    string
		ownWorkRouted map[string]bool
		researchKept  map[string]bool
	}{
		{"in a session that has touched no file", "", unionOf(ownWorkStillSentToTheLiteAgent, ownWorkSentToTheLiteAgentInAColdSession), researchKeptOnTheMainModel},
		{"in a coding session", sessionTranscript(t, editTurns(now-120)...), ownWorkStillSentToTheLiteAgent, unionOf(researchKeptOnTheMainModel, researchKeptOnTheMainModelInACodingSession)},
	}
	for _, session := range sessions {
		var tally routerTally
		for _, entry := range corpus {
			result := classifyPrompt(cfg, nil, entry.prompt, session.transcript, now)
			if result.route {
				tally.routed++
			}
			switch entry.kind {
			case "code":
				tally.ownWork++
				if !result.route {
					tally.ownWorkKept++
				} else if !session.ownWorkRouted[entry.prompt] {
					t.Errorf("%s the user's own work went to the lite agent (%s/%s): %q", session.name, result.reason, result.signal, entry.prompt)
				}
			case "research":
				tally.research++
				if result.route {
					tally.researchRouted++
				} else if !session.researchKept[entry.prompt] {
					t.Errorf("%s research stayed on the main model (%s): %q", session.name, result.reason, entry.prompt)
				}
			case "neutral":
				if result.route {
					t.Errorf("%s a prompt that asks for no research was routed (%s): %q", session.name, result.reason, entry.prompt)
				}
			}
		}
		t.Logf("router over %d prompts %s: %s", len(corpus), session.name, tally)
	}
}

func TestAReviewOfTheUsersOwnEndpointsKeepsItsFanOutAdviceInsteadOfTheLiteRoute(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	workflow := cloneObject(section(cfg, "workflow"))
	workflow["suggest"] = true
	cfg["workflow"] = workflow
	var output string
	func() {
		defer func(previous bool) { emitted = previous }(emitted)
		output = capturedStdout(t, func() {
			onUserPromptSubmit(object{"hook_event_name": "UserPromptSubmit", "session_id": "fan1", "cwd": project, "prompt": "review all 40 endpoints for missing auth checks"}, cfg)
		})
	}()
	if strings.Contains(output, "Non-code research") || getMap(readState(), "routes")["fan1"] != nil {
		t.Fatalf("a review of the user's own endpoints was routed to the lite agent: %s", output)
	}
	if !strings.Contains(output, "fan-out task") {
		t.Fatalf("a review of all 40 endpoints lost its fan-out advice: %s", output)
	}
}

var ownWorkNamingASourcePriceTrendOrArticle = []string{
	"find the source of the memory leak",
	"what's the source of this flaky behaviour in the worker",
	"what is the source of this error",
	"show me the source code of the upload handler",
	"trace the source of the null value in the report",
	"what's the source of truth for the user settings",
	"the source map is missing in production",
	"why is the price wrong on the checkout page",
	"where does the price come from in the cart",
	"the price field is empty after checkout",
	"add a price column to the orders table",
	"round the price to two decimals",
	"the trend line chart is broken on the dashboard",
	"the trend arrow points the wrong way on the dashboard",
	"find where the trend value gets rounded",
	"the article page shows a 404",
	"the article list is empty on the blog page",
	"why does the article preview render twice",
}

var researchNamingSourcesPricesTrendsOrArticles = []string{
	"look up the current price of a raspberry pi 5",
	"what's the price of the new macbook air",
	"price of gold today",
	"compare the prices of the top three vpn providers",
	"find sources on the economic impact of remote work",
	"best sources on the history of rome",
	"what are the trends in remote work",
	"yapay zeka trendleri neler",
	"find articles about the eu ai act",
}

func TestTheUsersOwnSourcePriceTrendOrArticleStaysOnTheMainModel(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	now := nowSec()
	coding := sessionTranscript(t, editTurns(now-120)...)
	for _, prompt := range ownWorkNamingASourcePriceTrendOrArticle {
		for _, session := range []struct{ name, transcript string }{{"in a session that has touched no file", ""}, {"in a coding session", coding}} {
			if result := classifyPrompt(cfg, nil, prompt, session.transcript, now); result.route {
				t.Errorf("%s the user's own work went to the lite agent (%s/%s): %q", session.name, result.reason, result.signal, prompt)
			}
		}
	}
}

func TestResearchNamingSourcesPricesTrendsOrArticlesStillRoutes(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	now := nowSec()
	coding := sessionTranscript(t, editTurns(now-120)...)
	for _, prompt := range researchNamingSourcesPricesTrendsOrArticles {
		for _, session := range []struct{ name, transcript string }{{"in a session that has touched no file", ""}, {"in a coding session", coding}} {
			if result := classifyPrompt(cfg, nil, prompt, session.transcript, now); !result.route {
				t.Errorf("%s research stayed on the main model (%s): %q", session.name, result.reason, prompt)
			}
		}
	}
}

var ownWorkAskedWithAWebWord = []string{
	"What's the best way to structure this?",
	"Bu modül için en iyi yapı hangisi?",
	"compare this module with the old one",
	"which is the best place for these two functions",
	"is my component the best fit for the new layout",
	"compare parseConfig and loadConfig",
	"which is better, retry_count or max_retries",
	"what's the best way to call fetchUser() here",
	"bu fonksiyonu yazmanın en iyi yolu ne",
	"şu iki dosyayı karşılaştır",
	"bizim api için en iyi hata biçimi hangisi",
	"projemizin klasör yapısı için en iyi düzen hangisi",
	"modülümüz için en iyi yapı hangisi",
}

var researchAskedWithAWebWord = []string{
	"find the best library for parsing yaml",
	"compare the prices of the top three CI services",
	"what are the best code editors this year",
	"what's the best database for our startup",
	"compare the iPhone 17 and Pixel 10 cameras for our trip",
	"which is better for students, macOS or Windows",
	"latest news from @the_verge about foldable phones",
	"bu yıl çıkan en iyi filmler hangileri",
	"bu yıl yapılan en iyi filmler hangileri",
	"şirketimiz için en iyi muhasebe programı hangisi",
	"en iyi ev yapımı pizza tarifi",
	"find the latest papers on arXiv about speculative decoding",
	"what are the latest changes to useState in React 19",
	"what's the best theme for my code editor",
	"what are this year's best code editors",
	"is this promo code the best deal",
	"best pizza near my zip code",
	"benim kod editörüm için en iyi tema hangisi",
	"bu indirim kodu en iyi fiyat mı",
	"bu modüler kanepenin en iyi fiyatı nerede",
	"bu projektör için en iyi fiyat nerede",
	"bu fonksiyonel antrenman için en iyi ayakkabı hangisi",
}

var comparisonsAboutThis = []string{
	"What's the best structure for this?",
	"What's the best way to handle this?",
	"Is this the best way to do it?",
	"compare these two implementations",
	"which is better for this, a mutex or a channel",
	"Bunun için en iyi yapı hangisi?",
	"bu iki yaklaşımı karşılaştır",
	"bu ikisini karşılaştır, hangisi daha hızlı",
}

var researchAboutThisWithANewsOrPriceWord = []string{
	"what are the latest reviews of this laptop",
	"compare the prices of this phone and the Pixel 10",
	"bu telefonla ilgili en son haberler neler",
}

func TestAWebWordLeavesAQuestionAboutTheUsersOwnCodeOnTheMainModel(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	now := nowSec()
	coding := sessionTranscript(t, editTurns(now-120)...)
	for _, prompt := range ownWorkAskedWithAWebWord {
		for _, session := range []struct{ name, transcript string }{{"in a session that has touched no file", ""}, {"in a coding session", coding}} {
			if result := classifyPrompt(cfg, nil, prompt, session.transcript, now); result.route {
				t.Errorf("%s the user's own work went to the lite agent (%s/%s): %q", session.name, result.reason, result.signal, prompt)
			}
		}
	}
}

func TestAWebWordStillRoutesResearchThatPointsAtNoCodeOfTheUser(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	for _, prompt := range researchAskedWithAWebWord {
		if result := classifyPrompt(cfg, nil, prompt, "", nowSec()); !result.route || result.reason != "web-words" {
			t.Errorf("in a session that has touched no file research stayed on the main model (%s): %q", result.reason, prompt)
		}
	}
}

func TestAComparisonAboutThisInACodingSessionStaysOnTheMainModel(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	now := nowSec()
	coding := sessionTranscript(t, editTurns(now-120)...)
	for _, prompt := range comparisonsAboutThis {
		if result := classifyPrompt(cfg, nil, prompt, coding, now); result.route {
			t.Errorf("in a coding session the user's own work went to the lite agent (%s/%s): %q", result.reason, result.signal, prompt)
		}
	}
}

func TestResearchAboutThisWithANewsOrPriceWordStillRoutesInACodingSession(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	now := nowSec()
	coding := sessionTranscript(t, editTurns(now-120)...)
	for _, prompt := range researchAboutThisWithANewsOrPriceWord {
		if result := classifyPrompt(cfg, nil, prompt, coding, now); !result.route {
			t.Errorf("in a coding session research with a news, review or price word stayed on the main model (%s): %q", result.reason, prompt)
		}
	}
}

func TestASingularIncelemeRoutesOnlyAsAnAskToInvestigate(t *testing.T) {
	cfg := object{"router": object{"enabled": true}}
	now := nowSec()
	command := sessionTurn("assistant", now-120, object{"type": "tool_use", "id": "toolu_bash", "name": "Bash", "input": object{"command": "go test ./..."}})
	sessions := []struct {
		name       string
		transcript string
		routed     bool
	}{
		{"in a session with no transcript", "", true},
		{"in a session whose last edit was 46 minutes ago", sessionTranscript(t, editTurns(now-46*60)...), true},
		{"in a session whose last edit was 44 minutes ago", sessionTranscript(t, editTurns(now-44*60)...), false},
		{"in a session that edited a file 2 minutes ago", sessionTranscript(t, editTurns(now-120)...), false},
		{"in a session that ran a command 2 minutes ago", sessionTranscript(t, command), false},
	}
	for _, prompt := range []string{"iPhone 17 incelemesi", "osmanlı donanmasının tarihini araştır"} {
		for _, session := range sessions {
			result := classifyPrompt(cfg, nil, prompt, session.transcript, now)
			if session.routed && (!result.route || result.reason != "investigate") {
				t.Errorf("%s %q was not routed as an ask to investigate (%s)", session.name, prompt, result.reason)
			}
			if !session.routed && (result.route || result.reason != "coding-session") {
				t.Errorf("%s %q did not stay on the main model as an ask to investigate (%s)", session.name, prompt, result.reason)
			}
		}
	}
	for _, session := range sessions {
		if result := classifyPrompt(cfg, nil, "kod incelemesi yap lütfen", session.transcript, now); result.route || result.reason != "code-signal" {
			t.Errorf("%s kod incelemesi was not kept on the main model by its code word (%s)", session.name, result.reason)
		}
	}
}

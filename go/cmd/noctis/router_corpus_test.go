package main

import "testing"

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

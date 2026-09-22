<p align="center">
  <img src="docs/banner.svg" alt="Noctis — geceleri de çalışan yapay zekâ kodlama ajanları" width="100%">
</p>

<p align="center">
  <a href="https://github.com/synex1437/noctis/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/synex1437/noctis/actions/workflows/ci.yml/badge.svg"></a>
  <img alt="Windows, macOS, Linux" src="https://img.shields.io/badge/Windows%20%C2%B7%20macOS%20%C2%B7%20Linux-tek%20binary-2a78d6">
  <img alt="Claude Code, Codex CLI, Antigravity CLI, Droid, Copilot CLI" src="https://img.shields.io/badge/Claude%20Code%20%C2%B7%20Codex%20%C2%B7%20Antigravity%20%C2%B7%20Droid%20%C2%B7%20Copilot-5%20ara%C3%A7-35b26e">
  <a href="LICENSE"><img alt="MIT lisansı" src="https://img.shields.io/badge/lisans-MIT-lightgrey"></a>
  <a href="README.md"><img alt="English README" src="https://img.shields.io/badge/README-English-2a78d6"></a>
</p>

Kırk iş kuyruğa koyup yattınız; sabah oturum 01:40'ta limit duvarına çarpıp ölmüş — ya da üçüncü işte *"şimdi diğer işe geçiyorum"* deyip durmuş.

**Noctis**, uzun Claude Code işlerini siz başında olmadan yürütür. 5 saatlik ya da haftalık limite çarpmadan *hemen önce* duraklatır, sıfırlanmayı bekler ve aynı oturumu sürdürür. `TASKS.md` listesini sormadan madde madde yapar, araştırmayı ucuz bir modele yollayıp pahalı modelin kotasını korur. Kendi kararları için hiçbir modele soru sormaz, token harcamaz. Gün içinde yazmanız gereken hiçbir şey yoktur.

**Pro veya Max** aboneleri için; 5.2'den beri OpenAI Codex CLI, Antigravity CLI, Factory Droid ve GitHub Copilot CLI içinde de çalışır.

**Kurulum** — Claude Code içinde dört satır, yaklaşık bir dakika. Üçüncüsü tek bir soru sorar: hangi model hangi işi yapsın.

```
/plugin marketplace add synex1437/noctis
/plugin install noctis
/noctis:setup
/reload-plugins
```

**Gereksinimler:** Claude Code 2.1.251 veya üstü, Pro ya da Max hesabı, Windows / macOS / Linux. Node, Git Bash, derleyici gerekmez.

Setup `~/.claude/settings.json` dosyasını düzenler: durum çubuğu, varsayılan model ve **`permissions.defaultMode` → `auto`, yani Claude dosyaları düzenler ve komutları sormadan çalıştırır**. Gece çalışmasını sağlayan budur; `--permissions keep` ile vazgeçilir. Her anahtar ve geri alma yolu [aşağıda listeli](#makinenizde-neyi-değiştirir--nasıl-geri-alınır).

Planınızda Fable var mı bilmiyor musunuz? Setup sorduğunda `balanced` ya da `economy` seçin — `noctis` Max varsayar. API anahtarıyla açılan oturumlarda kullanım penceresi yoktur, limit koruması bir bilgi mesajından sonra boşta kalır; kuyruk modu ve yönlendirici yine çalışır. Başka bir araç mı? [Diğer yapay zekâ kodlama araçları](#diğer-yapay-zekâ-kodlama-araçları).

<p align="center"><img src="docs/demo.svg" alt="Eklentiyle bir gece: 5 saatlik limitte duraklar, sıfırlanmadan sonra devam eder, araştırmayı ucuz modele yollar, kapsamlı kota bitince model değiştirir, yarım kalan workflow'u yeniden başlatır, sabaha kuyruğu bitirir" width="100%"></p>

## Makinenizde neyi değiştirir — nasıl geri alınır

<details>
<summary>Setup'ın yazdığı her ayar anahtarı, ağda neyle konuştuğu ve her birinin geri alma yolu.</summary>

Setup önce `settings.json.bak-<saat>` yedeğini alır, sonra tam olarak şunlara dokunur:

| Nerede | Ne | Geri alma |
|---|---|---|
| `settings.json` → `statusLine` | Durum çubuğunu eklentinin binary'sine yöneltir — Claude Code kullanımı böyle bildirir. Zaten kullandığınız durum çubuğu arkasında çalışmaya devam eder. | `noctis install --uninstall` |
| `settings.json` → `permissions.defaultMode` | `auto` yapılır (eski Claude Code'da `acceptEdits`). **Claude dosyaları düzenler ve komutları size sormadan çalıştırır** — siz uyurken çalışmasını sağlayan budur. Asla `bypassPermissions` kullanılmaz: gözetimsiz yeniden başlatma, config istese bile o modu reddeder. | Setup'ta `--permissions keep`; kaldırma tam olarak setup'ın yazdığını siler |
| `settings.json` → `model`, `env.CLAUDE_CODE_EFFORT_LEVEL` | Varsayılan model = profilinizin *kod* modeli (varsayılanda **Fable 5.1 · max**). Varsayılanınız zaten Fable ya da Opus ise atlanır. | Setup'ta `--no-model`; kaldırma eski değeri geri yazar |
| `~/.claude/noctis/` | Kendi `config.json`'ı, kullanım anlık görüntüleri, checkpoint'ler, günlükler | Klasörü silin |
| Zamanlanmış görev, yalnızca bekleyen bir devam varken | Task Scheduler `Noctis-…` (PC'yi uyandırabilir), launchd `com.synex.noctis.…`, systemd `noctis-…` | `noctis cancel`; `"alarm": {"wakePc": false}` uyandırmayı kapatır |
| Pazar yeri otomatik güncellemesi | Setup `claude plugin marketplace update noctis --auto-update` çalıştırır, yeni sürümler kendiliğinden gelir. | Setup'ta `--updates keep`, ya da `/plugin` → Marketplaces |

**Ağ.** İki sunucu. `api.anthropic.com` — Claude uygulamasının kendi kullandığı kullanım uç noktası, Claude Code'un zaten sakladığı giriş token'ıyla; macOS'ta token Keychain'de durur, bir kez "erişime izin ver" sorusu beklersiniz. 10 dakikada bir, duraklamaya iki puan kala 15 saniyede bire iner. Bir de günde bir kez GitHub'daki ham `plugin.json`, yeni sürüm var mı diye (`update.check: false` kapatır). Ayarladıysanız webhook adresiniz. Telemetri yok.

**Ücretli kullanım kredisi asla harcanmaz.** Eşikleriniz yanlış ayarlanmış olsa da, `/noctis:pause` açıkken bile iş bir pencerenin %100'ünde durur; çünkü o noktadan sonra taşan kullanımı hesap öder. Dinamik bir workflow aynı anda çok sayıda ajan açar ve son puanları iki kontrol arasında yakabilir, bu yüzden pencerede en az 25 puan boşluk yoksa reddedilir. Taşmayı siz *istiyorsanız* `"credits": {"allowPaid": true}`; `ceiling` ve `fanOutHeadroom` gerisini ayarlar. Noctis'in yapamayacağı şey, hesabın kendi otomatik kredi yüklemesini kapatmaktır — onu Anthropic faturalandırma ayarlarınızdan kapatın.

**Kuyruk modu listeyi sizin yerinize yürütür — ama izin verdikten sonra.** Proje klasöründe açık `- [ ]` maddeleri olan bir `TASKS.md` (ya da `tasks.md`, `.claude/TASKS.md`, `docs/TASKS.md`) varsa, oradaki ilk oturum dosyanın varlığını söyler ve henüz **hiçbir şeyi yönetmediğini** belirtir:

```text
☰ Bu klasördeki TASKS.md 12 açık madde içeriyor. Bu oturumu yönetmiyor: bir kontrol listesi
  depoyla birlikte gelebilir ve yönetmek, durup sormadan listeyi bitirmek demektir.
  Yönetmesine izin vermek için: noctis queue trust
```

`noctis queue trust` o dosya için açar, `untrust` kapatır, `status` durumu söyler. Noctis'in sizin isteğinizden yazdığı listeye izin gerekmez — zaten siz istediniz. Kapı şunun için var: bir `TASKS.md` `git clone` ile gelebilir ve "bu listeyi sormadan yap" indirilen bir dosyanın söyleyebileceği bir şey değildir. Eski her zaman açık davranış: `"queue": {"requireTrust": false}`. Hiç istemiyorsanız: `"queue": {"files": []}`.

**Kapatmak.** Bir süreliğine: `/noctis:pause 120` (dakika — Claude çalışmaya devam eder, koruma uyur), `:resume` geri açar. Yalnızca izlesin: `config.json` içinde `"mode": "observe"`. Tamamen:

```
noctis cancel                 # bekleyen devamları ve zamanlanmış görevlerini düşür
noctis install --uninstall    # settings.json'ı geri yükle — ÖNCE bunu çalıştırın, çünkü eklentiyi
                              # kaldırmak düzenlemeleri geri alacak binary'yi de siler
/plugin uninstall noctis      # Claude Code içinde
```

Klon kurulumunda ortadaki satır yerine `scripts/install.sh --uninstall` ya da `scripts\install.ps1 -Uninstall`.

Eklenti çoktan gittiyse ve ayarlar durduysa elle geri alın: `~/.claude/settings.json` içinden `permissions.defaultMode`, `model`, `env.CLAUDE_CODE_EFFORT_LEVEL` ve `statusLine` bloğunu silin ya da `settings.json.bak-<saat>` kopyasını geri yükleyin. Önemli olan `permissions.defaultMode`: geride kalırsa sonraki her oturum dosyaları düzenleyip komutları sormadan çalıştırmaya devam eder.

</details>

## İlk beş dakika

1. `/reload-plugins` sonrası pencerenin altına bakın: `∞ 5sa %41→14:35 · Hf %23▲→Pzt 21.09 · Fable 5.1/max · ctx %37` — iki kullanım penceresi, her birinin sıfırlanma zamanı ve güncel model. *limit verisi bekleniyor* yazıyorsa bir mesaj gönderin; ilk yanıttan sonra dolar.
2. `/noctis:status` yazın: kullanım, duraklama noktaları, hangi modelin hangi işi yaptığı ve eklentinin son kararları.
3. Aşağıdaki üç satırlık `TASKS.md`'yi yazın ve Claude'a "TASKS.md'yi bitir" deyin. Maddeleri kendi kendine işaretlemesini izleyin.
4. Limitte yapmanız gereken bir şey yok. Kısa sıfırlanma tur içinde beklenir, bağlam korunur. Uzun olan kaydedilir ve aynı oturumun `claude --resume`'u olarak zamanlanır — Windows'ta yeni bir terminal penceresinde, diğerlerinde arka planda, çıktısı `resume-output.log` içinde. Windows ve Linux makineyi uyandırmak ister (Linux'ta `CAP_WAKE_ALARM` gerekir); launchd Mac'i uyandıramaz, geceliğine açık bırakın.
5. Güvenmeye hazır değil misiniz? `~/.claude/noctis/config.json` içinde `"mode": "observe"` her kararı yazar, hiçbirini uygulamaz.

## Kuyruk dosyası

<details>
<summary>`TASKS.md` biçimi; öncelikler, etiketler, bağımlılıklar ve GitHub issue'ları.</summary>

`claude`'u başlattığınız klasöre, her satıra bir iş:

```markdown
- [ ] kayıt formuna girdi doğrulaması ekle
- [ ] ödeme modülü için testleri yaz
- [ ] yeni CLI bayrakları için README'yi güncelle
```

Biçimin tamamı bu. Claude ilk açık maddeyi alır, bitince `- [x]` işaretler ve sormadan diğerine geçer; sonunda `✔ Kuyruk bitti` deyip durur. Dağınık listeler kabul edilir (`-[ ]`, `* [ ]`, `1. [ ]`, `[]`, `TODO:`), birkaç satıra yayılan madde tek maddedir, `~~üstü çizili~~`, `(bitti)` ve `✓` bitmiş sayılır.

Öncelik ve bağımlılık:

```markdown
- [ ] (P0) giriş yönlendirmesini düzelt #auth
- [ ] (P1) kullanıcıları taşı (after #auth, #db)
- [ ] dağıt (after 2)
```

`(P0)`–`(P9)` sırayı belirler (varsayılan P5, küçük önce), `#ad` maddeyi etiketler. `(after #etiket)` ya da `(after 3)` referans verilen maddeler işaretlenene kadar bekletir; `(after 2)` dosyadaki 2. maddedir. Stop hook'u Claude'a uygun maddelerin en iyisini verir, kaç tanesinin beklediğini söyler ve hepsi tıkalıysa ya da kuyruk bittiyse temizce durur. Hiçbir şeye karşılık gelmeyen referans yok sayılır, yazım hatası geceyi kilitlemez.

`noctis queue import` açık GitHub issue'larını `gh` üzerinden `- [ ] (P1) #123 Başlık` olarak ekler — öncelik `P0`–`P9` ya da `priority: high` etiketinden, tekrar çalıştırmak güvenli — ve `queue.github.closeOnDone` maddesi işaretlenince issue'yu kapatır.

</details>

## Siz yokken ne yapar

<details>
<summary>Durum durum: limitler, erken sıfırlanmalar, kuyruk duruşları, kota geçişleri ve tek başına çözdüğü arızalar.</summary>

| Durum | Ne olur |
|---|---|
| Kullanım limite yaklaşır (varsayılan %92 / %89) | Tur duvara çarpmadan **önce** duraklar; son istek, dokunulan dosyalar, `git status`, todo'lar ve sıradaki kuyruk maddeleri checkpoint'e yazılır. Kısa sıfırlanma yerinde beklenir; uzun olan kaydedilir ve zamanlanmış bir görevle otomatik devam eder — Task Scheduler (PC'yi uyandırabilir), launchd ya da systemd — ayrıca masaüstü bildirimi ve ayarlıysa webhook. |
| Sıfırlanma duyurulandan erken gelir | Bekleme dakikalar içinde fark eder: 5 dakikada bir yeniden bakar, başka pencereden gelen taze veriyi de sayar, kullanım API'si olmayan araçlarda 10/20/30/45/60 dakikalık merdivenle dener. `⚡ 5sa limiti planlanandan önce sıfırlandı`. |
| Claude "şimdi diğer işe geçiyorum" deyip durur | Kuyruk modunda Stop hook'u sıradaki işaretsiz maddeyi verir, onay sormaz, kuyruk bitince temizce durur. |
| Birkaç iş içeren uzun bir istem yapıştırırsınız | Noctis listeyi kendisi yazar, kendi klasöründe tutar ve yürütür. `☰ 5 adımlı iş algılandı`. Kısa istemler ve tek işler rahat bırakılır (`queue.auto: false`). |
| Pahalı modelin haftalık kotası biter | Varsayılan model ve effort yedeğe geçer, oturum checkpoint'lenip onunla yeniden başlar; kota sıfırlanınca ikisi de geri alınır. |
| İstem kod değil, araştırma ya da yazı | Ucuz bir alt-ajana gider. Gürültülü test çıktısı en ucuz modelle özetlenir, dosya arama birincil modelin kotasına hiç dokunmaz. |
| 429 hatası turu yine de öldürür | Oturum sıfırlanmada **yerinde** uyandırılır, zamanlanmış yeniden başlatma emniyet ağıdır. Claude Code'un kendi hata metni yüzdelerden üstün tutulur. |
| API aşırı yüklenir (529 / 5xx) | Ölmek yerine büyüyen aralıklarla dener: 30 sn → 5 dk, iki saatlik bütçe, tek bilgi mesajı, kesinti gerçekse bir tane daha. |
| Kullanım verisi limite yakın kesilir | Tahmin yürütmek yerine durur: yakım öngörüsü, ani yükseliş tahmini, kör nokta yoklaması ve duvara iki puan kala 15 saniyelik yenileme. |
| Bir pencere zaten %100'de | Eşikler ne derse desin iş orada durur ve duraklatma bunu kaldırmaz: o noktadan sonra taşan kullanımı hesap öder. |
| İş bir dağıtım işi ("her endpoint'i taşı") | Claude'a rol profilinizin modelleriyle **dinamik workflow** çalıştırması söylenir. Uyarı bandında ve pencerede 25 puan boşluk yoksa reddedilir, her başlatma kaydedilir, duraklamadan sonra aynı koşu yeniden başlatılır; biten ajanlar kayıtlı sonuçlarını döndürür. |
| Haftalık kotanın %99'unda kurarsınız | Setup yine çalışır; ilk oturum işin ne zaman süreceğini söyler, ilk istem sıfırlanmaya kadar park edilir (`/noctis:pause 120` ile yine de çalışılır), `noctis check` durumu betiklere bildirir. |
| Claude'a Almanca, Japonca, Türkçe yazarsınız | Bildirimler, durum çubuğu ve bildirim balonları yazdığınız dili izler; dil model çağrısı olmadan algılanır. On dördü de eksiksiz: bir dilde mesaj eksikse, çeviri İngilizceden farklı argüman alıyorsa ya da `noctis status` sütunları bozuluyorsa bir Go testi build'i kırar. Claude'a verilen kısa `[noctis]` yönergeleri her dilde İngilizce kalır. |
| Bekleme sırasında dosyalar değişir | Checkpoint `git status` parmak izini tutar; devam ederken ağaç farklıysa Claude önce düzenlediği dosyaları yeniden okur (`wait.workspaceGuard`). `checkpoint.gitSnapshot` commit'lenmemiş değişiklikleri gizli bir git ref'ine sabitleyebilir. |
| İki şey aynı oturumu sürdürmeye kalkar | Yalnızca bir yeniden başlatma olur; oturum kilit altında sahiplenilir. |
| Uzun bir bekleme biter | Oturum görebileceğiniz bir yerde döner: Windows Terminal sekmesi, macOS'ta Terminal penceresi, Linux'ta masaüstü terminaliniz, hiçbiri yoksa görünmez. O oturum için önceki pencere önce kapatılır ve `↪ başka bir pencerede sürüyor` yazar. |
| Terminal kapanır, süreç öldürülür, dosya yarım yazılır | Motor kendini onarır: bozuk `state.json`/`usage.json` yedekten döner, süreci ölmüş devir oturumu tıkamaz, runner'ını kaybeden bekleme yenisini alır, artık geçici dosya ve kilitler süpürülür. |
| Bir eşik yanlışlıkla saçma bir değere çekilir | `noctis status`, `noctis doctor` ve sonraki oturum artık korunmayan pencerenin adını söyler; koruma sessizce açık kalmaz. |
| Yeni sürüm yayımlanır | Otomatik güncelleme oturum başlangıcından sonra indirir, sonraki oturum bir kez `⬆ noctis 5.5.3 indirildi (bu oturum hâlâ 5.5.2 çalıştırıyor): /reload-plugins` der. Otomatik güncelleme kapalıysa tek satırlık bir bildirim sürümü ve komutu söyler. |

<p align="center"><img src="docs/flow.svg" alt="Korumadan geçen tek bir araç turu: sinyaller hook'lara girer, deterministik kurallar sonucu seçer" width="100%"></p>

</details>

## Diğer eklentilerle yan yana

Noctis hook ve durum çubuğu ekler; kimseninkini silmez, yeniden yazmaz. Claude Code bir olayın tüm hook'larını çalıştırır, yani bir döngü eklentisiyle noctis'in kuyruğu aynı turu birlikte itebilir — zararsız, ama çift devam görürseniz birini duraklatın. Zaten kullandığınız durum çubuğu (ccstatusline, claude-powerline) noctis'inkinin arkasında çalışır; kullanım panoları (ccusage, Claude-Code-Usage-Monitor) aynı dosyaları okur, etkilenmez. İkisi de limitten sonra otomatik devam eden araçlar (unsnooze, claude-auto-resume) yarışır — birini tutun. `noctis doctor` görebildiği komşuları listeler.

## Sizin dilinizde

<p align="center"><img src="docs/languages.svg" alt="Aynı duraklama ve kuyruk-bitti bildirimleri İngilizce, Türkçe, Almanca, İspanyolca, Japonca ve Rusça" width="100%"></p>

## Diğer yapay zekâ kodlama araçları

5.2'den beri aynı motor **OpenAI Codex CLI**, **Antigravity CLI** (Google), **Factory Droid** ve **GitHub Copilot CLI** içinde çalışır. Zip ya da klondan `./scripts/install.sh` veya `.\scripts\install.ps1` hangi araç olduğunu sorar — ya da `--host codex` / `-Tool codex` verin — sonra o aracın kendi hook dosyasını yazar ve oturumları kendi komutuyla sürdürür. Codex ve Antigravity kullanım pencerelerini betiklere açar, korumanın tamamı orada çalışır; Droid ve Copilot kuyruk modunu, checkpoint'leri ve hata denemelerini alır. Ayrıntı: [docs/REFERENCE.md](docs/REFERENCE.md#other-ai-coding-tools) ve [docs/HOSTS.md](docs/HOSTS.md).

## Cuma gecesi → Pazartesi sabahı

<details>
<summary>Kimseye ihtiyaç duymayan bir hafta sonu: checkpoint, sıfırlanmayı bekleme, Pazartesi yeniden başlatma.</summary>

<p align="center"><img src="docs/timeline.svg" alt="Zaman çizelgesi: %92'de checkpoint, bekleme, sıfırlanmadan sonra devam, haftalık limitte kaydetme, Pazartesi yeniden başlatma" width="100%"></p>

O hafta sonunda size ihtiyaç duyan hiçbir şey yok. Checkpoint son isteği, dokunulan dosyaları, `git status`'u, todo'ları ve sıradaki kuyruk maddelerini tutar. Kısa sıfırlanma hook'un içinde beklenir, tur olduğu gibi sürer; uzun olan sıfırlanma saatinde yeniden başlatılır — Windows'ta görev PC'yi uyandırabilir, diğerlerinde makine uyanır uyanmaz çalışır.

<p align="center"><img src="docs/before-after.svg" alt="Aynı gece, eklentili ve eklentisiz: 48 işten 14'ü ile 48'i" width="100%"></p>

</details>

## Rakiplerle karşılaştırma

| Yetenek | noctis | kullanım panoları | döngü eklentileri | otomatik devam betikleri |
|---|:---:|:---:|:---:|:---:|
| Duvara çarpmadan **önce** durur (eşik + ani yükseliş + yakım öngörüsü) | ✅ | yalnız gösterir | — | 429'dan sonra tepki |
| Sıfırlanmadan sonra kendi devam eder (aynı oturum ya da zamanlanmış başlatma) | ✅ | — | — | kısmen |
| Ücretli kullanım kredisi harcamaz, dağıtımı ölçülen boşluğa göre kapar | ✅ | — | — | — |
| Kuyruğu duruşlar boyunca yürütür, onay sormaz — öncelik, bağımlılık, GitHub issue | ✅ | — | ✅ (düz) | — |
| Araştırma, dosya arama ve gürültülü çıktı için ucuz modeller | ✅ | — | — | — |
| Kapsamlı model yedeği (Fable → Opus) ve otomatik geri dönüş | ✅ | — | — | — |
| Deterministik, karar başına sıfır token, günlüklü (`noctis why`) | ✅ | ✅ | istem güdümlü | değişir |
| Aşırı yük (529/5xx) için limitten ayrı, jitter'lı geri çekilme | ✅ | — | — | bazıları |
| ccusage uyumlu maliyet raporu, cron/CI için çıkış kodu kapısı | ✅ | ✅ / — | — | — |
| Kurulacak çalışma ortamı yok (tek statik binary, hook başına ~7 ms) | ✅ | değişir | değişir | değişir |
| Hiçbir şeyi uygulamadan kararları izleme modu | ✅ | — | — | — |
| Dinamik workflow: dağıtım işinde önerilir, limite yakın kapatılır, duraklamadan sonra kurtarılır | ✅ | — | — | — |
| Rol profili: kodu, araştırmayı, planlamayı, özetleri, aramayı, yedeği hangi model yapar | ✅ | — | — | — |
| Yazdığınız dili izler (14 dil, hepsi eksiksiz) | ✅ | bazıları | — | — |
| Claude Code, Codex CLI, Antigravity CLI, Droid ve Copilot CLI içinde çalışır | ✅ | bazıları | yalnız Claude | bazıları |

## Kurulum (ayrıntı)

<details>
<summary>Pazar yeri ve klon kurulumu, rol profili, bayraklar ve binary çalışmazsa ne yapılacağı.</summary>

<p align="center"><img src="docs/install.svg" alt="Altmış saniyede kurulum: pazar yerini ekle, kur, setup'ı çalıştır (hangi model hangi işi yapsın diye sorar), yeniden yükle, TASKS.md yaz" width="100%"></p>

**Pazar yerinden** — yukarıdaki dört satır. `setup` tek bir şey sorar, **hangi model hangi işi yapsın**, ve cevabı *rol profili* olarak saklar:

| Profil | kod | araştırma & yazı | planlama | özet & arama | yedek |
|---|---|---|---|---|---|
| `noctis` (Max planlar) | Fable 5.1 · max | Opus 5 · xhigh | Fable 5.1 | Haiku 4.5 · high | Opus 5 · max |
| `balanced` | Opus 5 · high | Sonnet 5 · high | Opus 5 | Haiku 4.5 | Sonnet 5 |
| `economy` | Sonnet 5 · high | Haiku 4.5 · high | Opus 5 | Haiku 4.5 | Haiku 4.5 |
| `custom` | her role bir model, effort uygulanabilen yerlerde effort | | | | |

Değiştirmek için beceriyi yeniden çalıştırın, ya da soruyu `--profile noctis|balanced|economy` veya `--code opus:high --research sonnet:high …` ile atlayın; `/noctis:status` güncel dağılımı gösterir. Effort ana oturuma, araştırma alt-ajanına ve özet alt-ajanına ulaşır. `Plan` ve `Explore` yalnızca model alır, çünkü Agent aracının verebileceği bir effort yoktur; setup bunu gizlemek yerine söyler.

Setup ayrıca `settings.json`'da üç düzenleme yapar — durum çubuğu, izin modu, varsayılan model ve effort — geri alma yollarıyla birlikte [Makinenizde neyi değiştirir](#makinenizde-neyi-değiştirir--nasıl-geri-alınır) bölümünde. Bayraklar: `--permissions keep`, `--no-model`, `--updates keep`, ikinci hesap için `--config-dir <dizin>`, ve `--preset conservative|balanced|aggressive` (%85/82/90, %92/89/95 ya da %96/94/98'de duraklama).

**Klon ya da zip'ten** hesap başına bir kopya `~/.claude/skills/` altına iner: Windows'ta `.\scripts\install.ps1`, diğerlerinde `./scripts/install.sh` — ikisi de önce hangi araç olduğunu sorar — sonra `/reload-plugins`. O kopya 16 dosya, yaklaşık 7,5 MB: platformunuzun binary'si, hook'lar, ajanlar, beceriler ve varsayılan config. Hiçbir şey indirilmez ya da derlenmez; binary zaten depoda (`bin/<os>-<arch>/`) ve `bin/SHA256SUMS` ile uyuşmayan bir binary kurulmaz, reddedilir. Hook'lar kabuktan geçmez, Windows'ta Git Bash gerekmez.

**Hiçbir şey olmuyorsa** — durum çubuğu yok, bildirim yok, `noctis doctor` da çalışmıyor — binary'nin çalışmasına izin verilmiyordur. Binary'ler imzalı ya da noter onaylı değil:

- **macOS**: tarayıcıdan inen dosya karantina bayrağı alır ve görüldüğü yerde öldürülür. `xattr -dr com.apple.quarantine <eklenti klasörü>`, ya da bayrağı hiç koymayan `git clone` ile kurun.
- **Windows**: SmartScreen indirilen `.exe`'yi engelleyebilir — Özellikler → Engellemeyi kaldır, ya da klonlayın.
- **Zip'ten**: zip'ler çalıştırma bitini her zaman taşımaz. `chmod +x bin/noctis bin/*/noctis`.
- **Başka bir şeyse**: terminalde doğrudan `bin/<os>-<arch>/noctis version` çalıştırın — yazdığı hata gerçek hatadır. Claude Code dışında setup'ın yazdığı tam yolu kullanın ya da o klasörü PATH'e ekleyin. Kaynaktan derleme: `cd go && go build -trimpath -ldflags="-s -w" ./cmd/noctis` (yalnız standart kütüphane).

VS Code ve Cursor eklentilerinde her şey çalışır; tek fark, uzun bir beklemeden sonraki yeniden başlatmanın editörün dışında olmasıdır — Windows'ta ayrı bir terminal penceresi, diğerlerinde arka planda `claude --resume` — editör sekmesinde değil.

</details>

## Claude Code içindeki komutlar

`/noctis:setup` (modelleri değiştirmek için yeniden çalıştırın) · `:status` (kullanım, duraklama noktaları, roller, bekleyenler, son kararlar) · `:pause [dakika]` (**korumayı** bir süre kapatır — Claude çalışmaya devam eder) · `:resume` (koruma geri açılır).

Terminalden: `noctis setup`, `noctis status` ve `noctis why`, `noctis off [dakika]`, `noctis on`.

## Durum çubuğu

<p align="center"><img src="docs/statusline.svg" alt="Durum çubuğu: 5 saatlik pencere, tempo işaretli haftalık pencere, kapsamlı kova, ETA, model, bağlam" width="100%"></p>

```
∞ 5sa %41→14:35 · Hf %23▲→Pzt 21.09 · Fable %60 · ⌛ Fable ~1g 3sa · Fable 5.1/max · ctx %37
```

`▲ / ● / ▼` haftalık tempoda önde, tam üstünde ya da geride olduğunuzu gösterir. `⌛` mevcut yakım hızıyla duraklama noktasına ne kadar kaldığını söyler. `⏸` bekleyen devam saatini, `⚠ hook yok` durum çubuğunun güncellendiğini ama 30 dakikadır hiçbir hook'un çalışmadığını, `👁` gözlem modunu gösterir. Hiçbiri token harcamaz. `statusline.mode: silent` veri toplamayı sürdürür ama hiçbir şey yazmaz, ya da yalnız zincirlediğiniz çubuğu yazar.

## Referans ve sınırlar

Her komut, tam yapılandırma tablosu, host adaptörleri ve dosya yerleşimi [docs/REFERENCE.md](docs/REFERENCE.md) içinde. Tasarım notları ve hata günlüğü: [docs/PLAN.md](docs/PLAN.md). On test paketi ve son koşunun ölçtükleri: [docs/TESTING.md](docs/TESTING.md).

Bilinmesi gereken üç sınır. Kullanım verisi Claude Code'un durum çubuğu yükünden (`rate_limits`, Claude Code ≥ 2.1.251) ve kapsamlı kovalar için belgelenmemiş OAuth kullanım uç noktasından gelir — Claude Code güncellemelerinden sonra `errors.log`'a göz atın. Aynı oturumda uyandırma, belgelerde gözlemsel denen `asyncRewake`'e dayanır; yedeği zamanlanmış yeniden başlatmadır. Ve hook'lar `/clear` yazamaz, sıkıştırma Claude Code'da kalır.

## Katkı

`noctis doctor` çıktısı ve ilgili `errors.log` / `noctis why --last 20` satırlarıyla gönderilen hata raporları en işe yarar şeydir. Derleme ve test döngüsü için [CONTRIBUTING.md](CONTRIBUTING.md).

## Lisans

MIT — © 2026 synex

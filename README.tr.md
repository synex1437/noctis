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

Kırk iş kuyruğa koyup yattınız; sabah kalktığınızda oturum 01:40'ta limit duvarına çarpıp ölmüş — ya da üçüncü işte *"şimdi diğer işe geçiyorum"* deyip durmuş. Claude Code'u yoğun kullanan herkes o sabahı bilir.

**Noctis**, Claude **Pro veya Max** aboneliğiyle (eklentinin 5.2 sürümünden itibaren OpenAI Codex CLI, Antigravity CLI, Factory Droid ve GitHub Copilot CLI içinde de) uzun işler koşturup 5 saatlik ya da haftalık limite yarı yolda takılanlar için bir eklenti. Ajanı limite çarpmadan *hemen önce* duraklatır, sıfırlanmayı bekler ve aynı oturumu sürdürür. `TASKS.md` listesini "devam edeyim mi?" diye sormadan madde madde yapar; Claude Code'da araştırmayı ucuz modele yollayıp pahalı modelin kotasını korur. Kendi kararları için hiçbir modele soru sormaz, token harcamaz; her kararı okuyabileceğiniz bir günlüğe yazar. Arka planda sessizce çalışır; gün içinde yazmanız gereken hiçbir şey yoktur.

**Kurulum** — Claude Code'un içinde şu dört satırı yazın (bir dakika sürer; üçüncüsü tek bir soru sorar: hangi model hangi işi yapsın):

```
/plugin marketplace add synex1437/noctis
/plugin install noctis
/noctis:setup
/reload-plugins
```

**Gereksinimler:** Claude Code 2.1.251 veya üstü, Pro ya da Max hesabıyla giriş yapılmış, Windows / macOS / Linux. Başka hiçbir şey kurulmaz — Node, Git Bash, derleyici gerekmez. API anahtarıyla açılan oturumlarda kullanım penceresi olmadığından limit koruması boşta kalır (bir kez bilgi verir, sonra susar); kuyruk modu ve araştırma yönlendiricisi (ucuz modele devretme) yine çalışır. Planınızda Fable var mı bilmiyor musunuz? Setup sorduğunda `balanced` ya da `economy` seçin — `noctis` Max varsayar. Setup `~/.claude/settings.json` dosyasını düzenler: durum çubuğu, varsayılan model ve **`permissions.defaultMode` → `auto`, yani Claude dosyaları düzenler ve komutları sormadan çalıştırır** (gece çalışmasını sağlayan budur; `--permissions keep` ile vazgeçilir). Her anahtar ve geri alma yolu [aşağıda listeli](#makinenizde-neyi-değiştirir--nasıl-geri-alınır). Başka bir yapay zekâ aracı mı kullanıyorsunuz? [Diğer yapay zekâ kodlama araçları](#diğer-yapay-zekâ-kodlama-araçları).

<p align="center"><img src="docs/demo.svg" alt="Eklentiyle bir gece: 5 saatlik limitte duraklama, sıfırlanınca devam, araştırmanın ucuz modele yönlendirilmesi, kota bitince model geçişi, yarım kalan workflow'un yeniden başlatılması, sabaha kuyruğun bitmesi" width="100%"></p>

## Makinenizde neyi değiştirir — nasıl geri alınır

<details>
<summary>Setup'ın yazdığı her ayar, ağda konuştuğu yerler ve her birinin geri alınışı.</summary>

Setup önce `settings.json.bak-<zaman>` yedeğini alır, sonra yalnızca şunlara dokunur:

| Nerede | Ne | Geri alma |
|---|---|---|
| `~/.claude/settings.json` → `statusLine` | Durum çubuğunu eklentinin binary'sine bağlar (Claude Code kullanımınızı böyle bildirir). Zaten bir durum çubuğunuz varsa arkasında çalışmaya devam eder. | `noctis install --uninstall` geri koyar |
| `settings.json` → `permissions.defaultMode` | `auto` yapılır (eski Claude Code: `acceptEdits`). **Açık Türkçesi: Claude dosyaları düzenler ve komutları size sormadan çalıştırır** — siz uyurken çalışmasını sağlayan budur. `bypassPermissions` asla kullanılmaz; config isteseydi bile gözetimsiz devam ettirme o modu reddeder. | Setup'ta `--permissions keep`; kaldırma yalnızca setup'ın yazdığını siler |
| `settings.json` → `model` ve `env.CLAUDE_CODE_EFFORT_LEVEL` | Varsayılan model = seçtiğiniz profilin *kod* modeli (varsayılan `noctis` profilinde **Fable 5.1, effort `max`**). Varsayılanınız zaten Fable ya da Opus ise atlanır. | Setup'ta `--no-model`; kaldırma önceki değeri geri yükler |
| `~/.claude/noctis/` | Kendi `config.json`'u, kullanım anlık görüntüleri, checkpoint'ler, günlükler | Klasörü silin |
| Yalnızca bir devam beklerken, zamanlanmış görev | Windows Görev Zamanlayıcı `Noctis-…` (PC'yi uyandırabilir), launchd `com.synex.noctis.…`, systemd `noctis-…` | `noctis cancel` bekleyenlerin hepsini kaldırır; `"alarm": {"wakePc": false}` uyandırmayı kapatır |
| Marketplace otomatik güncelleme | Setup `claude plugin marketplace update noctis --auto-update` çalıştırır; yeni sürümler kendiliğinden gelir (üçüncü parti marketplace'lerde bu varsayılan kapalıdır). | Setup'ta `--updates keep` ya da `/plugin` → Marketplaces'teki anahtar |

**Ağ:** bağlandığı tek sunucular `api.anthropic.com` (Claude uygulamasının kendisinin kullandığı kullanım uç noktası; Claude Code'un zaten sakladığı giriş jetonuyla — macOS'ta Keychain'den, bir kez "izin ver" sorusu bekleyin — 10 dakikada bir, duraklamadan önceki son iki puanda 15 saniyede bir) ve günde bir kez GitHub'daki ham `plugin.json` (yeni sürüm var mı diye; `update.check: false` kapatır). Bir de siz ayarlarsanız webhook adresi. Telemetri yok; sizinle ilgili hiçbir şey hiçbir yere gönderilmez.

**Kuyruk modu sizin için bir listeyi yürütür — ama siz izin verdikten sonra.** Proje klasöründe açık `- [ ]` maddeleri olan bir `TASKS.md` (ya da `tasks.md`, `.claude/TASKS.md`, `docs/TASKS.md`) varsa, o klasördeki ilk oturum dosyanın orada olduğunu ve **henüz hiçbir şeyi yürütmediğini** söyler:

```text
☰ Bu klasördeki TASKS.md 12 açık madde içeriyor. Bu oturumu yürütmüyor: bir kontrol listesi bir
  depoyla birlikte gelebilir ve yürütmek, sormak için durmadan baştan sona yapmak demektir.
  İzin vermek için: noctis queue trust
```

`noctis queue trust` o dosya için açar, `noctis queue untrust` kapatır, `noctis queue status` durumu söyler. Noctis'in sizin kendi prompt'unuzdan yazdığı bir kontrol listesi izin istemez — zaten siz istediniz. Kapı şunun için var: bir `TASKS.md` `git clone` ile gelebilir ve "bu listeyi sormadan yap" cümlesini indirilen bir dosyanın söyleyebilmesi doğru değil. Eski hep-açık davranışı mı istiyorsunuz? `~/.claude/noctis/config.json` içine `"queue": {"requireTrust": false}`. Hiç istemiyor musunuz? `"queue": {"files": []}`.

**Kapatmak.** Bir süreliğine: `/noctis:pause 120` (dakika; Claude çalışmaya devam eder, koruma uyur) — `:resume` yeniden açar. Yalnızca izle, hiçbir şey uygulama: `config.json` içinde `"mode": "observe"`. Tamamen:

```
noctis cancel                              # bekleyen devamları ve zamanlanmış görevleri kaldırır
noctis install --uninstall                 # settings.json'ı geri alır (durum çubuğu, effort, izin modu, model)
                                           # ÖNCE bunu çalıştırın: eklentiyi önce kaldırmak binary'yi
                                           # siler, sonra düzenlemeleri geri alacak bir şey kalmaz
/plugin uninstall noctis   # Claude Code'un içinde
```

Eklenti çoktan gittiyse ve ayarlar duruyorsa elle geri alın: `~/.claude/settings.json` içinden `permissions.defaultMode`, `model`, `env.CLAUDE_CODE_EFFORT_LEVEL` ve `statusLine` bloğunu silin (ya da setup'ın yanına yazdığı `settings.json.bak-<zaman>` kopyasını geri koyun). Asıl önemlisi `permissions.defaultMode`: kalırsa her gelecek oturum dosyaları düzenlemeye ve komutları sormadan çalıştırmaya devam eder.

```text
```
Klon kurulumunda ortadaki satır yerine `scripts/install.sh --uninstall` (macOS/Linux) ya da `scripts\install.ps1 -Uninstall` (Windows).

</details>

## İlk beş dakika

1. `/reload-plugins` sonrası Claude Code penceresinin altına bakın: `∞ 5s %41→14:35 · Hf %23▲→Pzt 21.09 · Fable 5.1/max · ctx %37` gibi bir satır 5 saatlik ve haftalık kullanımınızı, sıfırlanma zamanlarını ve modeli gösterir. *limit verisi bekleniyor* diyorsa bir mesaj gönderin — ilk yanıttan sonra dolar.
2. `/noctis:status` yazın: kullanım pencereleri, duraklama noktaları (varsayılan 5 saatlik pencerenin %92'si, haftalığın %89'u) ve eklentinin son kararları.
3. Aşağıdaki üç satırlık `TASKS.md`'yi oluşturup Claude'a "TASKS.md'yi sırayla yap" deyin. Maddeleri işaretleyip kendi başına devam ettiğini izleyin.
4. Limitte sizin yapacağınız bir şey yok: duvardan *önce* duraklar, bekler (kısa sıfırlanma aynı turun içinde, bağlam bozulmadan; uzun olanı aynı oturumun zamanlanmış `claude --resume`'u — Windows'ta yeni bir terminal penceresinde, macOS/Linux'ta arka planda, çıktısı `resume-output.log`'da, `claude --resume <id>` ile devralınır) ve sürdürür. Windows ve Linux'ta zamanlanmış devam makineyi uyandırmayı ister (Linux'ta `CAP_WAKE_ALARM` gerekir; kullanıcı yöneticisi reddederse uyandırmasız zamanlanır). macOS'ta launchd Mac'i uyandıramaz, o yüzden gece boyunca uyanık tutun.
5. Henüz güvenmiyor musunuz? İlk günler için `~/.claude/noctis/config.json` içine `"mode": "observe"` yazın: her kararı günlüğe yazar (`/noctis:status` gösterir), hiçbir şey uygulamaz.

## Kuyruk dosyası

<details>
<summary>`TASKS.md` biçimi, artı öncelikler, etiketler, bağımlılıklar ve GitHub issue'ları.</summary>

`claude`'u başlattığınız klasöre bir `TASKS.md` koyun, her satıra bir iş:

```markdown
- [ ] kayıt formuna girdi doğrulaması ekle
- [ ] ödeme modülü için testleri yaz
- [ ] yeni CLI bayrakları için README'yi güncelle
```

Biçimin tamamı bu. Claude ilk açık maddeyi alır, bitince dosyada `- [x]` yapar, sormadan bir sonrakine geçer; hepsi işaretlenince `✔ Kuyruk bitti` der ve durur. Dosya şart da değil: birkaç iş içeren uzun bir prompt yapıştırın, Noctis checklist'i kendisi yazar (kendi klasöründe tutar, projenize dokunmaz) ve aynı şekilde yürütür — `☰ 5 adımlık iş algılandı` satırı bunun işaretidir. Kısa prompt'lar ve tek işler dokunulmaz. Özensiz listeler de kabul edilir (`-[ ]`, `* [ ]`, `1. [ ]`, `[]`, `TODO:`; iki–üç satıra taşan madde tek madde; `~~üstü çizili~~`, `(bitti)`, `✓` bitmiş sayılır).

<details><summary>Öncelik, etiket, bağımlılık, GitHub issue</summary>

```markdown
- [ ] (P0) giriş yönlendirmesini düzelt #auth
- [ ] (P1) kullanıcıları taşı (after #auth, #db)
- [ ] deploy (after 2)
- [ ] (P7) API dokümanını yaz
```

`(P0)`–`(P9)` sırayı belirler (varsayılan P5, küçük önce); `#ad` etiketler; `(after #etiket)` ya da `(after 3)` maddeyi başvurulan maddeler işaretlenene kadar bekletir (`(after 2)` = dosyadaki 2. madde). Stop hook'u Claude'a sıradaki en uygun maddeyi verir, kaç maddenin beklediğini söyler, tüm açık maddeler bloklandığında ya da kuyruk bittiğinde bildirimle temiz durur. Hiçbir şeye uymayan referanslar yok sayılır; bir yazım hatası geceyi kilitlemez. `noctis queue import` açık GitHub issue'larını `gh` ile `- [ ] (P1) #123 Başlık` olarak ekler (öncelik `P0`–`P9` ya da `priority: high` etiketlerinden; idempotent); `queue.github.closeOnDone` işaretlenen maddenin issue'sunu kapatır.
</details>

</details>

## Siz yokken ne yapar

<details>
<summary>Durum durum: limitler, erken sıfırlanmalar, kuyruk duruşları, kota geçişleri ve tek başına hallettiği arızalar.</summary>

| Durum | Ne olur |
|---|---|
| 5 saatlik ya da haftalık kullanım limite yaklaşır (varsayılan %92 / %89'da duraklama) | Tur duvara çarpmadan **önce** duraklatılır: checkpoint yazılır (son istek, dokunulan dosyalar, `git status`, yapılacaklar, sıradaki kuyruk maddeleri), kısa sıfırlanmalar yerinde beklenir, uzunlar kaydedilip zamanlanmış görevle sıfırlanma saatinde **otomatik sürdürülür** (Windows Görev Zamanlayıcı PC'yi uyandırabilir; macOS'ta launchd, Linux'ta systemd makine uyanıkken çalışır) + masaüstü bildirimi ve ayarlıysa webhook. |
| Claude bir işi bitirip "sıradakine geçiyorum" diyerek durur | **Kuyruk modunda** Stop hook'u oturumu sürdürür: sıradaki açık madde, onay sorusu yok. Kuyruk bitince (söyleyerek) ya da ilerleme olmayınca temiz durur. |
| Birkaç iş içeren uzun bir prompt yapıştırdınız | Bu, kimsenin dosyaya yazmadığı bir görev listesidir; Noctis dosyayı kendisi açar: maddeler kendi klasöründeki bir checklist'e yazılır (projenizde iz kalmaz), size `☰ 5 adımlık iş algılandı` görünür, oturum her madde işaretlenene kadar sürer. Kısa prompt'lar, sorular, log yapıştırılmış hata raporları ve tek işler dokunulmaz (`queue.auto: false` kapatır). |
| Pahalı modelin (örn. Fable) haftalık kotası biter | Varsayılan model yedeğe (örn. Opus) çevrilir, oturum checkpoint'lenip onunla yeniden başlatılır; kota sıfırlanınca kendiliğinden geri döner. |
| İstek kod değil araştırma/yazı | Daha ucuz bir yardımcı modele (*alt-ajan*) verilir, pahalı modelin kotası koda kalır; uzun test çıktılarını en ucuz model özetler; dosya araması birincil modelin kotasını yakmaz. |
| Bir 429 limit hatası turu yine de öldürür | Limit sıfırlanınca oturum **yerinde** uyandırılır; güvenlik ağı olarak zamanlanmış yeniden başlatma bekler. Claude Code'un hata mesajı ("weekly limit", "5-hour") yüzdelerden daha güvenilir sayılır. |
| API'nin kendisi aşırı yüklü (529 / 5xx) | Ölmek yerine büyüyen aralıklarla tekrar dener (30 sn → 5 dk, iki saatlik bütçe), bir kez haber verir, gerçek kesintide tek uyarıyla durur. |
| Limite yakınken kullanım verisi gelmez olur | Tahmin etmek yerine durur: yakım hızı öngörüsü, sıçrama tahmini, kör-nokta yoklaması, duvara 2 puan kala 15 saniyelik tazeleme. |

<details><summary>Ele aldığı diğer durumlar</summary>

| Durum | Ne olur |
|---|---|
| İş dağıtık ("tüm endpoint'leri taşı", "40 dosyayı gözden geçir") | Claude'a rol profilindeki model ve effort'larla **dinamik workflow** olarak koşması söylenir; *uyarı bandında* (duraklamadan önceki son birkaç puan) yeni workflow reddedilir, her başlatma kaydedilir, duraklama sonrası Claude'a **aynı koşuyu yeniden başlat** denir (biten ajanlar kayıtlı sonuçlarını döndürür), sıfırdan başlamaz. |
| Haftalık kotanın %99'undayken kurdunuz | Setup yine çalışır; ilk oturum ne olacağını ve ne zaman devam edileceğini anlatır, ilk prompt sıfırlanmaya park edilir (`/noctis:pause 120` ile yine de çalışılır), `noctis check` betiklere bildirir. Fable kovası ya da haftalık limiti olmayan planlar bu kuralları hiç görmez. |
| Claude'a Almanca, Japonca, Türkçe… yazıyorsunuz | Bildirim, durum çubuğu ve toast'lar o oturumun diline uyar (yazdığınızdan tespit edilir, model çağrısı yok); Claude'a giden kısa `[noctis]` yönergeleri İngilizce kalır. On dört dilin (en tr de fr es pt it nl pl ru ja zh ko ar) hepsi tam: biri tek bir mesajı bile eksik bırakırsa bir Go testi derlemeyi düşürür. Çeviriler `i18n/<kod>.json` dosyalarında; `node scripts/i18n.js build` katalogu yeniden üretir. |
| Oturum beklerken dosyalar değişti | Checkpoint `git status` parmak izi alır; devamda ağaç farklıysa Claude'a önce düzenlediklerini yeniden okuması söylenir (`wait.workspaceGuard`). `checkpoint.gitSnapshot` kaydedilmemiş değişiklikleri gizli bir git ref'ine de pinleyebilir. |
| Aynı oturumu iki şey birden sürdürmeye kalkar | Yalnızca tek yeniden başlatma olur: oturum kilit altında sahiplenilir. |
| Uzun bekleme bitip oturum geri gelir | Gördüğünüz bir yerde açılır: Windows Terminal'de yeni sekme, macOS'ta Terminal penceresi, Linux'ta masaüstü terminaliniz (ulaşılamıyorsa arka planda). O oturum için eklentinin en son açtığı pencere önce kapatılır, böylece pencere birikmez; eski pencerenin durum çubuğu `↪ başka bir pencerede sürüyor — burası kapatılabilir` der. |
| Terminal kapanır, süreç öldürülür, dosya yarım yazılır | Motor kendini onarır: bozuk `state.json`/`usage.json` bir sonraki okumada yedeğinden geri gelir, süreci ölmüş devir oturumu bloklamayı bırakır, runner'ını kaybeden bekleme yenisini alır, artık geçici dosyalar ve kilitler temizlenir. |
| Yeni sürüm yayınlandı | Marketplace otomatik güncellemesi (setup açar) oturum başlangıcından sonra indirir; sonraki oturum `⬆ noctis 5.4.0 indirildi (bu oturum hâlâ 5.3.0 çalıştırıyor): geçmek için /reload-plugins çalıştır ya da aracı yeniden başlat.` der (masaüstü bildirimiyle), bir kez. Otomatik güncelleme kapalıysa tek satırlık uyarı sürümü ve komutu söyler. |
</details>

<p align="center"><img src="docs/flow.svg" alt="Bir araç turu korumadan geçerken: sinyaller hook'ları besler, deterministik kurallar sonucu seçer" width="100%"></p>

</details>

## Diğer eklentilerle yan yana

Noctis hook ve durum çubuğu ekler; kimsenin hook'unu silmez ya da yeniden yazmaz. Claude Code bir olaya kayıtlı tüm hook'ları çalıştırır; bir döngü eklentisi (ralph-loop vb.) ile Noctis'in kuyruğu aynı turu birlikte itebilir — zararsızdır, çift devam görürseniz birini duraklatın. Var olan durum çubuğunuz (ccstatusline, claude-powerline, …) Noctis'in satırının arkasında çalışmaya devam eder. Kullanım panoları (ccusage, Claude-Code-Usage-Monitor) Claude Code'un yazdığı aynı dosyaları okur, etkilenmez. Limit sonrası otomatik devam ettiren iki araç (unsnooze, claude-auto-resume) birbiriyle yarışır — birini bırakın. `noctis doctor` makinenizde gördüğü komşu hook ve eklentileri listeler.

## Sizin dilinizde

<p align="center"><img src="docs/languages.svg" alt="Aynı duraklama ve kuyruk-bitti bildirimleri İngilizce, Türkçe, Almanca, İspanyolca, Japonca ve Rusça" width="100%"></p>

## Diğer yapay zekâ kodlama araçları

5.2'den itibaren aynı motor **OpenAI Codex CLI**, **Antigravity CLI** (Google), **Factory Droid** ve **GitHub Copilot CLI** içinde de çalışır. Zip ya da klondan: `./scripts/install.sh` (macOS/Linux) ya da `.\scripts\install.ps1` (Windows) hangi araç için olduğunu numaralı listeyle sorar — ya da `--host codex` / `-Tool codex` geçin — sonra o aracın kendi hook dosyasını bağlar ve oturumları onun komutuyla sürdürür. Codex ve Antigravity kullanım pencerelerini betiklere açtığından duvardan-önce-durma korumasının tamamı orada da çalışır; Droid ve Copilot'ta kuyruk modu, checkpoint ve hata sonrası yeniden deneme vardır. Araç tablosu, her birinin yapabildikleri ve duman testi adımları: [docs/REFERENCE.md](docs/REFERENCE.md#other-ai-coding-tools) ve [docs/HOSTS.md](docs/HOSTS.md).

## Cuma gecesi → Pazartesi sabahı

<details>
<summary>Kimseyi gerektirmeyen bir hafta sonu, ve panolar, döngü eklentileri ve otomatik devam betikleriyle karşılaştırması.</summary>

<p align="center"><img src="docs/timeline.svg" alt="Zaman çizgisi: %92'de checkpoint, bekleme, sıfırlanınca devam, haftalık limitte kayıt, Pazartesi yeniden başlatma" width="100%"></p>

O hafta sonunda size düşen hiçbir şey yok. Checkpoint son isteği, dokunulan dosyaları, `git status`'u, yapılacakları ve sıradaki kuyruk maddelerini tutar. Kısa sıfırlanma hook'un içinde beklenir, tur olduğu gibi sürer; uzun olanı kaydedilip sıfırlanma saatinde yeniden başlatılır — Windows'ta zamanlanmış görev PC'yi uykudan uyandırabilir, macOS ve Linux'ta makine uyanır uyanmaz çalışır.

<p align="center"><img src="docs/before-after.svg" alt="Aynı gece eklentisiz ve eklentiyle: 48 işin 14'ü yerine 48'i" width="100%"></p>

<details><summary>Rakiplerle karşılaştırma</summary>

| Yetenek | noctis | kullanım panoları / durum çubukları | döngü eklentileri ("devam et") | otomatik devam betikleri |
|---|:---:|:---:|:---:|:---:|
| Duvardan **önce** durur (eşik + sıçrama + yakım öngörüsü) | ✅ | yalnızca gösterir | — | 429'dan sonra tepki |
| Sıfırlanınca kendi başına sürer (aynı oturum ya da zamanlanmış yeniden başlatma) | ✅ | — | — | kısmen |
| Kuyruğu duraklamalar boyunca onaysız yürütür — öncelik, bağımlılık, GitHub issue ile | ✅ | — | ✅ (düz) | — |
| Araştırma, dosya arama ve gürültülü çıktı için ucuz modeller | ✅ | — | — | — |
| Kapsamlı model yedeği (örn. Fable → Opus) ve otomatik geri dönüş | ✅ | — | — | — |
| Deterministik, karar başına sıfır token, günlüklü (`noctis why`) | ✅ | ✅ | prompt'a bağlı | değişir |
| 529/5xx geri çekilmesi (jitter'lı), limit yönetiminden ayrı | ✅ | — | — | bazıları |
| ccusage uyumlu maliyet raporu, cron/CI için çıkış kodu kapısı | ✅ | ✅ / — | — | — |
| Kurulacak çalışma zamanı yok (tek statik binary, hook başına ~7 ms) | ✅ | değişir | değişir | değişir |
| Uygulamadan önce kararları izleme (gözlem modu) | ✅ | — | — | — |
| Dinamik workflow: dağıtık işte önerilir, limite yakın engellenir, duraklamadan sonra kurtarılır | ✅ | — | — | — |
| Rol profili: kod, araştırma, planlama, özet, arama, yedek için model ve effort | ✅ | — | — | — |
| Yazdığınız dile uyar (14 dil, hepsi tam) | ✅ | bazıları | — | — |
| Claude Code, Codex CLI, Antigravity CLI, Droid ve Copilot CLI içinde çalışır | ✅ | bazıları | yalnızca Claude | bazıları |
</details>

</details>

## Kurulum (ayrıntı)

<details>
<summary>Marketplace ve klon kurulumu, rol profili, bayraklar, ve binary çalışmazsa ne yapılacağı.</summary>

<p align="center"><img src="docs/install.svg" alt="Altmış saniyede kurulum: marketplace ekle, kur, setup'ı çalıştır (hangi model hangi işi yapsın diye sorar), yeniden yükle, TASKS.md yaz" width="100%"></p>

**Marketplace'ten (önerilen)** — en üstteki dört satır. `setup` tek soru sorar — **hangi model ve effort hangi işi yapsın** — ve yanıtı *rol profili* olarak saklar: `noctis` (kod ve planlama Fable 5.1 · max, araştırma ve yazı Opus 5 · xhigh, özet ve dosya arama Haiku · high, yedek Opus · max — Max planlar için), `balanced`, `economy` ya da rol rol `custom`. Değiştirmek için skill'i yeniden çalıştırın; soruyu `--profile noctis|balanced|economy` ya da `--code opus:high --research sonnet:high …` ile atlayın.

`settings.json`'da üç düzenleme yapar (durum çubuğu, izin modu, varsayılan model + effort) — her biri geri alma yoluyla birlikte [Makinenizde neyi değiştirir](#makinenizde-neyi-değiştirir--nasıl-geri-alınır) bölümünde. Bayraklar: `--permissions keep`, `--no-model`, `--updates keep`, `--preset conservative|balanced|aggressive` (5 saatlik / haftalık / Fable pencerelerinin %85/82/90, %92/89/95 ya da %96/94/98'inde duraklama), `--config-dir <dizin>` (yalnızca ikinci hesap için).

**Klon / zip'ten** (`~/.claude/skills/` altına hesap başına kopya): Windows'ta `.\scripts\install.ps1`, macOS/Linux'ta `./scripts/install.sh` — terminalde ikisi de önce hangi yapay zekâ aracı için olduğunu sorar. `--config-dir <dizin>` (PowerShell'de `-ConfigDir <dizin>`) yalnızca ikinci hesap için. Sonra `/reload-plugins`. Oraya inen şey 16 dosya ve yaklaşık 7,5 MB: sizin platformunuzun binary'si, hook'lar, ajanlar, skill'ler ve varsayılan config — Go kaynağı, test süitleri, dokümanlar ve diğer beş platformun binary'si değil.

Hiçbir şey indirilmez ya da derlenmez: işletim sisteminizin binary'si depoda (`bin/<os>-<mimari>/`, sağlama toplamları `bin/SHA256SUMS` — toplamı tutmayan ya da listede olmayan bir binary kurulmaz, reddedilir), hook'lar kabuktan geçmez, Windows'ta Git Bash gerekmez. Claude Code dışında `noctis`'i setup'ın yazdığı tam yolla (`setup complete: binary at …/bin/noctis`) çalıştırın ya da o klasörü PATH'e ekleyin. Kaynaktan derleme: `cd go && go build -trimpath -ldflags="-s -w" ./cmd/noctis` (yalnızca standart kütüphane).

**Hiçbir şey olmuyorsa** — durum çubuğu yok, bildirim yok, `noctis doctor` da çalışmıyor — binary'nin çalışmasına izin verilmiyordur. Binary'ler imzalı/noter onaylı değil, dolayısıyla:
- **macOS**: tarayıcıyla indirilen bir arşiv (release zip'i, "Download ZIP") karantina bayrağı alır ve görüldüğü yerde öldürülür. `xattr -dr com.apple.quarantine <eklenti klasörü>` ile temizleyin ya da bayrağı hiç koymayan `git clone` ile kurun.
- **Windows**: SmartScreen indirilen bir `.exe`'yi engelleyebilir — Özellikler → Engellemeyi Kaldır, ya da klonlayın.
- **Zip'ten Linux/macOS**: zip'ler çalıştırma bitini her zaman taşımaz. `chmod +x bin/noctis bin/*/noctis` çözer.
- Başka bir şeyse (alışılmadık işlemci mimarisi, kısıtlı makine): terminalde doğrudan `bin/<os>-<mimari>/noctis version` çalıştırın — yazdığı hata gerçek hatadır.

VS Code / Cursor eklentisinde her şey çalışır; tek fark uzun bekleme sonrası otomatik yeniden başlatmanın editör sekmesinde değil dışarıda çalışması (Windows'ta ayrı terminal penceresi, diğerlerinde arka planda `claude --resume`).

</details>

## Claude Code içindeki komutlar

`/noctis:setup` (modelleri değiştirmek için yeniden çalıştırın) · `:status` (kullanım, duraklama noktaları, bekleyenler, son kararlar) · `:pause [dakika]` (**korumayı** bir süre kapatır — Claude limiti geçse bile çalışmaya devam eder) · `:resume` (koruma yeniden açık). Terminalden aynıları: `noctis setup`, `noctis status` + `noctis why`, `noctis off [dakika]`, `noctis on`.

## Durum çubuğu

<p align="center"><img src="docs/statusline.svg" alt="Durum çubuğu: 5 saatlik pencere, tempolu haftalık pencere, kapsamlı kova, ETA, model, bağlam" width="100%"></p>

```
∞ 5s %41→14:35 · Hf %23▲→Pzt 21.09 · Fable %60 · ⌛ Fable ~1g 3sa · Fable 5.1/max · ctx %37
```

`▲ / ● / ▼` haftalık eşit tempoya göre önde / tempoda / geride olduğunuzu gösterir (token harcamaz). `⌛` mevcut yakım hızıyla duraklama noktasına ne zaman varılacağını söyler. `⏸` bekleyen devam saatini, `⚠ hook yok` durum çubuğunun güncellenip 30 dakikadır hiçbir hook'un çalışmadığını, `👁` gözlem modunu gösterir. `statusline.mode: silent` veriyi toplamaya devam eder ama hiçbir şey basmaz (ya da yalnızca zincirlenen durum çubuğunuzu).

## Referans ve sınırlar

Her komut (`noctis status`, `noctis check`, `noctis why`, `noctis doctor`, `noctis report`, `noctis queue import`, `noctis version`, …), tam yapılandırma tablosu, araç adaptörleri ve dosya düzeni [docs/REFERENCE.md](docs/REFERENCE.md) içinde. Bilinmesi gereken üç sınır: kullanım verisi Claude Code'un resmî durum çubuğu yükünden (`rate_limits`, Claude Code ≥ 2.1.251) ve kapsamlı kovalar için belgelenmemiş OAuth kullanım uç noktasından gelir — Claude Code güncellemelerinden sonra `errors.log`'a bakın; aynı oturumda uyanma "gözlemsel" belgelenen `asyncRewake`'e dayanır, zamanlanmış yeniden başlatma yedektir; hook'lar `/clear` yazamaz, sıkıştırma Claude Code'da kalır.

Süit hook sözleşmesini, binary'ye karşı kara-kutu laboratuvarını, aynı anda binlerce oturumu, enjekte edilen makine arızalarını, işletim sistemi zamanlayıcılarını, haftalarca süren soak'ları, kaynak hijyenini ve Go birim ile fuzz testlerini kapsıyor — her birinin ne sorduğu ve son ölçülen koşu [docs/TESTING.md](docs/TESTING.md) içinde (İngilizce).

Tasarım notları ve hata günlüğü: [docs/PLAN.md](docs/PLAN.md).

## Katkı

En yararlı hata raporu: `noctis doctor` çıktısı ve ilgili `errors.log` / `noctis why --last 20` satırları. Derleme ve test döngüsü için [CONTRIBUTING.md](CONTRIBUTING.md).

## Lisans

MIT — © 2026 synex

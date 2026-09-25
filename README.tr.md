<p align="center">
  <img src="docs/banner.svg" alt="Noctis — 5 saatlik ya da haftalık kullanım limitinden önce duraklayan ve sıfırlanmadan sonra devam eden bir Claude Code eklentisi" width="100%">
</p>

<p align="center">
  <a href="https://github.com/synex1437/noctis/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/synex1437/noctis/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/synex1437/noctis/releases"><img alt="Son sürüm" src="https://img.shields.io/github/v/release/synex1437/noctis"></a>
  <img alt="Windows, macOS, Linux" src="https://img.shields.io/badge/Windows%20%C2%B7%20macOS%20%C2%B7%20Linux-tek%20binary-2a78d6">
  <img alt="Claude Code, Codex CLI, Antigravity CLI, Droid, Copilot CLI" src="https://img.shields.io/badge/Claude%20Code%20%C2%B7%20Codex%20%C2%B7%20Antigravity%20%C2%B7%20Droid%20%C2%B7%20Copilot-5%20ara%C3%A7-35b26e">
  <a href="LICENSE"><img alt="MIT lisansı" src="https://img.shields.io/badge/lisans-MIT-lightgrey"></a>
  <a href="README.md"><img alt="English README" src="https://img.shields.io/badge/README-English-2a78d6"></a>
</p>

# Noctis — kullanım limitinden hemen önce duraklayan, sıfırlanınca aynı oturumu sürdüren Claude Code eklentisi

Kuyruğa kırk iş koyup yattınız; sabah sizi 01:40'ta düşmüş bir *"You've hit your session limit"* mesajı karşıladı — ya da üçüncü işte *"şimdi diğer işe geçiyorum"* deyip durmuş bir oturum.

**Noctis**, uzun Claude Code işlerini siz başında olmadan yürütür:

- 5 saatlik ya da haftalık limitten **hemen önce duraklar**, checkpoint alır ve sıfırlanmadan sonra **aynı oturumu sürdürür** — sıfırlanma yakınsa tur içinde bekleyerek, değilse zamanlanmış bir görevden `claude --resume` ile yeniden başlatarak, günler sonra bile.
- **Bir `TASKS.md` listesindeki işleri sırayla bitirir**, durup sormadan — öncelikler, bağımlılıklar, GitHub issue'ları.
- **Her iş türüne kendi modelini verir**: varsayılan olarak kod Opus 5.5 · max'ta, araştırma ve yazı bir alt-ajanda Opus 5.5 · xhigh'da, dosya arama ve çıktı özetleri Haiku 4.5'te.
- **Yalın sıkıştırır**: noctis sıkıştırmayı Claude Code'un daha geç eşiği yerine turlar arasında, bağlam %70'teyken ister; Claude Code'un istem önbelleği kapalıyken özetin okuduğu metinden eski araç çıktılarını da budar ([Yalın sıkıştırma](#yalın-sıkıştırma); Claude Code'un erken erişimdeki fonksiyon hook'larıyla çalışır, setup bunları açar).
- **Model çağırmadan karar verir**: kullanım verileriniz üzerinde sabit kurallar işler, yani karar vermek token harcamaz ve her karar kaydedilir (`noctis why`). Ücretli kullanım kredisi harcamak yerine bir pencerenin %100'ünde durur.
- Kendi **durum çubuğunu** çizer, **14 dilde** konuşur ve **Codex CLI, Antigravity CLI, Factory Droid ve Copilot CLI** için de adaptörleri vardır; orada daha az özellik sunar ([Diğer yapay zekâ kodlama araçları](#diğer-yapay-zekâ-kodlama-araçları)).

**Kurulum** — Claude Code içinde dört satır, yaklaşık bir dakika. Üçüncüsü tek bir soru sorar — hangi model hangi işi yapsın — ve Claude Code'u **auto izin moduna geçirir; yani Claude size sormadan dosya düzenler ve komut çalıştırır** — gece boyunca çalışabilmesini sağlayan budur. Bundan `--permissions keep` ile vazgeçebilirsiniz; her değişiklik ve geri alma yolu [aşağıda listeli](#makinenizde-neyi-değiştirir--nasıl-geri-alınır).

```
/plugin marketplace add synex1437/noctis
/plugin install noctis@noctis
/noctis:setup
/reload-plugins
```

`/noctis:setup` henüz bulunamıyorsa önce `/reload-plugins` çalıştırın. Terminalden kurmak isterseniz: `claude plugin marketplace add synex1437/noctis && claude plugin install noctis@noctis`, ardından Claude Code içinde `/noctis:setup`.

**Gereksinimler:** Claude Code 2.1.251 veya üstü (yalın sıkıştırma Claude Code'un fonksiyon hook'larını ister ve 2.1.281 ile denendi); Windows, macOS ya da Linux; limit koruması için Pro ya da Max aboneliği. API anahtarıyla kullanım pencereleri olmaz, bu yüzden limit korumasının izleyeceği bir şey yoktur: durum çubuğunda *limit verisi bekleniyor* yazar ve oturum açılışında, günde en fazla bir kez, OAuth token'ı olmadığını söyleyen bir not çıkar. Kuyruk modu, yönlendirici ve 529/5xx sonrası yeniden deneme yine çalışır. Node, Git Bash, derleyici gerekmez. Her profil tüm ücretli planlarda bulunan modellerle çalışır — Opus 5.5, Sonnet 5, Haiku 4.5 — yani hiçbiri Max gerektirmez; hiçbiri, Pro'da kullanım kredisinden düşen Fable'ı kullanmaz.

**Claude Code sıfırlanmadan sonra zaten devam ediyor — neden noctis?** Son Claude Code sürümleri, kullanım limiti sıfırlanınca bir oturumu kendiliğinden sürdürebilir; yeter ki o oturum açık kalsın, makine uyanık kalsın ve sıfırlanma 24 saatten yakın olsun. Gerisini noctis üstlenir: limitten *önce* checkpoint alarak duraklar, Claude Code kapatılmışsa ya da sıfırlanma günler sonraysa (haftalık limit) oturumu zamanlanmış bir görevden yeniden başlatır ve bir `TASKS.md` kuyruğunu yürütmeye devam eder. Claude Code'un kendi devamı tetiklenirse noctis kendi yeniden başlatmasını iptal eder.

<p align="center"><img src="docs/demo.svg" alt="Noctis'le bir gece: 5 saatlik limitten önce duraklar, sıfırlanmayı aynı tur içinde bekler, TASKS.md'de ilerlemeye devam eder ve kuyruk boşalınca temizce durur" width="100%"></p>

## Makinenizde neyi değiştirir — nasıl geri alınır

<details>
<summary>Noctis'in yazdığı her ayar anahtarı, ağda neyle konuştuğu, binary'lerin nasıl denetlendiği ve her birinin geri alma yolu.</summary>

Kurulumdan sonraki ilk oturumdan itibaren — siz setup'ı çalıştırmadan önce — noctis durum çubuğunu zaten kendine yöneltir (daha önce kullandığınız durum çubuğu arkasında çalışmaya devam eder; bu düzenleme için yedek alınmaz) ve limitleri varsayılan eşiklerle korumaya başlar. Setup ardından `settings.json.bak-<zaman>` yedeğini yazar ve tam olarak şunlara dokunur:

| Nerede | Ne | Geri alma |
|---|---|---|
| `settings.json` → `statusLine` | Durum çubuğunu eklentinin binary'sine yöneltir — Claude Code kullanımınızı böyle bildirir. Zaten kullandığınız durum çubuğu arkasında çalışmaya devam eder. | `noctis install --uninstall` |
| `settings.json` → `permissions.defaultMode` | Hesaptaki ilk setup bunu `auto` yapar (eski Claude Code'da `acceptEdits`). **Claude size sormadan dosya düzenler ve komut çalıştırır** — siz uyurken çalışabilmesini sağlayan budur. Sonraki bir setup çalışması, `--permissions` vermedikçe modu olduğu gibi bırakır — ister siz değiştirmiş olun, ister `--permissions keep` ile korumuş. Bir duraklamadan sonraki yeniden başlatma, oturumun çalıştığı modda çalışır (`resume.permissionMode: inherit`); önceki bir sürümün yazdığı `config.json` oradaki `auto` değerini, siz değiştirene ya da setup'ı `--permissions keep` ile çalıştırana kadar korur. Asla `bypassPermissions` değil: gözetimsiz yeniden başlatma, bir config istese bile o modu reddeder. | Setup'ta `--permissions keep`; setup'ın geri dönüş için söylediği `--permissions` değeri; `noctis install --uninstall` |
| `settings.json` → `model`, `env.CLAUDE_CODE_EFFORT_LEVEL` | Varsayılan model ve effort = profilinizin *kod* rolü (varsayılan olarak **Opus 5.5 · max**). Kendi seçtiğiniz Fable ya da Opus korunur; setup'ın daha önce yazdığı model profile uyar. Fable'ın tavanı varsayılanı Fable ya da Opus olan bir yedek modele geçirmişken çalıştırılan setup o modeli de korur ve yalnızca effort'u yazar; kod rolünüz o modeli belirtmiyorsa Fable sıfırlanınca setup'ın geçiş olmasaydı seçeceği model gelir: kod rolünün modeli ya da geçişten önce kendi seçtiğiniz Fable veya Opus. | Setup'ta `--no-model` modelinize dokunmaz (effort yine yazılır); `noctis install --uninstall` |
| `settings.json` → `model`, `env.CLAUDE_CODE_EFFORT_LEVEL`, çalışırken | Yalnızca Fable kullanıyorsanız: Fable'ın modele özel haftalık kotası bitince (noctis'in Türkçe çıktısında bu kotaya "kapsamlı" denir) ikisi de yedek role geçer, sıfırlanınca geri döner. Claude Code `model_not_found` bildirirse `model` yedek modele ayarlanır ya da silinir. | — |
| `settings.json` → `env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` | Eksikse `1` yapılır; böylece Claude Code [yalın sıkıştırma](#yalın-sıkıştırma) modülünü yükler. Erken erişimdeki bir anahtardır ve yalnızca noctis'in değil, kurulu **her** eklentinin hook modülünü yükler. Sizin kendi koyduğunuz bir değere dokunulmaz. | Setup'ta `--no-lean` (yalın sıkıştırmayı da kapatır); `noctis install --uninstall` |
| `~/.claude/noctis/` | Kendi `config.json`'ı, durum, kullanım anlık görüntüleri, checkpoint'ler, kuyruklar, günlükler | `noctis install --uninstall --purge`; `--purge` olmadan kaldırma klasörü bırakır ve yolunu yazar |
| Zamanlanmış görev, yalnızca bekleyen bir devam varken | Task Scheduler `Noctis-…` (PC'yi uyandırabilir), launchd `com.synex.noctis.…`, systemd `noctis-…` (ikisi de duraklayan oturumun PATH, ekran, sertifika ve proxy değişkenlerini alır; böylece yeniden başlatma `claude`'u o oturum gibi bulur. Parola içerebilen proxy değişkenleri çalıştırıcıya hiçbir komut satırından ya da plist'ten değil, `~/.claude/noctis/proxies/` içinde yalnızca sizin okuyabildiğiniz bir dosyadan ulaşır) — bunların hiçbiri yoksa arka planda bir `noctis sleeper` süreci (makineyi uyandıramaz, bilgisayar yeniden başlatılınca kaybolur). Zamanlanmış görevin yanında arka planda bir `noctis sleeper --watch` süreci de devam saatine kadar 5 dakikada bir erken sıfırlanma olup olmadığına bakar (`wait.earlyResetPollMinutes: 0` bunu kapatır). | `noctis cancel`; `noctis install --uninstall` bekleyenlerin hepsini iptal eder; `"alarm": {"wakePc": false}` uyandırmayı kapatır |
| Marketplace otomatik güncellemesi | Yeni sürümlerin kendiliğinden gelmesi için bir kez `/plugin` → Marketplaces → noctis → Enable auto-update ile açın. Setup da açmayı dener, ama güncel Claude Code sürümleri bunu reddeder; setup otomatik güncellemeyi açamadığını söylerse o menüyü kullanın. Setup açabildiği yerde bunu `~/.claude/noctis/config.json` dosyasına kaydeder. | Aynı menü → Disable auto-update; `noctis install --uninstall` onu yalnızca setup açtığını kaydettiyse önceki hâline döndürür, siz sonradan kapattıysanız dokunmaz |

Sembolik bağlantı olan bir `settings.json` — örneğin bir dotfiles deposuna — öyle kalır: setup, kaldırma ve model geçişi bağlantının gösterdiği dosyaya yazar ve o dosyanın izinlerini korur. Aynısı noctis'in kendi `config.json` dosyası ve başka bir aracın hook dosyası için de geçerlidir. noctis'in `settings.json` üzerindeki her düzenlemesi dosyadaki anahtarların sırasını ve sondaki satır sonunu korur; noctis'in eklediği bir anahtar en sona gelir.

Kaldırdıktan sonra `settings.json` içinde `permissions.defaultMode` ve `env.CLAUDE_CODE_EFFORT_LEVEL` değerlerine bakın: kaldırma, ilk setup'tan önceki değerinizi geri koyar — setup o zamandan beri kaç kez çalışmış olursa olsun (örneğin effort'u değiştiren bir profil geçişi ya da yedek role geçilmişken çalışan bir setup). İki setup çalışması arasında kendiniz ayarladığınız bir değer, sonraki bir çalışma onu değiştirdiyse geri gelmez. Son setup çalışmasından sonra değiştirdiğiniz bir değer olduğu gibi kalır; setup'ın zaten yazacağı değerde bulduğu ya da hakkında kaydı olmayan bir değer de öyle. Kaldırma ardından setup'ın kayıtlarını siler; böylece sonraki bir setup ilk kurulum gibi baştan başlar: `--permissions` vermezseniz izin modunu yeniden `auto` yapar ve o sırada kullandığınız durum çubuğunu zincirler; bir sonraki kaldırma onu geri koyar. `env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` ise yalnızca setup yazdıysa silinir.

**Ağ.** İki sunucu, artı sizin ayarladıklarınız. `api.anthropic.com` — Claude uygulamasının kendi kullandığı kullanım uç noktası, Claude Code'un zaten sakladığı giriş token'ıyla; macOS'ta token Keychain'de durur; her hesap için bir kez "erişime izin ver" sorusu beklersiniz. Bu uç noktaya yalnızca durum çubuğu yetmediğinde başvurulur: durum çubuğu 20 dakikadır sessizse, bir Fable oturumunda ve Fable'da bir alt-ajan başlarken en fazla 10 dakikada bir (Fable geçiş noktasına 8 puan kala dakikada bir), bir duraklama noktasına ya da %100'deki durdurmaya 8 puan kala 2 dakikada birden 15 saniyede bire kadar (durdurmanın yakınında `/noctis:pause` açıkken de), duraklatılmış bir oturum beklerken 5 dakikada bir (`wait.earlyResetPollMinutes: 0` bunu kapatır), ayrıca bir workflow başlatmadan önce, bir API hatasından sonra ve bir yeniden başlatmadan önce birer kez. Bir de günde bir kez GitHub'daki ham `plugin.json`, yeni sürüm var mı diye — ayrı bir süreç çeker, oturum açılışı bunu hiç beklemez (`update.check: false` kapatır). Bunlara ek olarak, ayarladıysanız webhook adresiniz ve `noctis queue import` ya da `queue.github.closeOnDone` kullandığınızda kendi `gh` CLI'ınız üzerinden GitHub. Telemetri yok.

**Ücretli kullanım kredisi harcamaz.** Eşikleriniz yanlış ayarlanmış ya da kapatılmış olsa da, `/noctis:pause` açıkken bile iş bir pencerenin %100'ünde durur; son okumalar yeterince sıçradıysa ya da bayat bir okumanın gidişatı sonraki turların %100'ü aşacağını söylüyorsa hemen öncesinde durur; çünkü o noktadan sonra taşan kullanımı hesap öder. Dinamik bir workflow aynı anda çok sayıda ajan açar ve son puanları iki kontrol arasında yakabilir; bu yüzden workflow'un harcayabileceği kullanım pencerelerinden herhangi birinde duraklama noktasına 25 puandan az kalmışsa yeni bir workflow reddedilir (varsayılanlarla bir workflow için 5 saatlik pencere en fazla %67, haftalık en fazla %64 olmalı). Fable'ın modele özel kovası, kullanım uç noktası onu bildiriyorsa ve workflow'un ajanlarını Fable çalıştırabilecekse sayılır: oturum Fable'daysa, workflow önerisinin adını verdiği bir rol Fable'daysa ya da başlatılan betik Fable'ı anıyorsa, onu args ile geçiriyorsa, başka bir workflow başlatıyorsa veya okunamıyorsa (örneğin adıyla başlatılan bir workflow). Tek istisna gözlem modudur: bu durdurmayı da uygulamaz. Taşmayı siz *istiyorsanız* `"credits": {"allowPaid": true}`; `ceiling` ve `fanOutHeadroom` gerisini ayarlar. Noctis'in yapamayacağı şey hesabın otomatik kredi yüklemesini kapatmaktır — o, Anthropic faturalandırma ayarlarınızdadır.

**Kuyruk modu bir listeyi sizin yerinize yürütür.** Proje klasöründe açık maddeleri olan bir `TASKS.md` (ya da `tasks.md`, `.claude/TASKS.md`, `docs/TASKS.md`) varsa Stop hook'u, Claude her durduğunda ona sıradaki açık maddeyi verir ve durmamasını, sormamasını söyler. Claude Code'da proje klasörü oturumun başladığı klasördür; Claude `cd` ile bir alt klasöre geçse de kuyruk oturumu sürüklemeye devam eder, alt klasörün kendi kuyruk dosyası yalnızca proje klasöründe hiç yoksa sayılır. Klasörün dışına çıkan bir bağlantı olan ya da dışarı çıkan bağlantılı bir klasörde duran kuyruk dosyası yok sayılır; `noctis queue import` da böyle bir bağlantının içinden yazmaz. Bir görev listesi `git clone` ile gelebileceği için noctis, bir oturuma listeyi yürütmesini *söylemeden* önce izin ister: o klasörde açılan her oturum şunu gösterir:

```text
☰ Bu klasördeki TASKS.md dosyasında 12 açık madde var. Bu oturumu sürüklemiyor: bir görev listesi
  depoyla birlikte gelmiş olabilir ve sürüklemek, sormadan maddeleri işlemek demek.
  Sürüklemesine izin vermek için: noctis queue trust (noctis)
```

Yeni bir oturum açılırken henüz yazdığınız bir dil olmadığı için bu bildirim `NOCTIS_LANG`, `~/.claude/noctis/config.json` içindeki `locale` ya da `LC_ALL`/`LC_MESSAGES`/`LANG` ortam değişkenlerinin dilinde çıkar; hiçbiri tanınan bir dil değilse İngilizce görünür. Windows'ta bu değişkenler çoğu zaman tanımlı değildir. Türkçe görmek için `"locale": "tr"` yazın ya da `NOCTIS_LANG=tr` tanımlayın; ikisi de dili sabitler, yazdığınız dile göre değişmez. Oturum, kuyruk yönergelerini ancak `noctis queue trust` sonrasında alır (Claude Code içinde: `!noctis queue trust`; `untrust` izni geri alır, `status` durumu söyler). **İzin verene kadar dosya hiçbir şey yaptırmaz: oturum başlarken yönerge verilmez, Claude durduğunda devam ettirilmez, checkpoint'e ve yeniden başlatma istemine madde yazılmaz, hiçbir issue kapatılmaz.** İzin, verdiğiniz anda dosyada olan maddeleri kapsar: bir maddeyi işaretlemek izni bozmaz, ama sonradan eklenen ya da metni değişen açık bir madde (bir pull ya da dal değişimi getirebilir) siz yeniden izin verene kadar dosyanın sürüklemesini durdurur ve noctis kaç maddenin yeni olduğunu söyler (`status` onları listeler). 30 gün kullanılmayan izin silinir. İzni yalnızca siz verebilirsiniz: Claude bir Bash ya da PowerShell çağrısında `noctis queue trust` çalıştırırsa PreToolUse hook'u çağrıyı reddeder ve size söyler; sizin yazdığınız `!` komutu ise hook'lardan geçmeden çalışır. Noctis'in sizin isteminizden yazdığı listeye izin gerekmez. 5.4 öncesi sürümlerdeki gibi her dosyaya izin vermek için: `"queue": {"requireTrust": false}`. Hiç kuyruk olmasın: `"queue": {"enabled": false}`; `TASKS.md` kuyrukları olmasın ama uzun istemlerden çıkan listeler kalsın: `"queue": {"files": []}`.

**Binary'ler.** `bin/` içindeki altı hazır binary, her push'ta CI tarafından kaynaktan yeniden derlenir; commit'lenmiş olanlarla bayt bayt aynı değilse build başarısız olur. Sürüm indirmeleri GitHub build-provenance attestation'ları taşır (`gh attestation verify noctis-linux-amd64 --repo synex1437/noctis`). Go kodu yalnızca standart kütüphaneyi kullanır; binary'ler kod imzalı değil, notarize de edilmemiş ([ne anlama geldiği](#kurulum-ayrıntı)).

**Kapatma.** Bir süreliğine: `/noctis:pause 120` — 120 dakika boyunca (varsayılan 60) limit duraklaması, araştırma yönlendirmesi ve kuyruk devamı olmaz; %100'deki durdurma yine geçerlidir. Önceden kurulmuş beklemeler kalır: kendiliğinden devam edecek olanları, her birini iptal eden `noctis cancel --sid <id>` komutuyla listeler. `/noctis:resume` erken bitirir. Yalnızca izlesin: `~/.claude/noctis/config.json` içinde `"mode": "observe"` (aşağıda 5. adım). Tamamen, Claude Code içinde:

```
!noctis install --uninstall --host claude  # bekleyen devamları iptal edin, settings.json'ı geri yükleyin — bunu sonraki
                                           # satırdan ÖNCE çalıştırın, çünkü eklentiyi kaldırmak, düzenlemeleri geri alacak binary'yi de siler
/plugin uninstall noctis
```

Kaldırma, hesabın bekleyen tüm devamlarını zamanlanmış görevleriyle birlikte `noctis cancel` gibi iptal eder ve kaç tane olduğunu söyler. `~/.claude/noctis/` klasörünü (config, durum, kullanım verisi, checkpoint'ler, kuyruklar, günlükler) bırakır ve yolunu yazar: klasörü kendiniz silin ya da `--purge` ekleyin, kaldırma o klasörü de siler — yalnızca onu; bağlantı olan bir klasörü ve içindeki bir bağlantının gösterdiği hiçbir şeyi silmez.

Klon kurulumunda ilk satır yerine terminalde `scripts/install.sh --uninstall` (macOS, Linux) ya da `scripts\install.ps1 -Uninstall` (Windows; `--purge` için `-Purge`) çalıştırın (hangi araç olduğunu sorar; `--host claude` / `-Tool claude` soruyu atlar). Kaldırma "… okunamıyor" diyerek durursa hiçbir şey silinmemiştir: eklentiyi kaldırmadan önce o dosyayı düzeltin ya da ayarları bir sonraki paragrafta anlatıldığı gibi elle geri alın.

Eklenti kaldırılmış ama ayarlar yerinde duruyorsa elle geri alın: `~/.claude/settings.json` içinden `permissions.defaultMode`, `model`, `env.CLAUDE_CODE_EFFORT_LEVEL`, `env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` ve `statusLine` bloğunu silin (noctis'in yerini aldığı durum çubuğu, önceden kullandığınız ya da kurulumdan sonra ayarladığınız, `~/.claude/noctis/config.json` içinde `statusline.chainCommand` olarak saklanır) ya da bir `settings.json.bak-<zaman>` kopyasını geri yükleyin — setup her çalışmadan önce bir yedek alır, yani en eskisi orijinalinize en yakın olandır (noctis'in durum çubuğunu zaten içeriyor olabilir). Önemli olan `permissions.defaultMode`: geride kalırsa sonraki her oturumda Claude size sormadan dosya düzenler ve komut çalıştırır.

</details>

## İlk beş dakika

1. `/reload-plugins` sonrası pencerenin altına bakın: `∞ 5sa %41→14:35 · Hf %23▲→Pzt 21.09 09:00 · Opus 5.5/max · ctx %37` — iki kullanım penceresi ve her birinin sıfırlanma zamanı, model ve effort, bağlamın ne kadar dolu olduğu. *limit verisi bekleniyor* yazıyorsa bir mesaj gönderin; ilk yanıttan sonra dolar. (Bu oturumda Türkçe bir şey yazana kadar çubuk `NOCTIS_LANG`, `locale` ya da sistem dilini izler; bunlardan hiçbiri tanınan bir dil değilse — Windows'ta çoğu zaman böyledir — `5h … Wk … Mon … ctx 37%` ve *waiting for limit data* görürsünüz. `NOCTIS_LANG` ya da sabit bir `locale` varsa yazdığınız dil çubuğu değiştirmez.)
2. `/noctis:status` yazın: kullanım, duraklama noktaları, hangi modelin hangi işi yaptığı ve eklentinin son kararları.
3. Aşağıdaki üç satırlık `TASKS.md`'yi yazın, listenin oturumu yürütmesine izin vermek için Claude Code içinde `!noctis queue trust` yazın (`!` onu kabuk komutu olarak çalıştırır) ve Claude'a "TASKS.md'deki maddeleri sırayla yap" deyin. Maddeleri kendi kendine işaretlemesini izleyin.
4. Limitte yapmanız gereken bir şey yok. Yaklaşık 5½ saat içindeki bir sıfırlanma tur içinde beklenir, bağlam korunur. Daha geç bir sıfırlanmada iş kaydedilir ve aynı oturum yeni bir terminalde `claude --resume` ile sürdürülür — Windows Terminal sekmesi (ya da bir konsol penceresi), macOS'ta Terminal penceresi, Linux'ta masaüstü terminaliniz. macOS ve Linux'ta hiçbiri açılamazsa oturum pencere açılmadan çalışır, çıktısı `resume-output.log`'a yazılır; Windows'ta pencere açılamazsa bu, çalıştırmanız gereken `claude --resume` komutuyla birlikte bildirilir. Windows ve Linux'ta görev makineyi uyandırma isteğiyle kurulur (Linux'ta bunun için `CAP_WAKE_ALARM` gerekir); launchd Mac'i uyandıramaz, Mac'in gece boyunca uykuya geçmemesini sağlayın.
5. Güvenmeye hazır değil misiniz? `~/.claude/noctis/config.json` içinde `"mode": "observe"` her limit, kuyruk ve yönlendirme kararını kaydeder, hiçbirini uygulamaz — %100'deki durdurmayı bile; bu yüzden izlerken Anthropic faturalandırma ayarlarınızdan otomatik kredi yüklemesini kapatın. Gözlem modunda üç şey yine olur: lite ve digest alt-ajanlarının yazma sınırları geçerli kalır, bir `model_not_found` hatasında `model` yine değiştirilir ve `queue.github.closeOnDone` issue'ları yine kapatır.

## Kuyruk dosyası

<details>
<summary>`TASKS.md` biçimi; öncelikler, etiketler, bağımlılıklar ve GitHub issue'ları.</summary>

`claude`'u başlattığınız klasöre, her satıra bir iş:

```markdown
- [ ] kayıt formuna girdi doğrulaması ekle
- [ ] ödeme modülü için testleri yaz
- [ ] yeni CLI bayrakları için README'yi güncelle
```

Biçimin tamamı bu. Claude ilk açık maddeyi alır, bitince `- [x]` işaretler ve sormadan diğerine geçer; bütün maddeler işaretlenince durur, noctis listeyi ilerletmek zorunda kaldıysa (kendi yazdığı listelerde her zaman) durmadan önce `✔ Kuyruk bitti` der. Dağınık listeler kabul edilir (`-[ ]`, `* [ ]`, `1. [ ]`, `[]`, `TODO:`), birkaç satıra yayılan madde tek maddedir. Çitli bir kod bloğunun (```` ``` ```` ya da `~~~`) içindeki satırlar madde değil, örnektir. Onay kutulu bir listede kutusu işaretlenmiş madde bitmiştir (`[x]`, `[X]`, `[✓]`, `[✔]`). Kutusuz düz bir madde listesi de olur: orada `~~üstü çizili~~`, `(done)`, `(bitti)`, `(tamam)`, `✓` ve `✔` maddeyi bitmiş sayar ve Claude'dan önce listeyi onay kutularıyla yeniden yazması istenir.

Öncelik ve bağımlılık:

```markdown
- [ ] (P0) giriş yönlendirmesini düzelt #auth
- [ ] (P1) kullanıcıları taşı (after #auth, #db)
- [ ] yayına al (after 2)
```

`(P0)`–`(P9)` sırayı belirler (varsayılan P5, küçük olan önce), `#ad` maddeyi etiketler. Etiketler ASCII'dir — bir harf, ardından harf, rakam, `_` ya da `-`: `#giriş` `#giri` olarak okunur; `(after #giriş)` ise hiçbir etikete karşılık gelmez ve yok sayılır. `#giris` yazın. `(after #etiket)` ya da `(after 3)` referans verilen maddeler işaretlenene kadar bekletir; `(after 2)` dosyadaki 2. maddedir. `(after #12)` ya da `(after sahip/depo#12)` o GitHub issue'sunun maddelerini bekler; bir maddenin kendisine verdiği referans yok sayılır. Stop hook'u Claude'a önceliği en yüksek uygun maddeyi verir, kaç maddenin hâlâ beklediğini söyler ve hepsi tıkalıysa ya da bittiyse temizce durur. Hiçbir etikete, issue'ya ya da madde numarasına karşılık gelmeyen referans yok sayılır, yani bir yazım hatası geceyi asla kilitlemez; böyle yazılmışsa (`#etiket`, `#12`, `sahip/depo#12` ya da bir sayı) düzeltebilmeniz için adı da söylenir: `noctis queue status` ve `noctis queue trust` onu listeler, Stop hook'u da oturum başına bir kez, Claude'a verdiği notta ve size gösterdiği uyarıda adını verir (en fazla beş tane, gerisini sayı olarak). Parantez içindeki başka sözcüklere, örneğin `(after the release)`, dokunulmaz.

`noctis queue import` açık GitHub issue'larını `gh` CLI üzerinden `- [ ] (P1) #123 Başlık` olarak ekler (`--repo sahip/depo` verilirse `sahip/depo#123`) — öncelik `P0`–`P9` ya da `priority: high` etiketlerinden gelir, tekrar çalıştırmak güvenlidir. Yalnızca sizin açtığınız issue'ları (`gh`'nin deponun sunucusunda giriş yaptığı hesap) ya da `--author kullanıcı1,kullanıcı2` ile yalnızca o yazarlarınkini alır (`@me` o hesabı belirtir); böylece herkese açık bir depoda bir yabancının issue'su asla kuyruk maddesi olmaz; kaç tanesini, kimlerinkini dışarıda bıraktığını söyler. Ayrıca `queue.github.closeOnDone`, issue'nun referansıyla başlayan maddelerin hepsi işaretlenince issue'yu kapatır; bir maddenin ortasında geçen `#123` hiçbir şeyi kapatmaz. `gh` komutunun geri çevirdiği bir kapatma (giriş yapılmamış, ağ yok) Claude sonraki kez durduğunda yeniden denenir, toplamda üç kez; sonra bırakılır ve `gh` komutunun gerekçesi `errors.log` dosyasına yazılır.

</details>

## Siz yokken ne yapar

<details>
<summary>Durumlara göre: limitler, erken sıfırlanmalar, kuyruk duruşları, kota geçişleri ve tek başına çözdüğü arızalar.</summary>

| Durum | Ne olur |
|---|---|
| Kullanım limite yaklaşır (%92 / %89) | Son istek, dokunulan dosyalar, `git status`, todo'lar ve sıradaki kuyruk maddeleri checkpoint'e yazılır ve tur duvara çarpmadan **önce** duraklar. Yaklaşık 5½ saat içindeki bir sıfırlanma yerinde beklenir; daha geç bir sıfırlanmada iş kaydedilir ve zamanlanmış bir görevle sürdürülür — Task Scheduler (PC'yi uyandırabilir), launchd ya da systemd. İş devam ettiğinde masaüstü bildirimi ve ayarlıysa webhook gelir. Duraklama eşikten değil de başka bir nedenden geliyorsa — günlük bütçe, ani yükseliş ya da yakım hızı öngörüsü, veri yokluğu, yaklaşan bağlam sıkıştırması ya da ücretli kredi tavanı — bildirimi bunu söyler: `Duraklama nedeni: ani yükseliş öngörüsü.` |
| Sıfırlanma planlanandan erken gelir | Bekleme bunu dakikalar içinde fark eder: 5 dakikada bir yeniden bakar ve duraklatılan pencere hem duraklama noktasının hem de duraklama anındaki seviyesinin en az 10 puan altına indiği anda devam eder. `⚡ 5sa limiti planlanandan önce sıfırlandı`. Hiç kullanım verisi olmayan bir limit hatasından sonra zamanlanmış yeniden başlatma veri bekler: 10, 10, 20, 30 ve 45 dakika arayla bakar, sonra bir bildirimle vazgeçer; açık bir oturum ise bunun yerine 10 dakika sonra yerinde uyandırılır. Bekleme sırasında Claude oturum açmasının süresi dolarsa Claude Code yeniden giriş yapana kadar kullanım okunamaz: bu size bir kez bildirilir ve bekleme yine planlanan saatte biter. |
| Claude "şimdi diğer işe geçiyorum" deyip durur | Kuyruk modunda Stop hook'u ona sıradaki işaretsiz maddeyi verir, onay sormaz, kuyruk boşalınca temizce durur. |
| Birkaç iş içeren uzun bir istem yapıştırırsınız | Noctis listeyi kendisi yazar, kendi klasöründe tutar ve yürütür: `☰ 5 adımlık iş algılandı`. Claude oradaki bir maddeyi işaretlemeden önce Claude Code izin isteyeceği için noctis yalnızca bu isteği kendisi onaylar. Kısa istemler ve tek işler olduğu gibi bırakılır; tamamen kapatmak için `queue.auto: false`. |
| Fable'ın haftalık tavanı dolar (yalnızca Fable'ı siz seçtiyseniz) | Varsayılan model ve effort yedek role geçer. İş ortasındaysa oturum checkpoint'lenir ve yeni bir pencerede yedek modelle yeniden başlatılır; yeni bir istem ise `/model` ile geçip yeniden göndermenizi söyleyen bir notla bekletilir. Fable'a sabitlenmiş ya da Fable'da çalışması istenen alt-ajanlar da yedek modeli kullanır; başka bir limiti bekleyen oturum da o sırada Fable'ın tavanı hâlâ doluysa yedek rolle (model ve effort) geri gelir. Sıfırlanınca model ve effort `settings.json` dosyasında önceden ne yazıyorsa ona döner, önceden yazmıyorsa yeniden silinir; bu arada sizin seçtiğiniz bir model ya da effort ve çalıştırdığınız setup olduğu gibi kalır. |
| İstem araştırma ya da yazı işi | `noctis:lite` alt-ajanına gider — varsayılan olarak Opus 5.5 · xhigh, `economy`'de Sonnet 5 — böylece ana oturum bağlamını kod için ayırır. Yalnızca metin belgeleri kaydedebilir; `CLAUDE.md`'ye, kuyruk dosyalarına, `.claude/` altına, hesap klasörüne (`CLAUDE_CONFIG_DIR`) ve eklentinin kendi klasörüne asla yazamaz. Dosya arama (Explore) ve uzun test ve build çıktıları için `noctis:digest` alt-ajanı Haiku 4.5'te çalışır. |
| 429 hatası turu yine de öldürür | Sıfırlanma yaklaşık 5½ saat içindeyse (`wake.maxMinutes`) oturum sıfırlanmada **yerinde** uyandırılır, zamanlanmış yeniden başlatma emniyet ağıdır; daha geç bir sıfırlanmada yalnızca zamanlanmış yeniden başlatma olur. Claude Code'un kendi hata metni yüzdelerden üstün tutulur. Claude Code'un kapanırken kancayı sonlandırdığı `claude -p`, Agent SDK ve GitHub Action çalıştırmalarında kanca hatayı noctis'in ayrık bir kopyasına devreder; yeniden denemeyi ve yeniden başlatmayı o kopya kaydeder. |
| API aşırı yüklenir (529 / 5xx) | Ölmek yerine büyüyen aralıklarla bekler: 30 sn → 5 dk, iki saatlik bütçe, tek bilgi mesajı, kesinti gerçekse bir tane daha. |
| Kullanım verisi limite yakın kesilir | Tahmin yürütmek yerine durur: yakım hızı öngörüsü, ani yükseliş tahmini, kör nokta yoklaması ve duraklama noktasına iki puan kala 15 saniyelik yenileme; eşiği kapatılmış bir pencerede %100'deki durdurmaya yaklaşırken de hepsi geçerlidir. Taze veri yeniden yer olduğunu gösterince duraklama erken biter. |
| Bir pencere %100'de ya da onu aşmak üzere | Eşikler ne derse desin iş orada durur; son okumalar yeterince sıçradıysa ya da bayat bir okumanın gidişatı sonraki turların onu aşacağını söylüyorsa hemen öncesinde durur; duraklatma bunu kaldırmaz (gözlem modu hariç): o noktadan sonra taşan kullanımı hesap öder. |
| Görev çok parçalı iş (fan-out) gerektirir ("her endpoint'i taşı") | `workflow.suggest` açıksa (varsayılan olarak kapalıdır) ve o an bir başlatmaya izin verilecekse, size bunun rol profilinizin modelleriyle bir **dinamik workflow** olarak çalışabileceği, isterseniz ultracode ile istemeniz söylenir; Claude'a asla bir workflow başlatması söylenmez. Uyarı bandındaysa ya da workflow'un harcayabileceği pencerelerden birinde (Fable kovası yalnızca ajanlarını Fable çalıştırabilecekse) duraklama noktasına 25 puandan az kalmışsa yeni başlatma reddedilir; her başlatma kaydedilir ve duraklamadan sonra Claude'a aynı koşuyu yeniden başlatması söylenir, böylece biten ajanlar kayıtlı sonuçlarını döndürür. Claude Code'da çalışmakta olan ajanlar da duraklama noktasına takılır: ellerindekini raporlamaları söylenir, sonra durdurulurlar; checkpoint yarıda kalan her ajanı adıyla yazar, böylece devam eden oturum onların kalan işini yeniden yapar. |
| Haftalık kotanın %99'unda kurarsınız | Setup yine çalışır: `/noctis:setup`, `:status`, `:pause` ve `:resume` duraklama noktasından geçer (%100'deki durdurmadan geçmez), ama setup'ın sorusuna verdiğiniz yanıt sıradan bir istemdir; bu yüzden `--profile` verin ya da önce `/noctis:pause 120` çalıştırın. İlk oturum işin ne zaman devam edeceğini söyler, ilk istem sıfırlanmaya kadar park edilir (yine de çalışmak için `/noctis:pause 120`), `noctis check` durumu betiklere bildirir. |
| Almanca, Japonca, Türkçe… yazarsınız | `locale: auto` (varsayılan) iken bildirimler, durum çubuğu ve oturumun kendi bildirim balonları yazdığınız dili izler; dil model çağrısı olmadan algılanır. Bir şey yazmadan önce sistem dili geçerlidir (`LC_ALL`, `LC_MESSAGES` ya da `LANG`; hiçbiri tanınan bir dil değilse İngilizce). `NOCTIS_LANG` ya da sabit bir `locale` bunun yerine tek bir dili sabitler. Zamanlanmış bir yeniden başlatmanın bildirim balonları `NOCTIS_LANG`, `locale` ya da sistem dilini kullanır. On dördü de eksiksizdir — bir dilde mesaj eksikse, çeviri İngilizceden farklı argüman alıyorsa ya da `noctis status` etiketleri hizadan kayarsa CI başarısız olur. Claude'a verilen `[noctis]` yönergeleri her dilde İngilizce kalır. |
| Oturum beklerken dosyalar değişir | Checkpoint çalışma ağacının parmak izini tutar: `git status`, `HEAD`'in gösterdiği commit ve en fazla 2000 yolun boyutu ile değişme zamanı: önce `git status`'un listelediği dosyalar, sonra izlenmeyen olarak listelenen bir klasörün içindekiler. Devam ederken bunlardan biri farklıysa — oturumun zaten değiştirdiği bir dosya yeniden düzenlenmişse, bir commit ya da pull yapılmışsa — Claude'a, duraklamadan önce okuduğu ya da düzenlediği dosyaların değişmiş olabileceği söylenir (`wait.workspaceGuard`). `checkpoint.gitSnapshot`, izlenen dosyalardaki commit'lenmemiş değişiklikleri gizli bir git ref'ine sabitleyebilir. İkisi de `.git/index.lock` almaz; bu yüzden başka bir oturumun ya da ajanın o anda çalıştırdığı `git add` veya `git commit` geri çevrilmez, noctis'in süre sınırında kestiği bir `git status` ya da anlık görüntü de arkasında `.git/index.lock` bırakmaz; kesilen bir anlık görüntü geçici bir index dosyası da bırakmaz. |
| İki şey aynı oturumu sürdürmeye kalkar | Yalnızca bir yeniden başlatma olur; oturum bir kilit altında sahiplenilir. |
| Uzun bir bekleme biter | Oturum görebileceğiniz bir yerde döner — Windows Terminal sekmesi (ya da bir konsol penceresi), macOS'ta Terminal penceresi, Linux'ta masaüstü terminaliniz. macOS ve Linux'ta hiçbiri açılamazsa pencere açılmadan arka planda çalışır, çıktısı `resume-output.log`'a yazılır; Windows'ta pencere açılamazsa bu, çalıştırmanız gereken `claude --resume` komutuyla birlikte bildirilir. Noctis'in o oturum için daha önce kendi açtığı pencere, son beş dakikada kullanılmadıysa önce kapatılır; sizin başladığınız pencere açık kalır, `↪ başka bir pencerede sürüyor` gösterir ve yeniden başlatılan oturum çalıştığı sürece yeni istemleri reddeder. Oturum yanıt vermeden biten bir yeniden başlatma — herhangi bir yanıttan önce hatayla çıkarsa ya da penceresi oturuma dokunmadan 30 saniye içinde kapanırsa — beklemeyi korur ve `wait.retryMinutes` adımlarıyla yeniden denenir; bu türden beşinci yeniden başlatma, proje klasörünü ve orada çalıştırılacak `claude --resume` komutunu söyleyen bir bildirimle bırakılır. Hiç başlayamayan bir yeniden başlatma — aracın komutu `PATH` içinde yoksa ya da proje klasörü artık yoksa — önce devam edildiğini duyurmaz, nedenini söyleyen o tek bildirimi gönderir. |
| Terminal kapanır, süreç öldürülür, dosya yarım yazılır | Motor kendini onarır: bozuk `state.json`/`usage.json` yedekten döner, süreci ölmüş bir devir artık oturumu tıkamaz, runner'ı ortadan kalkan bir bekleme (bilgisayar yeniden başladıysa, kullanıcı oturumu kapandıysa ya da sleeper öldürüldüyse) sonraki istemde, durum çubuğu yenilemesinde ya da oturum açılışında yeniden kurulur, artakalan geçici dosyalar ve kilitler temizlenir. |
| Bağlam dolar | Claude Code'un fonksiyon hook'ları açıksa, bağlam %70'teyken biten bir tur o anda sıkıştırılır; istem önbelleği de kapalıysa özet, eski araç çıktıları budanmış bir konuşmadan yazılır ([Yalın sıkıştırma](#yalın-sıkıştırma)). Fonksiyon hook'ları açık değilse durum çubuğu `ctx %72▲` gösterir ve sonraki istem, `/compact` yazmanızı söyleyen tek bir bildirim alır. |
| Bir eşik geçersiz bir değere ayarlanır | O pencereyi yerleşik varsayılan eşik korur; `noctis status`, `noctis doctor` ve sonraki oturum geçersiz eşiği adıyla bildirir, `config.json`'daki değeri sizin düzeltmeniz gerekir. Bir pencereyi bilerek korumasız bırakmak için eşiği `0`, `null` ya da `false` yapın; yine de %100'de durur. |
| Yeni sürüm yayımlanır | Marketplace otomatik güncellemesi açıksa Claude Code onu indirir ve sonraki oturum bir kez `⬆ noctis <yeni> indirildi (bu oturum hâlâ <mevcut> çalıştırıyor): geçmek için /reload-plugins çalıştır` der. Değilse tek satırlık bir bildirim sürümü ve komutu söyler. |

<p align="center"><img src="docs/flow.svg" alt="Korumadan geçen tek bir araç turu: sinyaller hook'lara girer, deterministik kurallar sonucu seçer" width="100%"></p>

</details>

## Yalın sıkıştırma

Claude Code bir konuşmayı sıkıştırdığında — `/compact`, kendi otomatik sıkıştırması ya da aşağıdaki erken sıkıştırma — noctis özetin yazılacağı metni budayabilir; böylece özet isteği daha az şey okur. Bunu yalnızca Claude Code'un istem önbelleği kapalıyken (`DISABLE_PROMPT_CACHING`) yapar: önbellek açıkken özet isteği konuşmayı istem önbelleğinden okur ve bunun bedeli giriş fiyatının onda biri ya da daha azıdır (Opus 5.5'te yirmide biri); budanmış bir satır ise ondan sonraki her şeyi en az tam giriş fiyatına çıkarır, bu yüzden noctis konuşmayı olduğu gibi aktarır. Kendisi hiçbir model çağrısı yapmaz; özeti yine Claude Code yazar.

- **Budananlar (yalnızca konuşmanın eski kısmında):** 2 000 karakterden uzun bir araç sonucu ya da araç girdisi, ilk ve son 1 000 karakterini bir `[noctis: N chars trimmed]` işaretinin iki yanında tutar; aynı çağrı aynı argümanlarla sonradan yeniden çalıştıysa bir `Read`, `Grep`, `Glob`, `LS`, `WebFetch` ya da `WebSearch` sonucu atılır; eski kullanıcı mesajlarından `<system-reminder>` blokları çıkarılır.
- **Dokunulmayanlar:** istemleriniz, Claude'un yanıtları, hangi aracın hangi argümanlarla çalıştığı (içlerindeki uzun metinler dışında) ve son 6 asistan mesajı ile onlardan sonraki her şey (`compaction.keepTurns`).
- **5 saatlik sınırın dibinde değil:** 5 saatlik pencerenin duraklama noktasına en fazla 6 puan kalmışken aşağıdaki erken sıkıştırma istenmez, çünkü özet isteği pencereyi o noktanın ötesine taşıyabilir — limit korumasının Claude Code'un kendi sıkıştırmasından önce bıraktığı pay da budur (`compaction.contextPercent`); sonraki bir tur yeniden ister. Haftalık duraklama noktasının yakınında her zamanki gibi istenir, çünkü limit koruması orada da sıkıştırma için duraklamaz.
- **Erken, turlar arasında:** bir tur bağlam %70 doluyken bittiğinde (`compaction.compactAtPercent`), noctis Claude Code'dan kendi daha geç eşiğini beklemeden hemen o anda sıkıştırmasını ister — her geçişte bir kez: ancak eşiğin altında biten bir turdan sonra yeniden ister. Bu yalnızca etkileşimli oturumda çalışır; Claude Code 2.1.281 bunu `claude -p` ve SDK oturumlarında reddeder, oralarda budama yine de `/compact`'e ve Claude Code'un otomatik sıkıştırmasına uygulanır. Claude Code'un kendi otomatik sıkıştırması kapalıyken (`autoCompactEnabled: false`, `DISABLE_AUTO_COMPACT`, `DISABLE_COMPACT`) bu da kapalı kalır.
- **Claude Code'un erken erişimdeki fonksiyon hook'larını ister:** `~/.claude/settings.json` içinde `"env": {"CLAUDE_CODE_ENABLE_FUNCTION_HOOKS": "1"}` (Claude Code 2.1.281 ile denendi; 2.1.251 bu anahtarı tanımaz); eksikse setup yazar, kaldırma geri alır. Bu anahtar yalnızca noctis'in değil, kurulu her eklentinin hook modülünü yükler. `/noctis:setup --no-lean` yalın sıkıştırmayı kapatır (`compaction.lean: false`; sonraki setup çalışmaları bunu korur) ve anahtarı setup yazdıysa geri alır; sizin koyduğunuz bir değer kalır. Anahtar olmadan noctis eskisi gibi çalışır; sıkıştırma yalnızca Claude Code'undur. Böyle bir oturumda bağlam %70'e ulaşınca durum çubuğu `ctx %72▲` gösterir ve sonraki istem, `/compact` yazmanızı söyleyen tek bir bildirim alır. 2.1.281'den eski bir Claude Code'da bu bildirim setup yerine sürümü söyler; son oturum modül olmadan böyle bir sürümde çalıştıysa `noctis doctor` da o sürümü işaretler.

noctis'in budadığı her sıkıştırma `~/.claude/noctis/compact.log` dosyasına bir satır ekler; `noctis status` bunları sayar, `noctis why` kararlarının arasında listeler. Kapatmak için: `~/.claude/noctis/config.json` içinde `"compaction": {"lean": false}`; diğer anahtarlar [docs/REFERENCE.md](docs/REFERENCE.md) içinde. Türü ya da aralığı yanlış bir anahtar eklentiyle gelen değerine döner; `noctis status` ve `noctis doctor` onu adıyla söyler.

## Diğer eklentilerle yan yana

Claude Code'da noctis hook ve durum çubuğu ekler; kimseninkini silmez, yeniden yazmaz (Antigravity CLI'da ise sizin özel durum çubuğunuzun yerine geçer). Claude Code bir olayın tüm hook'larını çalıştırır, yani bir döngü eklentisiyle noctis'in kuyruğu aynı turu birlikte itebilir — zararsız, ama çift devam görürseniz birini duraklatın. Zaten kullandığınız durum çubuğu (ccstatusline, claude-powerline) noctis'inkinin arkasında çalışmaya devam eder; kullanım panoları (ccusage, Claude-Code-Usage-Monitor) aynı dosyaları okur, etkilenmez. Limitten sonra oturumu kendisi sürdüren başka bir araç (unsnooze, claude-auto-resume) noctis'le birlikte çalışırsa ikisi yarışır — yalnızca birini kullanın. `noctis doctor` görebildiği komşuları listeler.

## Sizin dilinizde

<p align="center"><img src="docs/languages.svg" alt="Aynı duraklama ve kuyruk-bitti bildirimleri İngilizce, Türkçe, Almanca, İspanyolca, Japonca ve Rusça" width="100%"></p>

## Diğer yapay zekâ kodlama araçları

Aynı motor, daha az özellikle **OpenAI Codex CLI**, **Antigravity CLI** (Google), **Factory Droid** ve **GitHub Copilot CLI** içinde de çalışır: Codex ve Antigravity limitten önce duraklar, checkpoint alır, bekler ve yeniden başlatır (Antigravity ayrıca bir kota hatasından sonra yeniden dener); Copilot kuyruk modunu ve bir hız limiti hatasından sonra checkpoint ile yeniden denemeyi alır; Droid yalnızca kuyruk modunu alır. Hiçbirinde model yedeği, araştırma yönlendirmesi ya da aynı oturumda uyandırma yoktur. Klondan ya da GitHub ZIP'inden (Code → Download ZIP) `./scripts/install.sh` (macOS, Linux) veya `.\scripts\install.ps1` (Windows) hangi araç olduğunu sorar — ya da `--host codex` / `-Tool codex` verin — sonra o aracın hook dosyasını yazar ve oturumları aracın kendi komutuyla sürdürür. Bu adaptörler araçları taklit eden sahte programlarla test edilir; Claude Code, Codex ve Copilot'un komut satırı bayrakları da her hafta gerçek CLI'lara karşı denetlenir; bu adaptörlerin hiçbiri henüz gerçek bir oturumu baştan sona çalıştırmadı. Ayrıntı: [docs/REFERENCE.md](docs/REFERENCE.md#other-ai-coding-tools) ve [docs/HOSTS.md](docs/HOSTS.md) (İngilizce).

## Cuma gecesi → pazartesi sabahı

<details>
<summary>Kimseye ihtiyaç duymayan bir hafta sonu: checkpoint, sıfırlanmayı bekleme, pazartesi yeniden başlatma.</summary>

<p align="center"><img src="docs/timeline.svg" alt="Zaman çizelgesi: %92'de checkpoint, bekleme, sıfırlanmadan sonra devam, haftalık limitte kaydetme, pazartesi yeniden başlatma" width="100%"></p>

O hafta sonunda size ihtiyaç duyan hiçbir şey yok. Checkpoint son isteği, dokunulan dosyaları, `git status`'u, todo'ları ve sıradaki kuyruk maddelerini tutar. Kısa bir sıfırlanma hook'un içinde beklenir, tur olduğu gibi sürer; uzun olanı sıfırlanma saatinde yeniden başlatılır — Windows ve `CAP_WAKE_ALARM` olan Linux bunun için makineyi uyandırabilir, Mac ise uyanınca yeniden başlatır.

<p align="center"><img src="docs/before-after.svg" alt="Aynı gece, noctis'li ve noctis'siz, bir çizim olarak: Claude Code tek başına 01:40'taki limiti bekler, sonra 03:20'de 48 işin 21'i bitmişken durur; noctis'le 48'in hepsi 06:55'te bitmiştir" width="100%"></p>

</details>

## Benzer araçlarla karşılaştırma

| Yetenek | noctis | kullanım panoları / durum çubukları | döngü eklentileri ("devam et") | otomatik devam betikleri |
|---|:---:|:---:|:---:|:---:|
| Duvara çarpmadan **önce** durur (eşik + ani yükseliş + yakım hızı öngörüsü) | ✅ | yalnız gösterir | — | 429'dan sonra tepki verir |
| Sıfırlanmadan sonra kendiliğinden devam eder (aynı oturum ya da günler sonra bile zamanlanmış yeniden başlatma) | ✅ | — | — | kısmen |
| Ücretli kullanım kredisi harcamak yerine %100'de durur; ölçülen boşluk yetmezse çok parçalı işi reddeder | ✅ | — | — | — |
| Kuyruğu duruşlar boyunca, onay sormadan yürütür — öncelik, bağımlılık, GitHub issue | ✅ | — | ✅ (düz) | — |
| Ayrı bir araştırma alt-ajanı (varsayılan olarak Opus 5.5 · xhigh); dosya arama ve çıktı özetleri için Haiku 4.5 | ✅ | — | — | — |
| Fable kullanıyorsanız: haftalık tavanı dolunca yedek modelinize (hazır profillerin hepsinde kod modeli) geçer, sıfırlanınca geri döner | ✅ | — | — | — |
| Deterministik, karar başına sıfır token, günlüklü (`noctis why`) | ✅ | ✅ | istem güdümlü | değişir |
| Aşırı yük (529/5xx) için limitten ayrı, jitter'lı geri çekilme | ✅ | — | — | bazıları |
| ccusage uyumlu maliyet raporu (`noctis report --json`), cron/CI için çıkış kodu kapısı | ✅ | ✅ / — | — | — |
| Kurulacak çalışma ortamı yok (kendi başına çalışan tek binary; Linux'ta hook başına ~5 ms, macOS ya da Linux'ta marketplace kurulumunun sh başlatıcısı üzerinden ~11 ms) | ✅ | değişir | değişir | değişir |
| Limit kararlarını uygulanmadan önce izlemek için gözlem modu | ✅ | — | — | — |
| Dinamik workflow: çok parçalı işlerde size önerilir (varsayılan kapalı), limite yakınken reddedilir, duraklamadan sonra kurtarılır | ✅ | — | — | — |
| Rol profili: kod, araştırma, planlama, özet, arama ve yedek için hangi modelin çalışacağı | ✅ | — | — | — |
| Yazdığınız dili izler (14 dil, hepsi eksiksiz) | ✅ | bazıları | — | — |
| Claude Code içinde çalışır — daha az özellikle Codex CLI, Antigravity CLI, Droid ve Copilot CLI içinde de | ✅ | bazıları | yalnız Claude | bazıları |

## Kurulum (ayrıntı)

<details>
<summary>Marketplace ve klon kurulumu, rol profili, bayraklar ve binary çalışmazsa ne yapılacağı.</summary>

<p align="center"><img src="docs/install.svg" alt="Yaklaşık bir dakikada kurulum: marketplace'i ekle, kur, setup'ı çalıştır (hangi model hangi işi yapsın diye sorar), yeniden yükle, sonra TASKS.md yaz ve ona izin ver" width="100%"></p>

**Marketplace'ten** — yukarıdaki dört satır. `setup` tek bir şey sorar — **hangi model hangi işi yapsın** — ve cevabı *rol profili* olarak saklar:

| Profil | kod | araştırma & yazı | planlama | özet & arama | yedek |
|---|---|---|---|---|---|
| `noctis` | Opus 5.5 · max | Opus 5.5 · xhigh | Opus 5.5 | Haiku 4.5 | Opus 5.5 · max |
| `balanced` | Opus 5.5 · high | Opus 5.5 · medium | Opus 5.5 | Haiku 4.5 | Opus 5.5 · high |
| `economy` | Opus 5.5 · low | Sonnet 5 · high | Opus 5.5 | Haiku 4.5 | Opus 5.5 · low |
| `custom` | her role bir model, effort uygulanabilen yerlerde effort | | | | |

Değiştirmek için `/noctis:setup`'ı yeniden çalıştırın, ya da soruyu `--profile noctis|balanced|economy` veya `--code opus:high --research sonnet:high …` ile atlayın; `/noctis:status` güncel dağılımı gösterir. Neden bunlar: Anthropic'in Opus 5.5 ile yayımladığı üç kodlama benchmark'ında Opus 5.5 max'ta, yine max'taki Fable 5.1'i geçer ve görev başına daha ucuzdur (Terminal-Bench 4.0'da 64,8'e karşı 55,8, FrontierCode'da 54,4'e karşı 50,3, CursorBench'te 57,8'e karşı 51,8); xhigh'da araştırmada max'taki Fable 5.1 ve Opus 5'in üstündedir (WANDR 71,3'e karşı 68,7 ve 67,2); low'da grafikteki tüm modeller arasında çözülen kodlama görevi başına en ucuzudur. Düşük effort'ta araştırma çöker (WANDR 31,2), bu yüzden `economy` araştırmayı Sonnet 5'e verir. Yedek, kod modelidir. Noctis tek bir modele özel haftalık tavanı izler: Fable'ınkini. Fable'a kendiniz geçerseniz o tavan dolunca sizi yedek modele geçirir, sıfırlanınca yeniden Fable'a döndürür.

Effort ana oturuma, araştırma alt-ajanına ve — digest rolüne effort seviyeleri olan bir model verirseniz — digest alt-ajanına ulaşır. `Plan` ve `Explore` yalnızca model alır, çünkü Agent aracının onlara verebileceği bir effort yoktur. Haiku 4.5'in ise hiç effort seviyesi yoktur; setup bunu gizlemek yerine açıkça söyler.

Setup `settings.json`'da beş düzenleme yapar: durum çubuğu, izin modu, varsayılan model, effort seviyesi ve yalın sıkıştırmanın istediği fonksiyon hook'ları anahtarı. Hepsi geri alma yollarıyla birlikte [Makinenizde neyi değiştirir](#makinenizde-neyi-değiştirir--nasıl-geri-alınır) bölümünde listelenmiştir. Bayraklar: `--permissions keep`, `--no-model` (modelinize dokunmaz; effort seviyesi yine yazılır), `--no-lean` (fonksiyon hook'ları anahtarı yazılmaz, yalın sıkıştırma kapanır), başka bir hesap için `--config-dir <dizin>` (birden çok hesap için tekrarlayın), `--preset conservative|balanced|aggressive` (%85/82/90, %92/89/95 ya da %96/94/98'de duraklama). Bütün bayraklar: [docs/REFERENCE.md](docs/REFERENCE.md#commands) (İngilizce).

**Klondan ya da GitHub ZIP'inden** (Code → Download ZIP) hesap başına bir kopya `~/.claude/skills/` altına kurulur: Windows'ta PowerShell'de `.\scripts\install.ps1`, macOS ve Linux'ta `./scripts/install.sh` çalıştırın — ikisi de önce hangi yapay zekâ kodlama aracı için olduğunu sorar — sonra `/reload-plugins`. Bu kopya 17 dosyadır (Windows'ta 18) ve 7–8 MB tutar; neredeyse tamamı platformunuzun binary'sidir. Hiçbir şey indirilmez ya da derlenmez; binary zaten depoda (`bin/<os>-<arch>/`) ve `bin/SHA256SUMS` ile uyuşmayan bir binary kurulmaz, reddedilir. Bu kopyada hook'lar binary'yi doğrudan çağırır, asla kabuktan geçmez; Windows'ta Git Bash gerekmez (Git Bash'te çalıştırılan `install.sh` durur ve `install.ps1`'i gösterir).

**Hiçbir şey olmuyorsa** — durum çubuğu yok, bildirim yok, `noctis doctor` da çalışmıyor — binary'nin çalışmasına izin verilmiyordur. Binary'ler kod imzalı değil, Apple tarafından notarize de edilmemiş; bu yüzden:

- **macOS**: tarayıcıdan inen dosya karantina bayrağı alır ve macOS onu açılır açılmaz sonlandırır. `xattr -dr com.apple.quarantine <eklenti klasörü>`, ya da bayrağı hiç koymayan `git clone` ile kurun.
- **Windows**: SmartScreen indirilen `.exe`'yi engelleyebilir — Özellikler → Engellemeyi kaldır, ya da klonlayın.
- **Zip'ten**: zip'ler çalıştırma bitini her zaman taşımaz. `chmod +x bin/noctis bin/*/noctis`.
- **Başka bir şeyse**: terminalde `bin/<os>-<arch>/noctis version` çalıştırın — yazdığı hata gerçek hatadır. Claude Code dışında setup'ın yazdığı tam yolu kullanın ya da o klasörü PATH'e ekleyin. Kaynaktan derlemek için [CONTRIBUTING.md](CONTRIBUTING.md) dosyasını izleyin (İngilizce).

VS Code ve Cursor eklentilerinde her şey çalışır; tek fark, uzun bir beklemeden sonraki yeniden başlatmanın editörün dışında olmasıdır — bir terminal sekmesinde ya da penceresinde, macOS ve Linux'ta hiçbiri açılamazsa arka planda — asla bir editör sekmesinde değil.

</details>

## Komutlar

Claude Code içinde: `/noctis:setup` (modelleri değiştirmek için yeniden çalıştırın) · `/noctis:status` (kullanım, duraklama noktaları, roller, bekleyen devamlar, son kararlar) · `/noctis:pause [dakika]` (varsayılan 60; `2 saat` gibi bir süre de yazılabilir, en fazla bir hafta: o süre boyunca limit duraklaması, yönlendirme ya da kuyruk devamı olmaz; %100'deki durdurma yine geçerlidir) · `/noctis:resume` (hemen geri açar). `/noctis:setup` ve `/noctis:pause` yalnızca siz yazınca çalışır: Claude bunları kendi başına başlatamaz.

`noctis` komutunun kendisi: Claude Code içinde başına `!` koyarak çalıştırın — `!noctis status`, `!noctis why`, `!noctis queue trust`, `!noctis off 30`, `!noctis on` — çünkü eklentinin `bin/` klasörü Claude Code'un kabuğunun PATH'indedir. Terminalde setup'ın yazdığı tam yolu kullanın ya da o klasörü PATH'e ekleyin. Her komut ve bayrak: [docs/REFERENCE.md](docs/REFERENCE.md#commands) (İngilizce).

## Durum çubuğu

<p align="center"><img src="docs/statusline.svg" alt="Durum çubuğu: 5 saatlik pencere, tempo işaretli ve ETA'lı haftalık pencere, model ve effort, bağlam" width="100%"></p>

```
∞ 5sa %41→14:35 · Hf %60▼→Pzt 28.09 09:00 · ⌛ hafta eşiği ~14sa 27dk · Opus 5.5/max · ctx %37
```

`%41→14:35` pencerenin kullanılan payı ve sıfırlanma zamanıdır; başka bir güne düşen sıfırlanmada gün ve tarih de görünür. `▲ / ● / ▼` haftalık kullanımın, haftalık duraklama noktasına giden eşit tempodan yavaş mı, tempoda mı, hızlı mı gittiğini gösterir; `▼`, bu hızla sıfırlanmadan önce o noktaya varacağınız demektir. `⌛`, sıfırlanmadan önce gelecekse, mevcut hızla bir duraklama noktasına ne zaman varılacağını söyler. Kullanım uç noktası bir Fable kovası bildirdiğinde çubuk onu da gösterir (`· Fable %60`), bir Fable oturumu ise kendi ETA'sını alır (`· ⌛ Fable ~1g 3sa 0dk`). Bir pencerenin önündeki `⚠`, o pencerenin duraklama noktasına en fazla 6 puan kaldığını belirtir; `⏸ 02:36` bekleyen devam saatini gösterir; `↪ başka bir pencerede sürüyor`, oturumu başka yerde yeniden başlatılmış bir pencereyi işaretler; `⚠ hook yok`, durum çubuğunun güncellendiği ama 30 dakikadır hiçbir hook'un çalışmadığı anlamına gelir; `ctx %72▲`, [yalın sıkıştırmanın](#yalın-sıkıştırma) çalışmadığı bir oturumda bağlamın `compaction.compactAtPercent` değerine ulaştığını söyler (`/compact` yazın); `👁 gözlem` ise gözlem modunu gösterir. Hiçbiri token harcamaz. `statusline.mode: silent` veri toplamayı sürdürür ama hiçbir şey yazmaz, ya da yalnızca zincirlediğiniz durum çubuğunu yazar.

## Referans ve sınırlar

Her komut ve bayrak, tam yapılandırma tablosu, host adaptörleri ve dosya yerleşimi [docs/REFERENCE.md](docs/REFERENCE.md) içinde (İngilizce). Her test paketinin neyi sınadığı, CI'ın neyi çalıştırdığı ve son koşunun ne ölçtüğü: [docs/TESTING.md](docs/TESTING.md) (İngilizce). Tasarım notları ve hata günlüğü: [docs/PLAN.md](docs/PLAN.md). Sürüm geçmişi: [GitHub Releases](https://github.com/synex1437/noctis/releases).

Bilinmesi gereken üç sınır. Kullanım verisi Claude Code'un durum çubuğu yükünden (`rate_limits`, Claude Code ≥ 2.1.251) ve belgelenmemiş OAuth kullanım uç noktasından gelir; bu uç nokta Fable'ın kovasını verir ve durum çubuğu sessizleştiğinde 5 saatlik ve haftalık pencereleri de yedekler — bu yüzden Claude Code güncellemelerinden sonra `errors.log`'a göz atın. Aynı oturumda uyandırma, belgelerde gözlemsel denen `asyncRewake`'e dayanır; yedeği zamanlanmış yeniden başlatmadır. Son olarak yalın sıkıştırma, Claude Code'un sürümden sürüme değişebilecek erken erişimdeki fonksiyon hook'larıyla çalışır; onlar olmadan sıkıştırma yalnızca Claude Code'undur.

## Katkı

`noctis doctor` çıktısı ve ilgili `errors.log` / `noctis why --last 20` satırlarıyla gönderilen hata raporları en işe yarar şeydir; `noctis report --bundle` bunların hepsini bir zip'e koyar (token'lar ve webhook adresleri gizlenir, ev klasörünüz `~` olarak görünür; sıfırlanmaya kadar park edilen istemler, açık görevlerin başlıkları ve `settings.json` içindeki izin kuralları çıkarılır, yalnızca uzunlukları kalır; dosya yolları ve log'larda yazanlar gizlenmez, eklemeden önce okuyun). Derleme ve test döngüsü için [CONTRIBUTING.md](CONTRIBUTING.md) dosyasına bakın (İngilizce).

## Lisans

MIT — © 2026 synex

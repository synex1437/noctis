package main

import (
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type heldLanguage struct {
	code                                                string
	forbids, bare, asks, others, excepts                []string
	fillers, conditions, comparisons, talks, qualifiers string
	relatives, infinitives, modals, speakers            string
}

var heldLanguages = []heldLanguage{
	{
		code: "de",
		forbids: []string{
			"{implementiere|implementier|implementieren sie} {nichts|noch nicht|nichts davon|davon nichts|das noch nicht|es noch nicht|sie noch nicht}",
			"{setze|setz|setzen sie} {nichts|nichts davon|davon nichts}",
			"{setze|setz|setzen sie} {das|es|sie|diese|davon} {noch nicht|nicht|nichts} um",
			"{mach|mache|machen sie} {noch nichts|nichts davon|davon nichts|das noch nicht|es noch nicht|sie noch nicht}",
			"{ändere|änder|ändern sie|verändere|verändern sie|modifiziere|modifizieren sie|bearbeite|bearbeiten sie|editiere} " +
				"{nichts|kein|keine|keinen|keines|den code nicht|die dateien nicht}",
			"{fass|fasse|fassen sie|rühr|rühre|rühren sie|berühre|berühren sie} {nichts|kein|keine|keinen|den code nicht|die dateien nicht}",
			"{schreib|schreibe|schreiben sie} {kein|keinen} code",
			"{fang|fange|fangen sie} {noch nicht|nicht} an",
			"{beginne|beginn|beginnen sie|starte|starten sie} noch nicht",
			"noch nicht {anfangen|beginnen|starten|umsetzen|implementieren|damit anfangen|damit beginnen}",
			"{nichts|nichts davon|davon nichts} {umsetzen|implementieren|ändern|verändern|modifizieren|anfassen|anrühren|bearbeiten}",
			"{keine dateien|keinen code|den code nicht|die dateien nicht} {ändern|verändern|modifizieren|anfassen|anrühren|bearbeiten|schreiben}",
			"keine änderungen {vornehmen|machen}",
		},
		asks: []string{
			"nur {schätzen|einschätzen|planen|erklären|bewerten}", "{schätze|schätz|plane|erkläre|bewerte} nur",
			"nur {eine schätzung|eine einschätzung|einen plan}", "ich {brauche|will|möchte} nur {eine schätzung|eine einschätzung|einen plan}",
			"{gib|geben sie} mir nur {eine schätzung|eine einschätzung|einen plan}",
			"ohne {zu implementieren|etwas zu implementieren|etwas zu ändern|etwas umzusetzen|code zu schreiben|es umzusetzen|sie umzusetzen}",
			"ohne {dateien zu ändern|dateien anzufassen|die dateien anzufassen|den code anzufassen}",
		},
		fillers: `bitte jetzt erstmal erst vorerst noch doch mal hier gerade einfach auch überhaupt bloß nun heute momentan zunächst vorläufig
			eben halt ja lieber also da davon dazu daran wirklich absolut gar weiterhin`,
		others:      []string{"anderes", "andere", "anderen", "anderer", "anderem", "anders", "weiter", "weiteres", "sonst"},
		excepts:     []string{"außer", "ausgenommen", "abgesehen", "bis auf"},
		conditions:  `wenn falls sofern soweit solange weiteres`,
		comparisons: `als`,
		talks: `schätzen schätze schätz schätzung schätzungen einschätzung einschätzen planen plane plan erklären erkläre erklärung bewerten
			bewerte bewertung prüfen prüfe lesen lies analysieren analysiere analyse beschreiben beschreibe beschreibung antworten antworte
			auflisten zusammenfassen zusammenfassung überblick tabelle liste punkte aufgaben schritte ersten`,
		qualifiers: `welche welcher welches welchen welchem denen deren dessen`,
		relatives:  `die der das`,
		infinitives: `umsetzen implementieren ändern verändern modifizieren anfassen anrühren bearbeiten schreiben anfangen beginnen starten
			machen vornehmen`,
		modals: `kann können kannst könnt konnte konnten wird werden wirst werdet darf dürfen darfst dürft durfte durften
			soll sollen sollst sollt sollte sollten muss müssen musst musste mussten will wollen willst
			wollte wollten möchte möchten mag`,
		speakers: `ich wir du ihr sie claude man`,
	},
	{
		code: "nl",
		forbids: []string{
			"implementeer {niets|niks|nog niet|het nog niet|dit nog niet|deze nog niet|ze nog niet}",
			"{niets|niks} {implementeren|aanpassen|wijzigen|veranderen|bewerken|aanraken|doen}",
			"nog niet {implementeren|beginnen|starten|aanpassen}",
			"{de code|de bestanden} niet {aanpassen|wijzigen|veranderen|bewerken|aanraken}",
			"geen {code|bestanden} {aanpassen|wijzigen|veranderen|bewerken|aanraken}",
			"gelieve {niets|niks|de code niet|de bestanden niet} {te wijzigen|te veranderen|te implementeren|te bewerken|aan te passen|aan te raken}",
			"doe {niets|niks|er niets|er niks}",
			"{verander|wijzig|bewerk|modificeer} {niets|niks|geen|de code niet|de bestanden niet}",
			"{pas|raak} {niets|niks|de code niet|de bestanden niet} aan", "{pas|raak} geen",
			"schrijf geen code", "geen code schrijven",
			"begin {nog niet|er nog niet aan|nergens aan}", "start nog niet",
		},
		asks: []string{
			"alleen schatten", "schat alleen", "{alleen|enkel} een {schatting|plan}", "alleen plannen", "ik wil alleen een {schatting|plan}",
			"zonder {te implementeren|iets te wijzigen|iets te veranderen|code te schrijven|bestanden te wijzigen}",
		},
		fillers:     `nog even alsjeblieft alstublieft nu voorlopig eerst maar echt helemaal ook hier daar er nou gewoon toch vandaag`,
		others:      []string{"anders", "ander", "andere"},
		excepts:     []string{"behalve", "uitgezonderd", "buiten", "met uitzondering"},
		conditions:  `als tenzij wanneer indien zolang`,
		comparisons: `dan`,
		talks: `schatten schat schatting plannen plan uitleggen leg uitleg beoordelen beoordeel beoordeling lezen lees analyseren analyse
			beschrijven beschrijf antwoorden samenvatten samenvatting overzicht tabel lijst punten taken eerste`,
		qualifiers:  `welke welk waarin waarop`,
		relatives:   `die dat`,
		infinitives: `implementeren beginnen starten aanpassen wijzigen veranderen bewerken aanraken doen schrijven`,
		modals:      `kan kunnen kunt kon konden zal zullen zult zou zouden mag mogen mocht mochten moet moeten moest moesten wil willen wilde`,
		speakers:    `ik we wij je jij jullie u claude men`,
	},
	{
		code: "pl",
		forbids: []string{
			"nie {implementuj|implementujcie|implementować|wdrażaj|wdrażajcie|wdrażać|realizuj|realizujcie|realizować} " +
				"{jeszcze|niczego|nic|żadnych|żadnego|żadnej}",
			"nie {implementuj|implementujcie|implementować|wdrażaj|wdrażajcie|wdrażać|rób|róbcie|robić} {tego|tych|ich|go} jeszcze",
			"{niczego|nic} nie {implementuj|implementujcie|wdrażaj|wdrażajcie|realizuj|rób|róbcie|ruszaj|ruszajcie|dotykaj|dotykajcie|zmieniaj|" +
				"zmieniajcie|modyfikuj|modyfikujcie|edytuj|edytujcie|pisz|piszcie|zaczynaj|zaczynajcie|implementować|wdrażać|realizować|robić|" +
				"ruszać|dotykać|zmieniać|modyfikować|edytować|pisać|zaczynać}",
			"nie {rób|róbcie|robić} {nic|niczego|jeszcze}",
			"nie {ruszaj|ruszajcie|ruszać|dotykaj|dotykajcie|dotykać} {plików|kodu|niczego|nic|żadnych|żadnego|żadnej|repozytorium|projektu}",
			"nie {zmieniaj|zmieniajcie|zmieniać|modyfikuj|modyfikujcie|modyfikować|edytuj|edytujcie|edytować|poprawiaj|poprawiajcie|poprawiać} " +
				"{niczego|nic|plików|kodu|żadnych|żadnego|żadnej}",
			"nie {pisz|piszcie|pisać} {kodu|żadnego kodu}",
			"nie {zaczynaj|zaczynajcie|zaczynać|rozpoczynaj|rozpoczynajcie|rozpoczynać} {jeszcze|niczego|pracy}",
			"nie {trzeba|musisz|należy} {nic|niczego} {robić|zmieniać|implementować|ruszać}",
		},
		asks: []string{
			"tylko {oszacuj|oszacowanie|wycena|wyceń|plan|zaplanuj|oceń}", "{potrzebuję|chcę} tylko {oszacowania|oszacowanie|wyceny|wycenę|planu|plan}",
			"bez {implementacji|implementowania|wdrażania|zmian w kodzie|zmieniania czegokolwiek|zmieniania plików|zmieniania kodu|pisania kodu}",
		},
		fillers:     `jeszcze na razie proszę teraz tu tutaj w ogóle zupełnie absolutnie też także już po prostu dziś dzisiaj`,
		others:      []string{"więcej", "innego", "innych", "inne", "innej", "innym", "inny"},
		excepts:     []string{"oprócz", "poza", "prócz", "z wyjątkiem"},
		conditions:  `jeśli jeżeli chyba gdy kiedy dopóki`,
		comparisons: `niż`,
		talks: `oszacuj oszacowanie oszacować wycena wyceń wyceny zaplanuj plan planu wyjaśnij wyjaśnienie opisz opis oceń ocena ocenę
			przeczytaj przeanalizuj analiza analizę odpowiedz podsumuj podsumowanie lista listę punkty punktów zadania zadań pierwsze pierwszych`,
		qualifiers: `które których który która którym którymi którego której związane związanych związany związana dotyczące dotyczących
			odnoszące gdzie`,
		modals:   `może mogą mógłby mogłaby mogłoby mogliby powinien powinna powinno powinni powinny musi muszą próbuje próbują`,
		speakers: `ja ty wy my pan pani państwo claude`,
	},
	{
		code: "ru",
		forbids: []string{
			"не {реализуй|реализуйте|реализовывать|внедряй|внедряйте|внедрять|имплементируй|имплементируйте|имплементировать} " +
				"{пока|ничего|их пока|это пока|эти пункты|эти задачи}",
			"ничего не {реализуй|реализуйте|внедряй|внедряйте|делай|делайте|трогай|трогайте|меняй|меняйте|изменяй|изменяйте|правь|правьте|" +
				"редактируй|редактируйте|исправляй|исправляйте|пиши|пишите|начинай|начинайте|реализовывать|внедрять|делать|трогать|менять|" +
				"изменять|править|редактировать|исправлять|писать|начинать}",
			"пока не {реализуй|реализуйте|реализовывать|делай|делайте|делать|начинай|начинайте|начинать|приступай|приступайте|приступать}",
			"не {делай|делайте|делать} {ничего|пока}",
			"не {трогай|трогайте|трогать} {файлы|код|ничего|никакие|никаких|репозиторий|проект|кодовую базу}",
			"не {меняй|меняйте|менять|изменяй|изменяйте|изменять|правь|правьте|править|редактируй|редактируйте|редактировать|модифицируй|" +
				"модифицируйте|модифицировать} " +
				"{ничего|файлы|код|никакие|никаких|ни одного|ни одной|ни строчки|ни строки}",
			"не {пиши|пишите|писать} {код|никакого кода|ни строчки}",
			"не {начинай|начинайте|начинать|приступай|приступайте|приступать} {пока|ещё|к работе|к реализации}",
			"не {надо|нужно|стоит} ничего {делать|менять|трогать|реализовывать}",
			"ничего не {надо|нужно|стоит} {делать|менять|трогать|реализовывать}",
		},
		asks: []string{
			"только {оцени|оценку|оценка|план|спланируй}", "просто оцени", "{мне нужна|нужна} только оценка", "{мне нужен|нужен} только план",
			"без {реализации|внесения изменений|написания кода}", "не внося изменений", "ничего не {меняя|реализуя}",
		},
		fillers:     `пока пожалуйста ещё сейчас вообще тут здесь совсем абсолютно лучше уж просто сегодня`,
		others:      []string{"больше", "другого", "других", "другие", "другое", "другой", "другую", "другим", "иного", "иное", "иных"},
		excepts:     []string{"кроме", "помимо", "за исключением"},
		conditions:  `если когда`,
		comparisons: `чем`,
		talks: `оцени оценить оценку оценка оценки спланируй план плана объясни объяснение опиши описание проанализируй анализ ответь
			прочитай прочти перечисли резюме обзор таблицу таблица пункты пунктов задачи задач список списка первые первых`,
		qualifiers: `которые которых который которая которое которой котором которыми которому связанные связанных связанный связанная
			связанное относящиеся относящихся касающиеся касающихся где`,
		modals:   `может могут мог могла могло могли сможет смогут должен должна должно должны пытается пытаются`,
		speakers: `я ты вы мы claude`,
	},
	{
		code: "es",
		forbids: []string{
			"no {implementes|implemente|implementen|implementéis|implementar} {nada|todavía|aún|ningún|ninguna|ninguno|esto|eso}",
			"no {implementes|implemente|implementen|implementar|hagas|haga|hagan|hacer} {estas|estos|esas|esos} " +
				"{tareas|puntos|cambios|cosas|elementos|ítems|mejoras|funcionalidades|pasos|tickets}",
			"no {lo|la|los|las} {implementes|implemente|implementen|hagas|haga|hagan} {todavía|aún|ahora}",
			"no {implementarlo|implementarla|implementarlos|implementarlas|hacerlo|hacerla|hacerlos|hacerlas} {todavía|aún|ahora}",
			"no {hagas|haga|hagan|hagáis|hacer} {nada|ninguno|ninguna|ningún|todavía|aún}",
			"no {toques|toque|toquen|toquéis|tocar} {nada|ningún|ninguna|ninguno|los archivos|el código|el repositorio|el proyecto}",
			"no {modifiques|modifique|modifiquen|modificar|cambies|cambie|cambien|cambiar|edites|edite|editen|editar|alteres|altere|alteren|" +
				"alterar} " +
				"{nada|ningún|ninguna|ninguno|los archivos|el código|el repositorio}",
			"no {escribas|escriba|escriban|escribir} {código|ningún código|nada de código}",
			"no {empieces|empiece|empiecen|empezar|comiences|comience|comiencen|comenzar} {todavía|aún|nada|ninguno|ninguna}",
			"no {programes|programe|programen|programar} {nada|todavía|aún}",
		},
		asks: []string{
			"solo {estima|estimar|planifica|planea}", "{solo|solamente} una estimación", "{solo|solamente|únicamente} un plan",
			"solo {quiero|necesito} {una estimación|un plan}", "dame solo {una estimación|un plan}",
			"sin {implementar|modificar nada|tocar nada|tocar el código|tocar los archivos|escribir código|cambiar nada}",
		},
		fillers:     `por favor todavía aún ahora ya tampoco nunca jamás absolutamente realmente`,
		others:      []string{"más", "además", "otro", "otra", "otros", "otras", "demás"},
		excepts:     []string{"excepto", "salvo", "exceptuando", "aparte", "fuera", "a excepción"},
		conditions:  `si que cuando`,
		comparisons: `que`,
		talks: `estimar estima estimes estime estimación estimaciones planificar planifica planear planea plan planes explicar explica
			explicación revisar revisa revisión leer lee analizar analiza análisis decirme dime responder responde respuesta describir describe
			descripción listar enumerar evaluar evalúa evaluación valorar valora calcular calcula comentar comenta opinar opinión resumir resume
			resumen proponer propón propuesta tabla lista puntos tareas elementos primeros primeras dar dame`,
		modals: `puede pueden podría podrían podrá podrán debe deben debería deberían deberá deberán suele suelen intenta intentan trata
			tratan`,
		speakers: `yo tú usted ustedes vos nosotros nosotras claude`,
	},
	{
		code: "pt",
		forbids: []string{
			"não {implemente|implementa|implementem|implementes|implementar} {nada|ainda|nenhum|nenhuma|isso|isto}",
			"não {implemente|implementa|implementem|implementar|faça|faz|façam|fazer} {essas|esses|estas|estes} " +
				"{tarefas|itens|pontos|mudanças|coisas|melhorias|funcionalidades|passos|tickets}",
			"não {os|as|o|a} {implemente|implementa|implementem|faça|faz|façam} {ainda|agora}",
			"não {implementá-los|implementá-las|implementá-lo|implementá-la|fazê-los|fazê-las|fazê-lo|fazê-la} {ainda|agora}",
			"não {faça|faz|façam|faças|fazer} {nada|nenhum|nenhuma|ainda}",
			"não {toque|toca|toquem|toques|tocar|mexa|mexe|mexam|mexas|mexer} " +
				"{em nada|em nenhum|em nenhuma|nos arquivos|no código|no repositório|no projeto|em arquivo|em código|nada}",
			"não {altere|altera|alterem|alterar|modifique|modifica|modifiquem|modificar|mude|muda|mudem|mudar|edite|edita|editem|editar} " +
				"{nada|nenhum|nenhuma|os arquivos|o código|o repositório}",
			"não {escreva|escreve|escrevam|escrever} {código|nenhum código|nada de código}",
			"não {comece|começa|comecem|começar|inicie|inicia|iniciem|iniciar} {ainda|nada|nenhum|nenhuma}",
			"não {programe|programa|programem|programar} {nada|ainda}",
		},
		asks: []string{
			"{apenas|só|somente} {estime|estimar|planeje}", "{apenas|só|somente} uma estimativa", "{apenas|só|somente} um plano",
			"só {quero|preciso de} {uma estimativa|um plano}", "{quero|preciso} apenas {uma estimativa|um plano}",
			"sem {implementar|alterar nada|modificar nada|mexer em nada|mexer no código|mexer nos arquivos|tocar em nada|tocar no código|" +
				"escrever código|mudar nada}",
		},
		fillers:     `por favor ainda agora já nunca jamais absolutamente também realmente enquanto`,
		others:      []string{"mais", "outro", "outra", "outros", "outras"},
		excepts:     []string{"exceto", "salvo", "além", "fora", "a não ser"},
		conditions:  `se que quando`,
		comparisons: `que de`,
		talks: `estimar estime estima estimativa estimativas planejar planeje plano planos explicar explique explicação revisar revise
			revisão ler leia analisar analise dizer diga responder responda resposta descrever descreva listar liste avaliar avalie
			avaliação calcular calcule comentar comente opinião resumir resuma resumo propor proponha proposta tabela lista itens tarefas
			primeiros primeiras dar dê`,
		modals: `pode podem poderia poderiam poderá poderão deve devem deveria deveriam deverá deverão costuma costumam tenta tentam tende
			tendem`,
		speakers: `eu tu você vocês nós claude`,
	},
	{
		code: "fr",
		forbids: []string{
			"n'{implémente|implémentez} {rien|pas encore|aucun|aucune|pas ça|pas cela|pas tout de suite|pas maintenant|pas pour l'instant|" +
				"pas pour le moment}",
			"ne {l'implémente|l'implémentez|les implémente|les implémentez|le fais|le faites|les fais|les faites} pas {encore|tout de suite|maintenant}",
			"ne {fais|faites} {rien|pas encore}",
			"ne {touche|touchez} {à rien|à aucun|à aucune|pas aux fichiers|pas au code|pas au dépôt|pas au projet|rien}",
			"ne {modifie|modifiez|change|changez|édite|éditez|altère|altérez} {rien|aucun|aucune|pas les fichiers|pas le code}",
			"n'{écris|écrivez} {pas de code|aucun code}",
			"ne {code|codez|programme|programmez} rien",
			"ne {commence|commencez|démarre|démarrez} {pas encore|rien|pas tout de suite|pas maintenant|pas pour l'instant|pas pour le moment}",
			"ne {mets|mettez} {rien|pas encore} en œuvre",
			"ne rien {implémenter|modifier|changer|éditer|altérer|toucher|faire|coder|programmer|commencer|démarrer|mettre en œuvre}",
			"n'implémenter {aucun|aucune}", "ne pas implémenter {ça|cela}",
			"ne pas encore {implémenter|commencer|démarrer|mettre en œuvre|l'implémenter|les implémenter}",
			"ne pas {implémenter|commencer|démarrer|l'implémenter|les implémenter|le faire|les faire} " +
				"{tout de suite|maintenant|pour l'instant|pour le moment}",
			"ne toucher {à rien|à aucun|à aucune}", "ne pas toucher {aux fichiers|au code|au dépôt|au projet}",
			"ne {modifier|changer|éditer|altérer} {aucun|aucune}", "ne pas {modifier|changer|éditer|altérer} {les fichiers|le code}",
			"ne pas écrire de code", "n'écrire aucun code",
		},
		asks: []string{
			"{estime|estimez} {seulement|juste}", "{juste|seulement} estimer", "{juste|seulement|uniquement} une estimation",
			"{juste|seulement|uniquement} un plan", "juste planifier", "{planifie|planifiez} seulement", "je veux juste {une estimation|un plan}",
			"sans {implémenter|rien implémenter|rien modifier|rien changer|toucher au code|toucher aux fichiers|écrire de code|" +
				"modifier les fichiers|modifier le code}",
		},
		fillers:     `surtout plus encore pour l'instant s'il te plaît svp stp absolument vraiment donc jamais maintenant`,
		others:      []string{"d'autre", "autre", "autres"},
		excepts:     []string{"sauf", "excepté", "hormis", "à part", "en dehors", "à l'exception"},
		conditions:  `si s'il s'ils que quand lorsque`,
		comparisons: `que`,
		talks: `estimer estime estimez estimation estimations planifier planifie plan plans expliquer explique explication relire relis
			lire lis analyser analyse décrire décris répondre réponds réponse lister liste évaluer évalue évaluation chiffrer chiffre chiffrage
			résumer résume proposer propose proposition tableau avis points tâches éléments premiers premières donner donne`,
		modals: `peut peuvent pourrait pourraient pourra pourront doit doivent devrait devraient devra devront va vont risque risquent permet
			permettent essaie essaye essaient tente tentent`,
		speakers: `je tu vous nous on claude`,
	},
	{
		code: "it",
		forbids: []string{
			"non {implementare|implementate|implementi} {nulla|niente|ancora|nessun|nessuna|nessuno}",
			"non {implementarli|implementarlo|implementarla|implementarle|farli|farlo|farle} ancora",
			"non {li|lo|la|le} {implementare|implementate|fare|fate} ancora",
			"non {fare|fate|faccia} {nulla|niente|ancora|nessun|nessuna}",
			"non {toccare|toccate|tocchi} {nessun|nessuna|nulla|niente|i file|il codice|il repository|il progetto|alcun|alcuna}",
			"non {modificare|modificate|modifichi|cambiare|cambiate|cambi|alterare|alterate} {nulla|niente|nessun|nessuna|i file|il codice|alcun|alcuna}",
			"non {scrivere|scrivete|scriva} {codice|alcun codice|nessun codice}",
			"non {iniziare|iniziate|inizi|cominciare|cominciate|cominci} {ancora|nulla|niente}",
		},
		asks: []string{
			"{stima|stimate} solo", "{solo|soltanto} una stima", "solo stimare", "{solo|soltanto} un piano", "pianifica solo",
			"solo pianificare", "voglio solo {una stima|un piano}", "{dammi|dimmi} solo {una stima|un piano}",
			"senza {implementare|modificare nulla|modificare niente|toccare il codice|toccare i file|scrivere codice|cambiare nulla|cambiare niente}",
		},
		fillers:     `ancora assolutamente per favore ora adesso proprio mai più davvero già intanto`,
		others:      []string{"altro", "altra", "altri", "altre", "nient'altro", "altrove"},
		excepts:     []string{"tranne", "eccetto", "salvo", "fuorché", "oltre", "a parte", "al di fuori", "ad eccezione"},
		conditions:  `se che quando`,
		comparisons: `che di`,
		talks: `stimare stima stime stimate pianificare pianifica piano piani spiegare spiega spiegazione rivedere rivedi revisione leggere
			leggi analizzare analizza analisi descrivere descrivi descrizione rispondere rispondi risposta elencare elenca valutare valuta
			valutazione riassumere riassumi riassunto proporre proponi proposta tabella lista punti attività elementi primi prime dare dammi parere`,
		modals: `può possono potrebbe potrebbero potrà potranno deve devono dovrebbe dovrebbero dovrà dovranno rischia rischiano cerca
			cercano prova provano tende tendono`,
		speakers: `io tu lei voi noi claude`,
	},
	{
		code: "id",
		forbids: []string{
			"jangan {implementasikan|diimplementasikan|mengimplementasikan|modifikasi|memodifikasi|dieksekusi}",
			"jangan {implementasi|eksekusi} dulu",
			"jangan {terapkan|diterapkan|menerapkan} {dulu|apa pun|apapun|apa-apa|semua|semuanya}",
			"jangan {lakukan|dilakukan|kerjakan|dikerjakan|mengerjakan|melakukan} {apa pun|apa-apa|apapun|dulu|sekarang|semua|semuanya}",
			"jangan dulu {dikerjakan|kerjakan|dilakukan|lakukan|diubah|ubah|dimulai|mulai|diimplementasikan|implementasikan}",
			"jangan {ubah|diubah|mengubah|ganti|diganti|mengganti|dimodifikasi|edit|diedit|mengedit|sunting|menyunting|sentuh|disentuh|menyentuh|" +
				"utak-atik|diutak-atik|mengutak-atik|otak-atik|ngoprek} {apa pun|apa-apa|apapun|dulu|file|kode|berkas|sekarang|repo|repositori|proyek}",
			"jangan {tulis|menulis|buat|membuat|bikin} kode",
			"jangan {mulai|dimulai|memulai} {dulu|sekarang|apa pun|pekerjaan|mengerjakan}",
			"{tidak|belum|nggak|gak|ga|tak} {usah|perlu} {diimplementasikan|dikerjakan|diubah|dimulai}",
			"{tidak|nggak|gak|ga|tak} usah {mengubah|ubah|mengimplementasikan|implementasikan|mengerjakan|kerjakan|menyentuh|sentuh|mengedit|" +
				"edit|memulai|mulai|melakukan|lakukan} {apa pun|apa-apa|apapun|dulu|sekarang|file|kode|berkas}",
			"jangan {melakukan|lakukan|membuat|buat|bikin} perubahan {apa pun|apapun|apa-apa|dulu|sekarang}",
		},
		asks: []string{
			"{hanya|cukup} {perkirakan|perkiraan|estimasi|rencanakan|rencana|jelaskan|tinjau|review|nilai}",
			"{perkirakan|perkiraan|estimasi|rencanakan|rencana|jelaskan|tinjau|review|nilai} saja",
			"{beri|berikan} {perkiraan|estimasi} saja", "{hanya|cukup} buat rencana", "buat rencana saja", "hanya buatkan rencana",
			"buatkan rencana saja", "saya {hanya|cuma} {butuh|perlu|mau} {perkiraan|estimasi|rencana}", "cuma {perkiraan|estimasi|rencana}",
			"tanpa {mengimplementasikan|implementasi|mengubah apa pun|mengubah apa-apa|mengubah apapun|mengubah kode|mengubah file|" +
				"menyentuh kode|menyentuh file|menyentuh apa pun|menulis kode|mengerjakan|perubahan kode}",
		},
		fillers:     `dulu ya dong sama sekali sekarang dahulu pernah tolong sih deh lah kok juga benar-benar`,
		others:      []string{"lain", "lainnya", "selebihnya"},
		excepts:     []string{"kecuali", "selain"},
		conditions:  `jika kalau bila apabila`,
		comparisons: `dari`,
		talks: `perkirakan perkiraan estimasi estimasikan rencanakan rencana jelaskan penjelasan tinjau tinjauan review nilai penilaian
			analisis analisa baca jawab jawaban sebutkan daftar ringkas ringkasan beri berikan tabel poin tugas butir pertama`,
	},
	{
		code: "ar",
		forbids: []string{
			"لا {تنفذ|تنفذوا|تنفذي|تطبق|تطبقوا|تنجز} {أي|شي|الآن|بعد|هذه|هذا|البنود|المهام|القائمة}",
			"لا {تنفذها|تنفذوها|تطبقها|تنجزها}",
			"لا {تقم|تقوموا} {بتنفيذ|بأي|بتطبيق}",
			"لا {تلمس|تلمسوا|تمس} {أي|الملفات|الكود|شي|ملفات}",
			"لا {تعدل|تعدلوا|تعدلي|تغير|تغيروا|تغيري|تبدل|تحرر} {أي|الملفات|الكود|شي|ملفات|الشيفرة}",
			"لا {تكتب|تكتبوا} {أي كود|كود|أي شيفرة|الكود}",
			"لا {تبدأ|تبدأوا|تبدئي} {بعد|الآن|بأي|بالتنفيذ|العمل|التنفيذ}",
			"لا {تفعل|تعمل} {شي|أي شي}",
			"لا تعمل على {أي|هذه|البنود|المهام}",
			"عدم {تنفيذ|تطبيق|إنجاز} {أي|شي|هذه|هذا|البنود|المهام|القائمة}", "عدم {تعديل|تغيير|تبديل|تحرير|لمس} {أي|الملفات|الكود|شي|ملفات|الشيفرة}",
			"عدم كتابة {أي كود|كود|أي شيفرة|الكود}", "عدم البدء {الآن|بأي|بالتنفيذ|بالعمل}", "عدم القيام {بتنفيذ|بأي|بتطبيق}",
			"عدم العمل على {أي|هذه|البنود|المهام}", "عدم المساس {بأي|بالملفات|بالكود}",
		},
		asks: []string{
			"تقدير فقط", "فقط تقدير", "فقط قدر", "قدر فقط", "خطة فقط", "فقط خطة", "{دون|بدون} تنفيذ", "{دون|بدون} أي تعديل", "{دون|بدون} كتابة كود",
		},
		others:     []string{"آخر", "أخرى"},
		excepts:    []string{"غير", "غيره", "غيرها", "إلا", "سوى", "عدا", "ماعدا", "باستثناء"},
		conditions: `إذا لو إن عندما`,
		talks:      `التقدير تقدير الخطة خطة التخطيط تخطيط الشرح شرح المراجعة مراجعة التقييم تقييم القراءة البنود المهام القائمة قدر اشرح راجع خطط`,
	},
	{
		code: "ja",
		forbids: []string{
			"{まだ|何も|一切|どれも|いずれも}{実装|変更|修正|編集|着手|作業|開始}しない",
			"何も{書か|触ら|いじら|し}ない", "{何にも|一切}{触れ|触ら|手を付け|手をつけ}ない", "実装は{まだ|不要}", "実装せずに",
			"ファイル{を|は|には|にも|に}{変更|修正|編集}しない", "ファイル{に|には|にも|は|を}{触れ|触ら|いじら}ない", "ファイルに{手を加え|手を付け|手をつけ}ない",
			"コード{を|は|には|にも|に}{変更|修正|編集}しない", "コード{を|は}{書か|いじら|触ら}ない", "コード{に|には|にも}{触れ|触ら|手を加え|手を付け|手をつけ}ない",
			"{手を付け|手をつけ|手を加え|着手し}ない",
		},
		bare: []string{
			"{実装|変更|修正|編集|着手|作業|開始}しないで", "{触ら|触れ|いじら|手を付け|手をつけ|手を加え}ないで", "{実装|変更|修正|作業|着手}{は|を}控えて",
			"{実装|変更|修正|編集|着手|作業|開始}しないこと", "{触ら|触れ|いじら|手を付け|手をつけ|手を加え}ないこと", "{実装|変更|修正|編集}禁止",
		},
		asks: []string{
			"{見積もり|見積り|見積|計画|プラン|レビュー|説明}{だけ|のみ}", "見積もるだけ",
		},
		others: []string{"他の", "ほかの", "その他", "それ以外"},
	},
	{
		code: "zh",
		forbids: []string{
			"{先|暂时|还}不要{实现|实施|做|改|修改|动|动手|开始}", "{先|暂时}别{实现|实施|做|改|修改|动|动手|开始}",
			"不要{实现|实施|修改|改动|动|碰|做|改|编写|写|开发|处理}任何", "请勿{实现|实施|修改|改动|动}任何",
			"不要{写|编写|改|修改|动|碰}代码", "不要{写|编写}任何代码", "不要{修改|改|动|碰}文件", "不要开始实现", "不要动手",
			"什么都{不要|别}{做|改|动|实现|修改}", "什么也{不要|别}{做|改|动}",
			"先不要{實作|實現|實施|動|動手|開始}", "{暫時|還}不要{實作|實現|實施|做|改|修改|動|動手|開始}", "先別{實作|實現|做|改|修改|動|動手}",
			"不要{實作|實現|實施|改動|動|編寫|寫}任何", "不要{寫|編寫|改|修改|動|碰}程式碼", "不要{修改|改|動|碰}檔案",
			"什麼都{不要|別}{做|改|動|實作|修改}",
		},
		bare: []string{
			"不要{实现|实施|修改|改动|动|做|开发|处理|改}", "别{实现|实施|修改|改动|动|做|改}", "请勿{实现|实施|修改|改动|动}",
			"不要{實作|實現|實施|改動|動|處理}", "別{實作|實現|修改|改動|動|做|改}", "請勿{實作|修改|改動|動}",
			"{禁止|不得|不准|切勿}{实现|实施|修改|改动|动}", "{禁止|不得|不准|切勿}{實作|實現|實施|改動|動}",
		},
		asks: []string{
			"{只|只需|只要|仅|只做|只给出|只需要}估算", "{只要|只需|只做|只给出}计划", "{只|只需}评估", "只给我一个计划", "只要一个计划",
			"{不用|无需|不需要|不必}实现", "{只|只需}估計", "只要計劃", "只評估", "{不用|無需}實作",
		},
		others: []string{"其他", "别的", "其它", "其余", "別的", "其餘"},
	},
	{
		code: "ko",
		forbids: []string{
			"{아직|아무것도|아무 것도|하나도|절대|전혀} {구현|수정|변경|작업|작성|착수|시작}하지 {마|말}",
			"{아직|아무것도|아무 것도|하나도|절대} {건드리|손대}지 {마|말}",
			"구현은 아직", "구현은 하지 {마|말}",
			"파일{을|은|도|에} {수정|변경|편집|작성}하지 {마|말}", "파일{을|은|도|에} {건드리|손대}지 {마|말}",
			"코드{를|는|도|에} {작성|수정|변경|편집}하지 {마|말}", "코드{를|는|도|에} {건드리|손대}지 {마|말}",
		},
		bare: []string{
			"{구현|수정|변경|작업|착수|시작|진행}하지 {마|말}", "{건드리|손대}지 {마|말}", "{구현|수정|변경|편집|작업} 금지",
			"{구현|수정|변경|편집|작업}금지",
		},
		asks: []string{
			"{견적|추정|계획|검토|설명}만", "예상 시간만", "구현하지 않고", "구현 없이", "코드 변경 없이",
		},
		others: []string{"다른"},
	},
}

type heldGroup struct {
	code                                                string
	fillers, conditions, comparisons, talks, qualifiers func(string) bool
	relatives, infinitives, modals, speakers            func(string) bool
	others, excepts                                     [][]string
}

type heldPattern struct {
	words                []string
	phrase, written, key string
	group                *heldGroup
	bare                 bool
}

type heldPatternSet struct {
	tokens   map[[2]string][]*heldPattern
	groups   map[string][]*heldGroup
	scripted []*heldPattern
}

type heldTable struct {
	forbids, asks heldPatternSet
}

var heldPatterns = sync.OnceValue(func() *heldTable {
	table := &heldTable{}
	for _, set := range []*heldPatternSet{&table.forbids, &table.asks} {
		set.tokens, set.groups = map[[2]string][]*heldPattern{}, map[string][]*heldGroup{}
	}
	for _, language := range heldLanguages {
		group := &heldGroup{
			code:        language.code,
			fillers:     foldWordSet(language.fillers),
			conditions:  foldWordSet(language.conditions),
			comparisons: foldWordSet(language.comparisons),
			talks:       foldWordSet(language.talks),
			qualifiers:  foldWordSet(language.qualifiers),
			relatives:   foldWordSet(language.relatives),
			infinitives: foldWordSet(language.infinitives),
			modals:      foldWordSet(language.modals),
			speakers:    foldWordSet(language.speakers),
			others:      heldMarkers(language.others),
			excepts:     heldMarkers(language.excepts),
		}
		table.forbids.add(group, language.forbids, false)
		table.forbids.add(group, language.bare, true)
		table.asks.add(group, language.asks, false)
	}
	return table
})

func (set *heldPatternSet) add(group *heldGroup, patterns []string, bare bool) {
	for _, pattern := range patterns {
		for _, written := range heldExpand(pattern) {
			phrase := heldFold(heldNormal(written))
			entry := &heldPattern{words: strings.Fields(phrase), phrase: phrase, written: written, group: group, bare: bare}
			if first, _ := utf8.DecodeRuneInString(phrase); unicode.In(first, unicode.Latin, unicode.Cyrillic) {
				set.index(entry)
			} else {
				entry.key = scriptKey(phrase)
				set.scripted = append(set.scripted, entry)
			}
		}
	}
}

func (set *heldPatternSet) index(pattern *heldPattern) {
	words := pattern.words
	key := [2]string{words[0], ""}
	if len(words) > 2 || len(words) == 2 && !heldPrefixed(words[1]) {
		key[1] = words[1]
	}
	set.tokens[key] = append(set.tokens[key], pattern)
	if !slices.Contains(set.groups[words[0]], pattern.group) {
		set.groups[words[0]] = append(set.groups[words[0]], pattern.group)
	}
}

func heldExpand(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{strings.Join(strings.Fields(pattern), " ")}
	}
	end := open + strings.IndexByte(pattern[open:], '}')
	expanded := []string{}
	for _, choice := range strings.Split(pattern[open+1:end], "|") {
		expanded = append(expanded, heldExpand(pattern[:open]+choice+pattern[end+1:])...)
	}
	return expanded
}

func foldWordSet(words string) func(string) bool {
	var once sync.Once
	var set map[string]bool
	return func(word string) bool {
		once.Do(func() { set = wordSet(heldFold(heldNormal(words))) })
		return set[word]
	}
}

func heldMarkers(markers []string) [][]string {
	split := make([][]string, len(markers))
	for index, marker := range markers {
		split[index] = strings.Fields(heldFold(heldNormal(marker)))
	}
	return split
}

var heldFolds = sync.OnceValue(func() *strings.Replacer {
	pairs := []string{"ß", "ss", "œ", "oe", "æ", "ae", "\u0640", "", "\u0670", ""}
	for _, group := range strings.Fields(`aàáâãäåāą cçćč dď eèéêëēęě gğ iìíîïīį lł nñńň oòóôõöøōő rř sśšş tť uùúûüūůű yýÿ zźżž иий её اأإآٱ يى`) {
		base, size := utf8.DecodeRuneInString(group)
		for _, variant := range group[size:] {
			pairs = append(pairs, string(variant), string(base))
		}
	}
	for _, marks := range [][2]rune{{'\u0300', '\u036F'}, {'\u064B', '\u0652'}} {
		for mark := marks[0]; mark <= marks[1]; mark++ {
			pairs = append(pairs, string(mark), "")
		}
	}
	return strings.NewReplacer(pairs...)
})

func heldFold(text string) string {
	for index := 0; index < len(text); index++ {
		if text[index] >= utf8.RuneSelf {
			return heldFolds().Replace(text)
		}
	}
	return text
}

type heldWord struct {
	raw, word string
}

func heldWordAt(words []heldWord, index int) string {
	if index < 0 || index >= len(words) {
		return ""
	}
	return words[index].word
}

func heldCapitalized(raw string) bool {
	first, _ := utf8.DecodeRuneInString(raw)
	return unicode.IsUpper(first)
}

func heldComma(stop rune) bool {
	return stop == ',' || stop == '，' || stop == '،'
}

type heldText struct {
	tokens []heldToken
	folded []string
	lower  string
}

func newHeldText(prose string, tokens []heldToken) *heldText {
	folded := make([]string, len(tokens))
	for index, token := range tokens {
		if token.stop == 0 {
			folded[index] = heldFold(token.word)
		}
	}
	return &heldText{tokens: tokens, folded: folded, lower: heldFold(heldNormal(prose))}
}

func (text *heldText) find(set *heldPatternSet, forbids bool) string {
	for start, first := range text.folded {
		for _, group := range set.groups[first] {
			if quote := text.findAt(set, group, start, forbids); quote != "" {
				return quote
			}
		}
	}
	starts := map[string][]int{}
	for _, pattern := range set.scripted {
		found, seen := starts[pattern.key]
		if !seen {
			found = scriptStarts(text.lower, pattern.key)
			starts[pattern.key] = found
		}
		next := 0
		for _, at := range found {
			if at < next || !strings.HasPrefix(text.lower[at:], pattern.phrase) {
				continue
			}
			next = at + len(pattern.phrase)
			if scriptWordEnds(pattern, text.lower[next:]) && (!forbids || text.scriptHolds(pattern, at)) {
				return pattern.written
			}
		}
	}
	return ""
}

func (text *heldText) findAt(set *heldPatternSet, group *heldGroup, start int, forbids bool) string {
	first := text.folded[start]
	if quote := text.try(set.tokens[[2]string{first, ""}], group, start, forbids); quote != "" {
		return quote
	}
	for at, skipped := start+1, 0; at < len(text.tokens) && skipped <= heldMaxSkip; at, skipped = at+1, skipped+1 {
		if stop := text.tokens[at].stop; stop != 0 {
			if heldComma(stop) {
				continue
			}
			break
		}
		if quote := text.try(set.tokens[[2]string{first, text.folded[at]}], group, start, forbids); quote != "" {
			return quote
		}
		if !group.fillers(text.folded[at]) {
			break
		}
	}
	return ""
}

func (text *heldText) try(patterns []*heldPattern, group *heldGroup, start int, forbids bool) string {
	for _, pattern := range patterns {
		if pattern.group != group {
			continue
		}
		if last := text.match(pattern, start); last >= 0 && (!forbids || text.holds(pattern, start, last)) {
			return heldQuote(text.tokens[start : last+1])
		}
	}
	return ""
}

const heldMaxSkip = 3

func (text *heldText) match(pattern *heldPattern, start int) int {
	at, words := start, pattern.words
	for index := 1; index < len(words); index++ {
		fillers, commas := 0, 0
		for at++; ; at++ {
			if at >= len(text.tokens) || fillers+commas > heldMaxSkip {
				return -1
			}
			token := text.tokens[at]
			if token.stop == 0 && heldWordIs(text.folded[at], words[index], index == len(words)-1) {
				break
			}
			switch {
			case token.stop == 0 && pattern.group.fillers(text.folded[at]):
				fillers++
			case heldComma(token.stop):
				commas++
			default:
				return -1
			}
		}
		if commas > 0 && fillers == 0 {
			return -1
		}
	}
	return at
}

func heldWordIs(word, want string, last bool) bool {
	if last && heldPrefixed(want) {
		return strings.HasPrefix(word, want)
	}
	return word == want
}

func heldPrefixed(word string) bool {
	return utf8.RuneCountInString(word) > 3
}

func (text *heldText) foldedAt(index int) string {
	if index < 0 || index >= len(text.folded) {
		return ""
	}
	return text.folded[index]
}

func (text *heldText) wordsFrom(from, limit int) []string {
	words := []string{}
	for at := from; at < len(text.tokens) && len(words) < limit && text.tokens[at].stop == 0; at++ {
		words = append(words, text.folded[at])
	}
	return words
}

func (text *heldText) span(from, to int) []heldWord {
	words := make([]heldWord, 0, to-from)
	for at := from; at < to; at++ {
		words = append(words, heldWord{raw: text.tokens[at].raw, word: text.folded[at]})
	}
	return words
}

func (text *heldText) holds(pattern *heldPattern, start, last int) bool {
	return !text.goesOn(pattern.group, last) && !text.purpose(start) && !text.statement(pattern, start) && !text.narrowed(pattern, last)
}

var (
	heldPurposes = sync.OnceValue(func() [][]string {
		return heldMarkers([]string{"para", "pra", "pour", "per", "чтобы", "чтоб", "żeby", "aby", "ażeby", "afin de", "a fin de", "a fim de",
			"al fine di", "così da", "in modo da", "façon à", "manière à", "de modo a", "de forma a", "para que", "pra que", "de forma que",
			"de modo que", "de manera que", "pour que", "afin que", "affinché", "in modo che", "così che"})
	})
	heldComplementLeads = foldWordSet(`peço pedi pedimos peça peçam pede pedem pedindo proszę prosimy ważne istotne trzeba chcę chciałbym
		chciałabym прошу просим важно нужно надо необходимо хочу хотим`)
)

func (text *heldText) purpose(start int) bool {
	for _, marker := range heldPurposes() {
		if from := start - len(marker); from >= 0 && slices.Equal(text.folded[from:start], marker) {
			lead := from - 1
			if lead >= 0 && heldComma(text.tokens[lead].stop) {
				lead--
			}
			return !heldComplementLeads(text.foldedAt(lead))
		}
	}
	return false
}

func (text *heldText) goesOn(group *heldGroup, last int) bool {
	comma := false
	for at, seen := last+1, 0; at < len(text.tokens) && seen < 3; at++ {
		if stop := text.tokens[at].stop; stop != 0 {
			if comma || seen > 0 || !heldComma(stop) {
				return false
			}
			comma = true
			continue
		}
		if size := heldMarkerAt(group.excepts, text.folded, at); size > 0 {
			return !group.leavesTalk(text.wordsFrom(at+size, 4))
		}
		if size := heldMarkerAt(group.others, text.folded, at); size > 0 && !comma {
			after := text.wordsFrom(at+size, 4)
			return len(after) == 0 || !group.comparisons(after[0]) || !group.leavesTalk(after[1:])
		}
		seen++
	}
	return false
}

func heldMarkerAt(markers [][]string, folded []string, at int) int {
	for _, marker := range markers {
		if at+len(marker) <= len(folded) && slices.Equal(folded[at:at+len(marker)], marker) {
			return len(marker)
		}
	}
	return 0
}

func (group *heldGroup) leavesTalk(words []string) bool {
	if len(words) > 0 && group.conditions(words[0]) {
		return true
	}
	return slices.ContainsFunc(words[:min(3, len(words))], group.talks)
}

func (text *heldText) statement(pattern *heldPattern, start int) bool {
	from := start
	for from > 0 && start-from < 30 && text.tokens[from-1].stop == 0 {
		from--
	}
	before := text.span(from, start)
	if len(before) > 0 && heldStatementLeads(before[len(before)-1].word) {
		return true
	}
	group, words := pattern.group, pattern.words
	switch {
	case group.infinitives(words[len(words)-1]):
		return modalStatement(group, before)
	case romanceThirdForms(romanceVerb(words)):
		return romanceStatement(before)
	}
	return modalBefore(group, before)
}

func (text *heldText) narrowed(pattern *heldPattern, last int) bool {
	end := last + 1
	for end < len(text.tokens) && text.tokens[end].stop == 0 && end-last <= 16 {
		end++
	}
	group, object := pattern.group, text.folded[last]
	for _, word := range pattern.words {
		if narrowObjects(word) {
			object = word
		}
	}
	qualified := end < len(text.tokens) && heldComma(text.tokens[end].stop) &&
		(group.qualifiers(text.foldedAt(end+1)) || group.relatives(text.foldedAt(end+1)) && heldRelativeFollowers(text.foldedAt(end+2)))
	return narrowedTail(object, text.span(last+1, end), qualified)
}

func (text *heldText) scriptHolds(pattern *heldPattern, at int) bool {
	lower, end := text.lower, at+len(pattern.phrase)
	if scriptGoesOn(pattern.group, lower, at, end) || scriptNarrowed(pattern, lower, at) {
		return false
	}
	switch {
	case strings.HasPrefix(pattern.phrase, "لا ت"):
		return !arabicStatement(lower, at)
	case strings.HasSuffix(pattern.phrase, "ない"):
		return !japaneseStatement(lower, at, end)
	}
	return true
}

func scriptKey(phrase string) string {
	_, first := utf8.DecodeRuneInString(phrase)
	_, second := utf8.DecodeRuneInString(phrase[first:])
	return phrase[:first+second]
}

func scriptStarts(text, key string) []int {
	starts := []int{}
	_, step := utf8.DecodeRuneInString(key)
	for from := 0; ; {
		at := strings.Index(text[from:], key)
		if at < 0 {
			return starts
		}
		starts = append(starts, from+at)
		from += at + step
	}
}

var arabicEndings = []string{"ه", "ها", "هم", "هما", "كم", "نا", "ي", "ا", "ء", "ئ", "ئا", "ات"}

func scriptWordEnds(pattern *heldPattern, rest string) bool {
	if pattern.group.code != "ar" {
		return true
	}
	end := strings.IndexFunc(rest, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) })
	if end < 0 {
		end = len(rest)
	}
	return end == 0 || slices.Contains(arabicEndings, rest[:end])
}

func scriptGoesOn(group *heldGroup, lower string, start, end int) bool {
	next, last := []rune(lower[end:min(len(lower), end+16*utf8.UTFMax)]), []rune(lower[max(0, start-8*utf8.UTFMax):start])
	after, before := string(next[:min(16, len(next))]), string(last[max(0, len(last)-8):])
	if stop := strings.IndexAny(after, sentenceStops+clauseStops); stop >= 0 {
		after = after[:stop]
	}
	if stop := strings.LastIndexAny(before, sentenceStops+clauseStops); stop >= 0 {
		before = before[stop:]
	}
	if group.code == "ar" {
		words := heldWords(heldTokens(after))
		for index, word := range words {
			stems := arabicStems(word)
			switch {
			case slices.ContainsFunc(group.excepts, func(marker []string) bool { return slices.Contains(stems, marker[0]) }):
				return !group.leavesTalk(words[index+1:])
			case slices.ContainsFunc(group.others, func(marker []string) bool { return slices.Contains(stems, marker[0]) }):
				return true
			}
		}
		return false
	}
	for _, marker := range group.others {
		if strings.Contains(after, marker[0]) || strings.Contains(before, marker[0]) {
			return true
		}
	}
	return false
}

func arabicStems(word string) []string {
	stems := []string{word}
	for _, prefix := range []string{"و", "ف", "ال"} {
		if stem, found := strings.CutPrefix(stems[len(stems)-1], prefix); found && utf8.RuneCountInString(stem) > 1 {
			stems = append(stems, stem)
		}
	}
	return stems
}

var heldStatementLeads = foldWordSet(`sich ça cela sembra sembrano semble semblent paraît paraissent parece parecen parecem`)

var (
	romanceNegations  = foldWordSet(`no não ne non`)
	romanceThirdForms = foldWordSet(`faz implementa implemente implementem implementen faça façam haga hagan toque toquen toca mexa mexe altere
		altera alteren modifique modifiquen modifica mude muda cambie cambien escriba escreva escreve comece começa comience empiece inicie
		inicia programe programa edite edita editen touche modifie change code commence démarre programme mets fais faccia tocchi
		modifichi cambi implementi inizi cominci scriva`)
	romanceFillers = foldWordSet(`todavía aún ahora ya hoy mañana momento rato semana mes día sprint por el la los las este esta de del al
		ainda agora já hoje amanhã enquanto pelo pela do da no na neste nesta encore maintenant l'instant instant moment pour le ce cette
		semaine mois jour aujourd'hui demain d'ici lunes martes miércoles jueves viernes sábado domingo segunda terça quarta quinta sexta
		lundi mardi mercredi jeudi vendredi samedi dimanche noche tarde madrugada mediodía fin vez hora año días semanas meses próximo próxima
		siguiente noite manhã fim seguinte soir soirée matin matinée nuit midi après-midi l'après-midi week-end weekend fois heure
		année an l'an l'année jours journée semaines prochain prochaine suivant suivante`)
	romanceLeads = foldWordSet(`que favor plaît svp stp porfa y e et o ou mais mas pero pues então entonces alors donc puis luego
		ensuite surtout sobretudo todo simplemente simplesmente simplement juste solo só apenas también também aussi tampoco nem ni`)
	romanceSubjects = foldWordSet(`je il elle ils elles ça cela ceci qu'il qu'elle qu'ils qu'elles yo él ella ello ellos ellas esto eso eu ele
		ela eles elas isso isto`)
	romanceDeterminers = foldWordSet(`le la les un une ce cet cette ces mon ma mes son sa ses notre nos leur leurs du des el los las unos unas
		este esta estos estas ese esa esos esas mi mis su sus nuestro nuestra o a os as um uma uns umas estes esse essa esses essas meu minha
		seu sua nosso nossa dos das do da no na il lo gli uno questo quel quello quella quei quelle mio mia tuo tua suo nostro`)
	romancePrepositions = foldWordSet(`a durante antes después tras en con sin para por hasta desde sobre según entre hacia contra depois em
		com sem até após pendant avant après dans avec sans par pour jusqu'à depuis sur sous vers lors chez di da in su per tra fra
		senza prima dopo`)
	romanceContractions = foldWordSet(`no na nos nas do da dos das du au aux del al nel nella nello nei negli nelle sul sulla sullo sui sugli
		sulle dal dalla dallo dai dagli dalle pelo pela pelos pelas num numa`)
	romanceArticles = foldWordSet(`el la los las le les lo il i gli o a os as`)
	romanceOfWords  = foldWordSet(`de del do da dos das du des di della dello dei degli delle`)
)

func romanceVerb(words []string) string {
	if romanceNegations(words[0]) {
		return wordAt(words, 1)
	}
	return strings.TrimPrefix(words[0], "n'")
}

func modalStatement(group *heldGroup, before []heldWord) bool {
	modal := false
	for _, word := range before {
		if group.speakers(word.word) && word.raw != "sie" {
			return false
		}
		modal = modal || group.modals(word.word)
	}
	return modal
}

var heldModalLinks = foldWordSet(`de di a`)

func modalBefore(group *heldGroup, before []heldWord) bool {
	at := len(before) - 1
	if at > 0 && heldModalLinks(before[at].word) {
		at--
	}
	if at < 1 || !group.modals(before[at].word) {
		return false
	}
	subject := before[at-1].word
	return !group.speakers(subject) && !group.fillers(subject) && !narrowEnders(subject) && !romanceLeads(subject)
}

func romanceStatement(before []heldWord) bool {
	last := len(before) - 1
	for last >= 0 && romanceFillers(before[last].word) {
		last--
	}
	if last < 0 {
		return false
	}
	named := last
	for named > 0 && heldCapitalized(before[named].raw) && before[named].word != "claude" {
		named--
	}
	if !romanceDeterminers(before[named].word) {
		last = named
	}
	for range 2 {
		at := last - 1
		if romanceArticles(heldWordAt(before, at)) && romanceOfWords(heldWordAt(before, at-1)) {
			at--
		}
		if at < 1 || !romanceOfWords(before[at].word) {
			break
		}
		last = at - 1
	}
	word := before[last].word
	switch {
	case romanceLeads(word):
		return false
	case romanceSubjects(word):
		return true
	case strings.HasPrefix(word, "l'"):
		return !romancePrepositions(heldWordAt(before, last-1))
	}
	for _, at := range []int{last - 1, last - 2} {
		if determiner := heldWordAt(before, at); romanceDeterminers(determiner) {
			return !romanceContractions(determiner) && !romancePrepositions(heldWordAt(before, at-1))
		}
	}
	return false
}

var (
	arabicTimeWords = foldWordSet(`الآن اليوم الليلة الأسبوع الرجاء الصباح المساء الغد الحين الوقت`)
	arabicPointers  = foldWordSet(`هذه هذا هي هو تلك ذلك`)
)

func arabicStatement(lower string, at int) bool {
	if previous, _ := utf8.DecodeLastRuneInString(lower[:at]); unicode.IsLetter(previous) {
		return false
	}
	words := heldWords(heldTokens(clauseBefore(lower[:at])))
	for _, word := range words {
		if !arabicPointers(word) && (!strings.HasPrefix(word, "ال") || arabicTimeWords(word)) {
			return false
		}
	}
	return len(words) > 0
}

var japaneseTopicHolds = []string{
	"今日", "今", "当面", "当分", "現在", "本日", "今週", "今回", "現時点", "現段階", "まず", "とりあえず", "一旦", "いったん", "しばらく", "あなた", "君", "きみ",
	"claude", "クロード", "ファイル", "コード", "プロジェクト", "リポジトリ", "これら", "これ", "それ", "それら", "以下", "上記", "全て", "すべて", "全部", "項目",
	"タスク", "作業", "今後", "明日",
}

func japaneseStatement(lower string, at, end int) bool {
	for _, request := range []string{"で", "こと", "よう"} {
		if strings.HasPrefix(lower[end:], request) {
			return false
		}
	}
	before := strings.TrimRight(clauseBefore(strings.TrimRight(lower[:at], " 　、")), " 　")
	topic, found := strings.CutSuffix(before, "は")
	if !found {
		topic, found = strings.CutSuffix(before, "が")
	}
	if !found || topic == "" || strings.HasSuffix(topic, "で") || strings.HasSuffix(topic, "に") {
		return false
	}
	for _, hold := range japaneseTopicHolds {
		if strings.HasSuffix(topic, hold) {
			return false
		}
	}
	return true
}

const heldWindow = 200

func clauseBefore(text string) string {
	start := max(0, len(text)-heldWindow)
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	text = text[start:]
	if stop := strings.LastIndexAny(text, sentenceStops+clauseStops); stop >= 0 {
		_, size := utf8.DecodeRuneInString(text[stop:])
		text = text[stop+size:]
	}
	return text
}

func clauseAfter(text string) string {
	end := min(len(text), heldWindow)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	text = text[:end]
	if stop := strings.IndexAny(text, sentenceStops+clauseStops); stop >= 0 {
		text = text[:stop]
	}
	return text
}

var (
	narrowObjects = foldWordSet(`файлы файл файла файлов код кода plików pliki plik kodu kod file files codice archivos archivo código arquivos
		arquivo fichiers fichier code bestanden bestand dateien datei kode berkas الملفات الكود كود شيفرة ملفات الشيفرة`)
	narrowQuantifiers = foldWordSet(`aucun aucune ningún ninguna ninguno nenhum nenhuma nessun nessuno nessuna alcun alcuna alcuno أي kein keine
		keinen keines keiner keinem geen żadnych żadnego żadnej żaden żadne никакие никаких никакой никакого никакую одного одной`)
	narrowOfWords   = foldWordSet(`de des du di dei degli delle della del dos das do da من los las les gli le os as el la il lo i von vom van`)
	narrowListWords = foldWordSet(`estas estos esas esos ces cette ce cet d'entre questi queste quelle quegli quei esses essas estes destes destas
		desses dessas isto isso هذه هذا هؤلاء diese dieser dieses deze dit tych tego этих эти ini itu`)
	narrowWholeObjects = foldWordSet(`fichier fichiers archivo archivos arquivo arquivos file files codice código code kode ملف ملفات الملفات كود
		الكود شيفرة شيء أمر cambio cambios changement changements modification modifications mudança mudanças alteração alterações modifica
		modifiche cambiamento cambiamenti tarea tareas tâche tâches tarefa tarefas compito compiti punto puntos punti ponto pontos point points
		élément éléments elemento elementos elementi item items البنود بند المهام مهمة النقاط تعديل تغيير cosa cosas coisa coisas chose choses
		cose funcionalidad funcionalidades funcionalidade fonctionnalité fonctionnalités funzionalità función funciones função funções fonction
		fonctions funzione funzioni paso pasos passo passos passi passaggio passaggi étape étapes etapa etapas fase fasi ticket tickets bug bugs
		bogue error errores erro erros erreur erreurs errore errori corrección correcciones correção correções correction corrections
		correzione correzioni parte partes parti partie parties línea líneas linha linhas ligne lignes riga righe linee mejora mejoras
		melhoria melhorias amélioration améliorations miglioramento miglioramenti requisito requisitos requisiti exigence exigences feature
		features story stories issue issues ميزة ميزات خطوة خطوات خطأ أخطاء إصلاح جزء أجزاء سطر عمل datei dateien zeile zeilen änderung
		änderungen aufgabe aufgaben punkt punkte schritt schritte funktion funktionen teil teile sache sachen ding dinge stelle stellen fehler
		bestand bestanden regel regels wijziging wijzigingen taak taken punt punten stap stappen functie functies onderdeel onderdelen plik
		pliku plików linii linijki zmiany zmian zadania zadań punktu punktów kroku kroków funkcji rzeczy części файл файла файлов файлы
		строку строки изменения изменений задачи задач пункта пункты пунктов шага шаги шагов функции части частей berkas baris
		perubahan tugas poin langkah fitur bagian hal`)
	narrowEnders = foldWordSet(`todavía aún ahora ya hoy nunca jamás tampoco ainda agora já hoje jamais também tampouco
		encore maintenant aujourd'hui ancora ora adesso mai oggi neanche nemmeno noch jetzt heute vorerst erstmal erst mehr nog nu vandaag
		voorlopig eerst jeszcze teraz dziś dzisiaj nigdy пока сейчас ещё сегодня никогда вообще الآن بعد أبدا مطلقا
		dulu sekarang lagi apa apapun apa-apa sama bitte alsjeblieft alstublieft proszę пожалуйста svp stp s'il tolong mohon please pls y
		e et und i и а но или ни ou oder of lub albo ani pero mas mais aber maar ale sino sondern ni nem né ma tapi dan atau hasta até
		jusqu'à jusqu'au jusqu'aux fino finché bis tot dopóki póki aż до حتى sampai hingga sebelum antes avant prima bevor voordat zanim перед
		قبل porque parce perché weil omdat bo ponieważ потому لأن karena car puisque denn want poiché sin sem sans senza ohne zonder bez без
		بدون دون tanpa mientras enquanto durante pendant tant lorsque mentre solange während zolang terwijl podczas selama sementara أثناء
		خلال بينما ريثما aquí acá ahí ici hier tutaj tu здесь тут هنا inmediatamente enseguida imediatamente immédiatement tout
		subito immediatamente sofort meteen direct natychmiast сразу немедленно فورا langsung segera absolutamente absolument
		assolutamente affatto überhaupt keinesfalls helemaal absoluut wcale absolutnie совсем абсолютно إطلاقا نهائيا
		بتاتا sequer حاليا hari minggu التالية أدناه suivants suivantes ci-dessous ci-dessus siguientes
		seguintes abaixo seguenti elencati folgenden unten volgende onderstaande poniższych następujących следующие ниже berikut esistente
		esistenti existente existentes existant existants existante existantes vorhandenen bestehenden bestaande istniejącego istniejących
		istniejące существующий существующие существующего существующих الحالي الحالية الموجودة actual actuales atual atuais actuel actuels
		actuelle actuelles attuale attuali aktuellen huidige obecny obecnego obecnych aktualnego текущий текущие текущего текущих aan an um
		nenhum nenhuma ninguno ninguna alguno alguna`)
	narrowAsideLeads = foldWordSet(`por pour per de na w voor на من en para bajo sous unter onder pod od em in auf во على di yang du`)
	narrowArticles   = foldWordSet(`el le il la lo`)
	narrowAsideWords = foldWordSet(`ahora momento enquanto agora ora adesso l'instant instant moment maintenant razie ogóle nu данный сегодня
		favor favore فضلك absoluto nada niente nulla manera forma modo jeito hipótese tout inmediato imediato razu żadnym żadnej keinen keinem
		geen время الإطلاق sini ada`)
	narrowReferenceLeads = foldWordSet(`en dans no na nos nas em nel nella nello nei negli nelle in im am w we в во на في di dem den der die het deze dit
		diesem dieser dieses diese este esta estos estas ese esa esos esas ce cette ces cet questo questa questi queste neste nesta nestes nestas
		deste desta destes destas de`)
	narrowProjects = foldWordSet(`proyecto proyectos projeto projetos projet projets progetto progetti projekt projekts projektes projekte project
		projecten projektu projekcie проект проекта проекте проекту المشروع مشروع proyek projek repositorio dépôt repository
		repositorys repo repos repozytorium репозиторий репозитория репо المستودع مستودع repositori codebase aplicación aplicação
		aplicativo application applicazione anwendung applicatie aplikacja aplikacji приложение приложения التطبيق aplikasi app`)
	narrowTimes = foldWordSet(`semana semanas mes meses día días hora horas momento rato tarde noche mañana año sprint finde fin
		lunes martes miércoles jueves viernes sábado domingo noite manhã fim amanhã segunda-feira terça-feira quarta-feira quinta-feira
		sexta-feira semaine semaines mois jour jours heure heures soir soirée matin matinée nuit après-midi année week-end weekend demain
		lundi mardi mercredi jeudi vendredi samedi dimanche settimana settimane mese mesi giorno giorni ore sera serata notte mattina
		pomeriggio anno domani dopodomani lunedì martedì mercoledì giovedì venerdì sabato domenica woche wochen monat stunde stunden abend
		nacht morgen nachmittag jahr wochenende übermorgen montag dienstag mittwoch donnerstag freitag samstag sonntag week weken maand dag
		dagen uur avond ochtend middag jaar overmorgen maandag dinsdag woensdag donderdag vrijdag zaterdag zondag tygodniu tydzień tygodnia
		miesiąc miesiąca dzień dnia dni godziny chwili wieczór wieczorem rano popołudniu roku sprintu jutra jutro pojutrze poniedziałku
		poniedziałek wtorku wtorek środy środę czwartku czwartek piątku piątek soboty sobotę niedzieli niedzielę weekendu неделе неделю
		недели месяц месяца день дня часа момента вечером утром ночью года спринта выходные завтра послезавтра понедельника понедельник
		вторника вторник четверга четверг пятницы пятницу субботы субботу воскресенья воскресенье minggu bulan hari jam saat sore malam
		pagi siang tahun besok lusa senin selasa rabu kamis jumat sabtu الأسبوع الشهر اليوم الليلة الساعة الوقت الصباح المساء العام السنة
		يوم غدا الغد الاثنين الثلاثاء الأربعاء الخميس الجمعة السبت الأحد`)
	narrowTimeLeads = foldWordSet(`próxima próximo próximas próximos siguiente prochaine prochain prossima prossimo prossime prossimi nächste
		nächsten nächster nächstes kommende kommenden volgende komende przyszłym przyszłej przyszły następnym następnej najbliższym
		najbliższej следующей следующий следующем следующую ближайшей ближайший ближайшем этой этом`)
	heldRelativeFollowers = foldWordSet(`mit von vom für in im an am auf aus bei zu zum zur über unter nach vor ich du er sie es wir ihr man noch
		schon nicht gerade dort hier sich bereits met voor op uit naar ik je jij hij zij ze we wij jullie men nog al niet daar`)
	koreanHolds = lazyWordSet(`아직 절대 절대로 당분간 일단 우선 제발 그냥 지금 지금은 또 또한 그리고 먼저 아예 전혀 모든 어떤 아무 기존 이 그 저 전체 오늘 내일
		당장 이번 현재 결코 함부로 마음대로 임의로 전부 일체 어떠한 어느 각 프로젝트 저장소 리포지토리`)
	koreanBareHolds = lazyWordSet(`이것 이것들 그것 그것들 이거 이건 이걸 저것 아래 위 항목 항목들 목록 리스트 모두 전부 다 아무것도 하나도 작업 구현 일 것 것들 이들`)
)

var japaneseHolds = []string{
	"全", "各", "全ての", "すべての", "既存の", "どの", "この", "これらの", "その", "それらの", "あらゆる", "いかなる", "一切の", "以下の", "上記の", "今", "今日",
	"当面", "当分", "絶対", "一切", "全然", "現在", "本日", "今週", "今回", "現時点", "現段階", "プロジェクトの", "リポジトリの",
}

var japaneseBareHolds = []string{
	"これ", "これら", "それ", "それら", "あれ", "以下", "上記", "全て", "すべて", "全部", "何", "どれ", "いずれ", "一切", "リスト", "項目", "タスク", "作業",
	"まだ", "今", "当面", "当分", "しばらく", "とりあえず", "一旦", "いったん", "今日", "現時点", "今回", "絶対", "決して", "もう",
}

var chineseHolds = []string{
	"文件", "代码", "代碼", "程式", "东西", "東西", "事", "改动", "改動", "修改", "变更", "變更", "更改", "功能", "任务", "任務", "项", "項", "一项", "一項",
	"一个", "一個", "内容", "內容", "部分", "工作", "檔案", "地方", "条", "條", "一条", "一條", "实现", "實作", "操作", "步骤", "步驟", "计划", "計劃", "需求",
	"现有", "現有", "已有", "既有", "原有", "当前", "當前", "目前", "源代码", "源代碼", "源码", "源碼", "改变", "改變", "仓库", "倉庫", "新功能",
}

var chineseBareLeads = []string{"任何", "这些", "這些", "所有", "全部", "一切", "以下", "上面", "下面", "上述", "它们", "它們"}

func scriptNarrowed(pattern *heldPattern, lower string, at int) bool {
	phrase, rest := pattern.phrase, lower[at+len(pattern.phrase):]
	switch {
	case pattern.bare && pattern.group.code == "ko":
		return koreanBareNarrowed(heldWords(heldTokens(clauseBefore(lower[:at]))))
	case pattern.bare && pattern.group.code == "ja":
		return japaneseBareNarrowed(lower[:at])
	case pattern.bare:
		return chineseBareNarrowed(rest)
	case strings.HasPrefix(phrase, "파일") || strings.HasPrefix(phrase, "코드"):
		return koreanNarrowed(heldWords(heldTokens(clauseBefore(lower[:at]))))
	case strings.HasPrefix(phrase, "ファイル") || strings.HasPrefix(phrase, "コード"):
		return japaneseNarrowed(lower[:at])
	case strings.HasSuffix(phrase, "任何"):
		return chineseNarrowed(rest)
	}
	words := []heldWord{}
	for _, token := range heldTokens(clauseAfter(lower[at:])) {
		if token.stop == 0 {
			words = append(words, heldWord{raw: token.raw, word: token.word})
		}
	}
	if count := len(pattern.words); len(words) >= count {
		return narrowedTail(words[count-1].word, words[count:], false)
	}
	return false
}

func narrowedTail(last string, tail []heldWord, qualified bool) bool {
	switch {
	case narrowQuantifiers(last):
		for len(tail) > 0 && narrowOfWords(tail[0].word) {
			tail = tail[1:]
		}
		if len(tail) == 0 || narrowListWords(tail[0].word) {
			return false
		}
		if !narrowWholeObjects(tail[0].word) {
			return true
		}
		tail = tail[1:]
	case !narrowObjects(last):
		return false
	}
	if len(tail) == 0 {
		return qualified
	}
	return !narrowEnd(tail)
}

func narrowEnd(tail []heldWord) bool {
	first, next := tail[0].word, heldWordAt(tail, 1)
	if first == "bis" && next == "auf" {
		return heldWordAt(tail, 2) == "weiteres"
	}
	if narrowEnders(first) || strings.HasPrefix(first, "و") {
		return true
	}
	if narrowArticles(next) {
		next = heldWordAt(tail, 2)
	}
	if narrowAsideLeads(first) && (narrowAsideWords(next) || narrowQuantifiers(next)) {
		return true
	}
	for len(tail) > 1 && (narrowOfWords(tail[0].word) || narrowReferenceLeads(tail[0].word)) {
		tail = tail[1:]
	}
	switch word := tail[0].word; {
	case narrowTimes(word), narrowTimeLeads(word) && narrowTimes(heldWordAt(tail, 1)):
		return true
	case narrowProjects(word):
		rest := tail[1:]
		if len(rest) == 1 || len(rest) > 1 && heldCapitalized(rest[0].raw) {
			rest = rest[1:]
		}
		return len(rest) == 0 || narrowEnd(rest)
	}
	return narrowObjects(tail[0].word) && (len(tail) == 1 || narrowEnd(tail[1:]))
}

func koreanNarrowed(words []string) bool {
	if len(words) == 0 {
		return false
	}
	last := words[len(words)-1]
	final, _ := utf8.DecodeLastRuneInString(last)
	return !koreanHolds(last) && !strings.ContainsRune("은는도에서고요면만을를이가과와며", final)
}

func koreanBareNarrowed(words []string) bool {
	if len(words) == 0 {
		return false
	}
	last := words[len(words)-1]
	for _, particle := range []string{"을", "를", "은", "는", "도", "이", "가"} {
		if object, found := strings.CutSuffix(last, particle); found && object != "" && !koreanHolds(last) && !koreanBareHolds(last) {
			last = object
			break
		}
	}
	if last == "파일" || last == "코드" {
		return koreanNarrowed(words[:len(words)-1])
	}
	return !koreanHolds(last) && !koreanBareHolds(last)
}

func japaneseNarrowed(before string) bool {
	for _, hold := range japaneseHolds {
		if strings.HasSuffix(before, hold) {
			return false
		}
	}
	last, _ := utf8.DecodeLastRuneInString(before)
	return last == 'の' || last == 'ー' || unicode.IsDigit(last) || unicode.In(last, unicode.Katakana, unicode.Han, unicode.Latin)
}

func japaneseBareNarrowed(before string) bool {
	object := strings.TrimRight(clauseBefore(strings.TrimRight(before, " 　、")), " 　")
	for _, particle := range []string{"には", "を", "は", "も", "に", "の"} {
		if trimmed, found := strings.CutSuffix(object, particle); found {
			object = trimmed
			break
		}
	}
	for _, noun := range []string{"ファイル", "コード"} {
		if head, found := strings.CutSuffix(object, noun); found {
			return japaneseNarrowed(head)
		}
	}
	if object == "" {
		return false
	}
	for _, holds := range [][]string{japaneseBareHolds, japaneseHolds} {
		for _, hold := range holds {
			if strings.HasSuffix(object, hold) {
				return false
			}
		}
	}
	return true
}

func chineseNarrowed(rest string) bool {
	first, _ := utf8.DecodeRuneInString(rest)
	if !unicode.Is(unicode.Han, first) {
		return false
	}
	for _, hold := range chineseHolds {
		if strings.HasPrefix(rest, hold) {
			return false
		}
	}
	return true
}

func chineseBareNarrowed(rest string) bool {
	for _, lead := range chineseBareLeads {
		if object, found := strings.CutPrefix(rest, lead); found {
			return chineseNarrowed(object)
		}
	}
	for _, pronoun := range []string{"这个", "這個", "它", "这", "這", "了", "吧"} {
		if after, found := strings.CutPrefix(rest, pronoun); found {
			if next, _ := utf8.DecodeRuneInString(after); !unicode.Is(unicode.Han, next) {
				return false
			}
		}
	}
	return chineseNarrowed(rest)
}

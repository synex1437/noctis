# Contributing

## Build

```sh
cd go && go build -trimpath -ldflags="-s -w" -o ../bin/noctis ./cmd/noctis
```

Go 1.22, standard library only — no new dependencies, please. `gofmt -l ./cmd/noctis` must print nothing and `go vet ./...` must be clean.

## Test

Tests are black-box: Node drives the real binary with a fake `claude`, a fake usage API and compressed time.

```sh
node tests/contract.js                     # runs every hook the way the host declares it, ~1 min
node tests/chaos.js                        # network/disk/migration failures, ~2 min
                                           #   (add NOCTIS_CHAOS_FULL_DISK=/path/to/a/small/mount
                                           #    for the real ENOSPC case; skipped without it)
node tests/lab.js                          # 503 checks, ~5 min
node tests/torrent.js                      # thousands of parallel jobs: does the guard ever leak,
                                           #   and what does it cost per hook? ~10 min
node tests/monkey.js --seed 7 --rounds 400  # the chaos user: random order, broken files, killed processes
node tests/hygiene.js                      # control bytes, invisible spaces, checksums, versions
node tests/soak.js --days 3 --hard 1       # chaos soak: corrupted files, outages, SIGKILLed hooks, clock jumps
node tests/soak.js --days 7                # normal multi-day soak
```

The soak invariant is the contract: no model call above 100 %, no leaked locks or temp files, no session without a resume path, no `fatal`. A change that needs a new rule needs a lab check for it.

## Rules of the engine

- Decisions are deterministic. No LLM calls, nothing on the hot path that costs tokens or waits on the network (the OAuth poll is single-flight and rate-limited).
- Everything user-facing goes through the message catalog (`messages.go`: `en` and `tr` with identical keys and format verbs; `lang.go`: the person-facing subset for the other twelve languages, falling back to English). Directives sent to Claude stay English.
- Every enforcement path must have an `observe`-mode `would-*` journal entry and a `noctis why` reason.
- Shipping binaries: rebuild all six targets (see `go/README.md`), copy `bin/windows-amd64/noctis.exe` to `bin/noctis.exe`, regenerate `bin/SHA256SUMS` (`cd bin && for f in noctis.exe */noctis*; do sha256sum "$f"; done > SHA256SUMS`). The engine refuses to place a binary that does not match that manifest, so a local build without this step will say so on the next session start; the test harnesses regenerate it themselves.

## Pull requests

Keep the description to what changed and why; include the lab/soak result lines.

## Translations

Message catalogs live in `i18n/<code>.json`, one file per language, keyed the same as the English
one. English and Turkish are the reference pair and live in `go/cmd/noctis/messages.go`; the other
twelve are generated into `go/cmd/noctis/lang.go`:

```
node scripts/i18n.js check      # (the default) verify lang.go matches i18n/*.json
node scripts/i18n.js build      # rewrite lang.go from i18n/*.json
node scripts/i18n.js extract    # refresh i18n/en.json and i18n/tr.json from messages.go
```

Never edit `lang.go` by hand — `tests/hygiene.js` fails when it and the JSON disagree, and the next
`build` would throw the edit away. Adding a key to English and leaving the others empty also fails:
a missing message does not crash, it silently prints an English sentence in the middle of a
translated one, so `TestEveryLanguageIsComplete` treats a gap as a build failure.

When a sentence needs a different word order, use Go's explicit argument indexes (`%[2]s`) rather
than reordering the plain verbs — otherwise the values are printed in the wrong places, which is
exactly the kind of bug nobody reading that language would report.

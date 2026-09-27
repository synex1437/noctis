# Contributing

## Build

```sh
node scripts/build.js          # every platform, built as CI builds them, then bin/SHA256SUMS
node scripts/build.js --host   # only this machine's binary, then bin/SHA256SUMS
```

It needs Go and Node and nothing else, on Linux, macOS or Windows; `--go <path>` picks another Go toolchain than the `go` on PATH. Linux and Windows get one binary per CPU, `bin/<os>-<arch>/noctis` (`noctis.exe` on Windows); macOS gets one universal binary for both CPUs, `bin/darwin/noctis`. On a Mac, `--host` puts this Mac's build alone there: enough to run the suites, and `tests/hygiene.js` fails until a full build replaces it. `bin/noctis` is the POSIX launcher script and `bin/noctis.exe` a copy of the windows-amd64 binary — never build over `bin/noctis` (`tests/hygiene.js` fails). A binary built another way, with `go build -o ../bin/…` in `go/`, leaves `bin/SHA256SUMS` stale, and no suite fixes that for you: run `node -e "require('./tests/harness.js').refreshChecksums()"`. If you do not mean to ship binaries, `git checkout -- bin/` before committing.

Go 1.24.7+ compiles it: the `go` line in `go/go.mod` names that version, so the binaries run with its defaults (its TLS settings among them: see `GODEBUG` in [docs/REFERENCE.md](docs/REFERENCE.md) for what they change and how to turn them off), except that the `godebug` block there keeps Windows junctions and link targets read as Go 1.22 read them (`winsymlink=0`, `winreadlinkvolume=0`). Go refuses a `godebug` block that lists a setting it does not know (`unknown godebug "…"`), so a Go release that drops these two will not build the module until they leave the block; the checks that stop a queue file or an import from leading out of its folder (`filepath.EvalSymlinks` in `engine.go` and `github.go`) must then work under `winsymlink=1`, where `EvalSymlinks` no longer evaluates mount points (junctions). Binaries you commit must be built with the Go version in `.github/workflows/ci.yml` (1.24.7) and exactly CI's flags, which `node scripts/build.js` uses, or the "Committed binaries must match the source" step fails. Standard library only — no new dependencies, please. `gofmt -l ./cmd/noctis` (in `go/`) must print nothing and `go vet ./...` must be clean.

## Test

Most suites are black-box: Node drives the real binary with stand-ins for `claude`, `codex`, `agy`, `droid`, `copilot` and `gh`, a fake usage API and compressed time; `go test` covers the decision core directly. What each suite asks and what the latest run measured: [docs/TESTING.md](docs/TESTING.md).

```sh
(cd go && go test ./...)                   # unit tests, fuzz seed corpora, dead-code guards
node tests/contract.js                     # runs every hook the way the host declares it, seconds
node tests/chaos.js                        # network/disk/migration failures, ~15 s
                                           #   (add NOCTIS_CHAOS_FULL_DISK=/path/to/a/small/mount
                                           #    for the real ENOSPC case; skipped without it)
node tests/scheduler.js                    # the OS scheduler this platform really uses, ~1 min
node tests/lab.js                          # the black-box lab (count: see docs/TESTING.md), a few minutes
node tests/torrent.js                      # thousands of parallel jobs: does the guard ever leak,
                                           #   and what does it cost per hook? ~10 min
node tests/monkey.js --seed 7 --rounds 400  # the chaos user: random order, broken files, killed processes
node tests/hygiene.js                      # control bytes, invisible spaces, checksums, versions
node tests/soak.js --days 3 --hard 1       # chaos soak: corrupted files, outages, SIGKILLed hooks, clock jumps
node tests/soak.js --days 7                # normal multi-day soak
node tests/coverage.js                     # statement coverage of the lab plus go test (linux/amd64 only)
```

The soak invariant is the contract: no model call above 100 %, no leaked locks or temp files, no session without a resume path, no `fatal`. A change that needs a new rule needs a lab check for it.

## Rules of the engine

- Decisions are deterministic. No LLM calls, nothing on the hot path that costs tokens or waits on the network (the OAuth poll is single-flight and rate-limited).
- Everything user-facing goes through the message catalog (`messages.go`: `en` and `tr` with identical keys and format verbs; `lang.go`: the other twelve languages, each complete, generated from `i18n/*.json`). Directives sent to Claude stay English.
- Every enforcement path must have an `observe`-mode `would-*` journal entry and a `noctis why` reason. (Three paths do not follow this yet: the lite/digest write limits apply in observe mode too and are not journaled, and the `model` reset after `model_not_found` and the issue closing of `queue.github.closeOnDone` still act in observe mode, journaled as `model-unavailable` and `close-issue`.)
- Nothing may make Claude Code install packages into the plugin: `tests/hygiene.js` fails on a `package.json` at the root next to a `package-lock.json`, `npm-shrinkwrap.json`, `bun.lock` or `bun.lockb`.
- Every job of a workflow that runs the suites sets `timeout-minutes`, so a suite that hangs cannot hold a runner for six hours; `tests/hygiene.js` checks it.
- Releasing: raise the version in `store.go`, `plugin.json` and `marketplace.json` (`tests/hygiene.js` fails when they disagree) and push to main. Once ci passes on that commit, `.github/workflows/release.yml` builds its binaries from source with `node scripts/build.js`, checks them against `bin/SHA256SUMS`, attests them and publishes the GitHub release, which creates the tag `v<version>` on that commit. The macOS binary goes out as `noctis-darwin-universal` and, for links made before 7.5.0, also as `noctis-darwin-amd64` and `noctis-darwin-arm64`: the same universal file under all three names. Nobody pushes a version tag, and a version whose tag exists is not released again; `tests/hygiene.js` checks that the workflow keeps to this.
- Shipping binaries: rebuild every target with `node scripts/build.js`, using the Go version in `ci.yml`. It builds windows, darwin and linux for amd64 and arm64 at once, each exactly as CI does (`CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w"`), joins the two darwin builds into the universal `bin/darwin/noctis`, copies the windows-amd64 build to `bin/noctis.exe` and regenerates `bin/SHA256SUMS` with the same code CI uses (a hand-written `sha256sum` loop easily misses the `bin/noctis` launcher or orders the lines differently, and CI compares the file byte for byte). The universal binary is a header that lists the two builds, then each build exactly as Go's linker wrote it, x86_64 first, at a 16 KB boundary with zeros between: the same Go gives the same file byte for byte on any machine, and the ad hoc signature Go's linker gives the arm64 build (Apple silicon runs no unsigned code) stays valid, because putting a build in the universal binary changes none of its bytes. The script checks the file it wrote against both builds, and has `lipo`, `llvm-lipo` or `file` read it where the machine has them; `tests/hygiene.js` checks its layout and that signature on any system, and `tests/contract.js` runs it on a Mac.

  The engine checks a binary against that manifest whenever it places one in a plugin folder without `go/` — a clone install made by `noctis install` or `scripts/install.*`, and `ensure` or `setup` run from such a folder — and refuses a mismatch; inside a source checkout (`go/go.mod` present) it uses the binary as is. A marketplace install counts as a checkout here, because Claude Code's copy of the plugin carries `go/go.mod`: it keeps the `bin/noctis` launcher, so on macOS and Linux every hook goes through `sh`. The test suites never rewrite the manifest: each lab checksums its own snapshot of the tree, so a local build runs as it is, and `tests/hygiene.js` reports the manifest as stale until you regenerate it or restore `bin/` (`tests/coverage.js` swaps in an instrumented binary and restores both on exit).

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

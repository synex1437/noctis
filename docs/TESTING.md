# Tests

The suite behind the numbers in the README: what each run asks, and what the latest one measured.

Tests, each asking a different question:

| | |
| --- | --- |
| `node tests/contract.js` | Runs every hook in `hooks/hooks.json` the way Claude Code runs it — resolving `${CLAUDE_PLUGIN_ROOT}`, spawning that command with those arguments. Everything else in the suite pipes a payload into `noctis hook`, which is the one way the host never calls it. |
| `node tests/lab.js` | 504 black-box checks against the binary, with a fake `claude`, `codex`, `agy`, `droid`, `copilot`, `gh` and a usage endpoint in a separate process. |
| `node tests/torrent.js` | Thousands of long sessions at once. Asks the two questions only volume answers: does the guard ever let a session past the wall, and what does it cost per hook. |
| `node tests/chaos.js` | The failures a real machine produces: 429/403/500, invalid JSON, a body cut halfway, a chunked response cut before its terminating chunk, TLS that will not negotiate, a genuinely full disk, and state files written by older versions. |
| `node tests/scheduler.js` | The OS schedulers, which nothing had ever run. A strict stand-in for `systemd-run` goes on PATH, the plugin's own backend detection picks it, and a parked session really is relaunched by the timer with nobody watching. The launchd plist is validated by Python's `plistlib` — Apple's own parser — and the Windows task script by parsing it. |
| `node tests/soak.js --days 14 [--hard 1]` | A fortnight of one account, with 20 kinds of chaos injected. |
| `node tests/monkey.js` | A chaos user hammering it in random order. |
| `node tests/hygiene.js` | Control bytes, invisible characters, executable bits, checksum freshness, version agreement, the state schema, and that `lang.go` still matches `i18n/*.json`. |
| `go test ./...` | Unit tests for the decision core, the scheduler and the message catalogs, plus five fuzz targets — and three structural tests that fail the build on dead code: a capability flag nothing reads, a field set and never read, a parameter passed and discarded. |
| `node tests/coverage.js` | How much of the Go source the suite actually reaches, measured by running the lab against a `go build -cover` binary and merging that with the unit profile. |

Latest run: lab 504/504 · contract 109/109 · chaos 69/69 (including a genuinely full filesystem) · scheduler 23/23 (**a parked session really was relaunched by a systemd timer**) · **2 000 sessions and 9 768 hooks through the torrent with 0 leaks**, median hook 17.7 ms · 0 breaches and 0 anomalies over 7-day hard and 14-day hard soaks · monkey clean on seeds 41 and 77 · **80.5% of statements covered** (`node tests/coverage.js`).

What that costs, measured rather than asserted: a hook is ~7 ms (median) on a quiet single session, and the cost scales with the size of `state.json` — 1 KB of state is a 5 ms hook, 313 KB is a 31 ms hook — which is why the state file is pruned rather than left to grow.

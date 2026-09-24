# noctis 7.0.1

7.0.1 is about the part of noctis that does its work while nobody watches: bringing a paused
session back when the limit resets. That path was read again from the pause to the relaunched
session's first answer, on every backend, and so were the findings the 7.0 review had left for
later. Forty-five product bugs came out of it. Each fix came with a test that fails without it; the
scheduler stubs were made to start jobs the way launchd and systemd really do, and on the old code
they now fail the way a user's machine did.

## The ones that matter

**A relaunch that failed was taken for a resume.** The runner treated every relaunch that started
as done and deleted the wait, so a relaunch that failed at once — claude could not resume the
session, the network was not back after a wake, the API answered with an error, a node shim was
missing — ended the auto-resume without a retry or a word. A relaunch now has to show that the
session answered (a headless run that exits with an error before any reply, or a window that closes
within 30 seconds without touching the transcript, did nothing); otherwise the wait is kept and
retried on the `wait.retryMinutes` steps, and the fifth attempt gives up with a notification naming
the resume command.

**The error that ended a turn counted as the session carrying on.** For an overload, a Fable
switch and an unexplained failure, the wait's `until` is the second of the failure, and Claude Code
writes the API error to the transcript at that moment. The runner read the newer transcript as the
session having continued and dropped the wait, so none of those relaunches ever happened. Only a
real prompt or answer after the failure counts now.

**On Linux, a runner stopped itself.** Every schedule of a session shared one systemd unit, and
cancelling stopped the `.service` as well as the `.timer`. A runner that rescheduled itself stopped
its own service before the new timer existed, and a relaunched session that reached its next pause
stopped the runner, itself mid-turn and the hook doing the scheduling. Every schedule now gets a
unit of its own and only timers are stopped.

**Scheduled runners could not find claude.** A relaunch through launchd or systemd ran in the
service manager's environment: `PATH=/usr/bin:/bin:…`, no display, no proxy. `claude` from npm, nvm,
Homebrew or `~/.local/bin` — and the node its shim needs — was not found. The timer now carries the
PATH, display, certificate and proxy variables of the session that paused.

**On macOS, every resume left a job loaded for next year.** `StartCalendarInterval` repeats every
year, and a job that had run its resume removed its plist but was never booted out. It now boots
itself out once it is done.

**On Windows, a relaunch through npm's `claude.cmd` lost its prompt.** The quoted command line was
quoted a second time for `cmd.exe`, which splits `\"`, so a `claude.cmd` in a folder with a space
was never found and the relaunch prompt arrived in pieces; a chained status line failed the same
way. `cmd.exe` now gets its command line as written.

**Relaunch windows were killed or killed the wrong thing.** A terminal that stays in the foreground
(xterm, konsole, kitty, alacritty, wezterm) was killed 15 seconds into the resumed turn and the
session relaunched headless on top of it; xfce4-terminal got a command line it refused. And closing
the previous relaunch window used a pid that could, after a reboot, belong to any shell, node or
terminal — on Windows with its whole tree. A window is now closed only while the runner that opened
it still watches it, and a pid outside the kernel's range, a zombie or a protected Windows process
is no longer misjudged (`kill(2^32-1)` used to become `kill(-1)`). On macOS, a launcher in a folder
whose path has a space or a quote never opened its Terminal window, because the path reached the
shell unquoted; the session came back headless a minute later instead.

**A request that can never succeed was retried forever.** An unexplained failure always got the
first retry step, so a prompt that is too long came back every 10 minutes with a notification each
time. Repeated failures now climb the retry ladder and stop after the fifth retry.

**A Fable switch or a 429 retry could lose its runner.** Both scheduled the runner before storing
the wait; a runner that looked first found nothing and gave up. The wait is stored first now.

## Safety

- `resume.extraArgs` could lift an unattended relaunch to `--dangerously-skip-permissions` or a
  bypass mode (and the other tools' equivalents); those flags are dropped.
- `resume.permissionMode: "manual"` — Claude Code 2.1.281's name for asking every time — relaunched
  with acceptEdits.
- The OAuth usage fetch followed redirects, which keep the bearer token on the same host and its
  subdomains; it no longer follows any.
- An imported issue title could reorder, delay or tag the queue with `(P0)`, `(after …)` and
  `#tag`.
- A role's model or effort with a line break could add frontmatter lines to the research agent,
  widening the tools it is kept away from.
- A resume prompt that starts with a dash was read as an option and every relaunch failed.

## What the guard reads

- A status-line render without usable `rate_limits` stamped two-hour-old windows as fresh.
- A usage answer in an unknown shape wiped the last reading and was stamped fresh.
- `"thresholds": 90` (or any non-object) switched every window off without a word.
- A threshold switched off (0, null, false) was read as a pause point at 0 % in several places:
  Fable counted as exhausted, the model switch was never reverted, the badge warned, the pace marker
  lied.
- A UTF-8 byte order mark made `settings.json`, `config.json` or a hook file "broken" and hid the
  first `TASKS.md` item.
- Without a home folder, the working folder became the account.
- Setup left `CLAUDE_CONFIG_DIR` and `NOCTIS_PLUGIN_ROOT` set to empty for the programs it started;
  Claude Code takes an empty `CLAUDE_CONFIG_DIR` for its config folder.

## The queue

- The Stop hook's own continuation, sent back as a prompt (Copilot), reset the idle count, so a
  queue that made no progress ran to the 200-continuation cap.
- A 1 MiB item took 13.5 s to parse (quadratic); it takes 0.02 s.
- An item that named itself in `(after …)` waited forever; `(after #123)` never waited for issue
  #123's item; checklist lines inside a code fence were tasks — and ticking a fenced example closed
  the real issue on GitHub.

## What you see

- Notices named `claude --resume` on every tool; they name the tool's own resume command.
- With both windows in their warn bands, the warning named the one further from its pause point.
- `noctis report` counted every content block of a response, about twice the real usage.
- The status-line badge wrote percentages in Turkish order (`%41`) in every language.
- The Fable badge never warned, and a paused guard looked like a working one; it shows `∞⏸`.
- Pause notices advised `/noctis:pause 120` at the 100 % ceiling, where a pause does not hold, and
  promised auto-resume under `resume.mode: none`.
- Chinese, Korean and Japanese notices swapped their values; six Turkish messages glued a suffix to
  a time or a model name (`16:00'de`), and the catalog check now refuses that; the weekly label did
  not fit its sentences in eight languages.
- A Windows notification beeped three times before its toast, through Focus Assist.

## Checked, not changed

- Done markers (`~~…~~`, `(done)`, `✓`) inside a checkbox list: the box decides there, as README
  says.
- Claude Code 2.1.281 still reports the manual mode to hooks as `default`, so `inherit` was never
  affected; only the configured `"manual"` was.
- 2.1.281 refuses `claude plugin marketplace update --auto-update`; setup's attempt still fails for
  that reason, as README says.

## Tests

Every fix above came with a test that fails on the code just before it. The scheduler stubs now
start a job with `PATH=/usr/bin:/bin` and only what the unit or agent declares, as launchd and
systemd do; without the PATH fix the scheduler suite fails 10 of its 39 checks there. Local rounds: go test on Linux with
Windows and macOS builds and vets, hygiene, i18n, contract, chaos, scheduler, lab, a two-day hard
soak, the torrent and two monkey seeds.

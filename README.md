<p align="center">
  <img src="docs/banner.svg" alt="Noctis — a Claude Code plugin that pauses before the 5-hour or weekly usage limit and resumes after the reset" width="100%">
</p>

<p align="center">
  <a href="https://github.com/synex1437/noctis/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/synex1437/noctis/ci.yml?branch=main&event=push&style=flat-square&label=CI&labelColor=0d0d10&color=ff3347"></a>
  <a href="https://github.com/synex1437/noctis/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/synex1437/noctis?style=flat-square&label=release&labelColor=0d0d10&color=ff3347"></a>
  <img alt="Windows, macOS, Linux" src="https://img.shields.io/badge/Windows%20%C2%B7%20macOS%20%C2%B7%20Linux-one%20binary-ff3347?style=flat-square&labelColor=0d0d10">
  <img alt="Claude Code, Codex CLI, Antigravity CLI, Droid, Copilot CLI" src="https://img.shields.io/badge/Claude%20Code%20%C2%B7%20Codex%20%C2%B7%20Antigravity%20%C2%B7%20Droid%20%C2%B7%20Copilot-5%20tools-ff3347?style=flat-square&labelColor=0d0d10">
  <a href="LICENSE"><img alt="MIT license" src="https://img.shields.io/badge/license-MIT-ff3347?style=flat-square&labelColor=0d0d10"></a>
</p>

<p align="center">
  <a href="#install"><b>Install</b></a>&nbsp;&nbsp;·&nbsp;&nbsp;<a href="#quick-start"><b>Quick start</b></a>&nbsp;&nbsp;·&nbsp;&nbsp;<a href="#how-it-compares"><b>Compare</b></a>&nbsp;&nbsp;·&nbsp;&nbsp;<a href="#commands"><b>Commands</b></a>&nbsp;&nbsp;·&nbsp;&nbsp;<a href="#faq"><b>FAQ</b></a>&nbsp;&nbsp;·&nbsp;&nbsp;<a href="docs/GUIDE.md"><b>Guide</b></a>&nbsp;&nbsp;·&nbsp;&nbsp;<a href="README.tr.md"><b>Türkçe</b></a>
</p>

# Noctis — Claude Code plugin: pause before usage limits, auto-resume after the reset

**Noctis keeps long Claude Code jobs running overnight. It pauses just before the 5-hour or weekly usage limit, waits for the reset and resumes the same session on its own, while it works through your task queue without stopping to ask.**

You queued forty tasks, went to bed, and woke up to *"You've hit your session limit"* at 01:40 — or to a session that stopped at task three with *"moving on to the next thing"*. Noctis handles both: it stops Claude Code before the rate limit instead of after it, comes back when the limit resets, and keeps going down your list. Along the way it keeps Claude sharp: a fresh context once the conversation grows long, your check between items, and a stronger model for the item it gets stuck on.

<p align="center"><img src="docs/demo.svg" alt="A night with noctis: it pauses before the 5-hour limit, waits out the reset in the same turn, keeps working down TASKS.md and stops cleanly when the queue is empty" width="100%"></p>

<table align="center">
  <tr>
    <td align="center" width="25%"><b>~5 ms</b><br><sub>per hook on Linux</sub></td>
    <td align="center" width="25%"><b>0 tokens</b><br><sub>per decision</sub></td>
    <td align="center" width="25%"><b>1 binary</b><br><sub>no Node, no Git Bash</sub></td>
    <td align="center" width="25%"><b>14 languages</b><br><sub>the one you type in</sub></td>
  </tr>
</table>

## What it does

- **Pauses before the limit, not after the 429.** At 92 % of the 5-hour window and 95 % of the weekly one by default, or earlier when a burst or the burn rate says the next turns would cross it, after a checkpoint of the last request, the touched files, `git status`, todos and the next jobs.
- **Auto-resumes the same session.** A reset within about 5½ hours is waited out inside the turn, context intact. A later one, such as the weekly limit, relaunches `claude --resume` from Task Scheduler, launchd or systemd, even days later and even after Claude Code was closed; Windows, and Linux with `CAP_WAKE_ALARM`, can wake the machine for it.
- **Works through a task queue.** `/noctis:start jobs.md` runs the jobs in a file in order without stopping to ask; a long prompt with several tasks becomes such a list by itself; a `TASKS.md` you trust drives every session, with priorities, dependencies and GitHub issues. Steps marked `(human)` wait for you, a `noctis-verify` line runs your checks between items, and long builds run as background jobs the queue waits for.
- **Keeps Claude in its smart zone.** Once a session's context passes 100k tokens, each next item goes to a fresh `noctis:worker` subagent with a brief of its own; after a compaction Claude is told the item in hand, the files changed and how the check stands; a failing check is fixed in the code, not in its tests; an item Claude keeps stopping on goes once to a stronger model; and `noctis queue status` shows what each item took, so you can size the next ones.
- **Never spends paid usage credits.** It stops at 100 % of a window even when the thresholds are off, and refuses a fan-out workflow that would not fit in what is left.
- **Zero tokens per decision.** Fixed rules over your usage data, and every decision logged (`noctis why`). One self-contained Go binary, about 5 ms per hook on Linux; no Node, Git Bash or compiler.
- **The right model for each job.** Code on Opus 5.5 · xhigh by default (the Code profile), or on Sonnet 5.5 · high to spend less (Balanced), file search and output digests on Haiku 4.5, and, with the router on, research and writing in a subagent of their own.
- **Lean compaction, a status line and 14 languages.** It compacts between turns at 70 % context, draws a status line with both usage windows and their reset times, and speaks the language you type in.
- **Also runs in** OpenAI Codex CLI, Antigravity CLI, Factory Droid and GitHub Copilot CLI, with fewer features.

## Install

Inside Claude Code, about a minute:

```
/plugin marketplace add synex1437/noctis
/plugin install noctis@noctis
/noctis:setup
/reload-plugins
```

> [!IMPORTANT]
> Setup switches Claude Code to **auto permission mode: Claude edits files and runs commands without asking**. That is what lets it work while you sleep; `/noctis:setup --permissions keep` leaves your mode as it is. Setup also sets the default model and effort and the function hooks switch lean compaction needs, and the status line points at noctis from the first session. Setup backs up `settings.json` first, and every change has an undo: [what it changes and how to undo it](docs/GUIDE.md#what-it-changes-on-your-machine--and-how-to-undo-it).

Setup asks one question, which model does which kind of work, and keeps the answer as your roles profile. If `/noctis:setup` is not found yet, run `/reload-plugins` first. From a terminal: `claude plugin marketplace add synex1437/noctis && claude plugin install noctis@noctis`, then `/noctis:setup` inside Claude Code. Clone and ZIP installs, the profiles and every flag: [install details](docs/GUIDE.md#install-details).

**Requirements:** Claude Code 2.1.251 or newer (lean compaction was tested with 2.1.281); Windows, macOS or Linux; a Pro or Max plan for the limit guard. Every profile runs on models every paid plan includes: Opus 5.5 and Haiku 4.5, and Sonnet 5.5 in Code and Balanced, which needs Claude Code 2.1.284 or newer.

## Quick start

1. Look at the bottom of the window: `∞ 5h 41%→14:35 · Wk 23%▲→Mon 21.09 09:00 · Opus 5.5/xhigh · ctx 37%` shows both usage windows and when each resets, the model and effort, and how full the context is.
2. Put a few jobs in a file, one per line (`- [ ] write tests for the payments module`), and type `/noctis:start TASKS.md`. Claude works through them in order; `/noctis:stop` ends it early.
3. Go to bed. At a limit there is nothing to do: noctis pauses, waits and continues, and a desktop notification tells you when work resumes.

Not ready to let it act? `"mode": "observe"` in `~/.claude/noctis/config.json` logs every decision and enforces none of them, not even the stop at 100 %. More in [the first five minutes](docs/GUIDE.md#the-first-five-minutes).

## Why not just let Claude Code continue?

Recent Claude Code versions continue a session on their own once a usage limit resets, as long as that session stays open, the machine stays awake and the reset is less than 24 hours away. Noctis covers the rest: it pauses *before* the limit with a checkpoint, relaunches the session from a scheduled task when Claude Code was closed or the reset is days away (the weekly limit), and keeps a queue of jobs moving. When Claude Code's own continue fires, noctis cancels its relaunch.

<p align="center"><img src="docs/before-after.svg" alt="The same night with and without noctis, as an illustration: Claude Code alone waits out the 01:40 limit, then stops at 03:20 with 21 of 48 tasks done; with noctis all 48 are done by 06:55" width="100%"></p>

## How it compares

| | noctis | usage dashboards / status lines | loop plugins ("keep going") | auto-resume scripts |
|---|:---:|:---:|:---:|:---:|
| Stops **before** the limit (thresholds, burst and burn-rate projection) | ✅ | show only | — | react after the 429 |
| Resumes on its own after the reset (same session, or a relaunch days later) | ✅ | — | — | partly |
| Stops at 100 % instead of spending paid usage credits | ✅ | — | — | — |
| Keeps a task queue moving across stops: priorities, dependencies, GitHub issues | ✅ | — | ✅ (flat) | — |
| Deterministic, zero tokens per decision, journaled (`noctis why`) | ✅ | ✅ | prompt-driven | varies |
| Overload (529/5xx) backoff, separate from limit handling | ✅ | — | — | some |
| One self-contained binary, no runtime to install | ✅ | varies | varies | varies |
| Claude Code, and with fewer features Codex CLI, Antigravity CLI, Droid and Copilot CLI | ✅ | some | Claude only | some |

Status lines such as ccstatusline and claude-powerline keep running behind noctis's, and usage dashboards such as ccusage and Claude-Code-Usage-Monitor are unaffected. Another tool that resumes sessions after a limit, such as unsnooze or claude-auto-resume, would race noctis, so use one of them. [Works alongside other plugins](docs/GUIDE.md#works-alongside-other-plugins).

## Commands

| Command | What it does |
|---|---|
| `/noctis:setup` | First setup; run it again to change which model does what |
| `/noctis:status` | Usage, pause points, roles, pending waits and the last decisions |
| `/noctis:start <file>` | Works through the jobs in a file without stopping and says how many it found |
| `/noctis:stop` | Ends this session's queue |
| `/noctis:pause [minutes]` | No limit pauses, routing or queue continuation for a while (60 minutes by default); the stop at 100 % still applies |
| `/noctis:resume` | Ends a pause early |
| `!noctis why` | The decisions noctis made, and why; `--stats` sums up the last week of them |
| `!noctis doctor` | Checks the install and lists the neighbours it can see |

`/noctis:setup`, `/noctis:pause`, `/noctis:start` and `/noctis:stop` run only when you type them. The `noctis` command itself runs inside Claude Code with a leading `!` (`!noctis status`, `!noctis queue trust`, `!noctis off 30`), because the plugin's `bin/` folder is on the PATH of Claude Code's shell; in a terminal, use the full path setup prints. Every command and flag: [REFERENCE.md](docs/REFERENCE.md#commands).

## FAQ

<details>
<summary><b>Will it spend my extra usage credits?</b></summary>

No. Work stops at 100 % of a window, or just before it when the last readings jumped far enough, even when your thresholds are misconfigured or switched off and even during `/noctis:pause`. Set `"credits": {"allowPaid": true}` if you want the overflow. Noctis cannot switch off auto-reload on the account; that is in your Anthropic billing settings.
</details>

<details>
<summary><b>What does it send over the network?</b></summary>

No telemetry. When the status line is not enough, it asks the usage endpoint the Claude app itself uses on `api.anthropic.com`, with the login token Claude Code already keeps. Once a day it fetches `plugin.json` from GitHub to see whether a newer version exists (`update.check: false` turns that off). Nothing else, except a webhook you set and GitHub through your own `gh` CLI when you import issues. [Details](docs/GUIDE.md#what-it-changes-on-your-machine--and-how-to-undo-it).
</details>

<details>
<summary><b>Does it work with an API key?</b></summary>

An API key has no usage windows, so the limit guard has nothing to act on, and the status line shows *waiting for limit data*. The queue, the router and the 529/5xx retry still work.
</details>

<details>
<summary><b>Can a cloned repository's TASKS.md make Claude run things?</b></summary>

Not until you trust it. A `TASKS.md` drives nothing until you type `!noctis queue trust`, and a line added or changed later stops it again until you trust it anew. When Claude runs `noctis queue trust` itself, the hook refuses the call. A check command the file names on a `noctis-verify` line runs only while your trust covers that line. [Queue file](docs/GUIDE.md#queue-file).
</details>

<details>
<summary><b>Can it carry a large project on a server for days?</b></summary>

That is what the queue is for. Run `claude` inside tmux on the server, write the plan as a `TASKS.md` with priorities, dependencies and a `noctis-verify` check line, mark the steps only you can do `(human)`, give a hard or an easy step its model with `(opus)` or `(sonnet)`, and trust the file once you have read it. Claude works through it and commits as it goes, runs long builds as `noctis job` jobs, hands an item it gets stuck on once to a stronger model before it sets it aside, sets aside what waits on something outside the session, and stops with the list of what is yours once nothing else is left; a relaunch after a long wait opens as a new tmux window, `noctis queue status --json` tells a monitoring script where the queue stands, and with `alarm.digestAt` set your webhook sends you a digest of it every day, with what an item takes of the weekly limit, when the rest may be done and, once the numbers say so, which profile finishes the queue for less. [Large projects on a server](docs/GUIDE.md#large-projects-on-a-server).
</details>

<details>
<summary><b>How do I turn it off or uninstall it?</b></summary>

For a while: `/noctis:pause 120`. Completely, inside Claude Code, with the first line before the second, because removing the plugin deletes the binary that undoes the settings:

```
!noctis install --uninstall --host claude
/plugin uninstall noctis
```

The first line cancels pending resumes and restores `settings.json`; `--purge` also deletes `~/.claude/noctis/`. [Everything uninstall does](docs/GUIDE.md#what-it-changes-on-your-machine--and-how-to-undo-it).
</details>

<details>
<summary><b>How is it tested?</b></summary>

Every push runs the Go tests on Linux, macOS and Windows, fuzzing, a lab that drives the hooks against stand-ins for Claude Code and the usage endpoint, a simulated two-day soak, a torrent of 600 jobs and a chaos monkey. CI also rebuilds the binaries in `bin/` and fails unless they match the committed ones byte for byte. [TESTING.md](docs/TESTING.md).
</details>

## Documentation

- [Guide](docs/GUIDE.md): what it changes and how to undo it, the queue format, what it does while you are away, large projects on a server, lean compaction, the status line, install details and the other AI coding tools
- [Reference](docs/REFERENCE.md): every command, flag, configuration key and file
- [Testing](docs/TESTING.md): what each suite asks and what the latest runs measured
- [Release notes](https://github.com/synex1437/noctis/releases)

## Contributing

Bug reports with `noctis doctor` output and `noctis why --last 20` are the most useful thing you can send; `noctis report --bundle` zips them with the logs ([read it before you attach it](docs/GUIDE.md#bug-reports)). Build and test loop: [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT — © 2026 synex

<p align="center"><sub>If noctis kept your queue moving, a ⭐ helps other Claude Code users find it&nbsp;&nbsp;·&nbsp;&nbsp;<a href="#top">back to top</a></sub></p>

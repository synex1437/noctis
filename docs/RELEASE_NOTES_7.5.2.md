# noctis 7.5.2

7.5.2 makes a pause start fewer programs, and fixes a window relaunch that could start a session
twice. When a session stops on a limit or a failed request (`StopFailure`), and when it pauses at a
pause point or to leave Fable, noctis fingerprints the working tree, so that at resume it can tell
you and Claude whether the tree changed while the session waited. In a git repository that took two
git processes, `git status` and `git rev-parse HEAD`. noctis now reads the commit `HEAD` points at
from the repository's files, as git does, and starts `git status` alone. A runner that cannot be
scheduled natively, in a cloud session or with `NOCTIS_NO_TASKS` set, also no longer asks
`systemctl` which native scheduler it would have used. And a session noctis relaunches in a terminal
window, as it does by default, could now and then be started a second time, headless, while it was
starting in its window; it is now started once. 7.5.2 carries everything in 7.5.1
([RELEASE_NOTES_7.5.1.md](RELEASE_NOTES_7.5.1.md)).

## If you are upgrading

Nothing to do. 7.5.2 adds no settings, and the files noctis keeps are read and written as 7.5.1
reads and writes them.

## HEAD from the repository's files

With `wait.workspaceGuard` on, as it is by default, the fingerprint holds what `git status`
reports, the commit `HEAD` points at, and the size and modification time of the files `git status`
lists. noctis took the commit from `git rev-parse --verify HEAD`. It now reads `HEAD` in the
repository's folder (for a linked working tree or a submodule, the folder its `.git` file names),
and the branch `HEAD` names from its file under `refs/heads` or from `packed-refs`, in the folder
all working trees share. Where reading the files would be a guess, it asks git as before: a
reference table (`reftable`), a branch that is itself a symbolic reference, a branch with no commit
yet, a file that cannot be read or does not read as git writes it, and whenever `GIT_DIR`,
`GIT_WORK_TREE`, `GIT_COMMON_DIR`, `GIT_CEILING_DIRECTORIES` or `GIT_NAMESPACE` is set. The commit
read is the one git gives, so the fingerprint of a tree is the one 7.5.1 takes: a pause 7.5.1
recorded and 7.5.2 resumes is not taken for a change. On Windows the files are opened so that a git
command running at that moment can still replace them.

With `checkpoint.gitSnapshot` on, the snapshot a checkpoint pins found the index with
`git rev-parse --git-path index`. It now takes the index from the same folder, unless
`GIT_INDEX_FILE` names another one, and so starts one git process fewer as well.

## No scheduler question where no native runner is scheduled

Before it schedules the runner that resumes a session, noctis picks the native scheduler it would
use, and on Linux that means asking `systemctl --user is-system-running` whether systemd runs for
the user. It asked even where no native runner is scheduled: in a cloud session, which has no
runner, and with `NOCTIS_NO_TASKS` set, as in noctis's own labs. It now asks only where a native
runner may be scheduled. On a Linux desktop the question is still asked, since the answer picks
between systemd and a sleeper.

## A window relaunch starts the session once

With `resume.mode` `window`, the default, noctis relaunches a session in a terminal window through a
small launcher, which writes the process id of the claude it starts to a file. noctis waits for that
file to know the session started, then follows the process until it ends. The launcher's shell
creates the file a moment before it writes the id into it, and noctis read the file as soon as it
was there: read in that moment, it held no id, noctis took the window for one that had not started,
and it ran the session headless as well, so that two claude processes resumed the same session.
noctis now waits for the id, for as long as it waited for the file (60 seconds), and a launcher that
never writes one still counts as one that did not start. The pid file of the Windows launcher is
read through the same wait. A test found this: it failed once in a local round, with the race
detector slowing it down.

## Measured

On Linux with 4 CPUs, against the lab's stand-in for the usage endpoint, with native runners off
as in the labs (`NOCTIS_NO_TASKS`, `NOCTIS_NO_SCHEDULE`) and the hook process alone timed; the
medians of four runs of 40 calls, alternating call by call between the two versions, and what
7.5.2 saved in each run:

| | 7.5.1 | 7.5.2 | Saved |
| --- | --- | --- | --- |
| `StopFailure`, the project in a git repository | 23.57–25.75 ms | 17.87–19.51 ms | 5.39–6.48 ms |
| `StopFailure`, the project in no repository | 18.52–20.15 ms | 14.61–15.60 ms | 3.91–4.89 ms |

Two runs made earlier on the same machine, while it ran faster and before the version was raised,
gave 17.28–18.11 against 12.23–12.38 ms in the repository and 12.82–12.89 against 9.35–9.42 ms
outside one: the time saved stays about the same while the machine's speed moves.

In the git repository 7.5.2 starts one program where 7.5.1 started three (`git status`,
`git rev-parse` and `systemctl`); outside one it starts none where 7.5.1 started `systemctl`.
Outside a repository what is saved is the `systemctl` question, 3.4–4.9 ms in the six runs; in one
the git process saves about 1.5 ms more (0.9–2.3 ms from run to run), and that part is what a Linux
desktop saves, since with native runners on the question is still asked. On Windows, where each
start of Git for Windows takes tens of milliseconds, the git process saved is worth more; that was
not measured on Windows here.

## Known limits

- noctis reads `HEAD` itself only in the layouts listed above; elsewhere git answers, as in 7.5.1,
  and a pause starts the git process it did before.
- The limits listed for 7.5.1 still apply
  ([RELEASE_NOTES_7.5.1.md](RELEASE_NOTES_7.5.1.md#known-limits)).

## Tests

New Go tests compare the commit read from the files with what `git rev-parse` says for a branch
with a loose ref, a folder below the top of the working tree, a packed branch, a loose ref newer
than its packed one, a detached `HEAD`, a linked working tree before and after a commit in it and
with its branch packed, and a `.git` file with a relative and with an absolute `gitdir`. They check
that a branch with no commit, a symbolic reference, a `reftable` folder, a branch file that cannot
be read and `GIT_DIR` are left to git and get git's answer, that `HEAD` is read with no git on
`PATH`, and that the index is found where `git rev-parse --git-path index` finds it, from a
subfolder, in a linked working tree and with `GIT_INDEX_FILE` set. Another test schedules a runner
with `NOCTIS_NO_TASKS` set and in a cloud session, with a `systemd-run` on `PATH`, and fails if any
scheduler command is started.

With the files left unread and the scheduler always picked, as in 7.5.1, the test without git
fails, and so does the scheduler test, which sees `systemctl --user is-system-running` started.
With the packed branch taken where the loose one cannot be read, the test of that case fails. That
test needs file modes to bind the user running it, so it is skipped for root; it was run here as
root with the permission to read any file dropped.

Two new tests wait for the launcher's process id: in one the file is empty at first and holds the id
300 ms later, and the wait must return it; in the other it stays empty, and the launcher must count
as not started. A third relaunches a session through a terminal command that creates the file empty
and runs the launcher a second later, and fails if the session is launched headless or more than
once. With the file read as soon as it is there, as in 7.5.1, the first and the third fail, the
third on the headless launch.

The test of a window that closes at once without touching the session had a stand-in claude that
answered at once, within the clock tick that stamps the transcript's time, so the answer could be
stamped before the launch and the window taken for one that did nothing: it failed two times in 40
runs here for that reason alone. A real claude takes far longer to answer; the stand-in now waits a
second first, as the headless tests' stand-ins do.

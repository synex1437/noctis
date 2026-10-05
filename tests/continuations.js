'use strict';

const path = require('path');

const CLAIM_SLACK_MS = 1000;
const END_ACTIONS = new Set(['skip-launch', 'launch-failed']);

function samePath(a, b) {
  if (!a || !b) return false;
  const left = path.resolve(a);
  const right = path.resolve(b);
  return process.platform === 'win32' ? left.toLowerCase() === right.toLowerCase() : left === right;
}

function iso(seconds) {
  const date = new Date(Number(seconds) * 1000);
  return Number.isNaN(date.getTime()) ? String(seconds) : date.toISOString();
}

function claimRealMs(launch) {
  const at = Number(launch.handoff && launch.handoff.at);
  return Number.isFinite(at) && at > 0 ? (at - Number(launch.offset || 0)) * 1000 : launch.real - CLAIM_SLACK_MS;
}

function writtenBeforeClaim(answer, launch) {
  return answer.real < claimRealMs(launch) - CLAIM_SLACK_MS;
}

function writtenAfterThePause(answer, launch) {
  const recorded = Number(launch.wait.transcriptSize);
  return !(recorded > 0) || !Number.isFinite(answer.position) || answer.position >= recorded;
}

function sawAnswer(launch, answer) {
  return (launch.seen || []).some((entry) => samePath(entry.transcript, answer.transcript) && entry.ids.includes(answer.id));
}

function sessionLabel(launch) {
  return `${path.basename(launch.config)}/${launch.key}`;
}

function groupPauses(launches) {
  const pauses = [];
  const bySession = new Map();
  for (const launch of launches) {
    const session = `${launch.config}|${launch.key}`;
    if (!bySession.has(session)) bySession.set(session, []);
    const known = bySession.get(session);
    const holder = String(launch.wait.holder || '');
    const attempts = Number(launch.wait.launchAttempts || 0);
    const startedAt = Number(launch.wait.startedAt);
    let pause = holder ? known.find((candidate) => candidate.holder === holder) : null;
    if (!holder) {
      const latest = known.filter((candidate) => !candidate.holder).pop();
      if (latest && (latest.startedAts.has(startedAt) || (attempts > 0 && latest.lastAttempts === attempts - 1))) pause = latest;
    }
    if (!pause) {
      pause = { holder, startedAts: new Set(), lastAttempts: -1, launches: [] };
      known.push(pause);
      pauses.push(pause);
    }
    pause.startedAts.add(startedAt);
    pause.lastAttempts = attempts;
    pause.launches.push(launch);
  }
  return pauses;
}

function relaunchesAfterTheSessionWentOn(launches, answers) {
  const problems = [];
  for (const launch of launches) {
    const until = Number(launch.wait.until);
    if (launch.wait.kind === 'fable' || !Number.isFinite(until)) continue;
    const went = answers.find((answer) => samePath(answer.transcript, launch.transcript) && answer.ts > until + 1
      && writtenAfterThePause(answer, launch) && writtenBeforeClaim(answer, launch) && sawAnswer(launch, answer));
    if (went) {
      problems.push(`${sessionLabel(launch)} was relaunched although the session had answered at ${iso(went.ts)}, after its pause ended at ${iso(until)} (pause started ${iso(launch.wait.startedAt)})`);
    }
  }
  return problems;
}

function pausesContinuedTwice(pauses, answers) {
  const problems = [];
  for (const pause of pauses) {
    pause.launches.forEach((later, index) => {
      for (const earlier of pause.launches.slice(0, index)) {
        if (earlier.done && later.real < earlier.done.real) {
          problems.push(`${sessionLabel(later)} was relaunched while an earlier relaunch of the same pause was still running (pause started ${iso(earlier.wait.startedAt)}, launch ${index + 1})`);
          continue;
        }
        const answered = answers.find((answer) => answer.real > earlier.real
          && (samePath(answer.transcript, earlier.target) || samePath(answer.transcript, earlier.transcript))
          && writtenBeforeClaim(answer, later) && sawAnswer(later, answer));
        if (answered) {
          problems.push(`${sessionLabel(later)} was relaunched again for a pause an earlier relaunch had continued: the session answered at ${iso(answered.ts)} (pause started ${iso(earlier.wait.startedAt)}, earlier launch ${earlier.mode}, this is launch ${index + 1})`);
        }
      }
    });
  }
  return problems;
}

// One claude drives a session at a time, whatever pause each relaunch was for: a relaunch drives the
// session it resumes, and one that starts a fresh session drives that one too, as it takes the work over.
function sessionsDrivenTwice(launches) {
  const problems = [];
  const ran = launches.filter((launch) => launch.done).sort((a, b) => a.real - b.real);
  ran.forEach((later, index) => {
    const driven = new Set([later.key, later.fresh].filter(Boolean));
    for (const earlier of ran.slice(0, index)) {
      if (!samePath(earlier.config, later.config) || later.real >= earlier.done.real) continue;
      const shared = [earlier.key, earlier.fresh].find((sid) => sid && driven.has(sid));
      if (shared) {
        problems.push(`${path.basename(later.config)}/${shared} was driven by two relaunches at once: one for the pause started ${iso(earlier.wait.startedAt)}${earlier.fresh ? ` as ${earlier.fresh}` : ''}, one for the pause started ${iso(later.wait.startedAt)}${later.fresh ? ` as ${later.fresh}` : ''}`);
      }
    }
  });
  return problems;
}

function checkContinuations({ launches, answers }) {
  const attributed = launches.filter((launch) => launch.wait && launch.handoff);
  const realAnswers = answers.filter((answer) => !answer.apiError);
  const pauses = groupPauses(attributed);
  const modes = {};
  for (const launch of attributed) modes[launch.mode] = (modes[launch.mode] || 0) + 1;
  const problems = [...relaunchesAfterTheSessionWentOn(attributed, realAnswers), ...pausesContinuedTwice(pauses, realAnswers), ...sessionsDrivenTwice(attributed)];
  return {
    problems: [...new Set(problems)],
    summary: {
      launches: launches.length,
      pauseLaunches: attributed.length,
      pauses: pauses.length,
      pausesLaunchedMoreThanOnce: pauses.filter((pause) => pause.launches.length > 1).length,
      answers: answers.length,
      sessionAnswers: answers.filter((answer) => answer.by === 'session').length,
      modes,
    },
  };
}

function pauseEndedWithoutReason({ key, before, after, launched, journal }) {
  if (!before || after || launched) return '';
  if (journal.some((row) => row.sid === key && END_ACTIONS.has(row.action))) return '';
  return `the pause of ${key} (started ${iso(before.startedAt)}) ended in its runner with no relaunch and no reason in the journal`;
}

module.exports = { checkContinuations, pauseEndedWithoutReason };

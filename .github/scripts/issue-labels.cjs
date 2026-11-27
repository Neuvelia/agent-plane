// Deterministic issue triage labels (docs/roadmap/issue-triage.md).
//
// The decision is a pure function of the issue's current labels, the event and
// the issue body, so it is tested without GitHub (issue-labels.test.cjs). No
// model judges anything here: the workflow only keeps the labels consistent
// with the rules a person or an agent applied.

const decisions = require('./owner-decisions.cjs');

const READY = 'claude-ready';
const OWNER = 'needs-owner';
const TRIAGE = 'triage';

// Labels that always mean the owner has to act first; claude-ready never
// survives next to one of them.
const BLOCKING = ['needs_decision', 'epic', 'spike', 'feature'];

const LABELS = [
  { name: READY, color: '0e8a16', description: 'Bounded task Claude can take autonomously (docs/roadmap/issue-triage.md)' },
  { name: OWNER, color: 'd93f0b', description: "Needs the owner's decision, environment or review before work" },
  { name: TRIAGE, color: 'fbca04', description: 'Not classified yet: claude-ready or needs-owner' },
];

// The issue forms (.github/ISSUE_TEMPLATE) render the dropdown as
// "### Autonomy" followed by the chosen option.
const AUTONOMY = /^###\s+Autonomy\s*\n+\s*(.+?)\s*$/m;

function autonomyChoice(body) {
  const match = AUTONOMY.exec(body || '');
  if (!match) return null;
  if (match[1].startsWith('Claude can take it')) return READY;
  if (match[1].startsWith('Needs me')) return OWNER;
  return null;
}

// plan returns the labels to add and remove so that the issue ends with
// exactly one of claude-ready / needs-owner, or with triage when neither.
function plan({ labels, action, eventLabel, body }) {
  const have = new Set(labels);
  if (action === 'opened' && !have.has(READY) && !have.has(OWNER)) {
    const choice = autonomyChoice(body);
    if (choice) have.add(choice);
  }
  if (BLOCKING.some((name) => have.has(name))) {
    have.delete(READY);
    have.add(OWNER);
  }
  if (have.has(READY) && have.has(OWNER)) {
    // A person switching the class adds one label and removes the other; the
    // label added by this event is the newer decision.
    have.delete(action === 'labeled' && eventLabel === READY ? OWNER : READY);
  }
  if (have.has(READY) || have.has(OWNER)) have.delete(TRIAGE);
  else have.add(TRIAGE);

  const before = new Set(labels);
  return {
    add: [...have].filter((name) => !before.has(name)).sort(),
    remove: [...before].filter((name) => !have.has(name)).sort(),
  };
}

// decide applies the owner's ticked decisions to a needs-owner issue. It
// returns the labels the issue should have and a follow-up note (or null). It
// only acts when every decision has exactly one ticked option; the note carries
// a hidden key so the same answers are reported once.
function decide({ labels, text }) {
  const result = decisions.evaluate(decisions.parse(text));
  if (!result || !result.complete || !labels.includes(OWNER)) return { labels, note: null };
  const key = `<!-- owner-decisions: ${decisions.summary(result.chosen)} -->`;
  if (!result.ready) {
    const steps = result.chosen
      .filter((item) => item.outcome !== 'ready')
      .map((item) => `${item.id} → ${item.outcome}`)
      .join(', ');
    return { labels, note: `${key}\nAll owner decisions are ticked. Next steps before an agent can take this issue: ${steps}. It stays \`needs-owner\`.` };
  }
  const blocking = BLOCKING.filter((name) => labels.includes(name));
  if (blocking.length > 0) {
    const names = blocking.map((name) => `\`${name}\``).join(', ');
    return { labels, note: `${key}\nAll chosen options lead to \`claude-ready\`, but ${names} keeps the issue \`needs-owner\`. Remove it if the decision leaves a bounded task.` };
  }
  const next = labels.filter((name) => name !== OWNER).concat(READY);
  return { labels: next, note: `${key}\nAll owner decisions are ticked and lead to \`claude-ready\`. The issue is now \`claude-ready\`.` };
}

// TRUSTED is the minimum permission of the person whose edit may change the
// class: ticking boxes in a comment is open to anyone who can edit it.
const TRUSTED = new Set(['admin', 'maintain', 'write']);

async function ensureLabels(github, repo) {
  for (const label of LABELS) {
    try {
      await github.rest.issues.getLabel({ ...repo, name: label.name });
    } catch (error) {
      if (error.status !== 404) throw error;
      await github.rest.issues.createLabel({ ...repo, ...label });
    }
  }
}

async function senderTrusted(github, repo, username) {
  const { data } = await github.rest.repos.getCollaboratorPermissionLevel({ ...repo, username });
  return TRUSTED.has(data.permission);
}

// decisionText returns the text whose ticked boxes count for this event: an
// edited comment, or an edited issue body. Anything else has none.
function decisionText(context, issue) {
  const { eventName, payload } = context;
  if (eventName === 'issue_comment' && payload.action === 'edited') return payload.comment.body;
  if (eventName === 'issues' && payload.action === 'edited') return issue.body;
  return null;
}

async function postOnce(github, repo, number, note) {
  const key = note.split('\n')[0];
  const comments = await github.paginate(github.rest.issues.listComments, { ...repo, issue_number: number, per_page: 100 });
  if (comments.some((comment) => (comment.body || '').includes(key))) return;
  const footer = '\n\n---\n_Posted by the `issue-labels` workflow (docs/roadmap/issue-triage.md)._';
  await github.rest.issues.createComment({ ...repo, issue_number: number, body: note + footer });
}

async function applyLabels(github, repo, number, before, after) {
  const add = after.filter((name) => !before.includes(name));
  const remove = before.filter((name) => !after.includes(name));
  if (add.length > 0) await github.rest.issues.addLabels({ ...repo, issue_number: number, labels: add });
  for (const name of remove) await github.rest.issues.removeLabel({ ...repo, issue_number: number, name });
  return { add, remove };
}

// run handles one issues or issue_comment event. It reads the labels again
// instead of trusting the payload, because several events of one edit run one
// after another.
async function run({ github, context, core }) {
  const repo = context.repo;
  const number = context.payload.issue.number;
  await ensureLabels(github, repo);
  const { data: issue } = await github.rest.issues.get({ ...repo, issue_number: number });
  const current = issue.labels.map((label) => (typeof label === 'string' ? label : label.name));

  let labels = current;
  let note = null;
  const text = decisionText(context, issue);
  if (text && decisions.parse(text) && (await senderTrusted(github, repo, context.payload.sender.login))) {
    ({ labels, note } = decide({ labels: current, text }));
  }
  const fix = plan({
    labels,
    action: labels === current ? context.payload.action : 'labeled',
    eventLabel: labels === current ? (context.payload.label ? context.payload.label.name : null) : READY,
    body: issue.body,
  });
  const after = labels.filter((name) => !fix.remove.includes(name)).concat(fix.add);
  const changed = await applyLabels(github, repo, number, current, after);
  if (note) await postOnce(github, repo, number, note);
  core.info(`#${number}: add [${changed.add.join(', ')}], remove [${changed.remove.join(', ')}]${note ? ', decisions noted' : ''}`);
}

module.exports = { plan, decide, run, autonomyChoice, LABELS, BLOCKING };

// Deterministic issue triage labels (docs/roadmap/issue-triage.md).
//
// The decision is a pure function of the issue's current labels, the event and
// the issue body, so it is tested without GitHub (issue-labels.test.cjs). No
// model judges anything here: the workflow only keeps the labels consistent
// with the rules a person or an agent applied.

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

// run applies plan() to the issue of an `issues` event. It reads the labels
// again instead of trusting the payload, because several label events of one
// edit run one after another.
async function run({ github, context, core }) {
  const repo = context.repo;
  const number = context.payload.issue.number;
  await ensureLabels(github, repo);
  const { data: issue } = await github.rest.issues.get({ ...repo, issue_number: number });
  const decision = plan({
    labels: issue.labels.map((label) => (typeof label === 'string' ? label : label.name)),
    action: context.payload.action,
    eventLabel: context.payload.label ? context.payload.label.name : null,
    body: issue.body,
  });
  if (decision.add.length > 0) {
    await github.rest.issues.addLabels({ ...repo, issue_number: number, labels: decision.add });
  }
  for (const name of decision.remove) {
    await github.rest.issues.removeLabel({ ...repo, issue_number: number, name });
  }
  core.info(`#${number}: add [${decision.add.join(', ')}], remove [${decision.remove.join(', ')}]`);
}

module.exports = { plan, run, autonomyChoice, LABELS, BLOCKING };

// Run with: node --test .github/scripts/issue-labels.test.cjs .github/scripts/owner-decisions.test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const { plan, autonomyChoice } = require('./issue-labels.cjs');

const event = (labels, extra = {}) => plan({ labels, action: 'labeled', eventLabel: null, body: '', ...extra });

test('an unclassified issue gets triage', () => {
  assert.deepEqual(plan({ labels: ['bug'], action: 'opened', body: '' }), { add: ['triage'], remove: [] });
});

test('a classified issue loses triage', () => {
  assert.deepEqual(event(['triage', 'claude-ready'], { eventLabel: 'claude-ready' }), { add: [], remove: ['triage'] });
});

test('a blocking label removes claude-ready and adds needs-owner', () => {
  for (const blocking of ['needs_decision', 'epic', 'spike', 'feature']) {
    assert.deepEqual(event(['claude-ready', blocking], { eventLabel: blocking }), {
      add: ['needs-owner'],
      remove: ['claude-ready'],
    });
  }
});

test('a blocking label wins even when claude-ready is the label just added', () => {
  assert.deepEqual(event(['needs_decision', 'needs-owner', 'claude-ready'], { eventLabel: 'claude-ready' }), {
    add: [],
    remove: ['claude-ready'],
  });
});

test('adding claude-ready to a needs-owner issue switches the class', () => {
  assert.deepEqual(event(['needs-owner', 'claude-ready'], { eventLabel: 'claude-ready' }), {
    add: [],
    remove: ['needs-owner'],
  });
});

test('adding needs-owner to a claude-ready issue switches the class', () => {
  assert.deepEqual(event(['claude-ready', 'needs-owner'], { eventLabel: 'needs-owner' }), {
    add: [],
    remove: ['claude-ready'],
  });
});

test('both class labels without a labeled event keep needs-owner', () => {
  assert.deepEqual(plan({ labels: ['claude-ready', 'needs-owner'], action: 'opened', body: '' }), {
    add: [],
    remove: ['claude-ready'],
  });
});

test('removing the last class label brings triage back', () => {
  assert.deepEqual(plan({ labels: ['bug'], action: 'unlabeled', eventLabel: 'claude-ready', body: '' }), {
    add: ['triage'],
    remove: [],
  });
});

const formBody = (choice) => `### Feature ID\n\nF2.5b\n\n### Autonomy\n\n${choice}\n\n### Details\n\nx`;

test('the form choice classifies a new issue', () => {
  assert.deepEqual(plan({ labels: ['triage'], action: 'opened', body: formBody('Claude can take it autonomously') }), {
    add: ['claude-ready'],
    remove: ['triage'],
  });
  assert.deepEqual(plan({ labels: [], action: 'opened', body: formBody('Needs me (decision, environment or review)') }), {
    add: ['needs-owner'],
    remove: [],
  });
});

test('the form choice does not override a label already set', () => {
  assert.deepEqual(plan({ labels: ['needs-owner'], action: 'opened', body: formBody('Claude can take it autonomously') }), {
    add: [],
    remove: [],
  });
});

test('the form choice is ignored after the issue was opened', () => {
  assert.deepEqual(plan({ labels: [], action: 'edited', body: formBody('Claude can take it autonomously') }), {
    add: ['triage'],
    remove: [],
  });
});

test('the form choice cannot make a blocked issue claude-ready', () => {
  assert.deepEqual(plan({ labels: ['feature'], action: 'opened', body: formBody('Claude can take it autonomously') }), {
    add: ['needs-owner'],
    remove: [],
  });
});

test('an unknown or missing autonomy answer is no choice', () => {
  assert.equal(autonomyChoice(formBody('_No response_')), null);
  assert.equal(autonomyChoice('free text issue'), null);
  assert.equal(autonomyChoice(null), null);
});

const { decide } = require('./issue-labels.cjs');

const decisionComment = (a, b) =>
  `## Owner decisions\n\n**D1. Q?**\n- [${a}] A: x \`→ ready\`\n- [${b}] B: y \`→ ADR\`\n`;

test('ticked ready decisions turn needs-owner into claude-ready', () => {
  const result = decide({ labels: ['bug', 'needs-owner'], text: decisionComment('x', ' ') });
  assert.deepEqual(result.labels, ['bug', 'claude-ready']);
  assert.match(result.note, /^<!-- owner-decisions: D1=A:ready -->\n.*now `claude-ready`/);
});

test('a ticked ADR option keeps needs-owner and names the next step', () => {
  const result = decide({ labels: ['needs-owner'], text: decisionComment(' ', 'x') });
  assert.deepEqual(result.labels, ['needs-owner']);
  assert.match(result.note, /D1 → adr\. It stays `needs-owner`/);
});

test('a blocking label keeps needs-owner even when every option is ready', () => {
  const result = decide({ labels: ['feature', 'needs-owner'], text: decisionComment('x', ' ') });
  assert.deepEqual(result.labels, ['feature', 'needs-owner']);
  assert.match(result.note, /`feature` keeps the issue `needs-owner`/);
});

test('incomplete decisions change nothing and say nothing', () => {
  assert.deepEqual(decide({ labels: ['needs-owner'], text: decisionComment(' ', ' ') }), {
    labels: ['needs-owner'],
    note: null,
  });
});

test('decisions on an issue that is not needs-owner change nothing', () => {
  assert.deepEqual(decide({ labels: ['claude-ready'], text: decisionComment('x', ' ') }), {
    labels: ['claude-ready'],
    note: null,
  });
});
